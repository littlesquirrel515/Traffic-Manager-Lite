package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/subscription"
)

func TestSubscriptionPreviewAndPublicEmptyResponse(t *testing.T) {
	a, h := testAPI(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := a.Store.DB.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'server','localhost','old','old')")
	exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'instance','xray','localhost:10085','old','old')")
	exec("INSERT INTO users(id,display_name,created_at,updated_at) VALUES(1,'user','old','old')")
	p := core.NodeProfile{Name: "node", Protocol: "trojan", Address: "example.com", Port: 443, Password: "private-client-password", TLS: true, SNI: "example.com", Enabled: true}
	b, _ := json.Marshal(p)
	exec("INSERT INTO nodes(id,instance_id,inbound_tag,user_id,protocol,discovered_json,effective_json,source_hash,updated_at) VALUES('a',1,'in',1,'trojan',?,?,'hash','old')", string(b), string(b))
	d := subscription.Definition{Name: "bundle", Scope: "bundle", Format: "singbox", NodeIDs: []string{"a"}, Enabled: true}
	if r := call(t, h, "POST", "/api/v1/subscriptions/preview", d, false); r.Code != 401 {
		t.Fatal("preview exposed", r.Code)
	}
	r := call(t, h, "POST", "/api/v1/subscriptions/preview", d, true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"usable":1`) || strings.Contains(r.Body.String(), p.Password) {
		t.Fatal(r.Code, r.Body.String())
	}
	r = call(t, h, "POST", "/api/v1/subscriptions", d, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var saved struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if e := json.Unmarshal(r.Body.Bytes(), &saved); e != nil {
		t.Fatal(e)
	}
	url := "/sub/singbox/" + saved.Token
	r = call(t, h, "GET", url, nil, false)
	if r.Code != 200 || !strings.Contains(r.Body.String(), p.Password) || !strings.Contains(r.Header().Get("Cache-Control"), "no-store") {
		t.Fatal(r.Code, r.Body.String())
	}
	exec("UPDATE nodes SET present=0 WHERE id='a'")
	r = call(t, h, "GET", url, nil, false)
	if r.Code != 422 || !strings.Contains(r.Body.String(), "没有可用节点") || strings.Contains(r.Body.String(), p.Password) {
		t.Fatal("empty response hidden", r.Code, r.Body.String())
	}
	r = call(t, h, "POST", "/api/v1/subscriptions/preview", d, true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"usable":0`) {
		t.Fatal(r.Code, r.Body.String())
	}
	r = call(t, h, "POST", "/api/v1/subscriptions", d, true)
	if r.Code != 400 {
		t.Fatal("empty subscription saved", r.Code, r.Body.String())
	}
	if r = call(t, h, "GET", "/sub/singbox/"+strings.Repeat("x", 43), nil, false); r.Code != 404 {
		t.Fatal("invalid token exposed state", r.Code)
	}
}
