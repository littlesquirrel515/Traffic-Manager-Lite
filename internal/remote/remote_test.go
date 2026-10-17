package remote

import (
	"context"
	"encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

const fixtureUUID = "11111111-1111-4111-8111-111111111111"

type fixtureAPI struct {
	hp.UnimplementedHandlerServiceServer
	mu        sync.Mutex
	inbounds  []*hp.Inbound
	users     map[string]*hp.User
	mutations int
}

func (f *fixtureAPI) ListInbounds(ctx context.Context, r *hp.ListInboundsRequest) (*hp.ListInboundsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &hp.ListInboundsResponse{Inbounds: f.inbounds}, nil
}
func (f *fixtureAPI) GetInboundUsers(ctx context.Context, r *hp.GetInboundUserRequest) (*hp.GetInboundUserResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &hp.GetInboundUserResponse{}
	for _, u := range f.users {
		if r.Email == "" || u.Email == r.Email {
			out.Users = append(out.Users, u)
		}
	}
	return out, nil
}
func (f *fixtureAPI) AlterInbound(ctx context.Context, r *hp.AlterInboundRequest) (*hp.AlterInboundResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutations++
	if strings.HasSuffix(r.Operation.Type, ".AddUserOperation") {
		v := &hp.AddUserOperation{}
		if e := proto.Unmarshal(r.Operation.Value, v); e != nil {
			return nil, e
		}
		f.users[v.User.Email] = v.User
	} else {
		v := &hp.RemoveUserOperation{}
		if e := proto.Unmarshal(r.Operation.Value, v); e != nil {
			return nil, e
		}
		delete(f.users, v.Email)
	}
	return &hp.AlterInboundResponse{}, nil
}

func wire(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, e := proto.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func fixtureInbound(t *testing.T) *hp.Inbound {
	return &hp.Inbound{Tag: "remote-ws", ProxySettings: &hp.TypedAccount{Type: "xray.proxy.vless.inbound.Config"}, ReceiverSettings: &hp.TypedAccount{Type: "xray.app.proxyman.ReceiverConfig", Value: wire(t, &hp.ReceiverConfig{PortList: &hp.PortList{Range: []*hp.PortRange{{From: 10001, To: 10001}}}, StreamSettings: &hp.StreamConfig{ProtocolName: "websocket", TransportSettings: []*hp.TransportConfig{{ProtocolName: "websocket", Settings: &hp.TypedAccount{Type: "xray.transport.internet.websocket.Config", Value: wire(t, &hp.WSConfig{Path: "/real-path", Host: "origin.example"})}}}}})}}
}
func fixture(t *testing.T) (*storage.Store, core.Instance, *fixtureAPI) {
	t.Helper()
	s, e := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	f := &fixtureAPI{inbounds: []*hp.Inbound{fixtureInbound(t)}, users: map[string]*hp.User{}}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	g := grpc.NewServer()
	hp.RegisterHandlerServiceServer(g, f)
	go g.Serve(l)
	t.Cleanup(g.Stop)
	now := storage.Stamp(time.Now())
	if _, e = s.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','test',?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	i := core.Instance{ID: 1, ServerID: 1, CoreType: "xray", APIEndpoint: l.Addr().String(), Enabled: true, ServerEnabled: true}
	if _, e = s.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray',?,?,?)", i.APIEndpoint, now, now); e != nil {
		t.Fatal(e)
	}
	return s, i, f
}

func TestAPIOnlyInventoryRetainsAssetsOnParsingFailureAndEmptyStats(t *testing.T) {
	s, i, f := fixture(t)
	ctx := context.Background()
	account := &hp.TypedAccount{Type: "xray.proxy.vless.Account", Value: wire(t, &hp.UUIDAccount{Id: fixtureUUID})}
	f.users["offline"] = &hp.User{Email: "offline", Account: account}
	v, e := SyncXray(ctx, s, i, nil)
	if e != nil || v.Count != 1 || v.Status != "supported" {
		t.Fatal(v, e)
	}
	var id, raw string
	if e = s.DB.QueryRow("SELECT id,profile_json FROM catalog_inbounds").Scan(&id, &raw); e != nil {
		t.Fatal(e)
	}
	var p core.NodeProfile
	json.Unmarshal([]byte(raw), &p)
	if p.ListenPort != 10001 || p.Path != "/real-path" || p.UUID != "" || strings.Contains(raw, fixtureUUID) {
		t.Fatal("unsafe/incomplete config projection", raw)
	}
	var clients, cursors int
	s.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&clients)
	s.DB.QueryRow("SELECT count(*) FROM traffic_cursors").Scan(&cursors)
	if clients != 1 || cursors != 0 {
		t.Fatal("inventory depends on Stats or creates traffic baselines", clients, cursors)
	}
	f.mu.Lock()
	f.inbounds[0].ReceiverSettings.Value = []byte{0xff}
	f.mu.Unlock()
	v, e = SyncXray(ctx, s, i, nil)
	if e != nil || v.Status != "unknown" {
		t.Fatal(v, e)
	}
	var present bool
	var same string
	s.DB.QueryRow("SELECT id,present FROM catalog_inbounds").Scan(&same, &present)
	if same != id || !present {
		t.Fatal("parsing failure deleted asset")
	}
	f.mu.Lock()
	mutations := f.mutations
	f.mu.Unlock()
	if mutations != 0 {
		t.Fatal("read-only inventory mutated core")
	}
}

