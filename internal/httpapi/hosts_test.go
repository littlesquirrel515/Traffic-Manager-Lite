package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
)

func TestHostsKeepSynchronizationOwnershipForInactiveSlots(t *testing.T) {
	a, h := testAPI(t)
	ctx := context.Background()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','127.0.0.1:1','now','now')`} {
		if _, err := a.Store.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	in, err := remote.SaveManual(ctx, a.Store, remote.Inbound{InstanceID: 1, Tag: "ws", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := catalog.CreateConnection(ctx, a.Store, catalog.Connection{InboundID: in, Name: "managed", Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	template, err := catalog.SaveEndpoint(ctx, a.Store, catalog.Endpoint{Name: "standalone", Enabled: true, Profile: map[string]any{"address": "template.example.com", "port": 443}})
	if err != nil {
		t.Fatal(err)
	}
	group, err := catalog.SaveGroup(ctx, a.Store, catalog.Group{Name: "SG", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.DB.Exec(`INSERT INTO preferred_sync_jobs VALUES('job','SG',?,?, '{}','{}',0,1,0,'now','now','ok')`, entry.NodeID, group); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.DB.Exec(`INSERT INTO preferred_sync_slots VALUES('job',0,?,?,?,'revision','{}',0)`, entry.NodeID, entry.EndpointID, entry.BindingID); err != nil {
		t.Fatal(err)
	}
	if r := call(t, h, "GET", "/api/v1/catalog/endpoints", nil, false); r.Code != 401 {
		t.Fatal(r.Code)
	}
	r := call(t, h, "GET", "/api/v1/catalog/endpoints", nil, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var rows []catalog.Endpoint
	if err = json.Unmarshal(r.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal(len(rows))
	}
	for _, e := range rows {
		if e.ID == entry.EndpointID {
			if e.Sync == nil || e.Sync.JobID != "job" || e.Sync.Name != "SG" || e.Sync.GroupID != group || e.Sync.Active || e.Sync.Enabled {
				t.Fatalf("inactive slot lost ownership: %+v", e.Sync)
			}
		} else if e.ID != template || e.Sync != nil {
			t.Fatalf("template misclassified: %+v", e)
		}
	}
}
