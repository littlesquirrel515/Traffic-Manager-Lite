package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
)

func TestInboundConnectionsStableLifecycleIdempotencyAndSharedTemplate(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "inbound.json")
	body := `{"inbounds":[{"tag":"ws","port":10443,"protocol":"vless","settings":{"clients":[{"email":"alice","id":"11111111-1111-4111-8111-111111111111"}]},"streamSettings":{"network":"ws","security":"tls","wsSettings":{"path":"/ws"}}},{"tag":"xh","port":11443,"protocol":"vless","settings":{"clients":[{"email":"bob","id":"22222222-2222-4222-8222-222222222222"}]},"streamSettings":{"network":"xhttp","security":"tls","xhttpSettings":{"path":"/xh","mode":"packet-up"}}}]}`
	scan := func(b string) {
		t.Helper()
		os.WriteFile(path, []byte(b), 0600)
		if _, e := discovery.Scan(ctx, s, core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}, root); e != nil {
			t.Fatal(e)
		}
	}
	scan(body)
	var ws, xh string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='ws'").Scan(&ws)
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='xh'").Scan(&xh)
	var credentials int
	s.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&credentials)
	entries := []catalog.ConnectionResult{}
	for index, address := range []string{"edge.example.com", "203.0.113.9", "2001:db8::9"} {
		d := catalog.Connection{RequestID: strings.Repeat(string(rune('a'+index)), 20), InboundID: ws, Name: "entry " + address, Enabled: true, Profile: map[string]any{"address": address, "port": 443, "sni": "tls.example.com", "host": "http.example.com"}}
		result, e := catalog.CreateConnection(ctx, s, d)
		if e != nil || result.NodeID == "" || result.ValidationStatus != "ready" {
			t.Fatal(result, e)
		}
		repeated, e := catalog.CreateConnection(ctx, s, d)
		if e != nil || repeated != result {
			t.Fatal("retry duplicated resources", repeated, result, e)
		}
		d.Profile["port"] = 8443
		if _, e = catalog.CreateConnection(ctx, s, d); e == nil {
			t.Fatal("idempotency key reused for different input")
		}
		entries = append(entries, result)
	}
	shared, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: xh, EndpointID: entries[0].EndpointID, Enabled: true})
	if e != nil || shared.BindingID == entries[0].BindingID || shared.NodeID == entries[0].NodeID {
		t.Fatal("shared definition mixed bindings", shared, e)
	}
	a, _ := catalog.ResolveDefinition(ctx, s.DB, entries[0].NodeID)
	b, _ := catalog.ResolveDefinition(ctx, s.DB, shared.NodeID)
	if a.Profile.Transport != "ws" || a.Profile.Path != "/ws" || b.Profile.Transport != "xhttp" || b.Profile.Path != "/xh" {
		t.Fatal("protocols mixed", a, b)
	}
	group, e := catalog.SaveGroup(ctx, s, catalog.Group{Name: "g", Enabled: true, NodeIDs: []string{a.ID, b.ID}})
	if e != nil {
		t.Fatal(e)
	}
	ep, _ := catalog.ReadEndpoint(ctx, s.DB, entries[0].EndpointID)
	ep.Profile["address"] = "changed.example.com"
	ep.ConfirmShared = true
	if _, e = catalog.SaveEndpoint(ctx, s, ep); e != nil {
		t.Fatal(e)
	}
	a, _ = catalog.ResolveDefinition(ctx, s.DB, a.ID)
	b, _ = catalog.ResolveDefinition(ctx, s.DB, b.ID)
	if a.Profile.Address != "changed.example.com" || b.Profile.Address != "changed.example.com" {
		t.Fatal("shared address did not update")
	}
	renamed := strings.Replace(body, `"tag":"ws"`, `"tag":"renamed"`, 1)
	scan(renamed)
	a, _ = catalog.ResolveDefinition(ctx, s.DB, a.ID)
	if *a.InboundID != ws || *a.BindingID != entries[0].BindingID || a.Status != "ready" {
		t.Fatal("rename broke stable references", a)
	}
	changed := strings.Replace(renamed, "10443", "20443", 1)
	scan(changed)
	a, _ = catalog.ResolveDefinition(ctx, s.DB, a.ID)
	if a.Profile.Port != 443 || a.Status != "needs_validation" {
		t.Fatal("listen changed public port or republished", a)
	}
	binding, _ := catalog.ReadBinding(ctx, s.DB, *a.BindingID)
	if binding.ValidationStatus != "needs_validation" {
		t.Fatal(binding)
	}
	if _, e = catalog.UpdateBinding(ctx, s, binding, true); e == nil {
		t.Fatal("reviewed mapping before confirming source")
	}
	s.DB.Exec("UPDATE catalog_inbounds SET review=0 WHERE id=?", ws)
	if _, e = catalog.UpdateBinding(ctx, s, binding, true); e != nil {
		t.Fatal(e)
	}
	a, _ = catalog.ResolveDefinition(ctx, s.DB, a.ID)
	if a.Status != "ready" || a.Profile.Port != 443 {
		t.Fatal(a)
	}
	stale := binding
	if _, e = catalog.UpdateBinding(ctx, s, stale, false); e == nil {
		t.Fatal("stale binding accepted")
	}
	a.InboundID = &xh
	if _, e = catalog.SaveNode(ctx, s, a); e == nil {
		t.Fatal("node editor reassigned inbound")
	}
	var memberCount, after int
	s.DB.QueryRow("SELECT count(*) FROM node_group_members WHERE group_id=?", group).Scan(&memberCount)
	s.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&after)
	if memberCount != 2 || after != credentials {
		t.Fatal("group or core clients changed", memberCount, after)
	}
	scan(`{"inbounds":[]}`)
	a, _ = catalog.ResolveDefinition(ctx, s.DB, a.ID)
	if a.Status != "source_missing" {
		t.Fatal("missing source published", a)
	}
	s.DB.Exec("UPDATE catalog_inbounds SET retired=1 WHERE id=?", ws)
	scan(changed)
	var duplicate int
	s.DB.QueryRow("SELECT count(*) FROM catalog_inbounds WHERE instance_id=1").Scan(&duplicate)
	if duplicate != 2 {
		t.Fatal("retirement created another inbound", duplicate)
	}
	if _, e = catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: ws, Name: "retired", Enabled: true, Profile: ep.Profile}); e == nil {
		t.Fatal("retired inbound accepted entry")
	}
}

