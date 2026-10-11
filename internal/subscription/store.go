package subscription

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

func Token() (string, string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", "", e
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	return t, Hash(t), nil
}
func Hash(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

type Definition struct {
	Name    string   `json:"name"`
	Format  string   `json:"format"`
	UserID  int64    `json:"user_id"`
	NodeIDs []string `json:"node_ids"`
	Enabled bool     `json:"enabled"`
	Scope   string   `json:"selection_scope"`
}

var ErrNoUsableNodes = errors.New("订阅没有可用节点，请在后台检查节点选择、客户端地址和格式兼容性")

type PreviewResult struct {
	Selected int         `json:"selected"`
	Usable   int         `json:"usable"`
	Rejected []Rejection `json:"rejected"`
}
type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func scope(d Definition) string {
	if d.Scope != "" {
		return d.Scope
	}
	if d.UserID > 0 {
		return "user"
	}
	return "bundle"
}

// Public tokens grant only the explicit saved node set. Only administrators can
// create bundles; legacy user subscriptions keep their original ownership boundary.
func resolve(ctx context.Context, q queryer, d Definition) ([]core.Node, []Rejection, error) {
	nodes := []core.Node{}
	rejected := []Rejection{}
	seen := map[string]bool{}
	for pos, id := range d.NodeIDs {
		if seen[id] {
			return nil, nil, fmt.Errorf("节点重复")
		}
		seen[id] = true
		var n core.Node
		var raw string
		var enabled bool
		n.ID = id
		e := q.QueryRowContext(ctx, "SELECT instance_id,inbound_tag,user_id,effective_json,present,enabled FROM nodes WHERE id=?", id).Scan(&n.InstanceID, &n.InboundTag, &n.UserID, &raw, &n.Present, &enabled)
		if e == sql.ErrNoRows {
			rejected = append(rejected, Rejection{id, "节点已不存在"})
			continue
		}
		if e != nil {
			return nil, nil, e
		}
		if scope(d) == "user" && n.UserID != d.UserID {
			return nil, nil, fmt.Errorf("node belongs to another user")
		}
		if json.Unmarshal([]byte(raw), &n.Profile) != nil {
			rejected = append(rejected, Rejection{id, "节点配置无法解析"})
			continue
		}
		n.Profile.Enabled = n.Profile.Enabled && enabled
		n.Profile.Sort = pos
		nodes = append(nodes, n)
	}
	return nodes, rejected, nil
}
func preview(ctx context.Context, q queryer, d Definition) (PreviewResult, error) {
	p := PreviewResult{Selected: len(d.NodeIDs), Rejected: []Rejection{}}
	if len(d.NodeIDs) > 500 || (scope(d) != "user" && scope(d) != "bundle") || (scope(d) == "user" && d.UserID < 1) {
		return p, fmt.Errorf("订阅范围或节点数量无效")
	}
	nodes, missing, e := resolve(ctx, q, d)
	if e != nil {
		return p, e
	}
	_, _, filtered, e := Generate(d.Format, nodes)
	if e != nil {
		return p, e
	}
	p.Rejected = append(missing, filtered...)
	p.Usable = p.Selected - len(p.Rejected)
	return p, nil
}
func Preview(ctx context.Context, s *storage.Store, d Definition) (PreviewResult, error) {
	return preview(ctx, s.DB, d)
}

func Save(ctx context.Context, s *storage.Store, id int64, d Definition) (int64, string, error) {
	d.Name = strings.TrimSpace(d.Name)
	d.Scope = scope(d)
	if len(d.Name) < 1 || len(d.Name) > 200 || (d.Scope == "user" && d.UserID < 1) || (d.Scope != "user" && d.Scope != "bundle") || (d.Format != "v2ray" && d.Format != "mihomo" && d.Format != "singbox") {
		return 0, "", fmt.Errorf("invalid subscription")
	}
	if len(d.NodeIDs) > 500 {
		return 0, "", fmt.Errorf("too many nodes")
	}
	token, hash, e := Token()
	if e != nil {
		return 0, "", e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, "", e
	}
	defer tx.Rollback()
	p, e := preview(ctx, tx, d)
	if e != nil {
		return 0, "", e
	}
	if d.Enabled && p.Usable == 0 {
		return 0, "", ErrNoUsableNodes
	}
	var owner any
	if d.Scope == "user" {
		owner = d.UserID
	}
	now := storage.Stamp(time.Now())
	if id == 0 {
		r, e := tx.ExecContext(ctx, "INSERT INTO subscriptions(name,format,token_hash,user_id,enabled,created_at,updated_at,selection_scope) VALUES(?,?,?,?,?,?,?,?)", d.Name, d.Format, hash, owner, d.Enabled, now, now, d.Scope)
		if e != nil {
			return 0, "", e
		}
		id, e = r.LastInsertId()
		if e != nil {
			return 0, "", e
		}
	} else {
		token = ""
		r, e := tx.ExecContext(ctx, "UPDATE subscriptions SET name=?,format=?,user_id=?,enabled=?,updated_at=?,selection_scope=? WHERE id=?", d.Name, d.Format, owner, d.Enabled, now, d.Scope, id)
		if e != nil {
			return 0, "", e
		}
		n, _ := r.RowsAffected()
		if n == 0 {
			return 0, "", sql.ErrNoRows
		}
		if _, e = tx.ExecContext(ctx, "DELETE FROM subscription_nodes WHERE subscription_id=?", id); e != nil {
			return 0, "", e
		}
	}
	for pos, node := range d.NodeIDs {
		var uid int64
		if e = tx.QueryRowContext(ctx, "SELECT user_id FROM nodes WHERE id=?", node).Scan(&uid); e != nil {
			return 0, "", fmt.Errorf("node unavailable")
		}
		if d.Scope == "user" && uid != d.UserID {
			return 0, "", fmt.Errorf("node belongs to another user")
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO subscription_nodes VALUES(?,?,?)", id, node, pos); e != nil {
			return 0, "", e
		}
	}
	return id, token, tx.Commit()
}
func Rotate(ctx context.Context, s *storage.Store, id int64) (string, error) {
	t, h, e := Token()
	if e != nil {
		return "", e
	}
	r, e := s.DB.ExecContext(ctx, "UPDATE subscriptions SET token_hash=?,updated_at=? WHERE id=?", h, storage.Stamp(time.Now()), id)
	if e != nil {
		return "", e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return "", sql.ErrNoRows
	}
	return t, nil
}
func Output(ctx context.Context, s *storage.Store, format, token string) ([]byte, string, []Rejection, error) {
	if len(token) != 43 {
		return nil, "", nil, sql.ErrNoRows
	}
	var id int64
	var user sql.NullInt64
	var mode string
	if e := s.DB.QueryRowContext(ctx, "SELECT id,user_id,selection_scope FROM subscriptions WHERE token_hash=? AND format=? AND enabled=1", Hash(token), format).Scan(&id, &user, &mode); e != nil {
		return nil, "", nil, e
	}
	selected, e := s.Rows(ctx, "SELECT node_id,position FROM subscription_nodes WHERE subscription_id=? ORDER BY position", id)
	if e != nil {
		return nil, "", nil, e
	}
	d := Definition{Format: format, UserID: user.Int64, Scope: mode}
	for _, row := range selected {
		d.NodeIDs = append(d.NodeIDs, row["node_id"].(string))
	}
	nodes, missing, e := resolve(ctx, s.DB, d)
	if e != nil {
		return nil, "", nil, e
	}
	b, ct, rejected, e := Generate(format, nodes)
	rejected = append(missing, rejected...)
	if e == nil && len(d.NodeIDs)-len(rejected) == 0 {
		e = ErrNoUsableNodes
	}
	return b, ct, rejected, e
}
