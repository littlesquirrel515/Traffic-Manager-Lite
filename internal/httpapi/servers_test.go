package httpapi

import (
	"context"
	"strings"
	"testing"
)

func TestServersCRUDPreservesLinkedInstances(t *testing.T) {
	a, h := testAPI(t)
	create := map[string]any{"name": "VPS", "address": "example.com", "enabled": true}
	if r := call(t, h, "POST", "/api/v1/servers", create, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "GET", "/api/v1/servers/1", nil, false); r.Code != 401 {
		t.Fatal("unprotected server")
	}
	if r := call(t, h, "PATCH", "/api/v1/servers/1", map[string]any{"name": "Updated"}, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "GET", "/api/v1/servers/1", nil, true); r.Code != 200 || !strings.Contains(r.Body.String(), "Updated") || !strings.Contains(r.Body.String(), "example.com") {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "POST", "/api/v1/instances", map[string]any{"name": "core1", "server_id": 1, "core_type": "v2fly", "api_endpoint": "core1:10085", "enabled": true}, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "DELETE", "/api/v1/servers/1", nil, true); r.Code != 409 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call(t, h, "PATCH", "/api/v1/servers/1", map[string]any{"enabled": false}, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if e := a.Scheduler.Collect(context.Background(), 1); e == nil || !strings.Contains(e.Error(), "服务器已停用") {
		t.Fatalf("disabled server collected: %v", e)
	}
	if r := call(t, h, "POST", "/api/v1/servers", create, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "DELETE", "/api/v1/servers/2", nil, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := call(t, h, "DELETE", "/api/v1/servers/2", nil, true); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call(t, h, "PATCH", "/api/v1/servers/99", map[string]any{"name": "missing"}, true); r.Code != 404 {
		t.Fatal(r.Code)
	}
	if r := call(t, h, "PATCH", "/api/v1/servers/1", map[string]any{"name": " "}, true); r.Code != 400 {
		t.Fatal(r.Code)
	}
}

func TestStrictPolicyFailsAtInstanceSave(t *testing.T) {
	a, _ := testAPI(t)
	a.Config.AllowedTargets = []string{"xray"}
	h := a.Handler()
	r := call(t, h, "POST", "/api/v1/instances", map[string]any{"name": "xray1", "server_id": 1, "core_type": "xray", "api_endpoint": "xray1:10085", "enabled": true}, true)
	if r.Code != 400 || !strings.Contains(r.Body.String(), "TML_ALLOWED_TARGETS") {
		t.Fatal(r.Code, r.Body.String())
	}
}
