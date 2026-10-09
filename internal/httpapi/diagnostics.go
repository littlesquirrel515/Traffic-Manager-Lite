package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"traffic-manager-lite/internal/xraymonitor"
)

func (a *API) diagnosticSummary(w http.ResponseWriter, r *http.Request) {
	// Summary has no API Secret, declared version or credentials.
	rows, e := a.Store.Rows(r.Context(), `SELECT i.id instance_id,i.server_id,i.name instance_name,s.name server_name,i.api_endpoint,i.last_collected_at,i.enabled FROM instances i JOIN servers s ON s.id=i.server_id WHERE i.core_type='xray' ORDER BY i.id`)
	if e != nil {
		result(w, nil, e)
		return
	}
	out := []map[string]any{}
	for _, row := range rows {
		if server := r.URL.Query().Get("server_id"); server != "" && server != jsonID(row["server_id"]) {
			continue
		}
		if instance := r.URL.Query().Get("instance_id"); instance != "" && instance != jsonID(row["instance_id"]) {
			continue
		}
		var raw string
		er := a.Store.DB.QueryRowContext(r.Context(), "SELECT report_json FROM xray_diagnostics WHERE instance_id=? ORDER BY id DESC LIMIT 1", row["instance_id"]).Scan(&raw)
		if er == nil {
			var report xraymonitor.Report
			if json.Unmarshal([]byte(raw), &report) == nil {
				row["report"] = report
			}
		}
		if er != nil && er != sql.ErrNoRows {
			result(w, nil, er)
			return
		}
		out = append(out, row)
	}
	result(w, out, nil)
}
func jsonID(v any) string { b, _ := json.Marshal(v); return string(b) }
func (a *API) diagnosticDetails(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	var raw string
	e = a.Store.DB.QueryRowContext(r.Context(), "SELECT report_json FROM xray_diagnostics WHERE instance_id=? ORDER BY id DESC LIMIT 1", id).Scan(&raw)
	var report xraymonitor.Report
	if e == nil {
		e = json.Unmarshal([]byte(raw), &report)
	}
	result(w, report, e)
}
func (a *API) diagnose(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	report, e := a.Scheduler.Diagnose(r.Context(), id, "manual")
	result(w, report, e)
}
func (a *API) diagnosticHistory(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	a.rows(w, r, "SELECT id,checked_at,trigger,report_json FROM xray_diagnostics WHERE instance_id=? ORDER BY id DESC LIMIT 50", id)
}
func (a *API) collectorHealth(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	a.rows(w, r, "SELECT * FROM collector_health WHERE instance_id=? ORDER BY collector", id)
}
func (a *API) clients(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	a.rows(w, r, `SELECT c.instance_id,c.inbound_tag,c.email,c.protocol,c.level,c.source,c.present,c.observed_at,st.stats_state,st.online_state,st.online_basis,st.online_count,st.checked_at state_checked_at,
 CASE
 WHEN c.email='' THEN 'anonymous_unlinked'
 WHEN NOT EXISTS(SELECT 1 FROM xray_clients b WHERE b.instance_id=c.instance_id AND b.inbound_tag=c.inbound_tag AND b.email=c.email AND b.present=1 AND b.source<>c.source) THEN 'source_only'
 WHEN EXISTS(SELECT 1 FROM xray_clients b WHERE b.instance_id=c.instance_id AND b.inbound_tag=c.inbound_tag AND b.email=c.email AND b.present=1 AND b.source<>c.source AND (b.level<>c.level OR b.protocol<>c.protocol)) THEN 'metadata_mismatch'
 WHEN c.credential_fingerprint='' OR EXISTS(SELECT 1 FROM xray_clients b WHERE b.instance_id=c.instance_id AND b.inbound_tag=c.inbound_tag AND b.email=c.email AND b.present=1 AND b.source<>c.source AND b.credential_fingerprint='') THEN 'credential_comparison_unknown'
 WHEN EXISTS(SELECT 1 FROM xray_clients b WHERE b.instance_id=c.instance_id AND b.inbound_tag=c.inbound_tag AND b.email=c.email AND b.present=1 AND b.source<>c.source AND b.credential_fingerprint=c.credential_fingerprint) THEN 'matching_verified_fields'
 ELSE 'credential_mismatch' END comparison
 FROM xray_clients c LEFT JOIN xray_user_states st ON st.instance_id=c.instance_id AND st.email=c.email WHERE c.instance_id=? ORDER BY c.source,c.inbound_tag,c.email`, id)
}
