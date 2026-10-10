package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type DirectionPlan struct {
	InstanceID                                            int64  `json:"instance_id"`
	Cutoff                                                string `json:"cutoff"`
	Samples, Hourly, Daily, ArchivedHourly, ArchivedDaily int64
	AlreadyApplied                                        bool   `json:"already_applied"`
	Status                                                string `json:"status"`
	Backup                                                string `json:"backup,omitempty"`
}

const legacyHYProfile = "tml_hy_rx_upload_tx_download"

func (s *Store) DirectionPlan(ctx context.Context, id int64, cutoff time.Time) (DirectionPlan, error) {
	p := DirectionPlan{InstanceID: id, Cutoff: Stamp(cutoff), Status: "DirectionUnverified"}
	var kind string
	if e := s.DB.QueryRowContext(ctx, "SELECT core_type FROM instances WHERE id=?", id).Scan(&kind); e != nil {
		return p, e
	}
	if kind != "hysteria2" {
		return p, errors.New("only Hysteria2 legacy profile can be reviewed")
	}
	var n int
	s.DB.QueryRowContext(ctx, "SELECT count(*) FROM traffic_direction_repairs WHERE instance_id=? AND source_profile=?", id, legacyHYProfile).Scan(&n)
	if n > 0 {
		p.AlreadyApplied = true
		p.Status = "AlreadyRepaired"
		return p, nil
	}
	for _, v := range []struct {
		query string
		field *int64
	}{{"SELECT count(*) FROM traffic_samples WHERE instance_id=? AND collected_at<?", &p.Samples}, {"SELECT count(*) FROM traffic_hourly WHERE instance_id=? AND hour_start<?", &p.Hourly}, {"SELECT count(*) FROM traffic_daily WHERE instance_id=? AND date<?", &p.Daily}, {"SELECT count(*) FROM archive_ledger l JOIN identities i ON i.id=l.identity_id WHERE i.instance_id=? AND l.hour_start<?", &p.ArchivedHourly}, {"SELECT count(*) FROM archive_daily_ledger l JOIN identities i ON i.id=l.identity_id WHERE i.instance_id=? AND l.date<?", &p.ArchivedDaily}} {
		bound := p.Cutoff
		if v.field == &p.Daily || v.field == &p.ArchivedDaily {
			bound = cutoff.In(s.Location).AddDate(0, 0, 1).Format("2006-01-02")
		}
		if e := s.DB.QueryRowContext(ctx, v.query, id, bound).Scan(v.field); e != nil {
			return p, e
		}
	}
	return p, nil
}

