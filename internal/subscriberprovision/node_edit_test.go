package subscriberprovision

import (
	"context"
	"testing"
	"traffic-manager-lite/internal/subscription"
)

func editNodes(t *testing.T, s Service, user, request string, nodes ...string) Result {
	t.Helper()
	ctx := context.Background()
	d := NodeEditRequest{SubscriberID: user, RequestID: request, NodeIDs: nodes, Confirmed: true}
	p, e := s.PreviewNodeEdit(ctx, d)
	if e != nil || !p.Ready {
		t.Fatal(p, e)
	}
	d.PlanHash = p.Hash
	r, e := s.ApplyNodeEdit(ctx, d)
	if e != nil {
		t.Fatal(r, e)
	}
	again, e := s.ApplyNodeEdit(ctx, d)
	if e != nil || again.Status != r.Status {
		t.Fatal("edit replay differs", again, e)
	}
	return r
}
func TestNodePermissionCRUDPreservesClientsMeterAndOldLinks(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	a, b := nodeFixture(t, s, in, "A"), nodeFixture(t, s, in, "B")
	r, e := s.CreateNodes(ctx, nodeRequest(t, s, a, b))
	if e != nil || r.Status != "ready" {
		t.Fatal(r, e)
	}
	link, token, e := subscription.SaveCatalogSubscription(ctx, s.Store, subscription.CatalogDefinition{Name: "Existing", SubscriberID: r.SubscriberID, Format: "auto", Enabled: true, NodeIDs: []string{a, b}})
	if e != nil {
		t.Fatal(e)
	}
	_ = link
	if e = subscription.PersistVisibleToken(ctx, s.Store, s.Config.ManagedKeyFile, link, token); e != nil {
		t.Fatal(e)
	}
	editNodes(t, s, r.SubscriberID, "remove-one-node-0001", b)
	d, e := subscription.NodeAssignments(ctx, s.Store.DB, r.SubscriberID)
	if e != nil || len(d) != 1 || d[0].NodeID != b {
		t.Fatal(d, e)
	}
	out, rejected, _, e := subscription.ResolveCatalog(ctx, s.Store.DB, subscription.CatalogDefinition{SubscriberID: r.SubscriberID, NodeIDs: []string{a, b}})
	if e != nil || len(out) != 1 || len(rejected) != 1 {
		t.Fatal(out, rejected, e)
	}
	if _, _, e = subscription.CatalogOutput(ctx, s.Store, token, ""); e != nil {
		t.Fatal("existing token lost", e)
	}
	if f.adds != 1 || count(t, s, "SELECT count(*) FROM subscriber_credentials") != 1 || count(t, s, "SELECT count(*) FROM subscriber_usage_sources WHERE active=1") != 1 || count(t, s, "SELECT count(*) FROM remote_runtime_users WHERE restore_authorized=1") != 1 {
		t.Fatal("removal changed shared core user or meter")
	}
	editNodes(t, s, r.SubscriberID, "remove-all-nodes-0001")
	if _, _, e = subscription.CatalogOutput(ctx, s.Store, token, ""); e == nil {
		t.Fatal("empty permission still publishes")
	}
	if count(t, s, "SELECT count(*) FROM subscriber_credentials") != 1 || count(t, s, "SELECT count(*) FROM remote_runtime_users WHERE restore_authorized=1") != 0 || f.adds != 1 {
		t.Fatal("removal deleted account or kept restoring it")
	}
	editNodes(t, s, r.SubscriberID, "regrant-node-request-0001", a)
	if f.adds != 1 || count(t, s, "SELECT count(*) FROM remote_runtime_users WHERE restore_authorized=1") != 1 {
		t.Fatal("regrant duplicated client or lost restoration authorization")
	}
	if _, _, e = subscription.CatalogOutput(ctx, s.Store, token, ""); e != nil {
		t.Fatal("original token did not regain allowed node", e)
	}
}

func TestRemovingPendingNodesStopsOldTaskFromRestoringPermission(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	a := nodeFixture(t, s, in, "A")
	f.blocked = true
	d := nodeRequest(t, s, a)
	r, e := s.CreateNodes(ctx, d)
	if e != nil || r.Status != "pending" {
		t.Fatal(r, e)
	}
	editNodes(t, s, r.SubscriberID, "cancel-pending-nodes-001")
	f.mu.Lock()
	f.blocked = false
	f.mu.Unlock()
	_, e = s.RetryNodes(ctx, d.RequestID)
	if e != nil || f.adds != 0 {
		t.Fatal("old parent recreated removed client", e)
	}
	var child string
	s.Store.DB.QueryRow("SELECT child_request_id FROM subscriber_node_tasks WHERE request_id=?", d.RequestID).Scan(&child)
	retry, e := s.Retry(ctx, child)
	if e != nil || retry.Status != "cancelled" || f.adds != 0 {
		t.Fatal("child retry bypassed revocation", retry, e)
	}
	if count(t, s, "SELECT count(*) FROM subscribers WHERE enabled=1") != 0 {
		t.Fatal("cancelled workflow enabled user")
	}
	regranted := editNodes(t, s, r.SubscriberID, "regrant-pending-account-001", a)
	if regranted.Status != "ready" || f.adds != 1 || count(t, s, "SELECT count(*) FROM subscriber_provisioning") != 1 {
		t.Fatal("regrant failed to reuse original pending Desired", regranted, f.adds)
	}
	if count(t, s, "SELECT count(*) FROM subscribers WHERE enabled=1") != 0 {
		t.Fatal("regrant overrode disabled state")
	}
}

func TestNodeRemovalPreviewRejectsConcurrentPermissionChanges(t *testing.T) {
	s, _, _, in := setup(t)
	ctx := context.Background()
	a, b := nodeFixture(t, s, in, "A"), nodeFixture(t, s, in, "B")
	r, e := s.CreateNodes(ctx, nodeRequest(t, s, a, b))
	if e != nil {
		t.Fatal(e)
	}
	d := NodeEditRequest{RequestID: "stale-permissions-0001", SubscriberID: r.SubscriberID, Confirmed: true, NodeIDs: []string{a}}
	p, e := s.PreviewNodeEdit(ctx, d)
	if e != nil {
		t.Fatal(e)
	}
	d.PlanHash = p.Hash
	editNodes(t, s, r.SubscriberID, "other-admin-permissions-001", b)
	if _, e = s.ApplyNodeEdit(ctx, d); e == nil {
		t.Fatal("stale removal applied")
	}
	rows, e := subscription.NodeAssignments(ctx, s.Store.DB, r.SubscriberID)
	if e != nil || len(rows) != 1 || rows[0].NodeID != b {
		t.Fatal(rows, e)
	}
}
