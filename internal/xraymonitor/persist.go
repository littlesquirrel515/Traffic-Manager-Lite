package xraymonitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

func (s Service) saveObservation(ctx context.Context, inst core.Instance, o observation) error {
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := storage.Stamp(time.Now())
	// Remove only sources/inbounds whose enumeration actually completed.
	if o.listOK {
		old, e := tx.QueryContext(ctx, "SELECT DISTINCT inbound_tag FROM xray_clients WHERE instance_id=? AND source='runtime_api'", inst.ID)
		if e != nil {
			return e
		}
		tags := []string{}
		for old.Next() {
			var tag string
			old.Scan(&tag)
			tags = append(tags, tag)
		}
		old.Close()
		for _, tag := range tags {
			if !o.listedTags[tag] {
				o.completeTags[tag] = true
			}
		}
	}
	for tag := range o.completeTags {
		if _, e = tx.ExecContext(ctx, "UPDATE xray_clients SET present=0 WHERE instance_id=? AND source='runtime_api' AND inbound_tag=?", inst.ID, tag); e != nil {
			return e
		}
	}
	if o.configOK {
		if _, e = tx.ExecContext(ctx, "UPDATE xray_clients SET present=0 WHERE instance_id=? AND source='config_file'", inst.ID); e != nil {
			return e
		}
	}
	for _, c := range append(append([]Client{}, o.clients...), o.configClients...) {
		_, e = tx.ExecContext(ctx, "INSERT INTO xray_clients(instance_id,inbound_tag,asset_key,email,protocol,level,source,present,observed_at,credential_fingerprint) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,inbound_tag,asset_key,source) DO UPDATE SET email=excluded.email,protocol=excluded.protocol,level=excluded.level,present=1,observed_at=excluded.observed_at,credential_fingerprint=excluded.credential_fingerprint", inst.ID, c.Inbound, c.Key, c.Email, c.Protocol, c.Level, c.Source, 1, now, c.Fingerprint)
		if e != nil {
			return e
		}
		if c.Email == "" {
			continue
		}
		var identity int64
		e = tx.QueryRowContext(ctx, "SELECT id FROM identities WHERE instance_id=? AND scope='user' AND core_user_key=? LIMIT 1", inst.ID, c.Email).Scan(&identity)
		if e == sql.ErrNoRows {
			res, e := tx.ExecContext(ctx, "INSERT INTO users(display_name,created_at,updated_at) VALUES(?,?,?)", c.Email, now, now)
			if e != nil {
				return e
			}
			uid, _ := res.LastInsertId()
			if _, e = tx.ExecContext(ctx, "INSERT INTO identities(user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(?,?,'',?,'user')", uid, inst.ID, c.Email); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
	}
	for _, h := range o.report.Health {
		var success any
		if h.Status == "Healthy" {
			success = now
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO collector_health(instance_id,collector,status,result_count,checked_at,success_at,error_code,summary,failures) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,collector) DO UPDATE SET status=excluded.status,result_count=excluded.result_count,checked_at=excluded.checked_at,success_at=COALESCE(excluded.success_at,collector_health.success_at),error_code=excluded.error_code,summary=excluded.summary,failures=CASE WHEN excluded.status='Healthy' THEN 0 ELSE collector_health.failures+1 END`, inst.ID, h.Collector, h.Status, h.Count, now, success, h.Code, h.Summary, boolInt(h.Status != "Healthy"))
		if e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE xray_user_states SET online_state='unknown',online_count=NULL,stats_state='unknown',checked_at=? WHERE instance_id=?", now, inst.ID); e != nil {
		return e
	}
	for _, state := range o.states {
		_, e = tx.ExecContext(ctx, "INSERT INTO xray_user_states VALUES(?,?,?,?,?,?,?) ON CONFLICT(instance_id,email) DO UPDATE SET stats_state=excluded.stats_state,online_state=excluded.online_state,online_count=excluded.online_count,online_basis=excluded.online_basis,checked_at=excluded.checked_at", inst.ID, state.Email, state.Stats, state.Online, state.Count, state.Basis, now)
		if e != nil {
			return e
		}
	}
	b, _ := json.Marshal(o.online)
	_, e = tx.ExecContext(ctx, "INSERT INTO online_snapshots VALUES(?,?,?) ON CONFLICT(instance_id) DO UPDATE SET records_json=excluded.records_json,updated_at=excluded.updated_at", inst.ID, string(b), now)
	if e != nil {
		return e
	}
	caps := []core.Capability{{Metric: "user_traffic", Status: "unknown"}, {Metric: "inbound_traffic", Status: "unknown"}, {Metric: "online_ip", Status: "unknown", Reason: "逐用户在线项缺失时 Unknown，不从空列表推断离线"}}
	lastError := ""
	var collected any
	for _, h := range o.report.Health {
		if h.Collector == "stats" && h.Status == "Healthy" {
			collected = now
			caps[0].Status = "supported"
			caps[1].Status = "supported"
		}
		if h.Collector == "online" && h.Status == "Healthy" {
			caps[2].Status = "supported"
		}
		if (h.Status == "Error" || h.Status == "Unreachable" || h.Status == "AuthenticationFailed") || (h.Collector == "stats" && h.Status != "Healthy") {
			lastError += " " + h.Collector + ": " + h.Summary
		}
	}
	cb, _ := json.Marshal(caps)
	_, e = tx.ExecContext(ctx, "UPDATE instances SET last_collected_at=COALESCE(?,last_collected_at),last_error=?,capabilities_json=?,detected_version=? WHERE id=?", collected, lastError, string(cb), o.report.Version, inst.ID)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