func TestManualRegistryEntryIdentityAndMeterOwnership(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	id, e := SaveManual(ctx, s, Inbound{InstanceID: 1, Tag: "manual", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/ws"}})
	if e != nil {
		t.Fatal(e)
	}
	c, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: id, Name: "edge", Enabled: true, RequestID: "request-0123456789", Profile: map[string]any{"address": "edge.example", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	c2, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: id, Name: "edge", Enabled: true, RequestID: "request-0123456789", Profile: map[string]any{"address": "edge.example", "port": 443}})
	if e != nil || c.NodeID != c2.NodeID {
		t.Fatal("non-idempotent entry", c2, e)
	}
	credential, e := RegisterClient(ctx, s, Client{InboundID: id, Reference: "offline", UUID: fixtureUUID, Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	a, e := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "A", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	b, e := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "B", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = subscription.Bind(ctx, s, subscription.Binding{SubscriberID: a, InboundID: &id, CredentialID: credential, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = subscription.Bind(ctx, s, subscription.Binding{SubscriberID: b, InboundID: &id, CredentialID: credential, Enabled: true}); e == nil {
		t.Fatal("same meter assigned to two subscribers")
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM subscriber_credentials WHERE subscriber_id=?", b).Scan(&count)
	if count != 0 {
		t.Fatal("failed ownership change partially committed")
	}
	if _, e = SaveManual(ctx, s, Inbound{ID: id, InstanceID: 1, Tag: "manual-renamed", Revision: 1, Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/changed"}}); e != nil {
		t.Fatal(e)
	}
	n, e := catalog.ResolveDefinition(ctx, s.DB, c.NodeID)
	if e != nil || n.Profile.Address != "edge.example" || n.Profile.Port != 443 {
		t.Fatal("manual metadata replaced endpoint or identity", n, e)
	}
}

func TestExplicitRuntimeMutationsSealedDesiredAndReadback(t *testing.T) {
	s, i, f := fixture(t)
	ctx := context.Background()
	if _, e := SyncXray(ctx, s, i, nil); e != nil {
		t.Fatal(e)
	}
	var id string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds").Scan(&id)
	keyPath := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(keyPath, []byte(strings.Repeat("k", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	d := Mutation{Client: Client{InboundID: id, Reference: "created", UUID: fixtureUUID}, Action: "add"}
	if _, e := Mutate(ctx, s, keyPath, nil, d); e == nil {
		t.Fatal("unconfirmed mutation accepted")
	}
	d.Confirmed = true
	r, e := Mutate(ctx, s, keyPath, nil, d)
	if e != nil || r.Status != "runtime_verified" || r.Persistence != "runtime_only" {
		t.Fatal(r, e)
	}
	d.Revision = r.Revision
	r, e = Mutate(ctx, s, keyPath, nil, d)
	if e != nil || r.Status != "runtime_verified" {
		t.Fatal(r, e)
	}
	f.mu.Lock()
	mutations := f.mutations
	f.mu.Unlock()
	if mutations != 1 {
		t.Fatal("idempotent add duplicated core operation", mutations)
	}
	var sealed string
	s.DB.QueryRow("SELECT desired_sealed FROM remote_runtime_users").Scan(&sealed)
	if strings.Contains(sealed, fixtureUUID) || !strings.HasPrefix(sealed, "tmlenc1:") {
		t.Fatal("Desired not sealed")
	}
	d.Revision = r.Revision
	d.Action = "remove"
	r, e = Mutate(ctx, s, keyPath, nil, d)
	if e != nil || r.Status != "runtime_verified" {
		t.Fatal(r, e)
	}
	f.mu.Lock()
	remaining := len(f.users)
	f.mu.Unlock()
	if remaining != 0 {
		t.Fatal("removal not verified")
	}
}

func TestReconnectOnlyRestoresExplicitlyAuthorizedMissingUser(t *testing.T) {
	s, i, f := fixture(t)
	ctx := context.Background()
	if _, e := SyncXray(ctx, s, i, nil); e != nil {
		t.Fatal(e)
	}
	var id string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds").Scan(&id)
	key := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(key, []byte(strings.Repeat("z", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	d := Mutation{Client: Client{InboundID: id, Reference: "restore", UUID: fixtureUUID, Confirmed: true}, Action: "add"}
	r, e := Mutate(ctx, s, key, nil, d)
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	delete(f.users, "restore")
	before := f.mutations
	f.mu.Unlock()
	RecheckRuntime(ctx, s, i, key, nil)
	f.mu.Lock()
	after := f.mutations
	f.mu.Unlock()
	if before != after {
		t.Fatal("restored without authorization")
	}
	d.Revision = r.Revision
	d.RestoreAuthorized = true
	r, e = Mutate(ctx, s, key, nil, d)
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	delete(f.users, "restore")
	before = f.mutations
	f.mu.Unlock()
	RecheckRuntime(ctx, s, i, key, nil)
	RecheckRuntime(ctx, s, i, key, nil)
	f.mu.Lock()
	after = f.mutations
	exists := f.users["restore"] != nil
	f.mu.Unlock()
	if after != before+1 || !exists {
		t.Fatal("restore not idempotent", before, after)
	}
	f.mu.Lock()
	f.users["restore"].Account.Value = wire(t, &hp.UUIDAccount{Id: "22222222-2222-4222-8222-222222222222"})
	before = f.mutations
	f.mu.Unlock()
	RecheckRuntime(ctx, s, i, key, nil)
	f.mu.Lock()
	after = f.mutations
	f.mu.Unlock()
	if before != after {
		t.Fatal("restore overwrote conflicting credentials")
	}
}
