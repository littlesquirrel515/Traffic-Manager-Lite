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
	"traffic-manager-lite/internal/inboundidentity"
)

func TestHistoricalMergePreservesEndpointsAndFreedTagCanReappear(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "merge.json")
	body := `{"inbounds":[{"tag":"current","port":10443,"protocol":"vless","settings":{"clients":[]}}]}`
	scan := func(b string) error {
		os.WriteFile(path, []byte(b), 0600)
		_, e := discovery.Scan(ctx, s, core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}, root)
		return e
	}
	if e := scan(body); e != nil {
		t.Fatal(e)
	}
	var primary, hash string
	s.DB.QueryRow("SELECT id,config_hash FROM catalog_inbounds").Scan(&primary, &hash)
	_, e := s.DB.Exec(`INSERT INTO catalog_inbounds(id,instance_id,source_ref,tag,protocol,listen_port,profile_json,structure_hash,config_hash,present,discovered_at) SELECT 'duplicate',instance_id,source_ref,'old',protocol,listen_port,profile_json,structure_hash,config_hash,0,discovered_at FROM catalog_inbounds WHERE id=?`, primary)
	if e != nil {
		t.Fatal(e)
	}
	s.DB.Exec(`INSERT INTO inbound_identity_aliases SELECT instance_id,source_ref,tag,protocol,id,'now','now' FROM catalog_inbounds WHERE id='duplicate'`)
	ep, e := catalog.SaveEndpoint(ctx, s, catalog.Endpoint{Name: "separate entry", Enabled: true, Profile: map[string]any{"address": "cf.example.com", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	secondary := "duplicate"
	node, e := catalog.SaveNode(ctx, s, catalog.Node{Name: "historical multi entry", InboundID: &secondary, EndpointID: &ep, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = inboundidentity.Merge(ctx, s, secondary, primary, hash, hash, "fixture-backup"); e == nil {
		t.Fatal("missing primary accepted")
	}
	if e = inboundidentity.Merge(ctx, s, primary, secondary, hash, hash, "fixture-backup"); e != nil {
		t.Fatal(e)
	}
	n, e := catalog.ResolveDefinition(ctx, s.DB, node)
	if e != nil || *n.InboundID != primary || *n.EndpointID != ep {
		t.Fatal("multiple entry lost", n, e)
	}
	if e = scan(strings.Replace(body, "current", "old", 1)); e != nil {
		t.Fatal("retired tag blocked canonical identity", e)
	}
	var id string
	s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE present=1").Scan(&id)
	if id != primary {
		t.Fatal("canonical identity duplicated")
	}
}
