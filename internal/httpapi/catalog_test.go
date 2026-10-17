package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

func TestCatalogPublicProfileAuthAuditAndRateLimit(t *testing.T) {
	a, h := testAPI(t)
	ctx := context.Background()
	now := storage.Stamp(time.Now())
	if _, e := a.Store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'remote','test',?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'remote','xray','127.0.0.1:19999',?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	inbound, e := remote.SaveManual(ctx, a.Store, remote.Inbound{InstanceID: 1, Tag: "manual", Profile: core.NodeProfile{Protocol: "trojan", Transport: "tcp"}})
	if e != nil {
		t.Fatal(e)
	}
	connection, e := catalog.CreateConnection(ctx, a.Store, catalog.Connection{InboundID: inbound, Name: "edge", Enabled: true, Profile: map[string]any{"address": "edge.example.com", "port": 443, "sni": "edge.example.com", "tls": true}})
	if e != nil {
		t.Fatal(e)
	}
	nodeID := connection.NodeID
	manual := map[string]any{"client_ref": "test-client", "password": "sensitive-fixture-auth", "confirmed": true}
	if r := call(t, h, "POST", "/api/v1/catalog/inbounds/"+inbound+"/clients", manual, false); r.Code != 401 {
		t.Fatal("credential management exposed")
	}
	r := call(t, h, "POST", "/api/v1/catalog/inbounds/"+inbound+"/clients", manual, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var credential struct {
		ID string `json:"id"`
	}
	json.Unmarshal(r.Body.Bytes(), &credential)
	subscriber, e := subscription.SaveSubscriber(ctx, a.Store, subscription.Subscriber{Name: "subscriber", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = subscription.Bind(ctx, a.Store, subscription.Binding{SubscriberID: subscriber, InboundID: &inbound, CredentialID: credential.ID, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	id, token, e := subscription.SaveCatalogSubscription(ctx, a.Store, subscription.CatalogDefinition{Name: "中文订阅", SubscriberID: subscriber, Enabled: true, Format: "mihomo", NodeIDs: []string{nodeID}})
	if e != nil {
		t.Fatal(e)
	}
	r = call(t, h, "POST", "/api/v1/catalog/subscriptions/"+id+"/preview", map[string]any{}, true)
	if r.Code != 200 || strings.Contains(r.Body.String(), "sensitive-fixture-auth") {
		t.Fatal("preview leaks authentication", r.Code)
	}
	r = call(t, h, "GET", "/api/v1/catalog/credentials", nil, true)
	if strings.Contains(r.Body.String(), "sensitive-fixture-auth") {
		t.Fatal("credential list leaked authentication")
	}
	r = call(t, h, "GET", "/sub/"+token, nil, false)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "proxy-groups:") || !strings.Contains(r.Body.String(), "rules:") || !strings.Contains(r.Body.String(), "sensitive-fixture-auth") {
		t.Fatal(r.Code, "full profile missing")
	}
	if r.Header().Get("profile-update-interval") == "" || r.Header().Get("subscription-userinfo") != "" || r.Header().Get("X-TML-Usage-Status") != "unavailable" || r.Header().Get("X-TML-Expires-At") == "" {
		t.Fatal("metadata missing or fabricated billing")
	}
	var hash, ua string
	if e = a.Store.DB.QueryRow("SELECT client_ip_hash,user_agent FROM subscription_access_logs LIMIT 1").Scan(&hash, &ua); e != nil || len(hash) != 64 || strings.Contains(ua, token) {
		t.Fatal("unsafe audit", e)
	}
	settings, e := subscription.LoadSettings(ctx, a.Store)
	if e != nil {
		t.Fatal(e)
	}
	settings.RatePerMinute = 1
	if e = subscription.SaveSettings(ctx, a.Store, settings); e != nil {
		t.Fatal(e)
	}
	r = call(t, h, "GET", "/sub/"+token, nil, false)
	if r.Code != 429 {
		t.Fatal("rate limit missing", r.Code)
	}
	if r = call(t, h, "GET", "/sub/mihomo/invalid-legacy-token", nil, false); r.Code != 404 {
		t.Fatal("legacy public route still exists", r.Code)
	}
	if r = call(t, h, "DELETE", "/api/v1/catalog/endpoints/"+connection.EndpointID, nil, true); r.Code != 409 {
		t.Fatal("referenced endpoint deleted", r.Code)
	}
}
