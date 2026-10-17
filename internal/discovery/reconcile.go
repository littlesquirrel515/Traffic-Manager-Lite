package discovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/inboundidentity"
	"traffic-manager-lite/internal/storage"
)

var OverrideFields = map[string]bool{"name": true, "address": true, "port": true, "sni": true, "host": true, "path": true, "service_name": true, "public_key": true, "short_id": true, "fingerprint": true, "flow": true, "sort": true, "enabled": true, "transport": true, "tls": true, "uuid": true, "password": true, "method": true, "xhttp_mode": true, "alpn": true}

func Effective(p core.NodeProfile, over map[string]any) (core.NodeProfile, error) {
	b, _ := json.Marshal(p)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	for k, v := range over {
		if !OverrideFields[k] {
			return p, fmt.Errorf("invalid override field: %s", k)
		}
		m[k] = v
	}
	b, _ = json.Marshal(m)
	if e := json.Unmarshal(b, &p); e != nil {
		return p, fmt.Errorf("invalid override type")
	}
	if p.Port < 0 || p.Port > 65535 || len(p.Name) > 200 || len(p.Address) > 253 {
		return p, fmt.Errorf("invalid node parameters")
	}
	if len(p.ALPN) > 8 {
		return p, fmt.Errorf("too many ALPN protocols")
	}
	for _, protocol := range p.ALPN {
		if protocol == "" || len(protocol) > 255 || strings.ContainsAny(protocol, ",\r\n") {
			return p, fmt.Errorf("invalid ALPN protocol")
		}
	}
	return p, nil
}
func Issues(p core.NodeProfile) []string {
	v := append([]string{}, p.Limitations...)
	if p.Reality && p.PublicKey == "" {
		v = append(v, "Reality 缺少有效客户端公钥")
	}
	if p.Address == "" {
		v = append(v, "缺少客户端连接地址")
	}
	if p.Port < 1 || p.Port > 65535 {
		v = append(v, "缺少客户端连接端口（监听端口不会自动代入）")
	}
	if (p.Protocol == "vless" || p.Protocol == "vmess") && p.UUID == "" {
		v = append(v, "缺少 UUID")
	}
	if p.Protocol != "vless" && p.Protocol != "vmess" && p.Password == "" {
		v = append(v, "缺少客户端认证参数")
	}
	if p.TLS && p.SNI == "" {
		v = append(v, "缺少 SNI")
	}
	if p.Protocol == "shadowsocks" && p.Method == "" {
		v = append(v, "缺少加密方法")
	}
	if p.Protocol == "anytls" && !p.TLS {
		v = append(v, "AnyTLS 需要 TLS")
	}
	return v
}
func SafePath(root, path string) (string, error) {
	abs, e := filepath.Abs(root)
	if e != nil {
		return "", e
	}
	abs, e = filepath.EvalSymlinks(abs)
	if e != nil {
		return "", fmt.Errorf("TML_CONFIG_ROOT does not exist")
	}
	target, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	target, e = filepath.EvalSymlinks(target)
	if e != nil {
		return "", fmt.Errorf("configuration file unavailable")
	}
	rel, e := filepath.Rel(abs, target)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("configuration path outside TML_CONFIG_ROOT")
	}
	return target, nil
}
func Scan(ctx context.Context, s *storage.Store, inst core.Instance, root string) (int, error) {
	path, b, e := ReadSource(root, inst.ConfigPath, inst.CoreType)
	if e != nil {
		markMissing(ctx, s, inst, root)
		return 0, fmt.Errorf("configuration read failed")
	}
	nodes, e := Parse(inst.CoreType, b)
	if e != nil {
		if inst.CoreType != "hysteria2" || !(strings.Contains(e.Error(), "HTTP/command")) {
			return 0, e
		}
		// External authentication supplies no enumerable client credentials, but
		// its inbound can still be catalogued. Never invent clients from traffic.
		nodes = nil
	}
	h := sha256.Sum256(b)
	if len(nodes) > 10000 {
		return 0, fmt.Errorf("配置用户超过 10000 上限")
	}
	seenClients := map[string]bool{}
	for _, n := range nodes {
		key := n.Tag + "\x00" + n.Profile.UserKey
		if seenClients[key] {
			return 0, fmt.Errorf("入站中的用户标识重复，无法安全绑定")
		}
		seenClients[key] = true
	}
	hash := hex.EncodeToString(h[:])
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	mapping, e := inboundidentity.Sync(ctx, tx, inst.ID, path, hash, b, inst.CoreType)
	if e != nil {
		return 0, e
	}
	now := storage.Stamp(time.Now())
	if _, e = tx.ExecContext(ctx, "UPDATE credential_material SET status='source_missing' WHERE instance_id=? AND source IN ('config','legacy_config')", inst.ID); e != nil {
		return 0, e
	}
	for _, n := range nodes {
		inbound, matched := mapping[n.Tag]
		if !matched {
			continue
		}
		var uid int64
		e = tx.QueryRowContext(ctx, "SELECT user_id FROM identities WHERE instance_id=? AND core_user_key=? AND scope='user' ORDER BY id LIMIT 1", inst.ID, n.Profile.UserKey).Scan(&uid)
		if e == sql.ErrNoRows {
			res, er := tx.ExecContext(ctx, "INSERT INTO users(display_name,created_at,updated_at) VALUES(?,?,?)", n.Profile.UserKey, now, now)
			if er != nil {
				return 0, er
			}
			uid, e = res.LastInsertId()
			if e != nil {
				return 0, e
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO identities(user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(?,?,'',?,'user')", uid, inst.ID, n.Profile.UserKey)
		}
		if e != nil {
			return 0, e
		}
		id, e := inboundidentity.ClientKey(ctx, tx, inbound, n.Profile.UserKey, ID(inst.ID, n.Tag, n.Profile.UserKey))
		if e != nil {
			return 0, e
		}
		auth, _ := json.Marshal(map[string]string{"uuid": n.Profile.UUID, "password": n.Profile.Password})
		if _, e = tx.ExecContext(ctx, `INSERT INTO credential_material(id,instance_id,inbound_tag,core_client_ref,user_id,auth_json,status,source,verified_at) VALUES(?,?,?,?,?,?,'available','config',?) ON CONFLICT(id) DO UPDATE SET inbound_tag=excluded.inbound_tag,auth_json=excluded.auth_json,status='available',verified_at=excluded.verified_at`, id, inst.ID, n.Tag, n.Profile.UserKey, uid, string(auth), now); e != nil {
			return 0, e
		}

	}
	if e = catalog.Sync(ctx, tx, inst.ID, path, hash, b); e != nil {
		return 0, e
	}
	return len(nodes), tx.Commit()
}