// Repair is intentionally an explicit offline operation, never an automatic schema swap.
// The operator must attest that this instance's selected history came exclusively
// from the audited legacy TML adapter. Imported/mixed histories are not eligible.
func (s *Store) RepairHysteriaDirection(ctx context.Context, id int64, cutoff time.Time, evidence, backupDir string) (DirectionPlan, error) {
	p, e := s.DirectionPlan(ctx, id, cutoff)
	if e != nil || p.AlreadyApplied {
		return p, e
	}
	if evidence == "" || len(evidence) > 1000 {
		return p, errors.New("source evidence required; unknown history must remain unverified")
	}
	if cutoff.After(time.Now()) {
		return p, errors.New("future cutoff rejected")
	}
	p.Backup, e = s.Backup(ctx, backupDir)
	if e != nil {
		return p, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	var already int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM traffic_direction_repairs WHERE instance_id=? AND source_profile=?", id, legacyHYProfile).Scan(&already); e != nil {
		return p, e
	}
	if already > 0 {
		return p, errors.New("repair already applied")
	}
	var corrected int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM traffic_counter_observations o JOIN identities i ON i.id=o.identity_id WHERE i.instance_id=? AND o.collected_at<? AND (o.epoch_id='hy-client-direction-v2' OR o.source_upload_name='tx' AND o.source_download_name='rx')`, id, p.Cutoff).Scan(&corrected); e != nil || corrected > 0 {
		return p, errors.New("selected history includes corrected observations; narrow cutoff, no records changed")
	}
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM archived_counter_evidence o JOIN identities i ON i.id=o.identity_id WHERE i.instance_id=? AND o.source_upload_name='tx' AND o.source_download_name='rx'`, id).Scan(&corrected); e != nil || corrected > 0 {
		return p, errors.New("archived history includes corrected evidence; mixed history cannot be exchanged")
	}
	// Archived ledgers have no per-sample timestamp. Refuse a cutoff that splits
	// their scope instead of inventing which archived contribution was affected.
	var ambiguous int
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM archive_ledger l JOIN identities i ON i.id=l.identity_id WHERE i.instance_id=? AND l.hour_start>=?`, id, Stamp(cutoff.UTC().Truncate(time.Hour))).Scan(&ambiguous)
	if e != nil || ambiguous > 0 {
		return p, errors.New("archive cutoff is ambiguous; no records changed")
	}
	var current int64
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM traffic_samples WHERE instance_id=? AND collected_at<?", id, p.Cutoff).Scan(&current); e != nil || current != p.Samples {
		return p, errors.New("database changed after backup; stop manager before repair")
	}
	identity := "SELECT id FROM identities WHERE instance_id=?"
	for _, spec := range []struct{ aggregate, ledger, bucket string }{{"traffic_hourly", "archive_ledger", "hour_start"}, {"traffic_daily", "archive_daily_ledger", "date"}} {
		contributions := "SELECT " + spec.bucket + ",identity_id,upload_delta u,download_delta d FROM traffic_samples WHERE instance_id=? AND collected_at<? UNION ALL SELECT " + spec.bucket + ",identity_id,upload_bytes u,download_bytes d FROM " + spec.ledger + " WHERE identity_id IN (" + identity + ")"
		q := fmt.Sprintf(`WITH changes AS (SELECT %s,identity_id,SUM(d-u) diff FROM (%s) GROUP BY %s,identity_id) UPDATE %s AS a SET upload_bytes=upload_bytes+COALESCE((SELECT diff FROM changes c WHERE c.identity_id=a.identity_id AND c.%s=a.%s),0),download_bytes=download_bytes-COALESCE((SELECT diff FROM changes c WHERE c.identity_id=a.identity_id AND c.%s=a.%s),0) WHERE instance_id=?`, spec.bucket, contributions, spec.bucket, spec.aggregate, spec.bucket, spec.bucket, spec.bucket, spec.bucket)
		if _, e = tx.ExecContext(ctx, q, id, p.Cutoff, id, id); e != nil {
			return p, e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE traffic_samples SET upload_delta=download_delta,download_delta=upload_delta WHERE instance_id=? AND collected_at<?", id, p.Cutoff); e != nil {
		return p, e
	}
	for _, table := range []string{"archive_ledger", "archive_daily_ledger"} {
		if _, e = tx.ExecContext(ctx, "UPDATE "+table+" SET upload_bytes=download_bytes,download_bytes=upload_bytes WHERE identity_id IN ("+identity+")", id); e != nil {
			return p, e
		}
	}
	// Preserve the historical source evidence and correct its normalized values.
	if _, e = tx.ExecContext(ctx, "UPDATE traffic_counter_observations SET raw_upload=raw_download,raw_download=raw_upload,upload_delta=download_delta,download_delta=upload_delta,source_upload_name='tx',source_download_name='rx',traffic_direction='legacy_repaired:tx=client_upload;rx=client_download',mapped=1 WHERE identity_id IN ("+identity+") AND collected_at<?", id, p.Cutoff); e != nil {
		return p, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE archived_counter_evidence SET upload_delta=download_delta,download_delta=upload_delta,source_upload_name='tx',source_download_name='rx',traffic_direction='legacy_repaired:tx=client_upload;rx=client_download',mapped=1 WHERE identity_id IN ("+identity+")", id); e != nil {
		return p, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE traffic_provenance SET raw_upload=raw_download,raw_download=raw_upload,source_upload_name='tx',source_download_name='rx',traffic_direction='legacy_repaired:tx=client_upload;rx=client_download',mapped=1 WHERE identity_id IN ("+identity+") AND collected_at<?", id, p.Cutoff); e != nil {
		return p, e
	}
	// Current new-direction cursors must never be swapped. Only explicit old/no-source cursors qualify.
	if _, e = tx.ExecContext(ctx, "UPDATE traffic_cursors SET raw_upload=raw_download,raw_download=raw_upload,epoch_id='hy-direction-repaired',provider_type='hysteria2' WHERE identity_id IN ("+identity+") AND provider_type IN ('','hysteria2') AND epoch_id NOT IN ('hy-client-direction-v2','hy-direction-repaired') AND updated_at<?", id, p.Cutoff); e != nil {
		return p, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO traffic_direction_repairs VALUES(?,?,?,?,?,?,?)", id, legacyHYProfile, p.Cutoff, p.Samples+p.ArchivedHourly, p.Backup, evidence, Stamp(time.Now())); e != nil {
		return p, e
	}
	if e = verifyLedger(ctx, tx); e != nil {
		return p, e
	}
	if e = tx.Commit(); e != nil {
		return p, e
	}
	p.Status = "Repaired"
	return p, nil
}
func verifyLedger(ctx context.Context, tx *sql.Tx) error {
	for _, spec := range []struct{ aggregate, ledger, bucket string }{{"traffic_hourly", "archive_ledger", "hour_start"}, {"traffic_daily", "archive_daily_ledger", "date"}} {
		var n int
		q := fmt.Sprintf(`WITH contributions AS (SELECT %s,identity_id,upload_delta u,download_delta d FROM traffic_samples UNION ALL SELECT %s,identity_id,upload_bytes,download_bytes FROM %s),expected AS (SELECT %s,identity_id,SUM(u) u,SUM(d) d FROM contributions GROUP BY %s,identity_id) SELECT count(*) FROM %s h FULL OUTER JOIN expected e USING(%s,identity_id) WHERE h.identity_id IS NULL OR e.identity_id IS NULL OR h.upload_bytes!=e.u OR h.download_bytes!=e.d`, spec.bucket, spec.bucket, spec.ledger, spec.bucket, spec.bucket, spec.aggregate, spec.bucket)
		if e := tx.QueryRowContext(ctx, q).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("repaired ledger verification failed; transaction rolled back")
		}
	}
	return nil
}
