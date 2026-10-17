// Package remote implements API-only inventory and explicit manual registration.
// Inventory is independent of traffic samples and online connection snapshots.
package remote

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

type Inbound struct {
	ID         string           `json:"id"`
	InstanceID int64            `json:"instance_id"`
	Tag        string           `json:"tag"`
	Profile    core.NodeProfile `json:"profile"`
	Revision   int64            `json:"revision"`
}

func SaveManual(ctx context.Context, s *storage.Store, d Inbound) (string, error) {
	if strings.TrimSpace(d.Tag) == "" || len(d.Tag) > 200 || strings.ContainsAny(d.Tag, "\r\n\x00") {
		return "", fmt.Errorf("入站标识无效")
	}
	switch d.Profile.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "hysteria2", "anytls":
	default:
		return "", fmt.Errorf("暂不支持的订阅协议")
	}
	if d.Profile.ListenPort < 0 || d.Profile.ListenPort > 65535 {
		return "", fmt.Errorf("监听端口无效")
	}
	if len(d.Profile.Path) > 2048 || len(d.Profile.Host) > 253 || len(d.Profile.SNI) > 253 || len(d.Profile.ServiceName) > 200 {
		return "", fmt.Errorf("入站连接参数超出限制")
	}
	switch d.Profile.Transport {
	case "", "tcp", "ws", "grpc", "xhttp":
	default:
		return "", fmt.Errorf("暂不支持的传输类型")
	}
	// Credentials belong to clients, never inbound/endpoint definitions.
	if d.Profile.UUID != "" || d.Profile.Password != "" || d.Profile.UserKey != "" {
		return "", fmt.Errorf("入站定义不能包含客户端认证")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var kind string
	if e = tx.QueryRowContext(ctx, "SELECT core_type FROM instances WHERE id=?", d.InstanceID).Scan(&kind); e != nil {
		return "", e
	}
	if kind == "hysteria2" && d.Profile.Protocol != "hysteria2" || kind != "singbox" && kind != "hysteria2" && (d.Profile.Protocol == "hysteria2" || d.Profile.Protocol == "anytls") {
		return "", fmt.Errorf("该核心不支持所登记的服务端协议")
	}
	raw, _ := json.Marshal(d.Profile)
	now := storage.Stamp(time.Now())
	if d.ID == "" {
		d.ID = catalog.ID()
		_, e = tx.ExecContext(ctx, `INSERT INTO catalog_inbounds(id,instance_id,source_ref,tag,protocol,listen_port,profile_json,structure_hash,config_hash,present,runtime_state,discovered_at) VALUES(?,?,'manual',?,?,?,?,?,'manual',1,'manual_unverified',?)`, d.ID, d.InstanceID, d.Tag, d.Profile.Protocol, d.Profile.ListenPort, string(raw), "manual", now)
		if e == nil {
			_, e = tx.ExecContext(ctx, `INSERT INTO remote_inbound_registry(inbound_id,source_type,verification,manual_profile_json,revision,checked_at) VALUES(?,'manual','manual_unverified',?,1,?)`, d.ID, string(raw), now)
		}
	} else {
		var instance int64
		var protocol, oldProfile string
		if e = tx.QueryRowContext(ctx, "SELECT instance_id,protocol,profile_json FROM catalog_inbounds WHERE id=?", d.ID).Scan(&instance, &protocol, &oldProfile); e != nil {
			return "", e
		}
		if instance != d.InstanceID || protocol != d.Profile.Protocol {
			return "", fmt.Errorf("不能更改入站实例或协议，请登记独立入站")
		}
		res, err := tx.ExecContext(ctx, `UPDATE remote_inbound_registry SET source_type='manual',verification='manual_unverified',manual_profile_json=?,revision=revision+1 WHERE inbound_id=? AND revision=?`, string(raw), d.ID, d.Revision)
		if err != nil {
			return "", err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return "", fmt.Errorf("入站修订已变化，请重新读取")
		}
		_, e = tx.ExecContext(ctx, `UPDATE catalog_inbounds SET tag=?,profile_json=?,listen_port=?,source_ref='manual',runtime_state='manual_unverified',config_hash=? WHERE id=?`, d.Tag, string(raw), d.Profile.ListenPort, fmt.Sprintf("manual:%d", d.Revision+1), d.ID)
		if e == nil && oldProfile != string(raw) {
			_, e = tx.ExecContext(ctx, "UPDATE inbound_endpoint_bindings SET validation_status='needs_validation',revision=revision+1 WHERE inbound_id=?", d.ID)
		}
	}
	if e != nil {
		return "", e
	}
	nowAlias := storage.Stamp(time.Now())
	if _, e = tx.ExecContext(ctx, `INSERT INTO inbound_identity_aliases VALUES(?,'manual',?,?,?,?,?) ON CONFLICT DO UPDATE SET last_seen_at=excluded.last_seen_at`, d.InstanceID, d.Tag, d.Profile.Protocol, d.ID, nowAlias, nowAlias); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE credential_material SET inbound_tag=? WHERE id IN(SELECT credential_id FROM inbound_client_keys WHERE inbound_id=?)`, d.Tag, d.ID); e != nil {
		return "", e
	}
	return d.ID, tx.Commit()
}

type Client struct {
	InboundID string `json:"inbound_id"`
	Reference string `json:"client_ref"`
	UUID      string `json:"uuid"`
	Password  string `json:"password"`
	Flow      string `json:"flow,omitempty"`
	Level     uint32 `json:"level,omitempty"`
	Confirmed bool   `json:"confirmed"`
}

// RegisterClient records an administrator's assertion; it never calls a core.
func RegisterClient(ctx context.Context, s *storage.Store, d Client) (string, error) {
	return registerClient(ctx, s, d, "manual_unverified", false)
}

// RegisterHTTPAuthClient registers an account owned by TML's HTTP authenticator,
// not a core AddUser RPC or a claim that the core has read back this user.
func RegisterHTTPAuthClient(ctx context.Context, s *storage.Store, d Client) (string, error) {
	return registerClient(ctx, s, d, "hysteria_http_auth", true)
}

func registerClient(ctx context.Context, s *storage.Store, d Client, source string, protect bool) (string, error) {
	if !d.Confirmed || strings.TrimSpace(d.Reference) == "" || len(d.Reference) > 200 || strings.ContainsAny(d.Reference, "\r\n\x00") || strings.Contains(d.Reference, ">>>") || len(d.Password) > 4096 {
		return "", fmt.Errorf("需要明确确认真实客户端的身份和凭据")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if protect {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM catalog_inbounds b JOIN instances i ON i.id=b.instance_id JOIN hysteria_auth_backends h ON h.inbound_id=b.id WHERE b.id=? AND b.protocol='hysteria2' AND i.core_type='hysteria2' AND h.enabled=1 AND h.confirmed=1 AND h.target_endpoint=i.api_endpoint AND h.target_tag=b.tag AND b.present=1 AND b.retired=0 AND b.review=0`, d.InboundID).Scan(&n); e != nil || n != 1 {
			return "", fmt.Errorf("Hysteria2 HTTP 认证后端尚未配置确认或目标已变化")
		}
		var raw string
		e = tx.QueryRowContext(ctx, `SELECT c.auth_json FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id WHERE k.inbound_id=? AND k.client_ref=?`, d.InboundID, d.Reference).Scan(&raw)
		if e == nil {
			var auth map[string]string
			if json.Unmarshal([]byte(raw), &auth) != nil || auth["password"] != d.Password {
				return "", fmt.Errorf("已有客户端凭据冲突，禁止覆盖")
			}
		} else if e != sql.ErrNoRows {
			return "", e
		}
	}
	id, e := saveClient(ctx, tx, d, source)
	if e != nil {
		return "", e
	}
	// Registration is an asset source independent of Stats/Online. Non-Xray
	// collectors must see it even when the API cannot enumerate any clients.
	var instance int64
	var tag, protocol, kind string
	if e = tx.QueryRowContext(ctx, `SELECT c.instance_id,c.tag,c.protocol,i.core_type FROM catalog_inbounds c JOIN instances i ON i.id=c.instance_id WHERE c.id=?`, d.InboundID).Scan(&instance, &tag, &protocol, &kind); e != nil {
		return "", e
	}
	if kind != "xray" {
		now := storage.Stamp(time.Now())
		_, e = tx.ExecContext(ctx, `INSERT INTO core_clients(instance_id,inbound_tag,asset_key,email,protocol,source,present,observed_at) VALUES(?,?,?,?,?,?,1,?) ON CONFLICT(instance_id,inbound_tag,asset_key,source) DO UPDATE SET present=1,email=excluded.email,observed_at=excluded.observed_at`, instance, tag, "registered:"+id, d.Reference, protocol, source, now)
		if e != nil {
			return "", e
		}
		identityTag := ""
		if kind == "singbox" {
			identityTag = tag
		}
		var existing int64
		e = tx.QueryRowContext(ctx, `SELECT id FROM identities WHERE instance_id=? AND inbound_tag=? AND core_user_key=? AND scope='user'`, instance, identityTag, d.Reference).Scan(&existing)
		if e == sql.ErrNoRows {
			var res sql.Result
			res, e = tx.ExecContext(ctx, `INSERT INTO users(display_name,created_at,updated_at) VALUES(?,?,?)`, d.Reference, now, now)
			if e != nil {
				return "", e
			}
			var uid int64
			uid, e = res.LastInsertId()
			if e != nil {
				return "", e
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO identities(user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(?,?,?,?,'user')`, uid, instance, identityTag, d.Reference)
		}
		if e != nil {
			return "", e
		}
	}
	return id, tx.Commit()
}

func saveClient(ctx context.Context, tx *sql.Tx, d Client, source string) (string, error) {
	var instance int64
	var tag, protocol string
	if e := tx.QueryRowContext(ctx, "SELECT instance_id,tag,protocol FROM catalog_inbounds WHERE id=? AND retired=0", d.InboundID).Scan(&instance, &tag, &protocol); e != nil {
		return "", e
	}
	if protocol == "vless" || protocol == "vmess" {
		if len(d.UUID) != 36 {
			return "", fmt.Errorf("客户端 UUID 无效")
		}
		for j, c := range d.UUID {
			if j == 8 || j == 13 || j == 18 || j == 23 {
				if c != '-' {
					return "", fmt.Errorf("客户端 UUID 无效")
				}
			} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return "", fmt.Errorf("客户端 UUID 无效")
			}
		}
	} else if d.Password == "" {
		return "", fmt.Errorf("缺少客户端密码")
	}
	var id string
	e := tx.QueryRowContext(ctx, "SELECT credential_id FROM inbound_client_keys WHERE inbound_id=? AND client_ref=?", d.InboundID, d.Reference).Scan(&id)
	if e == sql.ErrNoRows {
		id = catalog.ID()
		_, e = tx.ExecContext(ctx, "INSERT INTO inbound_client_keys VALUES(?,?,?)", d.InboundID, d.Reference, id)
	}
	if e != nil {
		return "", e
	}
	if d.Flow != "" && d.Flow != "xtls-rprx-vision" {
		return "", fmt.Errorf("不支持的 Flow")
	}
	material := map[string]string{"uuid": d.UUID, "password": d.Password}
	if d.Flow != "" {
		material["flow"] = d.Flow
	}
	raw, _ := json.Marshal(material)
	_, e = tx.ExecContext(ctx, `INSERT INTO credential_material(id,instance_id,inbound_tag,core_client_ref,auth_json,status,source,verified_at) VALUES(?,?,?,?,?,'available',?,?) ON CONFLICT(id) DO UPDATE SET auth_json=excluded.auth_json,status='available',source=excluded.source,verified_at=excluded.verified_at`, id, instance, tag, d.Reference, string(raw), source, storage.Stamp(time.Now()))
	return id, e
}
