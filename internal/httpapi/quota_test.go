package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
	"traffic-manager-lite/internal/subscription"
)

func TestPublicSubscriberQuotaHeadersUpdatesExpiryAndAuthorization(t *testing.T) {
	a, h := testAPI(t)
	ctx := context.Background()
	for _, sql := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','localhost','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','localhost:10085','now','now')`} {
		if _, e := a.Store.DB.Exec(sql); e != nil {
			t.Fatal(e)
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	os.WriteFile(file, []byte(`{"inbounds":[{"tag":"xh","protocol":"vless","port":12345,"settings":{"clients":[{"email":"alice","id":"11111111-1111-4111-8111-111111111111"}]},"streamSettings":{"network":"xhttp","security":"tls","xhttpSettings":{"path":"/api/xh","mode":"packet-up"}}}]}`), 0600)
	if _, e := discovery.Scan(ctx, a.Store, core.Instance{ID: 1, CoreType: "xray", ConfigPath: file}, dir); e != nil {
		t.Fatal(e)
	}
	nodes, e := catalog.Nodes(ctx, a.Store.DB)
	if e != nil || len(nodes) != 1 {
		t.Fatal(e)
	}
	n := nodes[0]
	n.Overrides = map[string]catalog.Override{"address": {Mode: "set", Value: "edge.example.com"}, "port": {Mode: "set", Value: 443}, "sni": {Mode: "set", Value: "tls.example.com"}, "host": {Mode: "set", Value: "host.example.com"}}
	if _, e = catalog.SaveNode(ctx, a.Store, n); e != nil {
		t.Fatal(e)
	}
	now := time.Now().Add(-time.Minute)
	apply := func(at time.Time, up, down int64) {
		t.Helper()
		if e := a.Store.Apply(ctx, 1, []core.TrafficRecord{{Scope: "user", UserKey: "alice", Source: "xray", CounterMode: "cumulative", CollectedAt: at, UploadBytes: up, DownloadBytes: down}}); e != nil {
			t.Fatal(e)
		}
	}
	apply(now, 0, 0)
	subscriber, e := subscription.SaveSubscriber(ctx, a.Store, subscription.Subscriber{Name: "quota", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	var cred string
	a.Store.DB.QueryRow("SELECT id FROM credential_material LIMIT 1").Scan(&cred)
	if _, e = subscription.Bind(ctx, a.Store, subscription.Binding{SubscriberID: subscriber, InboundID: n.InboundID, CredentialID: cred, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	_, token, e := subscription.SaveCatalogSubscription(ctx, a.Store, subscription.CatalogDefinition{Name: "quota", SubscriberID: subscriber, Enabled: true, Format: "mihomo", NodeIDs: []string{n.ID}})
	if e != nil {
		t.Fatal(e)
	}
	apply(now.Add(time.Second), 1073741824, 2147483648)
	q, _ := subscription.ReadQuota(ctx, a.Store.DB, subscriber)
	end, _ := time.Parse(time.RFC3339Nano, *q.ExpiresAt)
	want := "upload=1073741824; download=2147483648; total=10737418240; expire=" + strconv.FormatInt(end.Unix(), 10)
	for pos := 0; pos < 2; pos++ {
		r := call(t, h, "GET", "/sub/"+token+"?format=mihomo", nil, false)
		if r.Code != 200 || r.Header().Get("subscription-userinfo") != want || r.Header().Get("X-TML-Usage-Status") != "complete" || !strings.Contains(r.Body.String(), "xhttp-opts:") {
			t.Fatal(r.Code, r.Header())
		}
	}
	for _, ua := range []string{"clash-verge/v2.5.8", "mihomo/1.19.32", "unrecognized-client"} {
		request := httptest.NewRequest("GET", "/sub/"+token+"?format=mihomo", nil)
		request.Header.Set("User-Agent", ua)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != 200 || recorder.Header().Get("subscription-userinfo") != want {
			t.Fatal("UA dropped metadata", ua, recorder.Code)
		}
	}
	configBytes, _ := os.ReadFile(file)
	os.WriteFile(file, []byte(strings.ReplaceAll(string(configBytes), `"tag":"xh"`, `"tag":"renamed"`)), 0600)
	if _, e = discovery.Scan(ctx, a.Store, core.Instance{ID: 1, CoreType: "xray", ConfigPath: file}, dir); e != nil {
		t.Fatal(e)
	}
	refreshed, e := catalog.Nodes(ctx, a.Store.DB)
	if e != nil || len(refreshed) != 1 || refreshed[0].ID != n.ID {
		t.Fatal("tag rename changed node", e)
	}
	renameResult := call(t, h, "GET", "/sub/"+token, nil, false)
	if renameResult.Code != 200 || !strings.Contains(renameResult.Body.String(), "/api/xh") {
		t.Fatal("renamed XHTTP cannot render", renameResult.Code)
	}
	unowned, e := subscription.SaveSubscriber(ctx, a.Store, subscription.Subscriber{Name: "no credential", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	unauthorizedPreview := call(t, h, "POST", "/api/v1/catalog/nodes/"+n.ID+"/preview", map[string]string{"subscriber_id": unowned}, true)
	if unauthorizedPreview.Code != 200 || strings.Contains(unauthorizedPreview.Body.String(), "xhttp-opts") || !strings.Contains(unauthorizedPreview.Body.String(), "credential") {
		t.Fatal("unbound subscriber gained node", unauthorizedPreview.Body.String())
	}
	if r := call(t, h, "GET", "/api/v1/catalog/subscribers/"+subscriber+"/quota", nil, false); r.Code != 401 {
		t.Fatal("quota exposed")
	}
	q.Limit *= 2
	newEnd := end.AddDate(0, 1, 0).Format(time.RFC3339)
	q.ExpiresAt = &newEnd
	r := call(t, h, "PUT", "/api/v1/catalog/subscribers/"+subscriber+"/quota", subscription.QuotaUpdate{Quota: q}, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var updated subscription.Quota
	json.Unmarshal(r.Body.Bytes(), &updated)
	r = call(t, h, "GET", "/sub/"+token, nil, false)
	if r.Code != 200 || !strings.Contains(r.Header().Get("subscription-userinfo"), "total=21474836480;") {
		t.Fatal(r.Code, r.Header())
	}
	r = call(t, h, "PUT", "/api/v1/catalog/subscribers/"+subscriber+"/quota", subscription.QuotaUpdate{Quota: q}, true)
	if r.Code != 409 {
		t.Fatal("optimistic lock", r.Code)
	}
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	updated.ExpiresAt = &past
	r = call(t, h, "PUT", "/api/v1/catalog/subscribers/"+subscriber+"/quota", subscription.QuotaUpdate{Quota: updated}, true)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r = call(t, h, "GET", "/sub/"+token, nil, false)
	if r.Code != 403 || strings.Contains(r.Body.String(), "uuid:") || r.Header().Get("X-TML-Quota-Status") != "expired" {
		t.Fatal("expired grants config", r.Code, r.Body.String())
	}
	q, _ = subscription.ReadQuota(ctx, a.Store.DB, subscriber)
	r = call(t, h, "POST", "/api/v1/catalog/subscribers/"+subscriber+"/quota/reset", map[string]any{"quota_revision": q.Revision, "confirmed": true}, true)
	if r.Code != 200 {
		t.Fatal("reset failed", r.Code, r.Body.String())
	}
	var reset subscription.Quota
	json.Unmarshal(r.Body.Bytes(), &reset)
	if reset.Limit != q.Limit || reset.ExpiresAt == nil || *reset.ExpiresAt != *q.ExpiresAt || reset.Revision != q.Revision+1 {
		t.Fatal("reset changed policy", reset, q)
	}

}
