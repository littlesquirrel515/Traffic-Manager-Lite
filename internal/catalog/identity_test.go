package catalog_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
	"traffic-manager-lite/internal/inboundidentity"
)

func TestConfirmedTagExchangeKeepsBothIdentities(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "swap.json")
	inst := core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}
	config := func(swapped bool) string {
		a, b := "a", "b"
		if swapped {
			a, b = b, a
		}
		return fmt.Sprintf(`{"inbounds":[{"tag":%q,"port":10443,"protocol":"vless","settings":{"clients":[]},"streamSettings":{"network":"ws","wsSettings":{"path":"/a"}}},{"tag":%q,"port":10444,"protocol":"vless","settings":{"clients":[]},"streamSettings":{"network":"ws","wsSettings":{"path":"/b"}}}]}`, a, b)
	}
	scan := func(b string) error {
		os.WriteFile(path, []byte(b), 0600)
		_, e := discovery.Scan(ctx, s, inst, root)
		return e
	}
	if e := scan(config(false)); e != nil {
		t.Fatal(e)
	}
	var a, b string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='a'").Scan(&a)
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='b'").Scan(&b)
	if e := scan(config(true)); e != nil {
		t.Fatal(e)
	}
	pending, e := s.Rows(ctx, "SELECT id,tag,config_hash FROM inbound_identity_pending WHERE status='needs_review'")
	if e != nil || len(pending) != 2 {
		t.Fatal(pending, e)
	}
	for _, p := range pending {
		target := a
		if p["tag"] == "a" {
			target = b
		}
		if e = inboundidentity.Resolve(ctx, s, p["id"].(string), target, p["config_hash"].(string), false); e != nil {
			t.Fatal(e)
		}
	}
	if e = scan(config(true)); e != nil {
		t.Fatal("confirmed exchange failed", e)
	}
	var newA, newB string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='a'").Scan(&newA)
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='b'").Scan(&newB)
	if newA != b || newB != a {
		t.Fatal("identities copied or stolen")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM node_profiles").Scan(&n)
	if n != 2 {
		t.Fatal("nodes duplicated", n)
	}
}

func TestStableRenameProtectsLegacyNodesCredentialsAndEndpoint(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "config.json")
	body := `{"inbounds":[{"tag":"old","port":10443,"protocol":"vless","settings":{"clients":[{"email":"alice","id":"11111111-1111-4111-8111-111111111111"}]},"streamSettings":{"network":"ws","wsSettings":{"path":"/old"}}}]}`
	inst := core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}
	scan := func(b string) {
		t.Helper()
		os.WriteFile(path, []byte(b), 0600)
		if _, e := discovery.Scan(ctx, s, inst, root); e != nil {
			t.Fatal(e)
		}
	}
	scan(body)
	var inbound, credential string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds").Scan(&inbound)
	s.DB.QueryRow("SELECT id FROM credential_material").Scan(&credential)
	nodes, _ := catalog.Nodes(ctx, s.DB)
	n := nodes[0]
	endpoint, e := catalog.SaveEndpoint(ctx, s, catalog.Endpoint{Name: "CF", Enabled: true, Profile: map[string]any{"address": "cf.example.com", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	n.EndpointID = &endpoint
	n.Overrides = map[string]catalog.Override{"path": {Mode: "set", Value: "/manual"}}
	catalog.SaveNode(ctx, s, n)
	n.ID = ""
	n.Name = "second"
	catalog.SaveNode(ctx, s, n)
	scan(strings.Replace(body, `"tag":"old"`, `"tag":"new"`, 1))
	for table, want := range map[string]int{"catalog_inbounds": 1, "credential_material": 1, "node_profiles": 2, "inbound_identity_history": 1} {
		var got int
		s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&got)
		if got != want {
			t.Fatalf("%s %d != %d", table, got, want)
		}
	}
	var actual string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds").Scan(&actual)
	if actual != inbound {
		t.Fatal("inbound ID changed")
	}
	s.DB.QueryRow("SELECT id FROM credential_material").Scan(&actual)
	if actual != credential {
		t.Fatal("credential ID changed")
	}
	scan(strings.Replace(strings.Replace(body, `"tag":"old"`, `"tag":"new"`, 1), `"path":"/old"`, `"path":"/changed"`, 1))
	n, e = catalog.ResolveDefinition(ctx, s.DB, nodes[0].ID)
	if e != nil || n.Profile.Path != "/manual" || *n.EndpointID != endpoint {
		t.Fatal("manual endpoint/override lost", e)
	}
	// A simultaneous tag, port and path change needs manual evidence, not a new Node.
	changed := strings.Replace(strings.Replace(strings.Replace(body, `"tag":"old"`, `"tag":"third"`, 1), "10443", "11443", 1), "/old", "/third", 1)
	scan(changed)
	var pending, revision string
	s.DB.QueryRow("SELECT id,config_hash FROM inbound_identity_pending WHERE status='needs_review'").Scan(&pending, &revision)
	if pending == "" {
		t.Fatal("ambiguous modification not staged")
	}
	if e = inboundidentity.Resolve(ctx, s, pending, inbound, revision, false); e != nil {
		t.Fatal(e)
	}
	scan(changed)
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE present=1").Scan(&actual)
	if actual != inbound {
		t.Fatal("confirmed identity lost")
	}
}
