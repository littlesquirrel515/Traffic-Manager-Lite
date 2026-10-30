package subscriberprovision

import (
	"context"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

func nodeFixture(t *testing.T, s Service, inbound, name string) string {
	t.Helper()
	v, e := catalog.CreateConnection(context.Background(), s.Store, catalog.Connection{InboundID: inbound, Name: name, Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com"}})
	if e != nil || v.ValidationStatus != "ready" {
		t.Fatal(v, e)
	}
	return v.NodeID
}
func nodeRequest(t *testing.T, s Service, ids ...string) NodeRequest {
	t.Helper()
	d := NodeRequest{RequestID: "node-workflow-request-0001", Subscriber: subscription.Subscriber{Name: "Node user", Enabled: true}, NodeIDs: ids, Confirmed: true}
	p, e := s.PlanNodes(context.Background(), d)
	if e != nil || !p.Ready {
		t.Fatal(p, e)
	}
	d.PlanHash = p.Hash
	return d
}
func count(t *testing.T, s Service, q string) int {
	t.Helper()
	var n int
	if e := s.Store.DB.QueryRow(q).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestNodesDeduplicateClientsAndEnforceExactNodeAuthorization(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	a, b, c := nodeFixture(t, s, in, "Edge A"), nodeFixture(t, s, in, "Edge B"), nodeFixture(t, s, in, "Not granted")
	group, e := catalog.SaveGroup(ctx, s.Store, catalog.Group{Name: "Edges", Enabled: true, NodeIDs: []string{a, b}})
	if e != nil {
		t.Fatal(e)
	}
	d := nodeRequest(t, s, a)
	d.GroupIDs = []string{group}
	p, e := s.PlanNodes(ctx, d)
	if e != nil || len(p.Nodes) != 2 || len(p.Targets) != 1 {
		t.Fatal(p, e)
	}
	d.PlanHash = p.Hash
	r, e := s.CreateNodes(ctx, d)
	if e != nil || r.Status != "ready" || r.ReadyCount != 1 || f.adds != 1 {
		t.Fatal(r, e, f.adds)
	}
	if count(t, s, "SELECT count(*) FROM subscribers WHERE enabled=1 AND node_authorization_required=1") != 1 || count(t, s, "SELECT count(*) FROM subscriber_node_grants") != 2 || count(t, s, "SELECT count(*) FROM subscriber_credentials") != 1 || count(t, s, "SELECT count(*) FROM subscriber_usage_sources WHERE active=1") != 1 {
		t.Fatal("nodes duplicated clients, subscribers or billing sources")
	}
	again, e := s.CreateNodes(ctx, d)
	if e != nil || again.SubscriberID != r.SubscriberID || f.adds != 1 {
		t.Fatal("retry duplicated user", again, e)
	}
	out, reject, _, e := subscription.ResolveCatalog(ctx, s.Store.DB, subscription.CatalogDefinition{SubscriberID: r.SubscriberID, NodeIDs: []string{a, b, c}})
	if e != nil || len(out) != 2 || len(reject) != 1 || !strings.Contains(reject[0].Reason, "node_not_authorized") {
		t.Fatal(out, reject, e)
	}
	// Groups never silently expand the user's authority after creation.
	_, e = catalog.SaveGroup(ctx, s.Store, catalog.Group{ID: group, Name: "Edges", Enabled: true, NodeIDs: []string{a, b, c}})
	if e != nil {
		t.Fatal(e)
	}
	out, reject, _, e = subscription.ResolveCatalog(ctx, s.Store.DB, subscription.CatalogDefinition{SubscriberID: r.SubscriberID, GroupIDs: []string{group}})
	if e != nil || len(out) != 2 || len(reject) != 1 {
		t.Fatal("group expanded user authority", out, reject, e)
	}
	// Granting another entrance for the same user reuses its core client.
	add := NodeRequest{RequestID: "additional-node-request-001", Subscriber: subscription.Subscriber{ID: r.SubscriberID}, NodeIDs: []string{a, b, c}, Confirmed: true}
	plan, e := s.PlanNodes(ctx, add)
	if e != nil || len(plan.Nodes) != 1 || plan.Nodes[0].ID != c || plan.Targets[0].Status != "reuse" {
		t.Fatal(plan, e)
	}
	add.PlanHash = plan.Hash
	more, e := s.CreateNodes(ctx, add)
	if e != nil || more.Status != "ready" || f.adds != 1 || count(t, s, "SELECT count(*) FROM subscriber_usage_sources WHERE active=1") != 1 {
		t.Fatal(more, e, f.adds)
	}
}

func TestNodesPartialFailureDurableRetryAndManualDisable(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	second, e := remote.SaveManual(ctx, s.Store, remote.Inbound{InstanceID: 1, Tag: "second", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	a, b := nodeFixture(t, s, in, "First"), nodeFixture(t, s, second, "Second")
	f.blockedTag = "second"
	d := nodeRequest(t, s, a, b)
	r, e := s.CreateNodes(ctx, d)
	if e != nil || r.Status != "pending" || r.ReadyCount != 1 || f.adds != 1 {
		t.Fatal(r, e, f.adds)
	}
	if count(t, s, "SELECT count(*) FROM subscribers WHERE enabled=1") != 0 {
		t.Fatal("partially provisioned user enabled")
	}
	// An explicit profile change must win over the saved activation preference.
	_, e = subscription.SaveSubscriber(ctx, s.Store, subscription.Subscriber{ID: r.SubscriberID, Name: "Keep disabled", Enabled: false})
	if e != nil {
		t.Fatal(e)
	}
	var seq int
	var databaseName, path string
	if e = s.Store.DB.QueryRow("PRAGMA database_list").Scan(&seq, &databaseName, &path); e != nil {
		t.Fatal(e)
	}
	s.Store.Close()
	reopened, e := storage.Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	s.Store = reopened
	f.mu.Lock()
	f.blockedTag = ""
	f.mu.Unlock()
	done, e := s.RetryNodes(ctx, d.RequestID)
	if e != nil || done.Status != "ready" || f.adds != 2 {
		t.Fatal(done, e, f.adds)
	}
	if count(t, s, "SELECT count(*) FROM subscribers WHERE enabled=1") != 0 || count(t, s, "SELECT count(*) FROM subscribers") != 1 {
		t.Fatal("retry overrode administrator or duplicated user")
	}
	if count(t, s, "SELECT count(*) FROM subscriber_node_tasks WHERE status='ready'") != 2 {
		t.Fatal("task results not persisted")
	}
}

func TestNodesChangedPreviewAndUnsupportedCoreHaveNoSideEffects(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	a := nodeFixture(t, s, in, "Edge")
	d := nodeRequest(t, s, a)
	s.Store.DB.Exec("UPDATE node_profiles SET name='changed' WHERE id=?", a)
	if _, e := s.CreateNodes(ctx, d); e == nil {
		t.Fatal("stale confirmation accepted")
	}
	if count(t, s, "SELECT count(*) FROM subscribers") != 0 || f.adds != 0 {
		t.Fatal("stale preview wrote user or core")
	}
	s.Store.DB.Exec("UPDATE instances SET core_type='singbox'")
	p, e := s.PlanNodes(ctx, d)
	if e != nil || p.Ready || p.Targets[0].Status != "blocked" {
		t.Fatal(p, e)
	}
	d.PlanHash = p.Hash
	if _, e = s.CreateNodes(ctx, d); e == nil {
		t.Fatal("unsupported core accepted")
	}
	if count(t, s, "SELECT count(*) FROM subscribers") != 0 || count(t, s, "SELECT count(*) FROM subscriber_node_requests") != 0 || f.adds != 0 {
		t.Fatal("unsupported plan had side effects")
	}
}

func TestNodesPendingTargetChangeDoesNotWriteNewCoreTarget(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	a := nodeFixture(t, s, in, "Edge")
	f.blocked = true
	d := nodeRequest(t, s, a)
	r, e := s.CreateNodes(ctx, d)
	if e != nil || r.Status != "pending" {
		t.Fatal(r, e)
	}
	s.Store.DB.Exec("UPDATE catalog_inbounds SET tag='unconfirmed-new-target' WHERE id=?", in)
	f.mu.Lock()
	f.blocked = false
	f.mu.Unlock()
	r, e = s.RetryNodes(ctx, d.RequestID)
	if e != nil || r.Status != "pending" || f.adds != 0 {
		t.Fatal(r, e, f.adds)
	}
}

func TestPendingNodeWorkflowRechecksReadyTargetAfterCoreRestart(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	second, e := remote.SaveManual(ctx, s.Store, remote.Inbound{InstanceID: 1, Tag: "second", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	d := nodeRequest(t, s, nodeFixture(t, s, in, "First"), nodeFixture(t, s, second, "Second"))
	f.blockedTag = "second"
	r, e := s.CreateNodes(ctx, d)
	if e != nil || r.Status != "pending" || f.adds != 1 {
		t.Fatal(r, e, f.adds)
	}
	// The already-ready target lost its runtime user while the parent was pending.
	f.mu.Lock()
	for ref := range f.users {
		delete(f.users, ref)
	}
	f.blockedTag = ""
	f.mu.Unlock()
	r, e = s.RetryNodes(ctx, d.RequestID)
	if e != nil || r.Status != "ready" || f.adds != 3 {
		t.Fatal("old ready record used without actual runtime verification", r, e, f.adds)
	}
	if count(t, s, "SELECT count(*) FROM subscribers WHERE enabled=1") != 1 {
		t.Fatal("verified user was not activated")
	}
}
