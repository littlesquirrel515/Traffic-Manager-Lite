package hysteriaauth_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/hysteriaauth"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscriberprovision"
	"traffic-manager-lite/internal/subscription"
)

func TestDatabaseAuthenticationLifecycleAndIndependentAssets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "db")
	s, e := storage.Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.Close() }()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','hysteria2','http://127.0.0.1:1','now','now')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	id, e := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "hy", Profile: core.NodeProfile{Protocol: "hysteria2", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = remote.RegisterClient(ctx, s, remote.Client{InboundID: id, Reference: "native", Password: "native:fixture-password", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	key := filepath.Join(t.TempDir(), "key")
	if e = os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	service := subscriberprovision.Service{Store: s, Config: config.Config{ManagedKeyFile: key}}
	request := subscriberprovision.Request{RequestID: "hy-managed-fixture-001", InboundID: id, Mode: "managed", Confirmed: true, Subscriber: subscription.Subscriber{Name: "Managed HY", Enabled: true}}
	if _, e = service.Create(ctx, request); e == nil {
		t.Fatal("static instance pretended to support dynamic auth")
	}
	prep, e := hysteriaauth.Prepare(ctx, s, id, hysteriaauth.Setup{Confirmed: true, ImportNative: true, NativeUserpass: true, AllowedCIDRs: []string{"127.0.0.0/8"}})
	if e != nil || prep.Imported != 1 {
		t.Fatal("native import failed", e)
	}
	token := strings.TrimPrefix(prep.CallbackPath, "/core-auth/hysteria2/")
	check := func(token, peer, password, want string) {
		t.Helper()
		got, err := hysteriaauth.Authenticate(ctx, s, token, peer, password)
		if err != nil || got != want {
			t.Fatalf("auth result mismatch: wanted accepted=%v got accepted=%v err=%v", want != "", got != "", err)
		}
	}
	check(token, "127.0.0.1", "native:fixture-password", "")
	if _, e = service.Create(ctx, request); e == nil {
		t.Fatal("unconfirmed backend accepted new user")
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM subscribers").Scan(&count)
	if count != 0 {
		t.Fatal("rejected create left phantom subscriber")
	}
	if e = hysteriaauth.SetState(ctx, s, id, prep.Revision, true, true); e != nil {
		t.Fatal(e)
	}
	check(token, "127.0.0.1", "native:fixture-password", "native")
	check(token, "127.0.0.1", "NATIVE:fixture-password", "native")
	check(token, "127.0.0.1", "NATIVE:FIXTURE-password", "")
	check(token, "203.0.113.5", "native:fixture-password", "")
	check(token, "127.0.0.1", "wrong", "")
	r, e := service.Create(ctx, request)
	if e != nil || r.Status != "ready" {
		t.Fatal("HTTP account create failed", r, e)
	}
	again, e := service.Create(ctx, request)
	if e != nil || again.SubscriberID != r.SubscriberID {
		t.Fatal("idempotency failed")
	}
	var raw, ref string
	s.DB.QueryRow(`SELECT c.auth_json,c.core_client_ref FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id WHERE b.subscriber_id=?`, r.SubscriberID).Scan(&raw, &ref)
	var auth map[string]string
	json.Unmarshal([]byte(raw), &auth)
	if auth["password"] == "" {
		t.Fatal("missing client payload")
	}
	check(token, "127.0.0.1", auth["password"], ref)
	// Stopping collection must not revoke authentication or delete Clients.
	s.DB.Exec("UPDATE instances SET enabled=0 WHERE id=1")
	check(token, "127.0.0.1", auth["password"], ref)
	s.DB.QueryRow("SELECT count(*) FROM core_clients WHERE source='hysteria_http_auth'").Scan(&count)
	if count != 1 {
		t.Fatal("managed asset missing")
	}
	for _, table := range []string{"remote_runtime_users", "traffic_cursors", "traffic_samples"} {
		s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
		if count != 0 {
			t.Fatal("auth touched core RPCs or counter baselines", table)
		}
	}
	s.DB.Exec("UPDATE subscribers SET enabled=0 WHERE id=?", r.SubscriberID)
	check(token, "127.0.0.1", auth["password"], "")
	s.DB.Exec("UPDATE subscribers SET enabled=1,quota_enabled=1,traffic_limit_bytes=0 WHERE id=?", r.SubscriberID)
	check(token, "127.0.0.1", auth["password"], "")
	s.DB.Exec("UPDATE subscribers SET quota_enabled=0,expiry_enabled=1,expires_at='2020-01-01T00:00:00Z' WHERE id=?", r.SubscriberID)
	check(token, "127.0.0.1", auth["password"], "")
	s.DB.Exec("UPDATE subscribers SET expiry_enabled=0 WHERE id=?", r.SubscriberID)
	s.Close()
	s, e = storage.Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	check(token, "127.0.0.1", auth["password"], ref)
	rotated, e := hysteriaauth.Prepare(ctx, s, id, hysteriaauth.Setup{Confirmed: true, Rotate: true, ImportNative: true, AllowedCIDRs: []string{"127.0.0.0/8"}})
	if e != nil {
		t.Fatal(e)
	}
	check(token, "127.0.0.1", auth["password"], "")
	newToken := strings.TrimPrefix(rotated.CallbackPath, "/core-auth/hysteria2/")
	check(newToken, "127.0.0.1", auth["password"], "")
	if e = hysteriaauth.SetState(ctx, s, id, rotated.Revision, true, true); e != nil {
		t.Fatal(e)
	}
	check(newToken, "127.0.0.1", auth["password"], ref)
	s.DB.Exec("UPDATE instances SET api_endpoint='http://127.0.0.1:2' WHERE id=1")
	check(newToken, "127.0.0.1", auth["password"], "")
}
