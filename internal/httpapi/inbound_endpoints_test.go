package httpapi

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
)

func TestInboundConnectionAPIAdministratorAtomicSaveAndReferenceProtection(t *testing.T) {
	a, h := testAPI(t)
	ctx := context.Background()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','localhost','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','localhost:10085','now','now')`} {
		if _, e := a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	root := t.TempDir()
	file := filepath.Join(root, "xray.json")
	source := `{"inbounds":[{"tag":"ws","port":10443,"protocol":"vless","settings":{"clients":[{"email":"alice","id":"11111111-1111-4111-8111-111111111111"}]},"streamSettings":{"network":"ws","security":"tls","wsSettings":{"path":"/ws"}}}]}`
	os.WriteFile(file, []byte(source), 0600)
	if _, e := discovery.Scan(ctx, a.Store, core.Instance{ID: 1, CoreType: "xray", ConfigPath: file}, root); e != nil {
		t.Fatal(e)
	}
	list := call(t, h, "GET", "/api/v1/catalog/inbound-management", nil, true)
	if list.Code != 200 {
		t.Fatal(list.Body.String())
	}
	var rows []map[string]any
	json.Unmarshal(list.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0]["client_count"] != float64(1) {
		t.Fatal(rows)
	}
	id := rows[0]["id"].(string)
	data := map[string]any{"inboundId": id, "name": "CF", "address": "203.0.113.10", "connectionPort": 443, "host": "http.example.com", "sni": "tls.example.com", "enabled": true}
	if r := call(t, h, "POST", "/api/v1/catalog/connections", data, false); r.Code != 401 {
		t.Fatal("creation not protected", r.Code)
	}
	saved := call(t, h, "POST", "/api/v1/catalog/connections", data, true)
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	var result catalog.ConnectionResult
	json.Unmarshal(saved.Body.Bytes(), &result)
	if result.NodeID == "" || result.BindingID == "" || result.ValidationStatus != "ready" || result.NetworkStatus != "unknown" {
		t.Fatal(result)
	}
	for _, route := range []string{"catalog/inbounds/" + id, "catalog/inbounds/" + id + "/clients", "catalog/inbounds/" + id + "/endpoints", "catalog/endpoints/" + result.EndpointID + "/references", "catalog/endpoint-bindings/" + result.BindingID, "catalog/migration-preview"} {
		r := call(t, h, "GET", "/api/v1/"+route, nil, true)
		if r.Code != 200 || strings.Contains(r.Body.String(), "11111111-1111-4111-8111-111111111111") {
			t.Fatal(route, r.Code, r.Body.String())
		}
	}
	repeat := call(t, h, "POST", "/api/v1/catalog/inbounds/"+id+"/endpoints", map[string]any{"endpoint_id": result.EndpointID, "enabled": true}, true)
	var again catalog.ConnectionResult
	json.Unmarshal(repeat.Body.Bytes(), &again)
	if repeat.Code != 200 || again != result {
		t.Fatal("template rebind not idempotent", repeat.Code, again, result)
	}
	if r := call(t, h, "DELETE", "/api/v1/catalog/endpoints/"+result.EndpointID, nil, true); r.Code != 409 {
		t.Fatal("referenced endpoint removed", r.Code)
	}
	if r := call(t, h, "DELETE", "/api/v1/catalog/inbounds/"+id, nil, true); r.Code != 409 {
		t.Fatal("inbound removed", r.Code)
	}
	if r := call(t, h, "PATCH", "/api/v1/catalog/inbounds/"+id, map[string]any{"retired": true, "confirmed": false}, true); r.Code == 200 {
		t.Fatal("unconfirmed retirement")
	}
	if r := call(t, h, "PATCH", "/api/v1/catalog/inbounds/"+id, map[string]any{"retired": true, "confirmed": true, "display_name": "Retired"}, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	node, _ := catalog.ResolveDefinition(ctx, a.Store.DB, result.NodeID)
	if node.Status != "retired" {
		t.Fatal(node)
	}
	actual, _ := os.ReadFile(file)
	if string(actual) != source {
		t.Fatal("endpoint operation modified core config")
	}
	var clients int
	a.Store.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&clients)
	if clients != 1 {
		t.Fatal("endpoint created client", clients)
	}
}
