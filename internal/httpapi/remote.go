package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
	"traffic-manager-lite/internal/coremonitor"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/xraymonitor"
)

func (a *API) remoteRoutes(admin *http.ServeMux) {
	admin.HandleFunc("GET /api/v1/catalog/inbounds/{id}/runtime-users", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT client_ref,desired_enabled,restore_authorized,revision,status,persistence,checked_at FROM remote_runtime_users WHERE inbound_id=? ORDER BY client_ref`, r.PathValue("id"))
	})
	admin.HandleFunc("POST /api/v1/catalog/inbounds/{id}/runtime-users", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 409, map[string]string{"error": "请从订阅用户创建流程管理核心 Client；入站页面不允许独立增删运行时用户"})
	})
	admin.HandleFunc("GET /api/v1/instances/{id}/capabilities", func(w http.ResponseWriter, r *http.Request) {
		id, e := pathID(r)
		if e != nil {
			result(w, nil, e)
			return
		}
		i, e := (coremonitor.Service{Store: a.Store, Config: a.Config}).Instance(r.Context(), id)
		if e != nil {
			result(w, nil, e)
			return
		}
		var raw string
		var report xraymonitor.Report
		_ = a.Store.DB.QueryRowContext(r.Context(), `SELECT report_json FROM (SELECT checked_at,report_json FROM xray_diagnostics WHERE instance_id=? UNION ALL SELECT checked_at,report_json FROM core_diagnostics WHERE instance_id=?) ORDER BY checked_at DESC LIMIT 1`, id, id).Scan(&raw)
		_ = json.Unmarshal([]byte(raw), &report)
		matrix := remote.Capabilities(i.CoreType, report)
		var inventoryState, checked string
		if i.CoreType == "xray" && a.Store.DB.QueryRowContext(r.Context(), "SELECT status,checked_at FROM remote_inventory_runs WHERE instance_id=? ORDER BY id DESC LIMIT 1", id).Scan(&inventoryState, &checked) == nil {
			state := "unknown"
			if inventoryState == "supported" {
				state = "supported"
			}
			if inventoryState == "unavailable" {
				state = "unavailable"
			}
			if at, e := time.Parse(time.RFC3339Nano, checked); e != nil || time.Since(at) > 5*time.Minute {
				state = "unknown"
			}
			for j := range matrix {
				if matrix[j].Name == "read_inbound" || matrix[j].Name == "list_inbounds" {
					matrix[j].Status = state
					matrix[j].CheckedAt = checked
					matrix[j].Evidence = "ListInbounds(isOnlyTags=false) + 官方 TypedMessage 白名单解码；空列表是有效结果，失败保留资产"
				}
			}
		}
		respond(w, 200, matrix)
	})
	admin.HandleFunc("POST /api/v1/catalog/inbounds", func(w http.ResponseWriter, r *http.Request) {
		var d remote.Inbound
		if !body(w, r, &d) {
			return
		}
		id, e := remote.SaveManual(r.Context(), a.Store, d)
		result(w, map[string]string{"id": id, "verification": "manual_unverified"}, e)
	})
	admin.HandleFunc("PUT /api/v1/catalog/inbounds/{id}/registry", func(w http.ResponseWriter, r *http.Request) {
		var d remote.Inbound
		if !body(w, r, &d) {
			return
		}
		d.ID = r.PathValue("id")
		id, e := remote.SaveManual(r.Context(), a.Store, d)
		result(w, map[string]string{"id": id}, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/inbounds/{id}/clients", func(w http.ResponseWriter, r *http.Request) {
		var d remote.Client
		if !body(w, r, &d) {
			return
		}
		d.InboundID = r.PathValue("id")
		id, e := remote.RegisterClient(r.Context(), a.Store, d)
		result(w, map[string]string{"id": id, "verification": "manual_unverified", "core_modified": "false"}, e)
	})
	admin.HandleFunc("POST /api/v1/instances/{id}/inventory", func(w http.ResponseWriter, r *http.Request) {
		id, e := pathID(r)
		if e != nil {
			result(w, nil, e)
			return
		}
		i, e := (coremonitor.Service{Store: a.Store, Config: a.Config}).Instance(r.Context(), id)
		if e != nil {
			result(w, nil, e)
			return
		}
		v, e := remote.SyncXray(r.Context(), a.Store, i, a.Config.AllowedTargets)
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/inbounds/{id}/registry", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT inbound_id,source_type,verification,revision,checked_at FROM remote_inbound_registry WHERE inbound_id=?`, r.PathValue("id"))
	})
	admin.HandleFunc("GET /api/v1/system/logs", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT instance_id,status,summary,checked_at FROM remote_inventory_runs ORDER BY id DESC LIMIT 200`)
	})
}
