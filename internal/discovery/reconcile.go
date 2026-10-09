package discovery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

var OverrideFields = map[string]bool{"name": true, "address": true, "port": true, "sni": true, "host": true, "path": true, "service_name": true, "public_key": true, "short_id": true, "fingerprint": true, "flow": true, "sort": true, "enabled": true, "transport": true, "tls": true, "uuid": true, "password": true, "method": true}

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
	path, e := SafePath(root, inst.ConfigPath)
	if e != nil {
		return 0, e
	}
	info, e := os.Stat(path)
	if e != nil || info.Size() > 4<<20 {
		return 0, fmt.Errorf("configuration file unavailable or too large")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, fmt.Errorf("configuration read failed")
	}
	nodes, e := Parse(inst.CoreType, b)
	if e != nil {
		return 0, e
	}
	h := sha256.Sum256(b)
	hash := hex.EncodeToString(h[:])
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	now := storage.Stamp(time.Now())
	if _, e = tx.ExecContext(ctx, "UPDATE nodes SET present=0 WHERE instance_id=?", inst.ID); e != nil {
		return 0, e
	}
	for _, n := range nodes {
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
		id := ID(inst.ID, n.Tag, n.Profile.UserKey)
		over := map[string]any{}
		var raw string
		e = tx.QueryRowContext(ctx, "SELECT overrides_json FROM nodes WHERE id=?", id).Scan(&raw)
		if e == nil {
			if e = json.Unmarshal([]byte(raw), &over); e != nil {
				return 0, e
			}
		} else if e != sql.ErrNoRows {
			return 0, e
		}
		effective, e := Effective(n.Profile, over)
		if e != nil {
			return 0, e
		}
		d, _ := json.Marshal(n.Profile)
		eff, _ := json.Marshal(effective)
		_, e = tx.ExecContext(ctx, `INSERT INTO nodes(id,instance_id,inbound_tag,user_id,protocol,discovered_json,effective_json,source_hash,enabled,present,updated_at) VALUES(?,?,?,?,?,?,?,?,?,1,?)
 ON CONFLICT(id) DO UPDATE SET discovered_json=excluded.discovered_json,effective_json=excluded.effective_json,source_hash=excluded.source_hash,enabled=excluded.enabled,present=1,updated_at=excluded.updated_at`, id, inst.ID, n.Tag, uid, n.Profile.Protocol, string(d), string(eff), hash, effective.Enabled, now)
		if e != nil {
			return 0, e
		}
	}
	return len(nodes), tx.Commit()
}
func Nodes(ctx context.Context, s *storage.Store) ([]core.Node, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,instance_id,inbound_tag,user_id,effective_json,overrides_json,source_hash,present FROM nodes ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []core.Node{}
	for rows.Next() {
		var n core.Node
		var eff, over string
		if e = rows.Scan(&n.ID, &n.InstanceID, &n.InboundTag, &n.UserID, &eff, &over, &n.SourceHash, &n.Present); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(eff), &n.Profile); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(over), &n.Overrides); e != nil {
			return nil, e
		}
		n.Issues = Issues(n.Profile)
		if !n.Present {
			n.Issues = append(n.Issues, "配置中已移除")
		}
		n.Complete = len(n.Issues) == 0
		out = append(out, n)
	}
	return out, rows.Err()
}
func Patch(ctx context.Context, s *storage.Store, id string, patch map[string]any) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var d, o string
	if e = tx.QueryRowContext(ctx, "SELECT discovered_json,overrides_json FROM nodes WHERE id=?", id).Scan(&d, &o); e != nil {
		return e
	}
	var p core.NodeProfile
	over := map[string]any{}
	if e = json.Unmarshal([]byte(d), &p); e != nil {
		return e
	}
	if e = json.Unmarshal([]byte(o), &over); e != nil {
		return e
	}
	for k, v := range patch {
		if !OverrideFields[k] {
			return fmt.Errorf("invalid override field")
		}
		if v == nil {
			delete(over, k)
		} else {
			over[k] = v
		}
	}
	p, e = Effective(p, over)
	if e != nil {
		return e
	}
	eff, _ := json.Marshal(p)
	ov, _ := json.Marshal(over)
	_, e = tx.ExecContext(ctx, "UPDATE nodes SET overrides_json=?,effective_json=?,enabled=?,updated_at=? WHERE id=?", string(ov), string(eff), p.Enabled, storage.Stamp(time.Now()), id)
	if e != nil {
		return e
	}
	return tx.Commit()
}
