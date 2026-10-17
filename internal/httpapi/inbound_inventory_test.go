package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestInboundManagementReadsStoredInventoryWithoutProbingCore(t *testing.T) {
	a, h := testAPI(t)
	var calls atomic.Int32
	coreAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "unavailable", 503) }))
	defer coreAPI.Close()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','display','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,enabled,created_at,updated_at) VALUES(1,1,'fixture','hysteria2','` + coreAPI.URL + `',0,'now','now')`} {
		if _, e := a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range []string{"active", "missing", "review", "retired"} {
		_, e := a.Store.DB.Exec(`INSERT INTO catalog_inbounds(id,instance_id,source_ref,tag,protocol,listen_port,profile_json,structure_hash,config_hash,present,review,retired,discovered_at) VALUES(?,1,'manual',?,'hysteria2',443,'{"protocol":"hysteria2"}','fixture','fixture',?,?,?,'now')`, id, id, id != "missing", id == "review", id == "retired")
		if e != nil {
			t.Fatal(e)
		}
	}
	for n := 0; n < 3; n++ {
		r := call(t, h, "GET", "/api/v1/catalog/inbound-management", nil, true)
		var rows []map[string]any
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &rows) != nil || len(rows) != 4 {
			t.Fatal("persisted unavailable inventory disappeared")
		}
		for _, row := range rows {
			if row["id"] == nil || row["present"] == nil || row["review"] == nil || row["retired"] == nil {
				t.Fatal("missing stable identity or status")
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("inventory page probed remote core")
	}
	for _, id := range []string{"missing", "review", "retired"} {
		r := call(t, h, "POST", "/api/v1/catalog/connections", map[string]any{"inbound_id": id, "name": "blocked", "enabled": true, "profile": map[string]any{"address": "edge.example", "port": 443}}, true)
		if r.Code == 200 {
			t.Fatal("unavailable inventory published")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("connection creation probed remote core")
	}
}
