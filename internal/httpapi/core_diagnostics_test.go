package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
	"traffic-manager-lite/internal/storage"
)

func TestUnifiedFourCoreDiagnosticsAuthFilteringAndHistory(t *testing.T) {
	a, h := testAPI(t)
	a.Scheduler.Config.Timeout = 50 * time.Millisecond
	a.Config.Timeout = 50 * time.Millisecond
	now := storage.Stamp(time.Now())
	a.Store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now)
	for n, kind := range []string{"xray", "hysteria2", "singbox", "v2fly"} {
		endpoint := "localhost:1"
		if kind == "hysteria2" {
			endpoint = "http://localhost:1"
		}
		id := n + 1
		if _, e := a.Store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(?,1,?,?,?, ?,?)", id, kind, kind, endpoint, now, now); e != nil {
			t.Fatal(e)
		}
		if r := call(t, h, "POST", fmt.Sprintf("/api/v1/instances/%d/diagnostics", id), nil, false); r.Code != 401 {
			t.Fatal("exposed diagnostic")
		}
		r := call(t, h, "POST", fmt.Sprintf("/api/v1/instances/%d/diagnostics", id), nil, true)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		for _, suffix := range []string{"diagnostics", "diagnostics/history", "health", "clients"} {
			r = call(t, h, "GET", fmt.Sprintf("/api/v1/instances/%d/%s", id, suffix), nil, true)
			if r.Code != 200 {
				t.Fatal(suffix, r.Body.String())
			}
		}
	}
	r := call(t, h, "GET", "/api/v1/cores/diagnostics", nil, true)
	var rows []map[string]any
	if json.Unmarshal(r.Body.Bytes(), &rows) != nil || len(rows) != 4 {
		t.Fatal("missing four-core summary", r.Body.String())
	}
	for _, kind := range []string{"xray", "hysteria2", "singbox", "v2fly"} {
		r = call(t, h, "GET", "/api/v1/cores/diagnostics?core_type="+kind, nil, true)
		json.Unmarshal(r.Body.Bytes(), &rows)
		if len(rows) != 1 || rows[0]["core_type"] != kind {
			t.Fatal("wrong filter")
		}
	}
	if r = call(t, h, "GET", "/api/v1/cores/diagnostics", nil, false); r.Code != 401 {
		t.Fatal("summary exposed")
	}
	var users, cursors int
	a.Store.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM users").Scan(&users)
	a.Store.DB.QueryRow("SELECT count(*) FROM traffic_cursors").Scan(&cursors)
	if users != 0 || cursors != 0 {
		t.Fatal("diagnosis fabricated users or baseline")
	}
}
