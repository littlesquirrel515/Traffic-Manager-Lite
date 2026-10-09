package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
	"traffic-manager-lite/internal/core"
)

// Apply commits the cursor and all three representations of each observed delta atomically.
func (s *Store) Apply(ctx context.Context, instanceID int64, records []core.TrafficRecord) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	batch := Stamp(time.Now())
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_batches VALUES(?,?) ON CONFLICT(instance_id) DO UPDATE SET batch_id=excluded.batch_id", instanceID, batch); e != nil {
		return e
	}
	seen := map[string]bool{}
	epochID := ""
	for _, r := range records {
		if r.BootEstimate != nil {
			var oldID, oldBoot, observed string
			e = tx.QueryRowContext(ctx, "SELECT epoch_id,boot_estimate,observed_at FROM instance_epochs WHERE instance_id=?", instanceID).Scan(&oldID, &oldBoot, &observed)
			if e != nil && e != sql.ErrNoRows {
				return e
			}
			estimate := r.BootEstimate.UTC()
			epochID = Stamp(estimate)
			if e == nil {
				previous, er := time.Parse(time.RFC3339Nano, oldBoot)
				if er != nil {
					return er
				}
				drift := estimate.Sub(previous)
				if drift < 0 {
					drift = -drift
				}
				if drift <= 5*time.Second || Stamp(r.CollectedAt) <= observed {
					epochID = oldID
				}
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO instance_epochs VALUES(?,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET epoch_id=excluded.epoch_id,boot_estimate=excluded.boot_estimate,observed_at=excluded.observed_at WHERE excluded.observed_at>instance_epochs.observed_at", instanceID, epochID, Stamp(estimate), Stamp(r.CollectedAt)); e != nil {
				return e
			}
			break
		}
	}
	for _, r := range records {
		if epochID != "" {
			r.EpochID = epochID
		}
		if r.UploadBytes < 0 || r.DownloadBytes < 0 || r.CollectedAt.IsZero() || r.CounterMode != "cumulative" {
			return fmt.Errorf("invalid cumulative counter")
		}
		if r.Scope != "user" && r.Scope != "inbound" && r.Scope != "instance" {
			return fmt.Errorf("invalid scope")
		}
		key := r.Scope + "\x00" + r.InboundTag + "\x00" + r.UserKey
		if seen[key] {
			return fmt.Errorf("duplicate counter in collection")
		}
		seen[key] = true
		var id int64
		var user sql.NullInt64
		e = tx.QueryRowContext(ctx, "SELECT id,user_id FROM identities WHERE instance_id=? AND inbound_tag=? AND core_user_key=? AND scope=?", instanceID, r.InboundTag, r.UserKey, r.Scope).Scan(&id, &user)
		if e == sql.ErrNoRows {
			if r.Scope == "user" {
				res, er := tx.ExecContext(ctx, "INSERT INTO users(display_name,created_at,updated_at) VALUES(?,?,?)", r.UserKey, Stamp(r.CollectedAt), Stamp(r.CollectedAt))
				if er != nil {
					return er
				}
				uid, er := res.LastInsertId()
				if er != nil {
					return er
				}
				user = sql.NullInt64{Int64: uid, Valid: true}
			}
			res, er := tx.ExecContext(ctx, "INSERT INTO identities(user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(?,?,?,?,?)", user, instanceID, r.InboundTag, r.UserKey, r.Scope)
			if er != nil {
				return er
			}
			id, e = res.LastInsertId()
		}
		if e != nil {
			return e
		}
		var up, down int64
		var epoch, last string
		e = tx.QueryRowContext(ctx, "SELECT raw_upload,raw_download,epoch_id,updated_at FROM traffic_cursors WHERE identity_id=?", id).Scan(&up, &down, &epoch, &last)
		du, dd := int64(0), int64(0)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == nil {
			if Stamp(r.CollectedAt) <= last {
				continue
			}
			// A reset establishes a fresh baseline. Unobserved traffic is never estimated.
			if (r.EpochID == "" || epoch == r.EpochID) && r.UploadBytes >= up && r.DownloadBytes >= down {
				du = r.UploadBytes - up
				dd = r.DownloadBytes - down
			}
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO traffic_cursors VALUES(?,?,?,?,?) ON CONFLICT(identity_id) DO UPDATE SET raw_upload=excluded.raw_upload,raw_download=excluded.raw_download,epoch_id=excluded.epoch_id,updated_at=excluded.updated_at", id, r.UploadBytes, r.DownloadBytes, r.EpochID, Stamp(r.CollectedAt))
		if e != nil {
			return e
		}
		if du == 0 && dd == 0 {
			continue
		}
		hour := Stamp(r.CollectedAt.UTC().Truncate(time.Hour))
		date := r.CollectedAt.In(s.Location).Format("2006-01-02")
		_, e = tx.ExecContext(ctx, "INSERT INTO traffic_samples(identity_id,instance_id,inbound_tag,upload_delta,download_delta,collected_at,hour_start,date,batch_id) VALUES(?,?,?,?,?,?,?,?,?)", id, instanceID, r.InboundTag, du, dd, Stamp(r.CollectedAt), hour, date, batch)
		if e != nil {
			return e
		}
		for _, table := range []string{"traffic_hourly", "traffic_daily"} {
			col, bucket := "hour_start", hour
			if table == "traffic_daily" {
				col, bucket = "date", date
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO "+table+"("+col+",identity_id,instance_id,upload_bytes,download_bytes) VALUES(?,?,?,?,?) ON CONFLICT("+col+",identity_id) DO UPDATE SET upload_bytes=upload_bytes+excluded.upload_bytes,download_bytes=download_bytes+excluded.download_bytes", bucket, id, instanceID, du, dd)
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, "UPDATE identities SET last_active_at=? WHERE id=?", Stamp(r.CollectedAt), id)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Archive verifies both aggregate representations including previously deleted contributions.
func (s *Store) Archive(ctx context.Context, now time.Time) (int64, error) {
	cutoff := Stamp(now.UTC().AddDate(0, 0, -30))
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var bad int
	e = tx.QueryRowContext(ctx, `WITH contributions AS (
 SELECT hour_start,identity_id,upload_delta u,download_delta d FROM traffic_samples UNION ALL SELECT hour_start,identity_id,upload_bytes,download_bytes FROM archive_ledger),
 expected AS (SELECT hour_start,identity_id,SUM(u) u,SUM(d) d FROM contributions GROUP BY hour_start,identity_id)
 SELECT count(*) FROM traffic_hourly h FULL OUTER JOIN expected e USING(hour_start,identity_id)
 WHERE h.identity_id IS NULL OR e.identity_id IS NULL OR h.upload_bytes!=e.u OR h.download_bytes!=e.d`).Scan(&bad)
	if e != nil {
		return 0, e
	}
	if bad != 0 {
		return 0, fmt.Errorf("hourly aggregate verification failed")
	}
	e = tx.QueryRowContext(ctx, `WITH contributions AS (
 SELECT date,identity_id,upload_delta u,download_delta d FROM traffic_samples UNION ALL SELECT date,identity_id,upload_bytes,download_bytes FROM archive_daily_ledger),
 expected AS (SELECT date,identity_id,SUM(u) u,SUM(d) d FROM contributions GROUP BY date,identity_id)
 SELECT count(*) FROM traffic_daily h FULL OUTER JOIN expected e USING(date,identity_id)
 WHERE h.identity_id IS NULL OR e.identity_id IS NULL OR h.upload_bytes!=e.u OR h.download_bytes!=e.d`).Scan(&bad)
	if e != nil {
		return 0, e
	}
	if bad != 0 {
		return 0, fmt.Errorf("daily aggregate verification failed")
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_ledger SELECT hour_start,identity_id,SUM(upload_delta),SUM(download_delta) FROM traffic_samples WHERE collected_at < ? GROUP BY hour_start,identity_id
 ON CONFLICT(hour_start,identity_id) DO UPDATE SET upload_bytes=upload_bytes+excluded.upload_bytes,download_bytes=download_bytes+excluded.download_bytes`, cutoff)
	if e != nil {
		return 0, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO archive_daily_ledger SELECT date,identity_id,SUM(upload_delta),SUM(download_delta) FROM traffic_samples WHERE collected_at < ? GROUP BY date,identity_id
 ON CONFLICT(date,identity_id) DO UPDATE SET upload_bytes=upload_bytes+excluded.upload_bytes,download_bytes=download_bytes+excluded.download_bytes`, cutoff)
	if e != nil {
		return 0, e
	}
	res, e := tx.ExecContext(ctx, "DELETE FROM traffic_samples WHERE collected_at < ?", cutoff)
	if e != nil {
		return 0, e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return 0, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO archive_runs(cutoff,deleted,created_at) VALUES(?,?,?)", cutoff, n, Stamp(now))
	if e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM collector_errors WHERE created_at < ?", cutoff); e != nil {
		return 0, e
	}
	return n, tx.Commit()
}
func (s *Store) offset(t time.Time) string {
	_, o := t.In(s.Location).Zone()
	return fmt.Sprintf("%+d seconds", o)
}
func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	if e := mkdir(dir); e != nil {
		return "", e
	}
	path := backupPath(dir, time.Now())
	_, e := s.DB.ExecContext(ctx, "VACUUM INTO ?", path)
	if e == nil {
		e = os.Chmod(path, 0600)
	}
	return path, e
}
