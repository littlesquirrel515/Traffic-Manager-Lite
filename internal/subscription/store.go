package subscription

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
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
}

func Save(ctx context.Context, s *storage.Store, id int64, d Definition) (int64, string, error) {
	if len(d.Name) < 1 || len(d.Name) > 200 || d.UserID < 1 || (d.Format != "v2ray" && d.Format != "mihomo" && d.Format != "singbox") {
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
	now := storage.Stamp(time.Now())
	if id == 0 {
		r, e := tx.ExecContext(ctx, "INSERT INTO subscriptions(name,format,token_hash,user_id,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", d.Name, d.Format, hash, d.UserID, d.Enabled, now, now)
		if e != nil {
			return 0, "", e
		}
		id, e = r.LastInsertId()
		if e != nil {
			return 0, "", e
		}
	} else {
		token = ""
		r, e := tx.ExecContext(ctx, "UPDATE subscriptions SET name=?,format=?,user_id=?,enabled=?,updated_at=? WHERE id=?", d.Name, d.Format, d.UserID, d.Enabled, now, id)
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
		if uid != d.UserID {
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
	var id, user int64
	if e := s.DB.QueryRowContext(ctx, "SELECT id,user_id FROM subscriptions WHERE token_hash=? AND format=? AND enabled=1", Hash(token), format).Scan(&id, &user); e != nil {
		return nil, "", nil, e
	}
	selected, e := s.Rows(ctx, "SELECT node_id,position FROM subscription_nodes WHERE subscription_id=? ORDER BY position", id)
	if e != nil {
		return nil, "", nil, e
	}
	nodes, e := discovery.Nodes(ctx, s)
	if e != nil {
		return nil, "", nil, e
	}
	m := map[string]core.Node{}
	for _, n := range nodes {
		if n.UserID == user {
			m[n.ID] = n
		}
	}
	out := []core.Node{}
	for pos, row := range selected {
		n, ok := m[row["node_id"].(string)]
		if ok {
			n.Profile.Sort = pos
			out = append(out, n)
		}
	}
	return Generate(format, out)
}
