package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
)

func TestEndpointBatchAtomicityAndDuplicateNodeReferenceMigration(t *testing.T) {
	s, root := fixture(t)
	ctx := context.Background()
	path := filepath.Join(root, "x.json")
	os.WriteFile(path, []byte(`{"inbounds":[{"tag":"v","port":10443,"protocol":"vless","settings":{"clients":[]}}]}`), 0600)
	if _, e := discovery.Scan(ctx, s, core.Instance{ID: 1, CoreType: "xray", ConfigPath: path}, root); e != nil {
		t.Fatal(e)
	}
	ep, e := catalog.SaveEndpoint(ctx, s, catalog.Endpoint{Name: "edge", Enabled: true, Profile: map[string]any{"address": "cf.example.com", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	ep2, e := catalog.SaveEndpoint(ctx, s, catalog.Endpoint{Name: "second", Enabled: true, Profile: map[string]any{"address": "cf.example.com", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	off := false
	if e = catalog.BatchEndpoints(ctx, s, catalog.EndpointBatch{IDs: []string{ep, "missing"}, Enabled: &off, ConfirmShared: true}); e == nil {
		t.Fatal("partial batch allowed")
	}
	value, e := catalog.ReadEndpoint(ctx, s.DB, ep)
	if e != nil || !value.Enabled {
		t.Fatal("batch did not roll back", e)
	}
	nodes, e := catalog.Nodes(ctx, s.DB)
	if e != nil || len(nodes) != 1 {
		t.Fatal(nodes, e)
	}
	n := nodes[0]
	n.EndpointID = &ep
	if _, e = catalog.SaveNode(ctx, s, n); e != nil {
		t.Fatal(e)
	}
	primary := n.ID
	n.ID = ""
	n.Name = "copy"
	secondary, e := catalog.SaveNode(ctx, s, n)
	if e != nil {
		t.Fatal(e)
	}
	group, e := catalog.SaveGroup(ctx, s, catalog.Group{Name: "g", Enabled: true, NodeIDs: []string{primary, secondary}})
	if e != nil {
		t.Fatal(e)
	}
	a, _ := catalog.ResolveDefinition(ctx, s.DB, primary)
	b, _ := catalog.ResolveDefinition(ctx, s.DB, secondary)
	if e = catalog.MergeNodes(ctx, s, primary, secondary, "backup", "stale", b.Revision); e == nil {
		t.Fatal("stale preview accepted")
	}
	n.ID = secondary
	n.EndpointID = &ep2
	catalog.SaveNode(ctx, s, n)
	b, _ = catalog.ResolveDefinition(ctx, s.DB, secondary)
	if e = catalog.MergeNodes(ctx, s, primary, secondary, "backup", a.Revision, b.Revision); e == nil {
		t.Fatal("distinct endpoints merged")
	}
	n.EndpointID = &ep
	catalog.SaveNode(ctx, s, n)
	b, _ = catalog.ResolveDefinition(ctx, s.DB, secondary)
	if e = catalog.MergeNodes(ctx, s, primary, secondary, "backup", a.Revision, b.Revision); e != nil {
		t.Fatal(e)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM node_group_members WHERE group_id=?", group).Scan(&count)
	if count != 1 {
		t.Fatal("reference collision not deduplicated")
	}
	var integrity string
	s.DB.QueryRow("PRAGMA integrity_check").Scan(&integrity)
	if integrity != "ok" {
		t.Fatal(integrity)
	}
}
