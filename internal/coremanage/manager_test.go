package coremanage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Manager, Target) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	b := []byte(`{"inbounds":[{"tag":"vless","type":"vless","listen_port":1443,"users":[{"name":"same","uuid":"00000000-0000-4000-8000-000000000001"}]},{"tag":"hy","type":"hysteria2","listen_port":2443,"users":[{"name":"same","password":"hy-secret"}]},{"tag":"tls","type":"anytls","listen_port":3443,"users":[{"name":"same","password":"tls-secret"}]}],"outbounds":[{"type":"direct"}]}`)
	os.WriteFile(p, b, 0600)
	target := Target{ID: 1, Container: "fixture", Path: p, ContainerPath: "/config/config.json"}
	m := &Manager{StateDir: filepath.Join(dir, "private"), Targets: map[int64]Target{1: target}, Check: func(context.Context, Target, string) (string, error) { return "1.14.3", nil }, Restart: func(context.Context, Target) error { return nil }}
	return m, target
}
func TestCRUDRevisionConfirmationAndRecovery(t *testing.T) {
	ctx := context.Background()
	m, target := fixture(t)
	initial, _ := os.ReadFile(target.Path)
	state, e := m.Read(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(state)
	if strings.Contains(string(b), "hy-secret") || strings.Contains(string(b), "00000000-0000") {
		t.Fatal("credentials returned")
	}
	req := Request{Operation: "add", Revision: state.Revision, Inbound: "tls", User: User{Name: "new-user", Password: "new-private-password"}}
	changed, e := m.Update(ctx, 1, req)
	if e != nil || changed.Status != "PendingRestart" {
		t.Fatal(changed, e)
	}
	if _, e = m.Update(ctx, 1, req); !errors.Is(e, ErrConflict) {
		t.Fatal("lost update allowed", e)
	}
	if _, e = m.Apply(ctx, 1, Request{Operation: "apply", Revision: changed.Revision}); e == nil {
		t.Fatal("unconfirmed restart allowed")
	}
	applied, e := m.Apply(ctx, 1, Request{Operation: "apply", Revision: changed.Revision, ConfirmRestart: true})
	if e != nil || applied.Status != "Applied" {
		t.Fatal(e)
	}
	updated, e := m.Update(ctx, 1, Request{Operation: "update", Revision: applied.Revision, Inbound: "tls", PreviousName: "new-user", User: User{Name: "renamed"}})
	if e != nil {
		t.Fatal(e)
	}
	current, _ := os.ReadFile(target.Path)
	if !strings.Contains(string(current), "new-private-password") {
		t.Fatal("credential was not preserved")
	}
	m.Apply(ctx, 1, Request{Operation: "apply", Revision: updated.Revision, ConfirmRestart: true})
	req = Request{Operation: "delete", Revision: updated.Revision, Inbound: "hy", PreviousName: "same"}
	if _, e = m.Update(ctx, 1, req); e == nil {
		t.Fatal("unconfirmed deletion allowed")
	}
	req.ConfirmDelete = true
	deleted, e := m.Update(ctx, 1, req)
	if e != nil {
		t.Fatal(e)
	}
	_, in, _ := parse(initial)
	_, other, _ := parse(current)
	if in[0].(map[string]any)["listen_port"] != other[0].(map[string]any)["listen_port"] {
		t.Fatal("listener changed")
	}
	calls := 0
	m.Restart = func(context.Context, Target) error {
		calls++
		if calls == 1 {
			return errors.New("failed health")
		}
		return nil
	}
	restored, e := m.Apply(ctx, 1, Request{Operation: "apply", Revision: deleted.Revision, ConfirmRestart: true})
	if e != nil || restored.Status != "RolledBack" || calls != 2 {
		t.Fatal("failed apply recovery", restored, e, calls)
	}
	again, e := m.Read(ctx, 1)
	if e != nil || again.Revision != updated.Revision {
		t.Fatal("wrong backup restored")
	}
	data, _ := os.ReadFile(filepath.Join(m.StateDir, "audit.jsonl"))
	if strings.Contains(string(data), "private-password") {
		t.Fatal("audit leaked credential")
	}
	m.Check = func(context.Context, Target, string) (string, error) { return "", errors.New("invalid") }
	if _, e = m.Update(ctx, 1, Request{Operation: "add", Revision: again.Revision, Inbound: "hy", User: User{Name: "fail", Password: "secret"}}); e == nil {
		t.Fatal("unchecked configuration saved")
	}
	original, _ := os.ReadFile(target.Path)
	if Hash(original) != again.Revision {
		t.Fatal("invalid check replaced original")
	}
}
func TestAPIRequiresPrivateToken(t *testing.T) {
	m, _ := fixture(t)
	handler := m.Handler("01234567890123456789012345678901")
	r := httptest.NewRequest("GET", "/instances/1/config", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("host agent unauthenticated access")
	}
	r = httptest.NewRequest("GET", "/instances/1/config", nil)
	r.Header.Set("Authorization", "Bearer 01234567890123456789012345678901")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if validUUID("$(anything)") {
		t.Fatal("invalid UUID accepted")
	}
}
