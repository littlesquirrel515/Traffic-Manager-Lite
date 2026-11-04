package storage_test

import (
	"context"
	"path/filepath"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

func TestLegacyGroupsFreezeAtRecordedSnapshotAndDoNotGrantNewHosts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v22.db")
	s, e := storage.Open(path, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','127.0.0.1:1','now','now')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	inbound, e := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "ws", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	credential, e := remote.RegisterClient(ctx, s, remote.Client{InboundID: inbound, Reference: "native", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for _, name := range []string{"Existing", "SG-5", "Explicit-late"} {
		r, err := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: name, Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.NodeID)
	}
	user, e := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "Legacy user", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = subscription.Bind(ctx, s, subscription.Binding{SubscriberID: user, InboundID: &inbound, CredentialID: credential, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	group, e := catalog.SaveGroup(ctx, s, catalog.Group{Name: "SG-G1", Enabled: true, NodeIDs: ids})
	if e != nil {
		t.Fatal(e)
	}
	old, token, e := subscription.SaveCatalogSubscription(ctx, s, subscription.CatalogDefinition{Name: "Original", SubscriberID: user, Format: "base64", Enabled: true, NodeIDs: []string{ids[2]}, GroupIDs: []string{group}})
	if e != nil {
		t.Fatal(e)
	}
	// Reconstruct legacy chronology: known entry was valid at the unified-link
	// snapshot; SG-5 was added only afterwards; direct late choice is explicit.
	_, e = s.DB.Exec(`INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,enabled,format,created_at,updated_at) VALUES('unified',?,'Unified','unused',1,'auto','2026-10-30T09:00:00Z','now')`, user)
	if e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("INSERT INTO subscriber_unified_links VALUES(?,'unified')", user)
	s.DB.Exec("UPDATE node_profiles SET created_at='2026-10-20T00:00:00Z' WHERE id=?", ids[0])
	s.DB.Exec("UPDATE node_profiles SET created_at='2026-10-30T12:00:00Z' WHERE id IN (?,?)", ids[1], ids[2])
	if _, e = s.DB.Exec(`INSERT INTO subscribers(id,name,enabled,created_at,updated_at) VALUES('no-unified','Older legacy',1,'before','before'); INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,enabled,format,created_at,updated_at) VALUES('older','no-unified','Older','unused-older',0,'raw','2026-10-21T00:00:00Z','now')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO catalog_subscription_groups VALUES('older',?,0)", group); e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("DELETE FROM schema_migrations WHERE version='006_explicit_subscriber_nodes.sql'")
	var hash string
	s.DB.QueryRow("SELECT token_hash FROM catalog_subscriptions WHERE id=?", old).Scan(&hash)
	s.Close()
	s, e = storage.Open(path, "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var explicit bool
	if e = s.DB.QueryRow("SELECT node_authorization_required FROM subscribers WHERE id=?", user).Scan(&explicit); e != nil || !explicit {
		t.Fatal("legacy user not converted", e)
	}
	assigned, e := subscription.NodeAssignments(ctx, s.DB, user)
	if e != nil || len(assigned) != 2 {
		t.Fatal(assigned, e)
	}
	for _, n := range assigned {
		if n.NodeID == ids[1] {
			t.Fatal("SG-5 silently authorized")
		}
	}
	older, e := subscription.NodeAssignments(ctx, s.DB, "no-unified")
	if e != nil || len(older) != 1 || older[0].NodeID != ids[0] {
		t.Fatal("legacy subscription cutoff was not used", older, e)
	}
	var after string
	s.DB.QueryRow("SELECT token_hash FROM catalog_subscriptions WHERE id=?", old).Scan(&after)
	if hash != after {
		t.Fatal("migration rotated URL")
	}
	res, _, e := subscription.CatalogOutput(ctx, s, token, "")
	if e != nil || len(res.Content) == 0 {
		t.Fatal("original link lost", e)
	}
	next, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "SG-6", Enabled: true, Profile: map[string]any{"address": "new.example.com", "port": 443, "sni": "origin.example.com"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = catalog.SaveGroup(ctx, s, catalog.Group{ID: group, Name: "SG-G1", Enabled: true, NodeIDs: append(ids, next.NodeID)})
	if e != nil {
		t.Fatal(e)
	}
	assigned, e = subscription.NodeAssignments(ctx, s.DB, user)
	if e != nil || len(assigned) != 2 {
		t.Fatal("group edits expanded frozen authority", assigned, e)
	}
	definition, e := subscription.CatalogSubscription(ctx, s.DB, old)
	if e != nil {
		t.Fatal(e)
	}
	resolved, _, _, e := subscription.ResolveCatalog(ctx, s.DB, definition)
	if e != nil || len(resolved) != 2 {
		t.Fatal("legacy output bypassed exact grants", len(resolved), e)
	}
}
