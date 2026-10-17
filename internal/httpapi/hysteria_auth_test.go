package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
)

func TestHysteriaHTTPAuthSecurityAndOfficialContract(t *testing.T) {
	a, h := testAPI(t)
	ctx := context.Background()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','hysteria2','http://127.0.0.1:1','now','now')`} {
		if _, e := a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	id, e := remote.SaveManual(ctx, a.Store, remote.Inbound{InstanceID: 1, Tag: "hy", Profile: core.NodeProfile{Protocol: "hysteria2", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = remote.RegisterClient(ctx, a.Store, remote.Client{InboundID: id, Reference: "native-id", Password: "native:private-password", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/catalog/hysteria-auth/" + id
	setup := map[string]any{"confirmed": true, "import_native": true, "allowed_cidrs": []string{"127.0.0.0/8"}}
	if r := call(t, h, "POST", path+"/prepare", setup, false); r.Code != 401 {
		t.Fatal("public setup")
	}
	unsafe := map[string]any{"confirmed": true, "allowed_cidrs": []string{"0.0.0.0/0"}}
	if r := call(t, h, "POST", path+"/prepare", unsafe, true); r.Code == 200 {
		t.Fatal("unrestricted callback sources")
	}
	r := call(t, h, "POST", path+"/prepare", setup, true)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var prepared struct {
		Path     string `json:"callback_path"`
		Revision int64  `json:"revision"`
	}
	if e = json.Unmarshal(r.Body.Bytes(), &prepared); e != nil {
		t.Fatal(e)
	}
	token := strings.TrimPrefix(prepared.Path, "/core-auth/hysteria2/")
	var hash string
	a.Store.DB.QueryRow("SELECT token_hash FROM hysteria_auth_backends").Scan(&hash)
	if len(token) != 64 || len(hash) != 64 || hash == token {
		t.Fatal("callback secret not independently hashed")
	}
	if r = call(t, h, "GET", "/api/v1/catalog/hysteria-auth", nil, false); r.Code != 401 {
		t.Fatal("backend metadata exposed")
	}
	r = call(t, h, "GET", "/api/v1/catalog/hysteria-auth", nil, true)
	if r.Code != 200 || strings.Contains(r.Body.String(), token) || strings.Contains(r.Body.String(), hash) || strings.Contains(r.Body.String(), "private-password") {
		t.Fatal("backend metadata leaked")
	}
	body := `{"addr":"203.0.113.4:12345","auth":"native:private-password","tx":123456}`
	callback := func(peer, payload string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", prepared.Path, strings.NewReader(payload))
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := callback("127.0.0.1:1", body); w.Code != 200 || strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal("unconfirmed backend allowed login")
	}
	state := map[string]any{"revision": prepared.Revision, "enabled": true, "confirmed": true, "acknowledged": true}
	if r = call(t, h, "PUT", path, state, true); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if w := callback("203.0.113.1:1", body); w.Code != 200 || strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal("trusted forwarded address")
	}
	if w := callback("127.0.0.1:1", body); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) || !strings.Contains(w.Body.String(), `"id":"native-id"`) || strings.Contains(w.Body.String(), "private-password") {
		t.Fatal("official contract failed")
	}
	for _, payload := range []string{`[]`, `{"auth":"x","tx":-1}`, body + `{}`, `{"auth":"` + strings.Repeat("x", 9000) + `"}`} {
		if w := callback("127.0.0.1:1", payload); w.Code != 400 {
			t.Fatal("invalid body accepted", w.Code)
		}
	}
	if r = call(t, h, "PUT", path, state, true); r.Code == 200 {
		t.Fatal("stale backend revision accepted")
	}
	var samples int
	a.Store.DB.QueryRow("SELECT count(*) FROM traffic_samples").Scan(&samples)
	if samples != 0 {
		t.Fatal("untrusted tx became traffic")
	}
}
