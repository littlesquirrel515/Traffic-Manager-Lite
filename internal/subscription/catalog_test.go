package subscription

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
	"traffic-manager-lite/internal/storage"
)

func catalogFixture(t *testing.T) (*storage.Store, catalog.Node, []string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	s, e := storage.Open(filepath.Join(dir, "db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','xray','localhost:10085','now','now')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"inbounds":[{"tag":"ws","port":10443,"protocol":"vless","settings":{"clients":[{"email":"alice","id":"11111111-1111-4111-8111-111111111111"},{"email":"bob","id":"22222222-2222-4222-8222-222222222222"}]},"streamSettings":{"network":"ws","security":"tls","wsSettings":{"path":"/ws"}}}]}`), 0600)
	if _, e = discovery.Scan(ctx, s, core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}, dir); e != nil {
		t.Fatal(e)
	}
	nodes, e := catalog.Nodes(ctx, s.DB)
	if e != nil {
		t.Fatal(e)
	}
	n := nodes[0]
	ep, e := catalog.SaveEndpoint(ctx, s, catalog.Endpoint{Name: "entry", Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com"}})
	if e != nil {
		t.Fatal(e)
	}
	n.EndpointID = &ep
	if _, e = catalog.SaveNode(ctx, s, n); e != nil {
		t.Fatal(e)
	}
	return s, n, []string{discovery.ID(1, "ws", "alice"), discovery.ID(1, "ws", "bob")}
}
func TestSubscribersShareNodeWithIsolatedCredentialsAndGroups(t *testing.T) {
	ctx := context.Background()
	s, n, creds := catalogFixture(t)
	group, e := catalog.SaveGroup(ctx, s, catalog.Group{Name: "region", Enabled: true, NodeIDs: []string{n.ID, n.ID}})
	if e != nil {
		t.Fatal(e)
	}
	for i, name := range []string{"A", "B"} {
		subscriber, e := SaveSubscriber(ctx, s, Subscriber{Name: name, Enabled: true})
		if e != nil {
			t.Fatal(e)
		}
		d := CatalogDefinition{Name: name, SubscriberID: subscriber, Format: "base64", Enabled: true, NodeIDs: []string{n.ID}, GroupIDs: []string{group}}
		preview, e := CatalogPreviewFor(ctx, s.DB, d)
		if e != nil || preview.Usable != 0 || preview.Selected != 1 || !strings.Contains(preview.Rejected[0].Reason, "credential_missing") {
			t.Fatal(preview, e)
		}
		if _, e = Bind(ctx, s, Binding{SubscriberID: subscriber, InboundID: n.InboundID, CredentialID: creds[i], Enabled: true}); e != nil {
			t.Fatal(e)
		}
		id, token, e := SaveCatalogSubscription(ctx, s, d)
		if e != nil {
			t.Fatal(e)
		}
		res, _, e := CatalogOutput(ctx, s, token, "")
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := base64.StdEncoding.DecodeString(string(res.Content))
		want, other := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
		if i == 1 {
			want, other = other, want
		}
		if !strings.Contains(string(raw), want) || strings.Contains(string(raw), other) || strings.Count(string(raw), "vless://") != 1 {
			t.Fatal("credentials mixed or duplicate output")
		}
		preview, e = CatalogPreviewFor(ctx, s.DB, d)
		if e != nil || strings.Contains(preview.Content, other) {
			t.Fatal("preview leaked credentials", e)
		}
		replacement, e := RotateCatalog(ctx, s, id)
		if e != nil {
			t.Fatal(e)
		}
		if _, _, e = CatalogOutput(ctx, s, token, ""); e == nil {
			t.Fatal("old token remains active")
		}
		if _, _, e = CatalogOutput(ctx, s, replacement, ""); e != nil {
			t.Fatal(e)
		}
		d.ID = id
		expiry := time.Now().Add(-time.Hour).Format(time.RFC3339)
		d.ExpiresAt = &expiry
		if _, _, e = SaveCatalogSubscription(ctx, s, d); e != nil {
			t.Fatal(e)
		}
		if _, _, e = CatalogOutput(ctx, s, replacement, ""); e == nil {
			t.Fatal("expired subscription accepted")
		}
		d.ExpiresAt = nil
		if _, _, e = SaveCatalogSubscription(ctx, s, d); e != nil {
			t.Fatal(e)
		}
		if _, e = SaveSubscriber(ctx, s, Subscriber{ID: subscriber, Name: name, Enabled: false}); e != nil {
			t.Fatal(e)
		}
		if _, _, e = CatalogOutput(ctx, s, replacement, ""); e == nil {
			t.Fatal("disabled subscriber accepted")
		}
	}
}

func TestRuntimeRemovalBlocksCredentialsWithoutDependingOnStatsOrOnline(t *testing.T) {
	ctx := context.Background()
	s, n, creds := catalogFixture(t)
	id, err := SaveSubscriber(ctx, s, Subscriber{Name: "runtime", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Bind(ctx, s, Binding{SubscriberID: id, InboundID: n.InboundID, CredentialID: creds[0], Enabled: true}); err != nil {
		t.Fatal(err)
	}
	d := CatalogDefinition{SubscriberID: id, NodeIDs: []string{n.ID}, Format: "mihomo"}
	check := func(want int) {
		t.Helper()
		nodes, _, _, e := ResolveCatalog(ctx, s.DB, d)
		if e != nil || len(nodes) != want {
			t.Fatalf("got %d, want %d: %v", len(nodes), want, e)
		}
	}
	// No Stats or Online records exist. Configured credentials remain usable.
	check(1)
	if _, err = s.DB.Exec(`INSERT INTO xray_clients(instance_id,inbound_tag,asset_key,email,source,present,observed_at) VALUES(1,'ws','runtime-alice','alice','runtime_api',0,?)`, storage.Stamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	check(0)
	if _, err = s.DB.Exec("UPDATE xray_clients SET present=1"); err != nil {
		t.Fatal(err)
	}
	check(1)
	n.Overrides = map[string]catalog.Override{"protocol": {Mode: "set", Value: "vmess"}}
	if _, err = catalog.SaveNode(ctx, s, n); err != nil {
		t.Fatal(err)
	}
	check(0)
}
