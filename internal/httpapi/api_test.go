package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/collector"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
)

func testAPI(t *testing.T) (*API, http.Handler) {
	s, e := storage.Open(filepath.Join(t.TempDir(), "traffic.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	cfg := config.Config{Interval: 10 * time.Second, Timeout: time.Second, ActiveWindow: 60 * time.Second, Timezone: "Asia/Shanghai", BackupDir: t.TempDir(), AdminUser: "admin", AdminPassword: "fixture-password-1234"}
	auth, e := security.NewAuth(cfg.AdminUser, cfg.AdminPassword, false)
	if e != nil {
		t.Fatal(e)
	}
	a := &API{Store: s, Scheduler: collector.New(context.Background(), s, cfg), Config: cfg, Auth: auth}
	return a, a.Handler()
}
func call(t *testing.T, h http.Handler, method, path string, b any, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if b != nil {
		raw, _ = json.Marshal(b)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if auth {
		r.SetBasicAuth("admin", "fixture-password-1234")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAPIAuthenticationHealthAndCRUD(t *testing.T) {
	_, h := testAPI(t)
	if r := call(t, h, "GET", "/api/v1/health", nil, false); r.Code != 200 || strings.Contains(r.Body.String(), "db") {
		t.Fatal("health failure")
	}
	if r := call(t, h, "GET", "/api/v1/dashboard", nil, false); r.Code != 401 {
		t.Fatal("dashboard exposed")
	}
	if r := call(t, h, "GET", "/", nil, false); r.Code != 303 {
		t.Fatal("UI accessible before login")
	}
	if r := call(t, h, "POST", "/api/v1/servers", map[string]any{"name": "test", "address": "127.0.0.1", "enabled": true}, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "POST", "/api/v1/instances", map[string]any{"name": "test", "server_id": 1, "core_type": "hysteria2", "api_endpoint": "http://127.0.0.1:9000", "api_secret": "never-expose", "enabled": true}, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "GET", "/api/v1/instances", nil, true); r.Code != 200 || strings.Contains(r.Body.String(), "never-expose") {
		t.Fatal("secret leaked")
	}
	for _, path := range []string{"dashboard", "users", "online", "nodes", "subscriptions", "settings", "identities", "traffic/summary?range=month", "traffic/history?range=last_month"} {
		if r := call(t, h, "GET", "/api/v1/"+path, nil, true); r.Code != 200 {
			t.Fatalf("%s %d %s", path, r.Code, r.Body.String())
		}
	}
	if r := call(t, h, "GET", "/api/v1/traffic/summary?dimension=invalid", nil, true); r.Code != 400 {
		t.Fatal("invalid dimension accepted")
	}
	if r := call(t, h, "PATCH", "/api/v1/settings", map[string]any{"collect_interval": "2s", "active_window": "60s", "log_level": "warn", "archive_enabled": true}, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
}
func TestOnlineStaleIsNotOffline(t *testing.T) {
	a, h := testAPI(t)
	now := storage.Stamp(time.Now())
	a.Store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now)
	a.Store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','hysteria2','http://localhost:1',?,?)", now, now)
	e := a.Store.Apply(context.Background(), 1, []core.TrafficRecord{{UserKey: "alice", Scope: "user", CounterMode: "cumulative", CollectedAt: time.Now()}})
	if e != nil {
		t.Fatal(e)
	}
	a.Store.DB.Exec("INSERT INTO online_snapshots VALUES(1,?,?)", `[{"user_key":"alice","count":2,"kind":"device"}]`, storage.Stamp(time.Now().Add(-5*time.Minute)))
	r := call(t, h, "GET", "/api/v1/online", nil, true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"stale"`) || strings.Contains(r.Body.String(), `"status":"offline"`) {
		t.Fatal(r.Body.String())
	}
}
