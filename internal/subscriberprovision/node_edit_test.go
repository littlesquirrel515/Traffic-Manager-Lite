package subscriberprovision

import (
	"bytes"
	"context"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscription"
	"traffic-manager-lite/migrations"
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

func TestDynamicGroupFollowsMembershipAndRevocation(t *testing.T) {
	s, _, fake, inbound := setup(t)
	ctx := context.Background()
	a, b, c := nodeFixture(t, s, inbound, "group-A"), nodeFixture(t, s, inbound, "group-B"), nodeFixture(t, s, inbound, "outside")
	group, err := catalog.SaveGroup(ctx, s.Store, catalog.Group{Name: "dynamic", Enabled: true, NodeIDs: []string{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	request := nodeRequest(t, s, a)
	request.NodeIDs = nil
	request.GroupIDs = []string{group}
	plan, err := s.PlanNodes(ctx, request)
	if err != nil || !plan.Ready {
		t.Fatal(plan, err)
	}
	request.PlanHash = plan.Hash
	result, err := s.CreateNodes(ctx, request)
	if err != nil || result.Status != "ready" {
		t.Fatal(result, err)
	}
	if count(t, s, "SELECT count(*) FROM subscriber_node_grants") != 0 || fake.adds != 1 {
		t.Fatal("group must not create per-node grants or repeated clients")
	}
	link, err := subscription.UnifiedLink(ctx, s.Store, s.Config.ManagedKeyFile, result.SubscriberID)
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := subscription.CatalogOutput(ctx, s.Store, link.Token, "mihomo")
	if err != nil || !bytes.Contains(before.Content, []byte("group-B")) {
		t.Fatalf("initial subscription: %v", err)
	}
	assignments, err := subscription.NodeAssignments(ctx, s.Store.DB, result.SubscriberID)
	if err != nil || len(assignments) != 2 {
		t.Fatal(assignments, err)
	}
	_, err = catalog.SaveGroup(ctx, s.Store, catalog.Group{ID: group, Name: "dynamic", Enabled: true, NodeIDs: []string{a, c}})
	if err != nil {
		t.Fatal(err)
	}
	assignments, err = subscription.NodeAssignments(ctx, s.Store.DB, result.SubscriberID)
	if err != nil || len(assignments) != 2 {
		t.Fatal(assignments, err)
	}
	seen := map[string]bool{}
	for _, n := range assignments {
		seen[n.NodeID] = true
	}
	if !seen[a] || !seen[c] || seen[b] {
		t.Fatal("incorrect dynamic members", seen)
	}
	after, _, err := subscription.CatalogOutput(ctx, s.Store, link.Token, "mihomo")
	if err != nil || bytes.Contains(after.Content, []byte("group-B")) || !bytes.Contains(after.Content, []byte("outside")) {
		t.Fatalf("subscription did not follow membership: %v", err)
	}

	second, err := remote.SaveManual(ctx, s.Store, remote.Inbound{InstanceID: 1, Tag: "group-other-inbound", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/ws", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	foreign := nodeFixture(t, s, second, "different-inbound")
	_, err = catalog.SaveGroup(ctx, s.Store, catalog.Group{ID: group, Name: "dynamic", Enabled: true, NodeIDs: []string{a, c, foreign}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, rejected, _, err := subscription.ResolveCatalog(ctx, s.Store.DB, subscription.CatalogDefinition{SubscriberID: result.SubscriberID, NodeIDs: []string{foreign}})
	if err != nil || len(resolved) != 0 || len(rejected) != 1 {
		t.Fatalf("new inbound without credential must not be delivered: %v %v %v", resolved, rejected, err)
	}
	if fake.adds != 1 {
		t.Fatal("adding a group member must not create a runtime client")
	}

	edit := NodeEditRequest{SubscriberID: result.SubscriberID, RequestID: "remove-group-dynamic-001", Confirmed: true}
	p, err := s.PreviewNodeEdit(ctx, edit)
	if err != nil || !p.Ready {
		t.Fatal(p, err)
	}
	edit.PlanHash = p.Hash
	if _, err = s.ApplyNodeEdit(ctx, edit); err != nil {
		t.Fatal(err)
	}
	assignments, err = subscription.NodeAssignments(ctx, s.Store.DB, result.SubscriberID)
	if err != nil || len(assignments) != 0 {
		t.Fatal(assignments, err)
	}
	if fake.adds != 1 {
		t.Fatal("unexpected runtime modifications")
	}
}

func TestExistingSubscriberCanSelectDynamicGroupAndRevoke(t *testing.T) {
	s, _, fake, inbound := setup(t)
	ctx := context.Background()
	a, b := nodeFixture(t, s, inbound, "old"), nodeFixture(t, s, inbound, "group-new")
	created, err := s.CreateNodes(ctx, nodeRequest(t, s, a))
	if err != nil || created.Status != "ready" {
		t.Fatal(created, err)
	}
	group, err := catalog.SaveGroup(ctx, s.Store, catalog.Group{Name: "group", Enabled: true, NodeIDs: []string{b}})
	if err != nil {
		t.Fatal(err)
	}
	edit := NodeEditRequest{SubscriberID: created.SubscriberID, RequestID: "edit-dynamic-group-001", NodeIDs: []string{a}, GroupIDs: []string{group}, Confirmed: true}
	p, err := s.PreviewNodeEdit(ctx, edit)
	if err != nil || !p.Ready {
		t.Fatal(p, err)
	}
	edit.PlanHash = p.Hash
	result, err := s.ApplyNodeEdit(ctx, edit)
	if err != nil || result.Status != "ready" {
		t.Fatal(result, err)
	}
	assignments, err := subscription.NodeAssignments(ctx, s.Store.DB, created.SubscriberID)
	if err != nil || len(assignments) != 2 {
		t.Fatal(assignments, err)
	}
	if count(t, s, "SELECT count(*) FROM subscriber_group_grants") != 1 || fake.adds != 1 {
		t.Fatal("group missing or client duplicated")
	}
	edit = NodeEditRequest{SubscriberID: created.SubscriberID, RequestID: "remove-dynamic-group-001", NodeIDs: []string{a}, Confirmed: true}
	p, err = s.PreviewNodeEdit(ctx, edit)
	if err != nil || !p.Ready {
		t.Fatal(p, err)
	}
	edit.PlanHash = p.Hash
	result, err = s.ApplyNodeEdit(ctx, edit)
	if err != nil || result.Status != "ready" {
		t.Fatal(result, err)
	}
	assignments, err = subscription.NodeAssignments(ctx, s.Store.DB, created.SubscriberID)
	if err != nil || len(assignments) != 1 || assignments[0].NodeID != a {
		t.Fatal(assignments, err)
	}
}

func TestSnapshotToDynamicGroupConversionPreservesExistingRights(t *testing.T) {
	s, _, fake, inbound := setup(t)
	ctx := context.Background()
	a, b, c := nodeFixture(t, s, inbound, "old1"), nodeFixture(t, s, inbound, "old2"), nodeFixture(t, s, inbound, "new")
	result, err := s.CreateNodes(ctx, nodeRequest(t, s, a, b))
	if err != nil || result.Status != "ready" {
		t.Fatal(result, err)
	}
	group, err := catalog.SaveGroup(ctx, s.Store, catalog.Group{Name: "old-group", Enabled: true, NodeIDs: []string{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	edit := NodeEditRequest{SubscriberID: result.SubscriberID, RequestID: "convert-group-snapshot-01", GroupIDs: []string{group}, Confirmed: true}
	plan, err := s.PreviewNodeEdit(ctx, edit)
	if err != nil || !plan.Ready || len(plan.Added) != 0 || len(plan.Removed) != 0 {
		t.Fatal(plan, err)
	}
	edit.PlanHash = plan.Hash
	out, err := s.ApplyNodeEdit(ctx, edit)
	if err != nil || out.Status != "ready" {
		t.Fatal(out, err)
	}
	if count(t, s, "SELECT count(*) FROM subscriber_node_grants") != 0 || count(t, s, "SELECT count(*) FROM subscriber_group_grants") != 1 {
		t.Fatal("snapshot not converted")
	}
	_, err = catalog.SaveGroup(ctx, s.Store, catalog.Group{ID: group, Name: "old-group", Enabled: true, NodeIDs: []string{a, b, c}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, rejected, _, err := subscription.ResolveCatalog(ctx, s.Store.DB, subscription.CatalogDefinition{SubscriberID: result.SubscriberID, NodeIDs: []string{a, b, c}})
	if err != nil || len(resolved) != 3 || len(rejected) != 0 || fake.adds != 1 {
		t.Fatal(resolved, rejected, err)
	}
}

func TestDynamicGroupMigrationPromotesOnlyCompleteSnapshots(t *testing.T) {
	s, _, _, inbound := setup(t)
	ctx := context.Background()
	a, b, c := nodeFixture(t, s, inbound, "migrated-a"), nodeFixture(t, s, inbound, "migrated-b"), nodeFixture(t, s, inbound, "migrated-c")
	full, err := s.CreateNodes(ctx, nodeRequest(t, s, a, b))
	if err != nil || full.Status != "ready" {
		t.Fatal(full, err)
	}
	d := nodeRequest(t, s, a)
	d.RequestID = "partial-migration-request-01"
	d.Subscriber.Name = "partial"
	partial, err := s.CreateNodes(ctx, d)
	if err != nil || partial.Status != "ready" {
		t.Fatal(partial, err)
	}
	group, err := catalog.SaveGroup(ctx, s.Store, catalog.Group{Name: "migration group", Enabled: true, NodeIDs: []string{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveGroup(ctx, s.Store, catalog.Group{Name: "partial-overlap", Enabled: true, NodeIDs: []string{a, c}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB.Exec("DROP TABLE subscriber_group_grants"); err != nil {
		t.Fatal(err)
	}
	sql, err := migrations.Files.ReadFile("010_dynamic_group_grants.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB.Exec(string(sql)); err != nil {
		t.Fatal(err)
	}
	var fullGroup, partialGroup, fullDirect, partialDirect int
	for _, q := range []struct {
		sql  string
		args []any
		dest *int
	}{
		{"SELECT count(*) FROM subscriber_group_grants WHERE subscriber_id=? AND group_id=?", []any{full.SubscriberID, group}, &fullGroup},
		{"SELECT count(*) FROM subscriber_group_grants WHERE subscriber_id=? AND group_id=?", []any{partial.SubscriberID, group}, &partialGroup},
		{"SELECT count(*) FROM subscriber_node_grants WHERE subscriber_id=?", []any{full.SubscriberID}, &fullDirect},
		{"SELECT count(*) FROM subscriber_node_grants WHERE subscriber_id=?", []any{partial.SubscriberID}, &partialDirect},
	} {
		if err = s.Store.DB.QueryRow(q.sql, q.args...).Scan(q.dest); err != nil {
			t.Fatal(err)
		}
	}
	if fullGroup != 1 || partialGroup != 0 || fullDirect != 1 || partialDirect != 1 {
		t.Fatal(fullGroup, partialGroup, fullDirect, partialDirect)
	}
}
