// Package hysteriaauth implements Hysteria2's official HTTP authentication
// protocol. It never changes core configuration or calls a mutation API.
package hysteriaauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"time"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

func Hash(value string) string                { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }
func accountHash(inbound, auth string) string { return Hash(inbound + "\x00" + auth) }
func userpass(auth string) string {
	user, pass, ok := strings.Cut(auth, ":")
	if !ok || user == "" {
		return ""
	}
	return strings.ToLower(user) + ":" + pass
}

type Setup struct {
	Confirmed      bool     `json:"confirmed"`
	Rotate         bool     `json:"rotate"`
	ImportNative   bool     `json:"import_native"`
	NativeUserpass bool     `json:"native_userpass"`
	AllowedCIDRs   []string `json:"allowed_cidrs"`
}
type Prepared struct {
	CallbackPath string `json:"callback_path"`
	Revision     int64  `json:"revision"`
	Imported     int    `json:"imported_native"`
}

func Prepare(ctx context.Context, s *storage.Store, id string, d Setup) (Prepared, error) {
	var out Prepared
	if !d.Confirmed || len(d.AllowedCIDRs) == 0 || len(d.AllowedCIDRs) > 16 {
		return out, fmt.Errorf("请确认独立认证后端并设置核心回调来源网段")
	}
	private := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "fc00::/7", "::1/128"}
	for _, raw := range d.AllowedCIDRs {
		p, e := netip.ParsePrefix(raw)
		allowed := false
		if e == nil {
			p = p.Masked()
			for _, v := range private {
				n := netip.MustParsePrefix(v)
				if n.Addr().BitLen() == p.Addr().BitLen() && n.Contains(p.Addr()) && p.Bits() >= n.Bits() {
					allowed = true
				}
			}
		}
		if !allowed {
			return out, fmt.Errorf("回调来源必须为明确的私网或回环 CIDR；跨服务器请通过受控私网访问")
		}
	}
	bytes := make([]byte, 32)
	if _, e := rand.Read(bytes); e != nil {
		return out, e
	}
	token := hex.EncodeToString(bytes)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var endpoint, tag string
	var instance int64
	e = tx.QueryRowContext(ctx, `SELECT i.api_endpoint,b.tag,i.id FROM catalog_inbounds b JOIN instances i ON i.id=b.instance_id WHERE b.id=? AND i.core_type='hysteria2' AND b.protocol='hysteria2' AND b.present=1 AND b.review=0 AND b.retired=0`, id).Scan(&endpoint, &tag, &instance)
	if e != nil {
		return out, fmt.Errorf("请选择独立 Hysteria2 实例的有效入站；sing-box 内置 HY2 不使用此认证接口")
	}
	var revision int64
	e = tx.QueryRowContext(ctx, "SELECT revision FROM hysteria_auth_backends WHERE inbound_id=?", id).Scan(&revision)
	if e == nil && !d.Rotate {
		return out, fmt.Errorf("后端已存在；仅在明确轮换 URL 密钥时重新准备")
	}
	if e != nil && e != sql.ErrNoRows {
		return out, e
	}
	out.Revision = revision + 1
	cidrs, _ := json.Marshal(d.AllowedCIDRs)
	_, e = tx.ExecContext(ctx, `INSERT INTO hysteria_auth_backends(inbound_id,instance_id,token_hash,allowed_cidrs,target_endpoint,target_tag,revision,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(inbound_id) DO UPDATE SET token_hash=excluded.token_hash,allowed_cidrs=excluded.allowed_cidrs,target_endpoint=excluded.target_endpoint,target_tag=excluded.target_tag,revision=excluded.revision,enabled=1,confirmed=0,created_at=excluded.created_at,last_callback_at=NULL,last_success_at=NULL`, id, instance, Hash(token), string(cidrs), endpoint, tag, out.Revision, storage.Stamp(time.Now()))
	if e != nil {
		return out, e
	}
	if d.ImportNative {
		rows, er := tx.QueryContext(ctx, `SELECT c.id,c.auth_json FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id WHERE k.inbound_id=? AND c.status='available' AND NOT EXISTS(SELECT 1 FROM subscriber_provisioning p WHERE p.credential_id=c.id AND p.origin='managed') LIMIT 10001`, id)
		if er != nil {
			return out, er
		}
		type entry struct{ id, auth string }
		var entries []entry
		for rows.Next() {
			var cid, raw string
			if er = rows.Scan(&cid, &raw); er != nil {
				break
			}
			var a map[string]string
			if json.Unmarshal([]byte(raw), &a) != nil || a["password"] == "" {
				er = fmt.Errorf("已有原生客户端缺少可迁移的认证载荷")
				break
			}
			entries = append(entries, entry{cid, a["password"]})
		}
		if er == nil {
			er = rows.Err()
		}
		rows.Close()
		if er != nil {
			return out, er
		}
		if len(entries) > 10000 {
			return out, fmt.Errorf("迁移客户端数量超过保护上限")
		}
		for _, a := range entries {
			mode := "exact"
			if d.NativeUserpass {
				mode = "userpass"
				a.auth = userpass(a.auth)
				if a.auth == "" {
					return out, fmt.Errorf("已登记原生凭据不是 username:password，不能按 userpass 迁移")
				}
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO hysteria_auth_accounts(inbound_id,credential_id,auth_hash,origin,match_mode) VALUES(?,?,?,'native',?) ON CONFLICT(credential_id) DO UPDATE SET auth_hash=excluded.auth_hash,match_mode=excluded.match_mode`, id, a.id, accountHash(id, a.auth), mode)
			if e != nil {
				return out, fmt.Errorf("原生认证载荷重复或冲突，未准备后端")
			}
			out.Imported++
		}
	}
	if e = tx.Commit(); e != nil {
		return out, e
	}
	out.CallbackPath = "/core-auth/hysteria2/" + token
	return out, nil
}

func SetState(ctx context.Context, s *storage.Store, id string, revision int64, enabled, confirmed bool) error {
	r, e := s.DB.ExecContext(ctx, `UPDATE hysteria_auth_backends SET enabled=?,confirmed=?,configured_at=CASE WHEN ? AND ? THEN COALESCE(configured_at,?) ELSE configured_at END,revision=revision+1 WHERE inbound_id=? AND revision=? AND EXISTS(SELECT 1 FROM catalog_inbounds b JOIN instances i ON i.id=b.instance_id WHERE b.id=hysteria_auth_backends.inbound_id AND i.core_type='hysteria2' AND b.protocol='hysteria2' AND b.tag=target_tag AND i.api_endpoint=target_endpoint AND b.present=1 AND b.review=0 AND b.retired=0)`, enabled, confirmed, enabled, confirmed, storage.Stamp(time.Now()), id, revision)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return fmt.Errorf("后端修订或目标已变化，请刷新并重新核对")
	}
	return nil
}

func Ready(ctx context.Context, s *storage.Store, id string) bool {
	var n int
	e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM hysteria_auth_backends h JOIN catalog_inbounds b ON b.id=h.inbound_id JOIN instances i ON i.id=b.instance_id WHERE b.id=? AND h.enabled=1 AND h.confirmed=1 AND i.core_type='hysteria2' AND b.protocol='hysteria2' AND h.target_endpoint=i.api_endpoint AND h.target_tag=b.tag AND b.present=1 AND b.review=0 AND b.retired=0`, id).Scan(&n)
	return e == nil && n == 1
}

func LinkAccount(ctx context.Context, s *storage.Store, inbound, credential, auth string) error {
	r, e := s.DB.ExecContext(ctx, `INSERT INTO hysteria_auth_accounts(inbound_id,credential_id,auth_hash,origin) SELECT h.inbound_id,?,?,'managed' FROM hysteria_auth_backends h JOIN catalog_inbounds b ON b.id=h.inbound_id JOIN instances i ON i.id=b.instance_id WHERE h.inbound_id=? AND h.enabled=1 AND h.confirmed=1 AND h.target_endpoint=i.api_endpoint AND h.target_tag=b.tag AND b.present=1 AND b.review=0 AND b.retired=0 ON CONFLICT(credential_id) DO UPDATE SET auth_hash=excluded.auth_hash`, credential, accountHash(inbound, auth), inbound)
	if e != nil {
		return fmt.Errorf("认证账户冲突，未覆盖其他用户")
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return fmt.Errorf("认证后端未确认或目标变化")
	}
	return nil
}

// Authenticate uses the immediate TCP peer, never forwarded headers or the
// untrusted client addr/tx fields. Empty Stats/Online do not remove accounts.
func Authenticate(ctx context.Context, s *storage.Store, token, peer, auth string) (string, error) {
	if len(token) != 64 || len(auth) == 0 || len(auth) > 4096 {
		return "", nil
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var inbound, cidrs string
	e = tx.QueryRowContext(ctx, `SELECT h.inbound_id,h.allowed_cidrs FROM hysteria_auth_backends h JOIN catalog_inbounds b ON b.id=h.inbound_id JOIN instances i ON i.id=b.instance_id WHERE h.token_hash=? AND h.enabled=1 AND h.confirmed=1 AND b.present=1 AND b.retired=0 AND b.review=0 AND i.core_type='hysteria2' AND b.protocol='hysteria2' AND h.target_endpoint=i.api_endpoint AND h.target_tag=b.tag`, Hash(token)).Scan(&inbound, &cidrs)
	if e == sql.ErrNoRows {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	addr, e := netip.ParseAddr(peer)
	if e != nil {
		return "", nil
	}
	addr = addr.Unmap()
	var networks []string
	_ = json.Unmarshal([]byte(cidrs), &networks)
	allowed := false
	for _, v := range networks {
		p, er := netip.ParsePrefix(v)
		if er == nil && p.Contains(addr) {
			allowed = true
		}
	}
	if !allowed {
		return "", nil
	}
	var ref, credential, origin, material, mode string
	e = tx.QueryRowContext(ctx, `SELECT c.core_client_ref,c.id,a.origin,c.auth_json,a.match_mode FROM hysteria_auth_accounts a JOIN credential_material c ON c.id=a.credential_id WHERE a.inbound_id=? AND ((a.match_mode='exact' AND a.auth_hash=?) OR (a.match_mode='userpass' AND a.auth_hash=?)) AND c.status='available'`, inbound, accountHash(inbound, auth), accountHash(inbound, userpass(auth))).Scan(&ref, &credential, &origin, &material, &mode)
	now := storage.Stamp(time.Now())
	if e == sql.ErrNoRows {
		_, _ = tx.ExecContext(ctx, "UPDATE hysteria_auth_backends SET last_callback_at=? WHERE inbound_id=?", now, inbound)
		if er := tx.Commit(); er != nil {
			return "", er
		}
		return "", nil
	}
	if e != nil {
		return "", e
	}
	var current map[string]string
	if json.Unmarshal([]byte(material), &current) != nil {
		return "", nil
	}
	expected, received := current["password"], auth
	if mode == "userpass" {
		expected, received = userpass(expected), userpass(received)
	}
	if accountHash(inbound, expected) != accountHash(inbound, received) {
		return "", nil
	}
	var subscriber string
	var bindings, active int
	e = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(SUM(enabled),0),COALESCE(MIN(CASE WHEN enabled=1 THEN subscriber_id END),'') FROM subscriber_credentials WHERE credential_id=?`, credential).Scan(&bindings, &active, &subscriber)
	if e != nil {
		return "", e
	}
	if active > 1 || (active == 0 && (bindings > 0 || origin == "managed")) {
		return "", nil
	}
	if active == 1 {
		usage, er := subscription.SubscriberUsage(ctx, tx, subscriber)
		if er != nil {
			return "", er
		}
		if subscription.CheckQuotaAccess(usage) != nil {
			return "", nil
		}
	}
	if origin == "managed" {
		var ready int
		e = tx.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_provisioning WHERE credential_id=? AND subscriber_id=? AND status='ready' AND origin='managed'", credential, subscriber).Scan(&ready)
		if e != nil {
			return "", e
		}
		if ready != 1 {
			return "", nil
		}
	}
	// Success is authentication evidence, not a counter or online snapshot.
	if _, e = tx.ExecContext(ctx, "UPDATE hysteria_auth_backends SET last_callback_at=?,last_success_at=? WHERE inbound_id=?", now, now, inbound); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE hysteria_auth_accounts SET last_success_at=? WHERE credential_id=?", now, credential); e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	return ref, nil
}
