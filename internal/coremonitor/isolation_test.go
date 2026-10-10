package coremonitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

// Fault injection tests are explicitly fixtures; official process tests are separate.
func TestHTTPFaultIsolationAndReadOnlyRequests(t *testing.T) {
	var failStats, failOnline atomic.Bool
	failStats.Store(true)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.RawQuery != "" || r.URL.Path == "/kick" {
			t.Errorf("mutating diagnostic %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "private-api-secret" {
			t.Error("missing auth")
		}
		switch r.URL.Path {
		case "/traffic":
			if failStats.Load() {
				http.Error(w, "private error secret must not persist", 500)
			} else {
				w.Write([]byte(`{"alice":{"tx":17,"rx":11}}`))
			}
		case "/online":
			if failOnline.Load() {
				http.Error(w, "private error", 503)
			} else {
				w.Write([]byte(`{"alice":2}`))
			}
		case "/dump/streams":
			w.Write([]byte(`{"streams":[]}`))
		default:
			t.Error("unexpected path")
		}
	}))
	defer fixture.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "hy.yaml")
	os.WriteFile(path, []byte("auth:\n  type: userpass\n  userpass:\n    alice: private-password\n    bob: private-password-two\n"), 0600)
	i := core.Instance{ID: 1, ServerID: 1, CoreType: "hysteria2", APIEndpoint: fixture.URL, APISecret: "private-api-secret", ConfigPath: path}
	s := testService(t, dir, i)
	r, e := s.Observe(context.Background(), i, false, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	requireCheck(t, r, "/traffic", "Error")
	requireCheck(t, r, "/online", "Available")
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 2 || count(t, s, "SELECT count(*) FROM core_user_states WHERE online_state='online'") != 1 || count(t, s, "SELECT count(*) FROM traffic_cursors") != 0 {
		t.Fatal("Stats failed but Clients/Online not independent")
	}
	failStats.Store(false)
	failOnline.Store(true)
	r, e = s.Observe(context.Background(), i, false, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	requireCheck(t, r, "/traffic", "Available")
	requireCheck(t, r, "/online", "Error")
	if count(t, s, "SELECT count(*) FROM traffic_cursors") != 1 || count(t, s, "SELECT count(*) FROM core_user_states WHERE online_state='unknown'") != 2 {
		t.Fatal("Online failed but Stats not collected or stale state retained")
	}
	before := count(t, s, "SELECT count(*) FROM users")
	beforeHealth, _ := s.Store.Rows(context.Background(), "SELECT * FROM collector_health")
	r, e = s.Observe(context.Background(), i, true, "manual")
	if e != nil {
		t.Fatal(e)
	}
	afterHealth, _ := s.Store.Rows(context.Background(), "SELECT * FROM collector_health")
	b1, _ := json.Marshal(beforeHealth)
	b2, _ := json.Marshal(afterHealth)
	if string(b1) != string(b2) || count(t, s, "SELECT count(*) FROM users") != before {
		t.Fatal("manual diagnostic mutated collectors/users")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("secret/error body persisted")
	}
	os.WriteFile(path, []byte("invalid: ["), 0600)
	if _, e = s.Observe(context.Background(), i, false, "invalid_file"); e != nil {
		t.Fatal(e)
	}
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 2 {
		t.Fatal("failed file parse removed users")
	}
}
func TestExternalAuthInventoryIsExplicitAndBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hy.yaml")
	os.WriteFile(path, []byte("auth:\n  type: http\n  http:\n    url: https://example.invalid/auth\n"), 0600)
	i := core.Instance{ID: 1, ServerID: 1, CoreType: "hysteria2", APIEndpoint: "http://localhost:1", ConfigPath: path}
	s := testService(t, dir, i)
	o := observation{}
	s.clients(i, &o)
	if o.clientsOK {
		t.Fatal("inferred complete backend users")
	}
	export := map[string]any{"complete": true, "observed_at": time.Now().UTC().Format(time.RFC3339), "users": []map[string]string{{"name": "alice"}, {"name": "bob"}}}
	b, _ := json.Marshal(export)
	os.WriteFile(path+".clients.json", b, 0600)
	o = observation{}
	s.clients(i, &o)
	if !o.clientsOK || len(o.clients) != 2 || o.clients[0].Source != "auth_export" {
		t.Fatal("valid explicit export ignored")
	}
	export["observed_at"] = time.Now().Add(-25 * time.Hour).Format(time.RFC3339)
	b, _ = json.Marshal(export)
	os.WriteFile(path+".clients.json", b, 0600)
	o = observation{}
	s.clients(i, &o)
	if o.clientsOK {
		t.Fatal("stale export accepted")
	}
}
