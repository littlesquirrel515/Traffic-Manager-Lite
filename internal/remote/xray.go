package remote

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"net"
	"strings"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
)

// DecodeXray only extracts whitelisted client-facing fields. Raw settings,
// certificate keys and REALITY private keys never enter persistent inventory.
func DecodeXray(in *hp.Inbound) (core.NodeProfile, error) {
	p := core.NodeProfile{Enabled: true, Transport: "tcp"}
	if in == nil || in.ProxySettings == nil || in.ReceiverSettings == nil {
		return p, fmt.Errorf("入站缺少序列化配置，保留现有登记")
	}
	switch in.ProxySettings.Type {
	case "xray.proxy.vless.inbound.Config":
		p.Protocol = "vless"
	case "xray.proxy.vmess.inbound.Config":
		p.Protocol = "vmess"
	case "xray.proxy.trojan.ServerConfig":
		p.Protocol = "trojan"
	case "xray.proxy.shadowsocks.ServerConfig":
		p.Protocol = "shadowsocks"
	default:
		return p, fmt.Errorf("入站协议尚未实现安全解析")
	}
	if in.ReceiverSettings.Type != "xray.app.proxyman.ReceiverConfig" {
		return p, fmt.Errorf("未知 ReceiverConfig 类型")
	}
	r := &hp.ReceiverConfig{}
	if e := proto.Unmarshal(in.ReceiverSettings.Value, r); e != nil {
		return p, fmt.Errorf("ReceiverConfig 解析失败")
	}
	if r.PortList != nil {
		for _, v := range r.PortList.Range {
			if v.From == 0 || v.From > 65535 || v.To > 65535 || v.To < v.From {
				return p, fmt.Errorf("监听端口范围无效")
			}
			if v.From == v.To {
				p.ListenPorts = append(p.ListenPorts, fmt.Sprint(v.From))
				if p.ListenPort == 0 {
					p.ListenPort = int(v.From)
				}
			} else {
				p.ListenPorts = append(p.ListenPorts, fmt.Sprintf("%d-%d", v.From, v.To))
			}
		}
	}
	s := r.StreamSettings
	if s == nil {
		return p, nil
	}
	if s.ProtocolName != "" {
		p.Transport = s.ProtocolName
	}
	if p.Transport == "splithttp" {
		p.Transport = "xhttp"
	}
	if p.Transport == "websocket" {
		p.Transport = "ws"
	}
	for _, t := range s.TransportSettings {
		if t.Settings == nil {
			continue
		}
		switch t.Settings.Type {
		case "xray.transport.internet.websocket.Config":
			v := &hp.WSConfig{}
			if e := proto.Unmarshal(t.Settings.Value, v); e != nil {
				return p, fmt.Errorf("WS 配置解析失败")
			}
			p.Host, p.Path = v.Host, v.Path
			if v.Ed > 0 || len(v.Header) > 0 {
				p.Limitations = append(p.Limitations, "WS 扩展参数需手动核对")
			}
		case "xray.transport.internet.splithttp.Config":
			v := &hp.XHTTPConfig{}
			if e := proto.Unmarshal(t.Settings.Value, v); e != nil {
				return p, fmt.Errorf("XHTTP 配置解析失败")
			}
			decodeXHTTP(v, &p)
		case "xray.transport.internet.grpc.encoding.Config":
			v := &hp.GRPCConfig{}
			if e := proto.Unmarshal(t.Settings.Value, v); e != nil {
				return p, fmt.Errorf("gRPC 配置解析失败")
			}
			p.ServiceName = v.ServiceName
			if v.MultiMode {
				p.Limitations = append(p.Limitations, "gRPC multi_mode 需手动核对")
			}
		default:
			if t.ProtocolName == s.ProtocolName {
				p.Limitations = append(p.Limitations, "传输参数尚未完整解析")
			}
		}
	}
	for _, security := range s.SecuritySettings {
		switch security.Type {
		case "xray.transport.internet.tls.Config":
			v := &hp.TLSConfig{}
			if e := proto.Unmarshal(security.Value, v); e != nil {
				return p, fmt.Errorf("TLS 配置解析失败")
			}
			p.TLS, p.SNI, p.ALPN = true, v.ServerName, v.NextProtocol
		case "xray.transport.internet.reality.Config":
			v := &hp.RealityConfig{}
			if e := proto.Unmarshal(security.Value, v); e != nil {
				return p, fmt.Errorf("REALITY 配置解析失败")
			}
			p.TLS, p.Reality = true, true
			if len(v.ServerNames) == 1 {
				p.SNI = v.ServerNames[0]
			}
			if len(v.ShortIds) == 1 {
				p.ShortID = hex.EncodeToString(v.ShortIds[0])
			}
			key, e := ecdh.X25519().NewPrivateKey(v.PrivateKey)
			if e == nil {
				p.PublicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
			}
			if len(v.Mldsa65Seed) > 0 {
				p.Limitations = append(p.Limitations, "REALITY ML-DSA 参数暂未验证")
			}
		default:
			return p, fmt.Errorf("未知安全配置类型")
		}
	}
	if s.SecurityType != "" && !p.TLS {
		return p, fmt.Errorf("安全配置缺失，不能当作明文入站")
	}
	return p, nil
}