func TestConnectionInvalidInputIsAtomic(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "source.json")
	os.WriteFile(path, []byte(`{"inbounds":[{"tag":"in","port":10000,"protocol":"vless","settings":{"clients":[]},"streamSettings":{"network":"ws","security":"tls"}}]}`), 0600)
	discovery.Scan(ctx, s, core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}, root)
	var inbound string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds").Scan(&inbound)
	var before int
	s.DB.QueryRow("SELECT count(*) FROM node_endpoints").Scan(&before)
	for _, profile := range []map[string]any{{"address": "https://bad.example", "port": 443}, {"address": "999.999.999.999", "port": 443}, {"address": "edge.example", "port": 65536}, {"address": "edge.example", "port": 443, "uuid": "forbidden"}, {"address": "edge.example", "port": 443}} {
		if _, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "bad", Profile: profile, Enabled: true}); e == nil {
			t.Fatal("invalid or incomplete input saved", profile)
		}
	}
	var after int
	s.DB.QueryRow("SELECT count(*) FROM node_endpoints").Scan(&after)
	if before != after {
		t.Fatal("failed save left orphan endpoints", before, after)
	}
}

func TestExplicitNodeMovePreservesGroupAndSubscriptionAndPausesPublication(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "move.json")
	os.WriteFile(path, []byte(`{"inbounds":[{"tag":"a","port":10443,"protocol":"vless","settings":{"clients":[]}},{"tag":"b","port":11443,"protocol":"trojan","settings":{"clients":[]}}]}`), 0600)
	if _, e := discovery.Scan(ctx, s, core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}, root); e != nil {
		t.Fatal(e)
	}
	var a, b string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='a'").Scan(&a)
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='b'").Scan(&b)
	old, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: a, Name: "old", Enabled: true, Profile: map[string]any{"address": "old.example.com", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	target, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: b, Name: "target", Enabled: true, Profile: map[string]any{"address": "new.example.com", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	group, e := catalog.SaveGroup(ctx, s, catalog.Group{Name: "g", Enabled: true, NodeIDs: []string{old.NodeID}})
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{"INSERT INTO subscribers(id,name,created_at,updated_at) VALUES('u','u','old','old')", "INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,format,enabled,created_at,updated_at) VALUES('sub','u','s','existing-token-hash','mihomo',0,'old','old')"} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	s.DB.Exec("INSERT INTO catalog_subscription_nodes VALUES('sub',?,0,1)", old.NodeID)
	node, _ := catalog.ResolveDefinition(ctx, s.DB, old.NodeID)
	if _, e = catalog.MoveNode(ctx, s, node.ID, target.BindingID, node.Revision, false); e == nil {
		t.Fatal("unconfirmed migration")
	}
	if _, e = catalog.MoveNode(ctx, s, node.ID, target.BindingID, "stale", true); e == nil {
		t.Fatal("stale migration")
	}
	moved, e := catalog.MoveNode(ctx, s, node.ID, target.BindingID, node.Revision, true)
	if e != nil || moved.ID != old.NodeID || moved.Status != "disabled" || *moved.InboundID != b || moved.Profile.Protocol != "trojan" || moved.Profile.Address != "new.example.com" {
		t.Fatal(moved, e)
	}
	var member, assigned, token string
	s.DB.QueryRow("SELECT node_id FROM node_group_members WHERE group_id=?", group).Scan(&member)
	s.DB.QueryRow("SELECT node_id FROM catalog_subscription_nodes WHERE subscription_id='sub'").Scan(&assigned)
	s.DB.QueryRow("SELECT token_hash FROM catalog_subscriptions WHERE id='sub'").Scan(&token)
	if member != old.NodeID || assigned != old.NodeID || token != "existing-token-hash" {
		t.Fatal("migration rewrote group or subscription")
	}
}
