package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
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
	for _, path := range []string{"catalog/native-clients", "catalog/subscriber-provisioning"} {
		if r := call(t, h, "GET", "/api/v1/"+path, nil, false); r.Code != 401 {
			t.Fatal("workflow exposed")
		}
		r := call(t, h, "GET", "/api/v1/"+path, nil, true)
		if r.Code != 200 || strings.Contains(r.Body.String(), "private-native-client-password") || strings.Contains(r.Body.String(), "desired_sealed") {
			t.Fatal("workflow metadata leaked credentials")
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
