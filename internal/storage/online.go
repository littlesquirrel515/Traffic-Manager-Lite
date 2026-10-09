package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"traffic-manager-lite/internal/core"
)

// Online identities are metadata only: no traffic cursor or sample is fabricated.
func (s *Store) SaveOnline(ctx context.Context, instance int64, records []core.OnlineRecord) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := Stamp(time.Now())
	if len(records) > 10000 {
		return fmt.Errorf("too many online identities")
	}
	for _, r := range records {
		if r.UserKey == "" || len(r.UserKey) > 512 || r.Count < 0 {
			return fmt.Errorf("invalid online identity")
		}
		var id int64
		e = tx.QueryRowContext(ctx, "SELECT id FROM identities WHERE instance_id=? AND core_user_key=? AND scope='user' LIMIT 1", instance, r.UserKey).Scan(&id)
		if e == sql.ErrNoRows {
			res, er := tx.ExecContext(ctx, "INSERT INTO users(display_name,created_at,updated_at) VALUES(?,?,?)", r.UserKey, now, now)
			if er != nil {
				return er
			}
			user, er := res.LastInsertId()
			if er != nil {
				return er
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO identities(user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(?,?,'',?,'user')", user, instance, r.UserKey)
		}
		if e != nil {
			return e
		}
	}
	b, e := json.Marshal(records)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO online_snapshots VALUES(?,?,?) ON CONFLICT(instance_id) DO UPDATE SET records_json=excluded.records_json,updated_at=excluded.updated_at", instance, string(b), now)
	if e != nil {
		return e
	}
	return tx.Commit()
}
