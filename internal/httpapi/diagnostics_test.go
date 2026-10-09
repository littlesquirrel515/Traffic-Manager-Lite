package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/storage"
)

func TestDiagnosticRoutesAreProtectedAndKeepAssetsOnFailure(t *testing.T) {
	a, h := testAPI(t)
	a.Scheduler.Config.Timeout = 100 * time.Millisecond
	now := storage.Stamp(time.Now())
	a.Store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now)
	a.Store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','xray','localhost:1',?,?)", now, now)
	a.Store.DB.Exec("INSERT INTO xray_clients(instance_id,inbound_tag,asset_key,email,protocol,level,source,present,observed_at) VALUES(1,'in','email:alice','alice','vless',0,'runtime_api',1,?)", now)
	for _, route := range []string{"xray/diagnostics", "instances/1/diagnostics", "instances/1/diagnostics/history", "instances/1/health", "instances/1/clients"} {
		if r := call(t, h, "GET", "/api/v1/"+route, nil, false); r.Code != 401 {
			t.Fatal("diagnostic exposed", route, r.Code)
		}
	}
	r := call(t, h, "POST", "/api/v1/instances/1/diagnostics", nil, true)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var report map[string]any
	if json.Unmarshal(r.Body.Bytes(), &report) != nil || report["version"] != "Unknown" {
		t.Fatal("invented version")
	}
	for _, route := range []string{"xray/diagnostics", "instances/1/diagnostics", "instances/1/diagnostics/history", "instances/1/health", "instances/1/clients"} {
		if r := call(t, h, "GET", "/api/v1/"+route, nil, true); r.Code != 200 {
			t.Fatal(route, r.Body.String())
		}
	}
	if r := call(t, h, "GET", "/api/v1/instances/99/diagnostics", nil, true); r.Code != 404 {
		t.Fatal(r.Code)
	}
	var n int
	a.Store.DB.QueryRow("SELECT count(*) FROM xray_clients WHERE present=1").Scan(&n)
	if n != 1 {
		t.Fatal("diagnostic removed users")
	}
	a.Store.DB.QueryRow("SELECT count(*) FROM traffic_cursors").Scan(&n)
	if n != 0 {
		t.Fatal("diagnostic created cursor")
	}
	if e := a.Scheduler.Collect(context.Background(), 1); e == nil || !strings.Contains(e.Error(), "clients") {
		t.Fatal("disconnected API should report collector failure", e)
	}
	a.Store.DB.QueryRow("SELECT count(*) FROM xray_clients WHERE present=1").Scan(&n)
	if n != 1 {
		t.Fatal("collection failure removed users")
	}
}
