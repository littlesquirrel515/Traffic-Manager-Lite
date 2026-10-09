package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectionProbeDoesNotCreateTrafficBaseline(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"alice":{"tx":2000,"rx":1000}}`))
	}))
	defer fixture.Close()
	a, h := testAPI(t)
	call(t, h, "POST", "/api/v1/servers", map[string]any{"name": "test", "enabled": true}, true)
	r := call(t, h, "POST", "/api/v1/instances", map[string]any{"name": "hy", "server_id": 1, "core_type": "hysteria2", "api_endpoint": fixture.URL, "enabled": true}, true)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r = call(t, h, "POST", "/api/v1/instances/1/test", nil, true)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	for _, table := range []string{"traffic_cursors", "traffic_samples", "traffic_daily", "identities"} {
		var n int
		if e := a.Store.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil || n != 0 {
			t.Fatalf("probe mutated %s: %d %v", table, n, e)
		}
	}
	if r := call(t, h, "POST", "/api/v1/instances/99/test", nil, true); r.Code != 404 {
		t.Fatal(r.Code)
	}
}
