package coremonitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/xraymonitor"
)

func (s Service) saveObservation(ctx context.Context, i core.Instance, o observation) error {
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := storage.Stamp(time.Now())
	if o.clientsOK {
		if _, e = tx.ExecContext(ctx, "UPDATE core_clients SET present=0 WHERE instance_id=? AND source IN ('config_file','auth_export')", i.ID); e != nil {
			return e
		}
		for _, asset := range o.clients {
			if _, e = tx.ExecContext(ctx, "INSERT INTO core_clients(instance_id,inbound_tag,asset_key,email,protocol,source,present,observed_at) VALUES(?,?,?,?,?,?,1,?) ON CONFLICT(instance_id,inbound_tag,asset_key,source) DO UPDATE SET email=excluded.email,protocol=excluded.protocol,present=1,observed_at=excluded.observed_at", i.ID, asset.Inbound, asset.Key, asset.Email, asset.Protocol, asset.Source, now); e != nil {
				return e
			}
			if asset.Email == "" {
				continue
			}
			var exists int
			e = tx.QueryRowContext(ctx, "SELECT id FROM identities WHERE instance_id=? AND scope='user' AND core_user_key=? LIMIT 1", i.ID, asset.Email).Scan(&exists)
			if e == sql.ErrNoRows {
				res, err := tx.ExecContext(ctx, "INSERT INTO users(display_name,created_at,updated_at) VALUES(?,?,?)", asset.Email, now, now)
				if err != nil {
					return err
				}
				uid, err := res.LastInsertId()
				if err != nil {
					return err
				}
				if _, e = tx.ExecContext(ctx, "INSERT INTO identities(user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(?,?,'',?,'user')", uid, i.ID, asset.Email); e != nil {
					return e
				}
			} else if e != nil {
				return e
			}
		}
	}
	for _, h := range o.report.Health {
		var success any
		failures := 0
		if h.Status == "Healthy" {
			success = now
		} else {
			failures = 1
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO collector_health(instance_id,collector,status,result_count,checked_at,success_at,error_code,summary,failures) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,collector) DO UPDATE SET status=excluded.status,result_count=excluded.result_count,checked_at=excluded.checked_at,success_at=COALESCE(excluded.success_at,collector_health.success_at),error_code=excluded.error_code,summary=excluded.summary,failures=CASE WHEN excluded.status='Healthy' THEN 0 ELSE collector_health.failures+1 END`, i.ID, h.Collector, h.Status, h.Count, now, success, h.Code, h.Summary, failures); e != nil {
			return e
		}
	}
	rows, e := tx.QueryContext(ctx, "SELECT DISTINCT core_user_key FROM identities WHERE instance_id=? AND scope='user'", i.ID)
	if e != nil {
		return e
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			rows.Close()
			return e
		}
		keys = append(keys, key)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, key := range keys {
		state := &xraymonitor.State{Email: key, Stats: "unknown", Online: "unknown", Basis: o.onlineBasis}
		if o.userStatsOK {
			state.Stats = "not_generated"
		}
		if i.CoreType == "v2fly" {
			state.Online = "unsupported"
		}
		if o.onlineOK && i.CoreType == "hysteria2" {
			zero := int64(0)
			state.Online = "offline"
			state.Count = &zero
		}
		// A sing-box user must have an actual observable user mapping before absence
		// in a subsequent complete snapshot can be considered offline.
		if o.onlineOK && i.CoreType == "singbox" {
			var previous sql.NullInt64
			tx.QueryRowContext(ctx, "SELECT online_count FROM core_user_states WHERE instance_id=? AND email=? AND online_kind='session'", i.ID, key).Scan(&previous)
			if previous.Valid {
				zero := int64(0)
				state.Online = "offline"
				state.Count = &zero
			} else {
				state.Basis += "；尚未实际验证此用户的连接身份映射"
			}
		}
		for _, record := range o.records {
			if record.Scope == "user" && record.UserKey == key && o.statsOK {
				state.Stats = "available"
				if record.UploadBytes == 0 && record.DownloadBytes == 0 {
					state.Stats = "zero"
				}
			}
		}
		for _, online := range o.online {
			if online.UserKey == key && o.onlineOK {
				n := online.Count
				state.Count = &n
				state.Online = "offline"
				if n > 0 {
					state.Online = "online"
				}
				state.Basis = o.onlineBasis
			}
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO core_user_states(instance_id,email,stats_state,online_state,online_count,online_basis,checked_at,online_kind) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,email) DO UPDATE SET stats_state=excluded.stats_state,online_state=excluded.online_state,online_count=excluded.online_count,online_basis=excluded.online_basis,checked_at=excluded.checked_at,online_kind=excluded.online_kind", i.ID, key, state.Stats, state.Online, state.Count, state.Basis, now, o.onlineKind); e != nil {
			return e
		}
	}
	// Diagnostics do not enter this function. Failed Online does not overwrite snapshots.
	if o.onlineOK {
		b, _ := json.Marshal(o.online)
		if _, e = tx.ExecContext(ctx, "INSERT INTO online_snapshots VALUES(?,?,?) ON CONFLICT(instance_id) DO UPDATE SET records_json=excluded.records_json,updated_at=excluded.updated_at", i.ID, string(b), now); e != nil {
			return e
		}
	}
	caps := []core.Capability{{Metric: "user_traffic", Status: "unknown", Reason: "只按实际用户计数器确认；空结果不证明每种协议可统计"}, {Metric: "inbound_traffic", Status: "unknown"}}
	for _, r := range o.records {
		if o.statsOK && r.Scope == "user" {
			caps[0].Status = "supported"
		}
		if o.statsOK && r.Scope == "inbound" {
			caps[1].Status = "supported"
		}
	}
	metric := "online_sessions"
	if i.CoreType == "hysteria2" {
		metric = "online_devices"
	}
	state := "unknown"
	if o.onlineOK {
		state = "supported"
	}
	if i.CoreType == "v2fly" {
		state = "unsupported"
	}
	caps = append(caps, core.Capability{Metric: metric, Status: state, Reason: o.onlineBasis})
	if i.CoreType == "v2fly" {
		caps = append(caps, core.Capability{Metric: "online", Status: "unsupported", Reason: o.onlineBasis})
	}
	cb, _ := json.Marshal(caps)
	var success any
	if o.statsOK {
		success = now
	}
	errorText := ""
	for _, h := range o.report.Health {
		if h.Status == "Error" || h.Status == "Unreachable" || h.Status == "AuthenticationFailed" {
			errorText += h.Collector + ": " + h.Summary + "; "
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE instances SET last_collected_at=COALESCE(?,last_collected_at),last_error=?,capabilities_json=?,detected_version=? WHERE id=?", success, errorText, string(cb), o.report.Version, i.ID); e != nil {
		return e
	}
	return tx.Commit()
}
