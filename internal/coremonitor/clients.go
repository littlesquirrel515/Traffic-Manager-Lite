package coremonitor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
)

// Enumerate configuration assets, never infer an inventory from traffic/connection lists.
// Credentials are parsed transiently, never copied into the client inventory or report.
func (s Service) clients(i core.Instance, o *observation) {
	start := time.Now()
	var e error
	if i.ConfigPath == "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		rows, err := s.Store.DB.QueryContext(ctx, `SELECT c.inbound_tag,c.asset_key,c.email,c.protocol,c.source FROM core_clients c WHERE c.instance_id=? AND c.source IN ('manual_unverified','hysteria_http_auth') AND c.present=1 AND EXISTS (SELECT 1 FROM catalog_inbounds b WHERE b.instance_id=c.instance_id AND b.tag=c.inbound_tag AND b.retired=0 AND b.present=1) LIMIT 10001`, i.ID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var a Client
				if err = rows.Scan(&a.Inbound, &a.Key, &a.Email, &a.Protocol, &a.Source); err != nil {
					break
				}
				o.clients = append(o.clients, a)
			}
			if err != nil || rows.Err() != nil || len(o.clients) > 10000 {
				o.clients = nil
			}
		}
		if len(o.clients) > 0 {
			o.report.ConfigSource = "TML 已登记资产 / HTTP 认证账户（不证明核心完整资产）"
		}
		// A known manual subset is useful, but never marks full enumeration OK.
		o.note("clients", "完整用户枚举", "Unknown", fmt.Sprintf("已登记 %d 个客户端；当前 API 未提供完整 Clients，登记不证明运行时完整清单；流量和连接列表不是用户资产清单", len(o.clients)), "通过入站管理登记已有客户端（manual_unverified）；可选配置来源须显式启用")
		return
	}
	path, e := discovery.SafePath(s.Config.ConfigRoot, i.ConfigPath)
	var b []byte
	if e == nil {
		var info os.FileInfo
		info, e = os.Stat(path)
		if e == nil && info.Size() > 4<<20 {
			e = fmt.Errorf("config limit")
		}
		if e == nil {
			b, e = os.ReadFile(path)
		}
	}
	if e == nil {
		o.configRevision = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	var root map[string]any
	if e == nil {
		if i.CoreType == "hysteria2" {
			e = yaml.Unmarshal(b, &root)
		} else {
			e = json.Unmarshal(b, &root)
		}
	}
	str := func(m map[string]any, key string) string { v, _ := m[key].(string); return v }
	obj := func(v any) map[string]any { m, _ := v.(map[string]any); return m }
	list := func(v any) []any { a, _ := v.([]any); return a }
	add := func(tag, protocol, email string, index int) {
		o.clients = append(o.clients, Client{Inbound: tag, Protocol: protocol, Email: email, Key: fmt.Sprintf("asset:%x", sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", tag, email, index)))), Source: "config_file"})
	}
	if e == nil && root == nil {
		e = fmt.Errorf("invalid config")
	}
	if e == nil && i.CoreType == "hysteria2" {
		auth := obj(root["auth"])
		switch str(auth, "type") {
		case "password":
			if str(auth, "password") == "" {
				e = fmt.Errorf("missing password")
			} else {
				add("hysteria2", "hysteria2", "user", 0)
			}
		case "userpass":
			for username, password := range obj(auth["userpass"]) {
				if _, ok := password.(string); !ok {
					e = fmt.Errorf("invalid auth")
					break
				}
				add("hysteria2", "hysteria2", strings.ToLower(username), 0)
			}
		case "http", "command":
			o.clients, e = s.authExport(i)
			if e != nil {
				o.note("clients", "认证后端完整用户枚举", "Unknown", "HTTP/command 配置无完整用户清单，未取得有效只读导出；不能调用认证端点猜测用户", "提供 <配置路径>.clients.json 完整资产导出（不含密码、24h 有效）")
			}
		default:
			e = fmt.Errorf("unknown auth")
		}
	} else if e == nil {
		inboundList, ok := root["inbounds"].([]any)
		if !ok {
			e = fmt.Errorf("missing inbounds")
		}
		if len(inboundList) > 256 {
			e = fmt.Errorf("inbound limit")
		}
		inbounds := []map[string]any{}
		for inIndex, raw := range inboundList {
			inbound := obj(raw)
			if inbound == nil {
				e = fmt.Errorf("invalid inbound")
				break
			}
			tag := str(inbound, "tag")
			if tag == "" {
				tag = fmt.Sprintf("inbound-%d", inIndex)
			}
			protocol := str(inbound, "type")
			users := list(inbound["users"])
			if i.CoreType == "v2fly" {
				protocol = str(inbound, "protocol")
				settings := obj(inbound["settings"])
				users = append(list(settings["clients"]), list(settings["accounts"])...)
			}
			inbounds = append(inbounds, map[string]any{"inbound": tag, "protocol": protocol, "user_count": len(users), "scope": "inbound", "runtime_effective": "Unknown"})
			for n, rawUser := range users {
				u := obj(rawUser)
				if u == nil {
					e = fmt.Errorf("invalid user")
					break
				}
				email := str(u, "name")
				if i.CoreType == "v2fly" {
					email = str(u, "email")
				} else if email == "" {
					email = str(u, "username")
				}
				add(tag, protocol, email, n)
			}
		}
		if e == nil {
			o.providerSnapshots = append(o.providerSnapshots, core.ProviderSnapshot{Provider: "Config", Scope: "inbound", Status: "Available", Summary: inbounds})
		}
	}
	if len(o.clients) > 10000 {
		e = fmt.Errorf("asset limit")
	}
	for _, c := range o.clients {
		if len(c.Email) > 512 || len(c.Inbound) > 512 {
			e = fmt.Errorf("invalid asset")
		}
	}
	if e != nil {
		o.clients = nil
	} else {
		o.clientsOK = true
		o.report.ConfigSource = "config_file（只读文件资产，不证明等于运行时加载配置）"
	}
	o.check("clients", "配置用户枚举", "读取 TML_CONFIG_ROOT 内只读文件", "只保存用户名称/协议/inbound；不保存凭据", "Hysteria2 password/userpass；sing-box users；V2Fly clients/accounts", start, len(o.clients), e)
}

// External authentication has no standard enumeration API. Only accept an
// explicit, recent, complete metadata export inside the approved config root.
func (s Service) authExport(i core.Instance) ([]Client, error) {
	unknown := apiError{"Unknown", "ExternalAuthInventoryUnavailable"}
	path, e := discovery.SafePath(s.Config.ConfigRoot, i.ConfigPath+".clients.json")
	if e != nil {
		return nil, unknown
	}
	info, e := os.Stat(path)
	if e != nil || info.Size() > 4<<20 {
		return nil, unknown
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, unknown
	}
	var data struct {
		Complete bool   `json:"complete"`
		Observed string `json:"observed_at"`
		Users    []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&data) != nil || !data.Complete || data.Users == nil || len(data.Users) > 10000 {
		return nil, unknown
	}
	at, e := time.Parse(time.RFC3339, data.Observed)
	if e != nil || time.Since(at) > 24*time.Hour || at.After(time.Now().Add(time.Minute)) {
		return nil, unknown
	}
	out := []Client{}
	seen := map[string]bool{}
	for _, u := range data.Users {
		if u.Name == "" || len(u.Name) > 512 || seen[u.Name] {
			return nil, unknown
		}
		seen[u.Name] = true
		out = append(out, Client{Inbound: "hysteria2", Protocol: "hysteria2", Email: u.Name, Key: "name:" + u.Name, Source: "auth_export"})
	}
	return out, nil
}
