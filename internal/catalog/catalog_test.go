package catalog_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
	"traffic-manager-lite/internal/storage"
)

func fixture(t *testing.T) (*storage.Store, string) {
	t.Helper()
	root := t.TempDir()
	s, e := storage.Open(filepath.Join(root, "db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','xray','localhost:10085','now','now')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	return s, root
}
func TestCatalogDiscoveryCopiesEndpointsAndTriState(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "config.json")
	config := `{"inbounds":[{"tag":"ws","port":10443,"protocol":"vless","settings":{"clients":[{"email":"alice","id":"11111111-1111-4111-8111-111111111111"},{"email":"bob","id":"22222222-2222-4222-8222-222222222222"}]},"streamSettings":{"network":"ws","security":"tls","wsSettings":{"path":"/ws"}}}]}`
	os.WriteFile(path, []byte(config), 0600)
	inst := core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}
	if _, e := discovery.Scan(ctx, s, inst, root); e != nil {
		t.Fatal(e)
	}
	nodes, e := catalog.Nodes(ctx, s.DB)
	if e != nil || len(nodes) != 1 {
		t.Fatal(nodes, e)
	}
	n := nodes[0]
	ep, e := catalog.SaveEndpoint(ctx, s, catalog.Endpoint{Name: "shared", Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com"}})
	if e != nil {
		t.Fatal(e)
	}
	n.EndpointID = &ep
	n.Overrides = map[string]catalog.Override{"host": {Mode: "clear"}, "path": {Mode: "set", Value: "/custom"}}
	if _, e = catalog.SaveNode(ctx, s, n); e != nil {
		t.Fatal(e)
	}
	n, e = catalog.ResolveDefinition(ctx, s.DB, n.ID)
	if e != nil || n.Profile.Host != "" || n.Profile.Path != "/custom" || n.Sources["host"] != "node_clear" {
		t.Fatal(n, e)
	}
	n.ID = ""
	n.Name = "second"
	if _, e = catalog.SaveNode(ctx, s, n); e != nil {
		t.Fatal(e)
	}
	if e = catalog.Delete(ctx, s, "endpoints", ep); e != catalog.ErrReferenced {
		t.Fatal("shared endpoint was deleted", e)
	}
	os.WriteFile(path, []byte(`{"inbounds":[]}`), 0600)
	if _, e = discovery.Scan(ctx, s, inst, root); e != nil {
		t.Fatal(e)
	}
	nodes, e = catalog.Nodes(ctx, s.DB)
	if e != nil || len(nodes) != 2 || nodes[0].Status != "source_missing" {
		t.Fatal(nodes, e)
	}
	var raw string
	if e = s.DB.QueryRow("SELECT profile_json FROM node_profiles LIMIT 1").Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var saved map[string]any
	json.Unmarshal([]byte(raw), &saved)
	if _, ok := saved["uuid"]; ok {
		t.Fatal("credential persisted in node")
	}
}
func TestCatalogRejectsCredentialOverride(t *testing.T) {
	s, _ := fixture(t)
	n := catalog.Node{Name: "manual", Enabled: true, Profile: core.NodeProfile{Protocol: "vless"}, Overrides: map[string]catalog.Override{"uuid": {Mode: "set", Value: "secret"}}}
	if _, e := catalog.SaveNode(context.Background(), s, n); e == nil {
		t.Fatal("credential override accepted")
	}
}

func TestSyncDoesNotStealAnInboundThatStillExists(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "config.json")
	inbound := `{"tag":"existing","port":10443,"protocol":"vless","settings":{"clients":[]},"streamSettings":{"network":"ws"}}`
	inst := core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}
	scan := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := discovery.Scan(ctx, s, inst, root); err != nil {
			t.Fatal(err)
		}
	}
	scan(`{"inbounds":[` + inbound + `]}`)
	var original string
	if err := s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='existing'").Scan(&original); err != nil {
		t.Fatal(err)
	}
	// A new identical inbound appears before the unchanged source. It is not a rename.
	scan(`{"inbounds":[` + strings.Replace(inbound, "existing", "new", 1) + `,` + inbound + `]}`)
	var current string
	var count int
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='existing'").Scan(&current)
	s.DB.QueryRow("SELECT count(*) FROM catalog_inbounds WHERE present=1").Scan(&count)
	if current != original || count != 2 {
		t.Fatalf("source identity stolen: %s != %s, count=%d", current, original, count)
	}
}
