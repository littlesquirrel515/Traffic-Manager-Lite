package subscription

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriberPreviewWithoutLinkIsMaskedAndReadOnly(t *testing.T) {
	ctx := context.Background()
	s, n, creds := catalogFixture(t)
	uid, e := SaveSubscriber(ctx, s, Subscriber{Name: "Preview", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Bind(ctx, s, Binding{SubscriberID: uid, InboundID: n.InboundID, CredentialID: creds[0], Enabled: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO subscriber_node_grants VALUES(?,?,?,?)", uid, n.ID, *n.InboundID, "fixture"); e != nil {
		t.Fatal(e)
	}
	counts := func() map[string]int {
		out := map[string]int{}
		for _, table := range []string{"catalog_subscriptions", "subscriber_unified_links", "subscription_link_secrets", "subscription_admin_audit", "subscriber_credentials", "subscriber_node_grants"} {
			var v int
			if err := s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&v); err != nil {
				t.Fatal(err)
			}
			out[table] = v
		}
		return out
	}
	before := counts()
	p, e := SubscriberPreview(ctx, s, uid)
	if e != nil || p.Usable == 0 || len(p.Nodes) == 0 {
		t.Fatal(p, e)
	}
	for _, node := range p.Nodes {
		if node.Profile.UUID != "11111111-1111-4111-8111-111111111111" || node.Profile.Password != "REDACTED" || node.CredentialID != "" {
			t.Fatal("unmasked preview")
		}
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(counts())
	if string(a) != string(b) {
		t.Fatal("preview created links or changed ownership")
	}
	other, e := SaveSubscriber(ctx, s, Subscriber{Name: "Other", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	empty, e := SubscriberPreview(ctx, s, other)
	if e != nil || empty.Usable != 0 {
		t.Fatal("another user's nodes exposed", e)
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), "token_sealed") {
		t.Fatal("link material exposed")
	}
}
