package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"traffic-manager-lite/internal/core"
)

// Only differences actually observed for a stable connection are accumulated.
// A first observation, epoch/ownership change or counter reset is a baseline.
func (s *Store) nativeRecords(ctx context.Context, tx *sql.Tx, instance int64, records []core.TrafficRecord) ([]core.TrafficRecord, error) {
	out := []core.TrafficRecord{}
	for _, r := range records {
		if r.Connections == nil {
			out = append(out, r)
			continue
		}
		totals := map[string]core.ConnectionCounter{}
		for _, c := range r.Connections {
			if c.ID == "" || c.Epoch == "" || c.Inbound == "" || c.User == "" || c.Upload < 0 || c.Download < 0 {
				return nil, fmt.Errorf("invalid native attribution")
			}
			var up, down int64
			var inbound, user, at string
			err := tx.QueryRowContext(ctx, "SELECT raw_upload,raw_download,inbound_tag,user_key,observed_at FROM native_connection_cursors WHERE instance_id=? AND epoch_id=? AND connection_id=?", instance, c.Epoch, c.ID).Scan(&up, &down, &inbound, &user, &at)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			du, dd := int64(0), int64(0)
			if err == nil {
				if Stamp(r.CollectedAt) <= at {
					continue
				}
				if inbound != c.Inbound || user != c.User {
					return nil, fmt.Errorf("connection ownership changed")
				}
				if c.Upload >= up && c.Download >= down {
					du = c.Upload - up
					dd = c.Download - down
				}
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO native_connection_cursors VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,epoch_id,connection_id) DO UPDATE SET raw_upload=excluded.raw_upload,raw_download=excluded.raw_download,observed_at=excluded.observed_at", instance, c.Epoch, c.ID, c.Inbound, c.User, c.Upload, c.Download, Stamp(r.CollectedAt)); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO native_user_totals VALUES(?,?,?,?,?) ON CONFLICT(instance_id,inbound_tag,user_key) DO UPDATE SET upload=upload+excluded.upload,download=download+excluded.download", instance, c.Inbound, c.User, du, dd); err != nil {
				return nil, err
			}
			totals[c.Inbound+"\x00"+c.User] = c
		}
		for _, c := range totals {
			v := r
			v.Connections = nil
			v.Scope = "user"
			v.InboundTag = c.Inbound
			v.UserKey = c.User
			v.EpochID = "observed-connection-ledger-v1"
			v.UploadCounter = "observed_connection_deltas.upload_sum"
			v.DownloadCounter = "observed_connection_deltas.download_sum"
			v.Mapped = true
			if err := tx.QueryRowContext(ctx, "SELECT upload,download FROM native_user_totals WHERE instance_id=? AND inbound_tag=? AND user_key=?", instance, c.Inbound, c.User).Scan(&v.UploadBytes, &v.DownloadBytes); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
	// Bounded observation cache; old connection IDs cannot reappear in later live snapshots.
	_, err := tx.ExecContext(ctx, "DELETE FROM native_connection_cursors WHERE observed_at<?", Stamp(time.Now().AddDate(0, 0, -30)))
	return out, err
}
func saveProvenance(ctx context.Context, tx *sql.Tx, id int64, r core.TrafficRecord) error {
	direction := r.Direction
	if direction == "" {
		direction = "client_upload/client_download"
	}
	upload, download := r.UploadCounter, r.DownloadCounter
	if upload == "" {
		upload = "uplink"
	}
	if download == "" {
		download = "downlink"
	}
	coverage := r.Coverage
	if coverage == "" {
		coverage = "cumulative_counter"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_provenance VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(identity_id) DO UPDATE SET provider_type=excluded.provider_type,metric_scope=excluded.metric_scope,capability_status=excluded.capability_status,collected_at=excluded.collected_at,instance_version=excluded.instance_version,config_revision=excluded.config_revision,source_upload_name=excluded.source_upload_name,source_download_name=excluded.source_download_name,raw_upload=excluded.raw_upload,raw_download=excluded.raw_download,traffic_direction=excluded.traffic_direction,mapped=excluded.mapped,coverage=excluded.coverage`, id, r.Source, r.Scope, "Available", Stamp(r.CollectedAt), r.InstanceVersion, r.ConfigRevision, upload, download, r.UploadBytes, r.DownloadBytes, direction, r.Mapped, coverage)
	return err
}

func saveCounterObservation(ctx context.Context, tx *sql.Tx, id int64, r core.TrafficRecord, up, down int64, location *time.Location) error {
	// Copy the normalized evidence already saved in this transaction; raw values
	// retain their explicitly named source counters, not a guessed tx/rx meaning.
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_counter_observations
 SELECT identity_id,collected_at,provider_type,metric_scope,capability_status,instance_version,config_revision,?,source_upload_name,source_download_name,raw_upload,raw_download,traffic_direction,mapped,coverage,?,?,?
 FROM traffic_provenance WHERE identity_id=?`, r.EpochID, up, down, r.CollectedAt.In(location).Format("2006-01-02"), id)
	return err
}

func archiveCounterEvidence(ctx context.Context, tx *sql.Tx, cutoff string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO archived_counter_evidence
 SELECT identity_id,date,provider_type,instance_version,config_revision,traffic_direction,source_upload_name,source_download_name,metric_scope,capability_status,mapped,coverage,MIN(collected_at),MAX(collected_at),COUNT(*),SUM(upload_delta),SUM(download_delta)
 FROM traffic_counter_observations WHERE collected_at<?
 GROUP BY identity_id,date,provider_type,instance_version,config_revision,traffic_direction,source_upload_name,source_download_name,metric_scope,capability_status,mapped,coverage
 ON CONFLICT(identity_id,date,provider_type,instance_version,config_revision,traffic_direction,source_upload_name,source_download_name,metric_scope,capability_status,mapped,coverage)
 DO UPDATE SET first_observed_at=MIN(first_observed_at,excluded.first_observed_at),last_observed_at=MAX(last_observed_at,excluded.last_observed_at),observations=observations+excluded.observations,upload_delta=upload_delta+excluded.upload_delta,download_delta=download_delta+excluded.download_delta`, cutoff)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM traffic_counter_observations WHERE collected_at<?", cutoff)
	return err
}
