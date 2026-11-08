package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscription"
)

func TestUnifiedLinkAdminRetrievalNegotiationAndRevocation(t *testing.T) {
	a, _ := testAPI(t)
	ctx := context.Background()
	key := filepath.Join(t.TempDir(), "managed.key")
	os.WriteFile(key, []byte(strings.Repeat("s", 32)), 0600)
	a.Config.ManagedKeyFile = key
	h := a.Handler()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','127.0.0.1:1','now','now')`} {
		if _, e := a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	in, e := remote.SaveManual(ctx, a.Store, remote.Inbound{InstanceID: 1, Tag: "trojan", Profile: core.NodeProfile{Protocol: "trojan", Transport: "tcp"}})
	if e != nil {
		t.Fatal(e)
	}
	node, e := catalog.CreateConnection(ctx, a.Store, catalog.Connection{InboundID: in, Name: "Edge", Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "origin.example.com"}})
	if e != nil {
		t.Fatal(e)
	}
	credential, e := remote.RegisterClient(ctx, a.Store, remote.Client{InboundID: in, Reference: "native", Password: "private-fixture-client-password", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	user, e := subscription.SaveSubscriber(ctx, a.Store, subscription.Subscriber{Name: "User", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = subscription.Bind(ctx, a.Store, subscription.Binding{SubscriberID: user, InboundID: &in, CredentialID: credential, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	id, legacy, e := subscription.SaveCatalogSubscription(ctx, a.Store, subscription.CatalogDefinition{Name: "Old", SubscriberID: user, Enabled: true, Format: "mihomo", NodeIDs: []string{node.NodeID}})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"catalog/subscribers/" + user + "/link", "catalog/subscriptions/" + id + "/link", "catalog/subscriber-node-edits/preview", "catalog/subscriber-node-edits", "catalog/subscriber-node-edits/absent/retry"} {
		if r := call(t, h, "POST", "/api/v1/"+path, map[string]any{}, false); r.Code != 401 {
			t.Fatal("management endpoint exposed", path, r.Code)
		}
	}
	r := call(t, h, "POST", "/api/v1/catalog/subscribers/"+user+"/link", map[string]any{}, true)
	var link subscription.AdminLink
	if e = json.Unmarshal(r.Body.Bytes(), &link); e != nil || r.Code != 200 || len(link.Token) != 43 || r.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("link retrieval failed", r.Code, e)
	}
	again := call(t, h, "POST", "/api/v1/catalog/subscribers/"+user+"/link", map[string]any{}, true)
	var second subscription.AdminLink
	json.Unmarshal(again.Body.Bytes(), &second)
	if second.Token != link.Token {
		t.Fatal("viewing link rotated it")
	}
	for ua, format := range map[string]string{"v2rayN/7.19.5": "base64", "Clash-Verge/v2.4.4": "mihomo", "mihomo/1.19.32": "mihomo", "Mozilla/5.0": "base64"} {
		req := httptest.NewRequest("GET", "/sub/"+link.Token, nil)
		req.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 || w.Header().Get("X-TML-Format") != format || w.Header().Get("Vary") != "User-Agent, Accept" {
			t.Fatal("wrong auto format", ua, w.Code, w.Header())
		}
		if format == "mihomo" && !strings.Contains(w.Body.String(), "proxy-groups:") {
			t.Fatal("Clash got a node-only or invalid profile")
		}
	}
	if r = call(t, h, "GET", "/sub/"+legacy, nil, false); r.Code != 200 {
		t.Fatal("legacy URL stopped working")
	}
	for _, path := range []string{"catalog/subscribers", "catalog/subscriptions", "catalog/subscriber-node-edits", "catalog/access-logs"} {
		r = call(t, h, "GET", "/api/v1/"+path, nil, true)
		if strings.Contains(r.Body.String(), link.Token) || strings.Contains(r.Body.String(), "token_sealed") {
			t.Fatal("list exposed token", path)
		}
	}
	// Node revocation filters every URL without rotating their tokens or touching auth.
	a.Store.DB.Exec("INSERT INTO subscriber_node_revocations VALUES(?,?,'now')", user, node.NodeID)
	for _, token := range []string{legacy, link.Token} {
		if r = call(t, h, "GET", "/sub/"+token, nil, false); r.Code != 422 {
			t.Fatal("revoked node published", r.Code)
		}
	}
}

func TestUnifiedSubscriptionDeletionAndResetRequireCorrectIntent(t *testing.T) {
	a, _ := testAPI(t)
	key := filepath.Join(t.TempDir(), "key")
	os.WriteFile(key, []byte(strings.Repeat("z", 32)), 0600)
	a.Config.ManagedKeyFile = key
	h := a.Handler()
	uid, e := subscription.SaveSubscriber(context.Background(), a.Store, subscription.Subscriber{Name: "Protected", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	read := func() subscription.AdminLink {
		t.Helper()
		r := call(t, h, "POST", "/api/v1/catalog/subscribers/"+uid+"/link", map[string]any{}, true)
		var link subscription.AdminLink
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &link) != nil {
			t.Fatal(r.Code, r.Body.String())
		}
		return link
	}
	first := read()
	if r := call(t, h, "DELETE", "/api/v1/catalog/subscriptions/"+first.SubscriptionID, nil, true); r.Code != 409 || !strings.Contains(r.Body.String(), "统一订阅") {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call(t, h, "POST", "/api/v1/catalog/subscriptions/"+first.SubscriptionID+"/rotate-token", map[string]any{}, true); r.Code != 409 {
		t.Fatal(r.Code)
	}
	if again := read(); again.Token != first.Token {
		t.Fatal("rejected actions changed link")
	}
	r := call(t, h, "POST", "/api/v1/catalog/subscriptions/"+first.SubscriptionID+"/rotate-token", map[string]any{"confirmed": true}, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if rotated := read(); rotated.Token == first.Token {
		t.Fatal("explicit reset did not replace token")
	}
	// Historical dangling mappings produce a domain message, never raw SQL errors.
	a.Store.DB.Exec("PRAGMA foreign_keys=OFF")
	a.Store.DB.Exec("DELETE FROM catalog_subscriptions WHERE id=?", first.SubscriptionID)
	r = call(t, h, "POST", "/api/v1/catalog/subscribers/"+uid+"/link", map[string]any{}, true)
	if r.Code != 409 || strings.Contains(r.Body.String(), "sql:") {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestSubscriberPreviewRequiresAdminAndDoesNotCreateDelivery(t *testing.T) {
	a, h := testAPI(t)
	uid, e := subscription.SaveSubscriber(context.Background(), a.Store, subscription.Subscriber{Name: "Readonly preview", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/catalog/subscribers/" + uid + "/preview"
	if r := call(t, h, "POST", path, map[string]any{}, false); r.Code != 401 {
		t.Fatal(r.Code)
	}
	r := call(t, h, "POST", path, map[string]any{}, true)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(r.Code, r.Body.String())
	}
	var count int
	a.Store.DB.QueryRow("SELECT count(*) FROM catalog_subscriptions").Scan(&count)
	if count != 0 {
		t.Fatal("preview created a token")
	}
}
