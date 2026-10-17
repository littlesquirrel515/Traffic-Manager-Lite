package subscription

import (
	"context"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
)

func TestThreeEndpointNodesShareOneCredentialAndOneQuotaSource(t *testing.T) {
	s, n, credentials := catalogFixture(t)
	ctx := context.Background()
	now := time.Now().Add(-time.Minute)
	apply := func(at time.Time, up, down int64) {
		t.Helper()
		if e := s.Apply(ctx, 1, []core.TrafficRecord{{Scope: "user", UserKey: "alice", Source: "xray", CounterMode: "cumulative", CollectedAt: at, UploadBytes: up, DownloadBytes: down}}); e != nil {
			t.Fatal(e)
		}
	}
	apply(now, 0, 0)
	user, e := SaveSubscriber(ctx, s, Subscriber{Name: "one client", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Bind(ctx, s, Binding{SubscriberID: user, InboundID: n.InboundID, CredentialID: credentials[0], Enabled: true}); e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for _, name := range []string{"primary", "preferred", "backup"} {
		result, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: *n.InboundID, Name: name, Enabled: true, Profile: map[string]any{"address": name + ".example.com", "port": 443, "sni": "tls.example.com"}})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, result.NodeID)
	}
	definition := CatalogDefinition{SubscriberID: user, Enabled: true, Format: "mihomo", NodeIDs: ids}
	resolved, rejected, count, e := ResolveCatalog(ctx, s.DB, definition)
	if e != nil || count != 3 || len(resolved) != 3 || len(rejected) != 0 {
		t.Fatal(e, count, rejected)
	}
	for _, node := range resolved {
		if node.CredentialID != credentials[0] || node.Profile.UUID != "11111111-1111-4111-8111-111111111111" {
			t.Fatal("nodes did not share one real credential")
		}
	}
	apply(now.Add(time.Second), 1073741824, 2147483648)
	usage, e := SubscriberUsage(ctx, s.DB, user)
	if e != nil || len(usage.Sources) != 1 || usage.Used != 3221225472 || usage.Remaining == nil || *usage.Remaining != 7516192768 {
		t.Fatal("endpoint multiplied billing", usage, e)
	}
	var bindings, clients int
	s.DB.QueryRow("SELECT count(*) FROM subscriber_credentials WHERE subscriber_id=?", user).Scan(&bindings)
	s.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&clients)
	if bindings != 1 || clients != 2 {
		t.Fatal("endpoint operation created client or billing bindings", bindings, clients)
	}
}
