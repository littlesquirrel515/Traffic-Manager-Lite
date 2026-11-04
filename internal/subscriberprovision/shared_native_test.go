package subscriberprovision

import (
	"context"
	"testing"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscription"
)

func TestSharedNativeNodeDeliveryNoCoreWritesOrDuplicateMeters(t *testing.T) {
	s, _, f, in := setup(t)
	ctx := context.Background()
	if _, err := s.Store.DB.Exec("UPDATE instances SET core_type='singbox'"); err != nil {
		t.Fatal(err)
	}
	cred, err := remote.RegisterClient(ctx, s.Store, remote.Client{InboundID: in, Reference: "native", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := subscription.SaveSubscriber(ctx, s.Store, subscription.Subscriber{Name: "owner", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = subscription.Bind(ctx, s.Store, subscription.Binding{SubscriberID: owner, InboundID: &in, CredentialID: cred, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	node := nodeFixture(t, s, in, "SG-3")
	d := nodeRequest(t, s, node)
	p, err := s.PlanNodes(ctx, d)
	if err != nil || p.Targets[0].Status != "shared_native" {
		t.Fatal(p, err)
	}
	r, err := s.CreateNodes(ctx, d)
	if err != nil || r.Status != "ready" {
		t.Fatal(r, err)
	}
	again, err := s.CreateNodes(ctx, d)
	if err != nil || again.SubscriberID != r.SubscriberID {
		t.Fatal(again, err)
	}
	if f.adds != 0 || count(t, s, "SELECT count(*) FROM subscriber_usage_sources WHERE active=1") != 1 || count(t, s, "SELECT count(*) FROM subscriber_credentials WHERE usage_mode='shared_native' AND billable=0") != 1 {
		t.Fatal("core writes, duplicated meters, or shared binding missing")
	}
	nodes, rejected, _, err := subscription.ResolveCatalog(ctx, s.Store.DB, subscription.CatalogDefinition{SubscriberID: r.SubscriberID, NodeIDs: []string{node}})
	if err != nil || len(nodes) != 1 || len(rejected) != 0 || nodes[0].Profile.UUID != "11111111-1111-4111-8111-111111111111" {
		t.Fatal("original credential was not delivered", err)
	}
	for _, id := range []string{owner, r.SubscriberID} {
		u, err := subscription.SubscriberUsage(ctx, s.Store.DB, id)
		if err != nil || u.Completeness == "complete" || u.Remaining != nil || !u.SharedNative {
			t.Fatal(u, err)
		}
	}
	editNodes(t, s, r.SubscriberID, "shared-node-remove-0001")
	if nodes, _, _, err = subscription.ResolveCatalog(ctx, s.Store.DB, subscription.CatalogDefinition{SubscriberID: r.SubscriberID, NodeIDs: []string{node}}); err != nil || len(nodes) != 0 {
		t.Fatal("removed shared node was published", err)
	}
	editNodes(t, s, r.SubscriberID, "shared-node-regrant-0001", node)
	if f.adds != 0 || count(t, s, "SELECT count(*) FROM subscriber_credentials") != 2 {
		t.Fatal("regrant created accounts or duplicate bindings")
	}
}

func TestSharedNativeAmbiguityAndChangedConfirmation(t *testing.T) {
	s, _, _, in := setup(t)
	ctx := context.Background()
	s.Store.DB.Exec("UPDATE instances SET core_type='singbox'")
	_, err := remote.RegisterClient(ctx, s.Store, remote.Client{InboundID: in, Reference: "one", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	node := nodeFixture(t, s, in, "SG-3")
	d := nodeRequest(t, s, node)
	_, err = remote.RegisterClient(ctx, s.Store, remote.Client{InboundID: in, Reference: "two", UUID: "22222222-2222-4222-8222-222222222222", Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateNodes(ctx, d); err == nil {
		t.Fatal("stale candidate accepted")
	}
	p, err := s.PlanNodes(ctx, d)
	if err != nil || p.Ready {
		t.Fatal(p, err)
	}
	if count(t, s, "SELECT count(*) FROM subscribers") != 0 {
		t.Fatal("ambiguous native account created a user")
	}
}

func TestSharedNativeRepairsLegacyGrantAndRespectsDisabledBinding(t *testing.T) {
	s, _, _, in := setup(t)
	ctx := context.Background()
	s.Store.DB.Exec("UPDATE instances SET core_type='singbox'")
	_, err := remote.RegisterClient(ctx, s.Store, remote.Client{InboundID: in, Reference: "native", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	node := nodeFixture(t, s, in, "SG-3")
	user, err := subscription.SaveSubscriber(ctx, s.Store, subscription.Subscriber{Name: "legacy", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Store.DB.Exec("INSERT INTO subscriber_node_grants VALUES(?,?,?,'before')", user, node, in)
	d := NodeEditRequest{SubscriberID: user, RequestID: "repair-native-grant-001", NodeIDs: []string{node}, Confirmed: true}
	p, err := s.PreviewNodeEdit(ctx, d)
	if err != nil || !p.Ready || len(p.Repaired) != 1 || len(p.Added) != 0 {
		t.Fatal(p, err)
	}
	d.PlanHash = p.Hash
	r, err := s.ApplyNodeEdit(ctx, d)
	if err != nil || r.Status != "ready" {
		t.Fatal(r, err)
	}
	if count(t, s, "SELECT count(*) FROM subscriber_credentials WHERE usage_mode='shared_native'") != 1 {
		t.Fatal("existing grant was not repaired")
	}
	s.Store.DB.Exec("UPDATE subscriber_credentials SET enabled=0 WHERE subscriber_id=?", user)
	p, err = s.PreviewNodeEdit(ctx, d)
	if err != nil || len(p.Repaired) != 0 {
		t.Fatal("disabled binding was implicitly restored", p, err)
	}
}

func TestSharedNativeNeverDistributesAnotherManagedAccount(t *testing.T) {
	s, _, _, in := setup(t)
	ctx := context.Background()
	r, err := s.Create(ctx, Request{RequestID: "managed-private-account-001", Subscriber: subscription.Subscriber{Name: "private", Enabled: true}, InboundID: in, Mode: "managed", Confirmed: true})
	if err != nil || r.Status != "ready" {
		t.Fatal(r, err)
	}
	s.Store.DB.Exec("UPDATE instances SET core_type='singbox'")
	node := nodeFixture(t, s, in, "private")
	p, err := s.PlanNodes(ctx, NodeRequest{NodeIDs: []string{node}})
	if err != nil || p.Ready {
		t.Fatal("private managed account proposed for sharing", p, err)
	}
}
