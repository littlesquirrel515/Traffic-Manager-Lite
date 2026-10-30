package subscriberprovision

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

type handlerFixture struct {
	hp.UnimplementedHandlerServiceServer
	mu         sync.Mutex
	users      map[string]*hp.User
	adds       int
	blocked    bool
	blockedTag string
}

func (f *handlerFixture) GetInboundUsers(_ context.Context, d *hp.GetInboundUserRequest) (*hp.GetInboundUserResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blocked || f.blockedTag == d.Tag {
		return nil, status.Error(codes.Unavailable, "fixture unavailable")
	}
	r := &hp.GetInboundUserResponse{}
	for _, u := range f.users {
		if d.Email == "" || u.Email == d.Email {
			r.Users = append(r.Users, u)
		}
	}
	return r, nil
}
func (f *handlerFixture) AlterInbound(_ context.Context, d *hp.AlterInboundRequest) (*hp.AlterInboundResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := &hp.AddUserOperation{}
	if e := proto.Unmarshal(d.Operation.Value, v); e != nil {
		return nil, e
	}
	f.users[v.User.Email] = v.User
	f.adds++
	return &hp.AlterInboundResponse{}, nil
}
func setup(t *testing.T) (Service, core.Instance, *handlerFixture, string) {
	t.Helper()
	ctx := context.Background()
	s, e := storage.Open(filepath.Join(t.TempDir(), "db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	g := grpc.NewServer()
	f := &handlerFixture{users: map[string]*hp.User{}}
	hp.RegisterHandlerServiceServer(g, f)
	go g.Serve(l)
	t.Cleanup(g.Stop)
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','` + l.Addr().String() + `','now','now')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	id, e := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "vless", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	key := filepath.Join(t.TempDir(), "key")
	if e = os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	return Service{Store: s, Config: config.Config{ManagedKeyFile: key}}, core.Instance{ID: 1, CoreType: "xray", APIEndpoint: l.Addr().String()}, f, id
}
func TestCreateSubscriberAddsClientBindsMeterAndRestoresMissingOnly(t *testing.T) {
	s, i, f, id := setup(t)
	ctx := context.Background()
	d := Request{RequestID: "test-managed-request-001", Mode: "managed", InboundID: id, Confirmed: true, Subscriber: subscription.Subscriber{Name: "Alice", Enabled: true}}
	r, e := s.Create(ctx, d)
	if e != nil || r.Status != "ready" {
		t.Fatalf("create: %s %v", r.Message, e)
	}
	again, e := s.Create(ctx, d)
	if e != nil || again.SubscriberID != r.SubscriberID || f.adds != 1 {
		t.Fatal("idempotent retry duplicated core user")
	}
	var n int
	for _, q := range []string{"SELECT count(*) FROM subscribers WHERE enabled=1", "SELECT count(*) FROM subscriber_credentials", "SELECT count(*) FROM remote_runtime_users WHERE restore_authorized=1"} {
		if e = s.Store.DB.QueryRow(q).Scan(&n); e != nil || n != 1 {
			t.Fatal("missing user/client association")
		}
	}
	// Reopen the real SQLite file: recovery must use persisted Desired after
	// both the manager and the core have lost their in-memory state.
	var seq int
	var databaseName, databasePath string
	if e = s.Store.DB.QueryRow("PRAGMA database_list").Scan(&seq, &databaseName, &databasePath); e != nil {
		t.Fatal(e)
	}
	s.Store.Close()
	reopened, e := storage.Open(databasePath, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	s.Store = reopened
	f.mu.Lock()
	f.users = map[string]*hp.User{}
	f.mu.Unlock()
	remote.RecheckRuntime(ctx, s.Store, i, s.Config.ManagedKeyFile, nil)
	if f.adds != 2 {
		t.Fatal("lost managed client not restored")
	}
	remote.RecheckRuntime(ctx, s.Store, i, s.Config.ManagedKeyFile, nil)
	if f.adds != 2 {
		t.Fatal("existing client added again")
	}
	changed := d
	changed.InboundID = "other"
	if _, e = s.Create(ctx, changed); e == nil {
		t.Fatal("request reused for different target")
	}
}
func TestUnsupportedCoreFailsBeforeCreatingSubscriberAndNativeAdoptionDoesNotAdd(t *testing.T) {
	s, _, f, id := setup(t)
	ctx := context.Background()
	s.Store.DB.Exec("UPDATE instances SET core_type='singbox' WHERE id=1")
	d := Request{RequestID: "test-unsupported-001", Mode: "managed", InboundID: id, Confirmed: true, Subscriber: subscription.Subscriber{Name: "No fake user", Enabled: true}}
	if _, e := s.Create(ctx, d); e == nil {
		t.Fatal("unsupported user claimed ready")
	}
	var n int
	s.Store.DB.QueryRow("SELECT count(*) FROM subscribers").Scan(&n)
	if n != 0 || f.adds != 0 {
		t.Fatal("unsupported request had side effects")
	}
	cred, e := remote.RegisterClient(ctx, s.Store, remote.Client{InboundID: id, Reference: "native", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	d.RequestID = "test-native-adopt-001"
	d.Mode = "native"
	d.CredentialID = cred
	r, e := s.Create(ctx, d)
	if e != nil || r.Status != "ready" || f.adds != 0 {
		t.Fatalf("native adoption: %s %v", r.Message, e)
	}
}

func TestPendingCreationRetriesSameUserAndRefusesChangedTarget(t *testing.T) {
	s, _, f, id := setup(t)
	ctx := context.Background()
	f.blocked = true
	d := Request{RequestID: "test-pending-request-001", Mode: "managed", InboundID: id, Confirmed: true, Subscriber: subscription.Subscriber{Name: "Pending", Enabled: true}}
	r, e := s.Create(ctx, d)
	if e != nil || r.Status != "pending" || f.adds != 0 {
		t.Fatalf("pending creation: %s %v", r.Status, e)
	}
	var enabled bool
	s.Store.DB.QueryRow("SELECT enabled FROM subscribers WHERE id=?", r.SubscriberID).Scan(&enabled)
	if enabled {
		t.Fatal("unverified subscriber was enabled")
	}
	s.Store.DB.Exec("UPDATE catalog_inbounds SET tag='changed' WHERE id=?", id)
	f.mu.Lock()
	f.blocked = false
	f.mu.Unlock()
	stopped, e := s.Retry(ctx, d.RequestID)
	if e != nil || stopped.Status != "pending" || f.adds != 0 {
		t.Fatal("new target used without authorization")
	}
	s.Store.DB.Exec("UPDATE catalog_inbounds SET tag='vless' WHERE id=?", id)
	s.Store.DB.Exec("UPDATE remote_runtime_users SET status='applying',checked_at='2000-01-01T00:00:00.000000000Z'")
	done, e := s.Create(ctx, d)
	if e != nil || done.Status != "ready" || done.SubscriberID != r.SubscriberID || f.adds != 1 {
		t.Fatalf("retry did not finish same user: %s %v", done.Status, e)
	}
	var n int
	s.Store.DB.QueryRow("SELECT count(*) FROM subscribers").Scan(&n)
	if n != 1 {
		t.Fatal("retry duplicated subscriber")
	}
	s.Store.DB.Exec("UPDATE subscribers SET enabled=0 WHERE id=?", r.SubscriberID)
	f.mu.Lock()
	f.users = map[string]*hp.User{}
	f.mu.Unlock()
	var endpoint string
	s.Store.DB.QueryRow("SELECT api_endpoint FROM instances WHERE id=1").Scan(&endpoint)
	remote.RecheckRuntime(ctx, s.Store, core.Instance{ID: 1, CoreType: "xray", APIEndpoint: endpoint}, s.Config.ManagedKeyFile, nil)
	if f.adds != 1 {
		t.Fatal("disabled subscriber automatically restored")
	}
}

func TestAttachSecondInboundPreservesExistingSubscriber(t *testing.T) {
	s, _, f, id := setup(t)
	ctx := context.Background()
	first, e := s.Create(ctx, Request{RequestID: "attach-first-request-001", Mode: "managed", InboundID: id, Confirmed: true, Subscriber: subscription.Subscriber{Name: "Shared subscriber", Enabled: true}})
	if e != nil || first.Status != "ready" {
		t.Fatal(first, e)
	}
	second, e := remote.SaveManual(ctx, s.Store, remote.Inbound{InstanceID: 1, Tag: "second", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws"}})
	if e != nil {
		t.Fatal(e)
	}
	next, e := s.Create(ctx, Request{RequestID: "attach-second-request-001", Mode: "managed", InboundID: second, Confirmed: true, Subscriber: subscription.Subscriber{ID: first.SubscriberID}})
	if e != nil || next.Status != "ready" || next.SubscriberID != first.SubscriberID {
		t.Fatal(next, e)
	}
	var n int
	s.Store.DB.QueryRow("SELECT count(*) FROM subscribers WHERE enabled=1").Scan(&n)
	if n != 1 || f.adds != 2 {
		t.Fatal("attach recreated or disabled subscriber")
	}
	s.Store.DB.QueryRow("SELECT count(*) FROM subscriber_credentials WHERE subscriber_id=?", first.SubscriberID).Scan(&n)
	if n != 2 {
		t.Fatal("missing second inbound authorization")
	}
}
