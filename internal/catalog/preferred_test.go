package catalog_test

import (
	"context"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscription"
)

func TestPreferredGenerationAtomicIdempotentAndPreservesProtocolAndRights(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	inbound, e := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "xh", Profile: core.NodeProfile{Protocol: "vless", Transport: "xhttp", Path: "/fixed-path", XHTTPMode: "packet-up", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	template, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "Template", Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com", "fingerprint": "chrome"}})
	if e != nil {
		t.Fatal(e)
	}
	user, e := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "existing", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("UPDATE subscribers SET node_authorization_required=1 WHERE id=?", user)
	group, e := catalog.SaveGroup(ctx, s, catalog.Group{Name: "existing", Enabled: true, NodeIDs: []string{template.NodeID}})
	if e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("INSERT INTO subscriber_node_grants VALUES(?,?,?,'before')", user, template.NodeID, inbound)
	d := catalog.PreferredRequest{TemplateNodeID: template.NodeID, GroupID: group, Prefix: "Optimized", Input: "104.16.1.1:443#A\n104.16.1.2:8443#B", Port: 443, Limit: 10, RequestID: "preferred-batch-test-001", Confirmed: true}
	p, e := catalog.PlanPreferred(ctx, s.DB, d)
	if e != nil || len(p.Entries) != 2 {
		t.Fatal(p, e)
	}
	d.PlanHash = p.Hash
	result, e := catalog.GeneratePreferred(ctx, s, d)
	if e != nil || result.Created != 2 {
		t.Fatal(result, e)
	}
	again, e := catalog.GeneratePreferred(ctx, s, d)
	if e != nil || again.NodeIDs[0] != result.NodeIDs[0] {
		t.Fatal(again, e)
	}
	for _, id := range result.NodeIDs {
		n, e := catalog.ResolveDefinition(ctx, s.DB, id)
		if e != nil || n.Profile.Path != "/fixed-path" || n.Profile.XHTTPMode != "packet-up" || n.Profile.Host != "origin.example.com" || n.Profile.SNI != "origin.example.com" || n.Profile.Fingerprint != "chrome" || n.InboundID == nil || *n.InboundID != inbound {
			t.Fatal(n, e)
		}
	}
	assigned, e := subscription.NodeAssignments(ctx, s.DB, user)
	if e != nil || len(assigned) != 1 || assigned[0].NodeID != template.NodeID {
		t.Fatal("generation expanded user rights", assigned, e)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&count)
	if count != 0 {
		t.Fatal("generated core credentials")
	}
	d.RequestID = "preferred-second-batch-001"
	p, e = catalog.PlanPreferred(ctx, s.DB, d)
	if e != nil {
		t.Fatal(e)
	}
	d.PlanHash = p.Hash
	reuse, e := catalog.GeneratePreferred(ctx, s, d)
	if e != nil || reuse.Created != 0 || reuse.Reused != 2 {
		t.Fatal(reuse, e)
	}
	s.DB.QueryRow("SELECT count(*) FROM node_group_members WHERE group_id=?", group).Scan(&count)
	if count != 3 {
		t.Fatal("duplicated group members", count)
	}
	// Stale confirmations cannot overwrite a template changed by the operator.
	d.RequestID = "preferred-stale-batch-001"
	d.Input = "104.16.1.3"
	p, e = catalog.PlanPreferred(ctx, s.DB, d)
	if e != nil {
		t.Fatal(e)
	}
	d.PlanHash = p.Hash
	s.DB.Exec("UPDATE node_profiles SET name='changed' WHERE id=?", template.NodeID)
	if _, e = catalog.GeneratePreferred(ctx, s, d); e == nil {
		t.Fatal("stale plan accepted")
	}
	// A second-node failure rolls back the new group and the first entry too.
	d.GroupID = ""
	d.GroupName = "must rollback"
	d.Input = "104.16.9.1\n104.16.9.2"
	d.RequestID = "preferred-failure-batch-001"
	p, e = catalog.PlanPreferred(ctx, s.DB, d)
	if e != nil {
		t.Fatal(e)
	}
	d.PlanHash = p.Hash
	s.DB.Exec("CREATE TRIGGER fail_second BEFORE INSERT ON node_profiles WHEN NEW.name LIKE 'Optimized-02%' BEGIN SELECT RAISE(ABORT,'fixture failure'); END")
	if _, e = catalog.GeneratePreferred(ctx, s, d); e == nil {
		t.Fatal("injected failure did not abort")
	}
	for _, q := range []string{"SELECT count(*) FROM node_groups WHERE name='must rollback'", "SELECT count(*) FROM node_endpoints WHERE name LIKE 'Optimized-01%' AND profile_json LIKE '%104.16.9.1%'"} {
		s.DB.QueryRow(q).Scan(&count)
		if count != 0 {
			t.Fatal("partial generation committed", q, count)
		}
	}
}