type InventoryResult struct {
	Status      string   `json:"status"`
	Count       int      `json:"count"`
	Limitations []string `json:"limitations"`
}

func SyncXray(ctx context.Context, s *storage.Store, i core.Instance, allowed []string) (result InventoryResult, err error) {
	defer func() {
		if err != nil {
			c, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer stop()
			_, _ = s.DB.ExecContext(c, "INSERT INTO remote_inventory_runs(instance_id,status,summary,checked_at) VALUES(?,'unavailable','API 清单未完整取得，保留已有资产',?)", i.ID, storage.Stamp(time.Now()))
		}
	}()
	result = InventoryResult{Status: "unknown", Limitations: []string{}}
	if i.CoreType != "xray" {
		result.Status = "unsupported"
		result.Limitations = append(result.Limitations, "当前官方 API 无完整入站配置枚举；请手动登记")
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	policy := (security.Policy{Allowed: allowed}).ForEndpoints(i.APIEndpoint)
	if err = policy.Check(ctx, security.TargetAddress(i.APIEndpoint)); err != nil {
		return result, err
	}
	conn, e := grpc.NewClient("passthrough:///"+i.APIEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(policy.Dial), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20)))
	if e != nil {
		return result, fmt.Errorf("API 连接初始化失败")
	}
	defer conn.Close()
	c := hp.NewHandlerServiceClient(conn)
	list, e := c.ListInbounds(ctx, &hp.ListInboundsRequest{IsOnlyTags: false})
	if e != nil {
		return result, fmt.Errorf("ListInbounds: %s；未修改既有登记", status.Code(e))
	}
	if len(list.Inbounds) > 256 {
		return result, fmt.Errorf("入站数量超过 256 保护上限")
	}
	type asset struct {
		in      *hp.Inbound
		profile core.NodeProfile
		users   []*hp.User
		usersOK bool
	}
	assets := []asset{}
	seen := map[string]bool{}
	clientCount := 0
	for _, in := range list.Inbounds {
		if in == nil || in.Tag == "" || len(in.Tag) > 200 || seen[in.Tag] {
			return result, fmt.Errorf("入站标识缺失或重复")
		}
		seen[in.Tag] = true
		p, pe := DecodeXray(in)
		if pe != nil {
			result.Limitations = append(result.Limitations, "部分入站解析不可用，保留原登记")
			continue
		}
		u, ue := c.GetInboundUsers(ctx, &hp.GetInboundUserRequest{Tag: in.Tag})
		a := asset{in: in, profile: p, usersOK: ue == nil}
		if ue == nil {
			a.users = u.Users
			clientCount += len(u.Users)
		}
		if clientCount > 10000 {
			return result, fmt.Errorf("客户端数量超过保护上限")
		}
		assets = append(assets, a)
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	now := storage.Stamp(time.Now())
	for _, a := range assets {
		var id string
		var existingProtocol string
		var retired bool
		e = tx.QueryRowContext(ctx, "SELECT id,protocol,retired FROM catalog_inbounds WHERE instance_id=? AND tag=? ORDER BY retired,id LIMIT 1", i.ID, a.in.Tag).Scan(&id, &existingProtocol, &retired)
		if e != nil && e != sql.ErrNoRows {
			return result, e
		}
		if retired {
			result.Limitations = append(result.Limitations, "已退役身份保留，不自动创建替代入站")
			continue
		}
		if id != "" && existingProtocol != a.profile.Protocol {
			if _, e = tx.ExecContext(ctx, "UPDATE catalog_inbounds SET review=1,runtime_state='protocol_conflict' WHERE id=?", id); e != nil {
				return result, e
			}
			result.Limitations = append(result.Limitations, "协议变更需要新身份或显式迁移确认")
			continue
		}
		raw, _ := json.Marshal(a.profile)
		revision := fmt.Sprintf("%x", sha256.Sum256(raw))
		if id == "" {
			id = catalog.ID()
			_, e = tx.ExecContext(ctx, `INSERT INTO catalog_inbounds(id,instance_id,source_ref,tag,protocol,listen_address,listen_port,profile_json,structure_hash,config_hash,present,runtime_state,discovered_at) VALUES(?,?,'native_api',?,?,?,?,?,'runtime',?,1,'verified',?)`, id, i.ID, a.in.Tag, a.profile.Protocol, listenAddress(a.in), a.profile.ListenPort, string(raw), revision, now)
			if e == nil {
				_, e = tx.ExecContext(ctx, `INSERT INTO remote_inbound_registry VALUES(?,'native_api','verified','{}',1,?)`, id, now)
			}
		} else {
			if _, e = tx.ExecContext(ctx, `UPDATE inbound_endpoint_bindings SET validation_status='needs_validation',revision=revision+1 WHERE inbound_id=? AND EXISTS(SELECT 1 FROM catalog_inbounds WHERE id=? AND source_ref='native_api' AND config_hash<>?)`, id, id, revision); e != nil {
				return result, e
			}
			// Manual field assertions take precedence and are never silently replaced.
			_, e = tx.ExecContext(ctx, `UPDATE catalog_inbounds SET present=1,runtime_state='verified',config_hash=CASE WHEN source_ref='native_api' THEN ? ELSE config_hash END,profile_json=CASE WHEN source_ref='native_api' THEN ? ELSE profile_json END,discovered_at=? WHERE id=?`, revision, string(raw), now, id)
			if e == nil {
				_, e = tx.ExecContext(ctx, "UPDATE remote_inbound_registry SET verification='verified',checked_at=? WHERE inbound_id=?", now, id)
			}
		}
		if e != nil {
			return result, e
		}
		result.Count++
		if _, e = tx.ExecContext(ctx, `INSERT INTO inbound_identity_aliases VALUES(?,'native_api',?,?,?,?,?) ON CONFLICT DO UPDATE SET last_seen_at=excluded.last_seen_at`, i.ID, a.in.Tag, a.profile.Protocol, id, now, now); e != nil {
			return result, e
		}
		if a.usersOK {
			for _, u := range a.users {
				if u == nil || u.Email == "" || len(u.Email) > 200 || u.Account == nil {
					continue
				}
				d := Client{InboundID: id, Reference: u.Email, Level: u.Level}
				switch {
				case strings.HasSuffix(u.Account.Type, ".vless.Account"), strings.HasSuffix(u.Account.Type, ".vmess.Account"):
					v := &hp.UUIDAccount{}
					if proto.Unmarshal(u.Account.Value, v) != nil {
						continue
					}
					d.UUID = v.Id
					if a.profile.Protocol == "vless" {
						d.Flow = v.Flow
					}
				case u.Account.Type == "xray.proxy.trojan.Account":
					v := &hp.PasswordAccount{}
					if proto.Unmarshal(u.Account.Value, v) != nil {
						continue
					}
					d.Password = v.Password
				default:
					continue
				}
				if _, e = saveClient(ctx, tx, d, "native_api"); e != nil {
					return result, e
				}
			}
		}
	}
	// Only a complete, successful list can establish absence. Parsing failures
	// retain assets and never turn an unknown protocol into an absent inbound.
	rows, e := tx.QueryContext(ctx, "SELECT id,tag FROM catalog_inbounds WHERE instance_id=? AND source_ref='native_api'", i.ID)
	if e != nil {
		return result, e
	}
	missing := []string{}
	for rows.Next() {
		var id, tag string
		if e = rows.Scan(&id, &tag); e != nil {
			rows.Close()
			return result, e
		}
		if !seen[tag] {
			missing = append(missing, id)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	for _, id := range missing {
		if _, e = tx.ExecContext(ctx, "UPDATE catalog_inbounds SET present=0,runtime_state='source_missing' WHERE id=?", id); e != nil {
			return result, e
		}
	}
	result.Status = "supported"
	if len(result.Limitations) > 0 {
		result.Status = "unknown"
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO remote_inventory_runs(instance_id,status,summary,checked_at) VALUES(?,?,?,?)", i.ID, result.Status, fmt.Sprintf("%d 入站；%d 项限制", result.Count, len(result.Limitations)), now)
	if e != nil {
		return result, e
	}
	return result, tx.Commit()
}

func listenAddress(in *hp.Inbound) string {
	r := &hp.ReceiverConfig{}
	if in.ReceiverSettings != nil && proto.Unmarshal(in.ReceiverSettings.Value, r) == nil && r.Listen != nil {
		if len(r.Listen.Ip) > 0 {
			return net.IP(r.Listen.Ip).String()
		}
		return r.Listen.Domain
	}
	return ""
}
