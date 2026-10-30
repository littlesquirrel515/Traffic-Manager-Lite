package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscription"
)

func TestSubscriberWorkflowIsAdminOnlyAndInboundWritesAreClosed(t *testing.T) {
	a, h := testAPI(t)
	ctx := context.Background()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','hysteria2','http://127.0.0.1:1','now','now')`} {
		if _, e := a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	inbound, e := remote.SaveManual(ctx, a.Store, remote.Inbound{InstanceID: 1, Tag: "hy", Profile: core.NodeProfile{Protocol: "hysteria2", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	credential, e := remote.RegisterClient(ctx, a.Store, remote.Client{InboundID: inbound, Reference: "native", Password: "private-native-client-password", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"catalog/native-clients", "catalog/subscriber-provisioning", "catalog/subscriber-node-grants", "catalog/subscriber-node-assignments", "catalog/subscriber-node-requests", "catalog/subscriber-node-requests/absent/tasks"} {
		if r := call(t, h, "GET", "/api/v1/"+path, nil, false); r.Code != 401 {
			t.Fatal("workflow exposed")
		}
		r := call(t, h, "GET", "/api/v1/"+path, nil, true)
		if r.Code != 200 || strings.Contains(r.Body.String(), "private-native-client-password") || strings.Contains(r.Body.String(), "desired_sealed") {
			t.Fatal("workflow metadata leaked credentials")
		}
	}
	for _, path := range []string{"catalog/subscriber-node-requests/preview", "catalog/subscriber-node-requests", "catalog/subscriber-node-requests/absent/retry"} {
		if r := call(t, h, "POST", "/api/v1/"+path, map[string]any{}, false); r.Code != 401 {
			t.Fatal("node workflow exposed", path, r.Code)
		}
	}
	if r := call(t, h, "POST", "/api/v1/catalog/inbounds/"+inbound+"/runtime-users", map[string]any{}, true); r.Code != 409 {
		t.Fatal("inbound can independently mutate clients")
	}
	if r := call(t, h, "POST", "/api/v1/catalog/subscribers", map[string]any{"name": "empty user"}, true); r.Code != 409 {
		t.Fatal("subscriber can bypass provisioning")
	}
	request := map[string]any{"request_id": "api-native-workflow-001", "subscriber": map[string]any{"name": "Native user", "enabled": true}, "mode": "native", "inbound_id": inbound, "credential_id": credential, "confirmed": true}
	r := call(t, h, "POST", "/api/v1/catalog/subscriber-provisioning", request, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var result map[string]any
	json.Unmarshal(r.Body.Bytes(), &result)
	if result["status"] != "ready" {
		t.Fatal("native adoption not ready")
	}
	request["request_id"] = "api-unsupported-workflow"
	request["mode"] = "managed"
	request["credential_id"] = ""
	if r := call(t, h, "POST", "/api/v1/catalog/subscriber-provisioning", request, true); r.Code == 200 || r.Code == 202 {
		t.Fatal("unsupported core pretended to provision")
	}
	var n int
	a.Store.DB.QueryRow("SELECT count(*) FROM subscribers").Scan(&n)
	if n != 1 {
		t.Fatal("unsupported create made phantom subscriber")
	}
}

func TestNodeAssignmentsRecallLegacySelectionsWithoutExpandingInboundAuthority(t *testing.T) {
	a, h := testAPI(t)
	ctx := context.Background()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','127.0.0.1:1','now','now')`} {
		if _, e := a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	in, e := remote.SaveManual(ctx, a.Store, remote.Inbound{InstanceID: 1, Tag: "shared", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	cred, e := remote.RegisterClient(ctx, a.Store, remote.Client{InboundID: in, Reference: "native", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	user, e := subscription.SaveSubscriber(ctx, a.Store, subscription.Subscriber{Name: "Legacy", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = subscription.Bind(ctx, a.Store, subscription.Binding{SubscriberID: user, InboundID: &in, CredentialID: cred, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for _, name := range []string{"direct", "grouped", "not-selected"} {
		n, err := catalog.CreateConnection(ctx, a.Store, catalog.Connection{InboundID: in, Name: name, Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.NodeID)
	}
	group, e := catalog.SaveGroup(ctx, a.Store, catalog.Group{Name: "Group", Enabled: true, NodeIDs: []string{ids[0], ids[1]}})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = subscription.SaveCatalogSubscription(ctx, a.Store, subscription.CatalogDefinition{Name: "Existing", SubscriberID: user, Format: "raw", Enabled: true, NodeIDs: []string{ids[0]}, GroupIDs: []string{group}}); e != nil {
		t.Fatal(e)
	}
	r := call(t, h, "GET", "/api/v1/catalog/subscriber-node-assignments?subscriber_id="+user, nil, true)
	var rows []subscription.NodeAssignment
	if e = json.Unmarshal(r.Body.Bytes(), &rows); e != nil || r.Code != 200 || len(rows) != 2 {
		t.Fatal(r.Code, r.Body.String(), e)
	}
	for _, n := range rows {
		if n.NodeID == ids[2] || n.Explicit {
			t.Fatal("inferred or wrote new authorization", n)
		}
	}
	var count int
	a.Store.DB.QueryRow("SELECT count(*) FROM subscriber_node_grants").Scan(&count)
	if count != 0 {
		t.Fatal("read-only recall changed grants")
	}
	// New v2.1 users must never inherit a group's later members in this view.
	a.Store.DB.Exec("UPDATE subscribers SET node_authorization_required=1 WHERE id=?", user)
	a.Store.DB.Exec("INSERT INTO subscriber_node_grants VALUES(?,?,?,'now')", user, ids[0], in)
	rows, e = subscription.NodeAssignments(ctx, a.Store.DB, user)
	if e != nil || len(rows) != 1 || rows[0].NodeID != ids[0] || !rows[0].Explicit {
		t.Fatal(rows, e)
	}
}
