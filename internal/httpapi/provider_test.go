package httpapi

import (
	"encoding/json"
	"testing"
)

func TestDirectionAuditIsReadOnlyAndShowsRepairMetadata(t *testing.T) {
	a, h := testAPI(t)
	a.Store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'s','','now','now')")
	a.Store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'hy','hysteria2','localhost:1','now','now')")
	a.Store.DB.Exec("INSERT INTO traffic_direction_repairs VALUES(1,'legacy','cutoff',3,'private-backup-path','private evidence not for frontend','now')")
	if w := call(t, h, "GET", "/api/v1/instances/1/direction-audit", nil, false); w.Code != 401 {
		t.Fatal("audit not admin protected", w.Code)
	}
	w := call(t, h, "GET", "/api/v1/instances/1/direction-audit", nil, true)
	var result map[string]json.RawMessage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	var repairs []map[string]any
	json.Unmarshal(result["repairs"], &repairs)
	if len(repairs) != 1 || repairs[0]["affected_records"] != float64(3) || repairs[0]["backup_path"] != nil || repairs[0]["evidence"] != nil {
		t.Fatal("missing or sensitive repair metadata", repairs)
	}
	var n int
	if err := a.Store.DB.QueryRow("SELECT COUNT(*) FROM traffic_direction_repairs").Scan(&n); err != nil || n != 1 {
		t.Fatal("GET mutated audit", n, err)
	}
}
