package subscription

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedVisibleLinksKeepLegacyTokenAndRotateBoth(t *testing.T) {
	ctx := context.Background()
	s, n, creds := catalogFixture(t)
	user, e := SaveSubscriber(ctx, s, Subscriber{Name: "User", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Bind(ctx, s, Binding{SubscriberID: user, InboundID: n.InboundID, CredentialID: creds[0], Enabled: true}); e != nil {
		t.Fatal(e)
	}
	id, legacy, e := SaveCatalogSubscription(ctx, s, CatalogDefinition{Name: "Old", SubscriberID: user, Format: "auto", NodeIDs: []string{n.ID}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	key := filepath.Join(t.TempDir(), "key")
	if e = os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	visible, e := VisibleLink(ctx, s, key, id)
	if e != nil || visible.Token == legacy {
		t.Fatal("legacy hash incorrectly treated as recoverable", e)
	}
	second, e := VisibleLink(ctx, s, key, id)
	if e != nil || second.Token != visible.Token {
		t.Fatal("view rotated token", e)
	}
	for _, token := range []string{legacy, visible.Token} {
		if _, _, e = CatalogOutput(ctx, s, token, ""); e != nil {
			t.Fatal("compatible link unavailable", e)
		}
	}
	var sealed, hash string
	s.DB.QueryRow("SELECT token_sealed,token_hash FROM subscription_link_secrets WHERE subscription_id=?", id).Scan(&sealed, &hash)
	if strings.Contains(sealed, visible.Token) || hash != Hash(visible.Token) {
		t.Fatal("visible token not encrypted/hashed")
	}
	unified, e := UnifiedLink(ctx, s, key, user)
	if e != nil {
		t.Fatal(e)
	}
	again, e := UnifiedLink(ctx, s, key, user)
	if e != nil || again.Token != unified.Token || again.SubscriptionID != unified.SubscriptionID {
		t.Fatal("unified link unstable", e)
	}
	for _, format := range []string{"base64", "mihomo"} {
		v, d, err := CatalogOutput(ctx, s, unified.Token, format)
		if err != nil || len(v.Content) == 0 || d.Format != format || !d.Unified {
			t.Fatal("unified format failed", d, err)
		}
	}
	rotated, e := RotateVisibleLink(ctx, s, key, id)
	if e != nil {
		t.Fatal(e)
	}
	for _, old := range []string{legacy, visible.Token} {
		if _, _, e = CatalogOutput(ctx, s, old, ""); e == nil {
			t.Fatal("rotation did not revoke every previous alias")
		}
	}
	if _, _, e = CatalogOutput(ctx, s, rotated.Token, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = VisibleLink(ctx, s, "missing", id); e == nil {
		t.Fatal("missing key silently regenerated links")
	}
}

func TestNewDeliveryVisibleAddressEqualsInitiallyIssuedAddress(t *testing.T) {
	ctx := context.Background()
	s, n, creds := catalogFixture(t)
	user, _ := SaveSubscriber(ctx, s, Subscriber{Name: "U", Enabled: true})
	Bind(ctx, s, Binding{SubscriberID: user, InboundID: n.InboundID, CredentialID: creds[0], Enabled: true})
	id, token, e := SaveCatalogSubscription(ctx, s, CatalogDefinition{Name: "New", SubscriberID: user, Format: "auto", Enabled: true, NodeIDs: []string{n.ID}})
	if e != nil {
		t.Fatal(e)
	}
	key := filepath.Join(t.TempDir(), "key")
	os.WriteFile(key, []byte(strings.Repeat("p", 32)), 0600)
	if e = PersistVisibleToken(ctx, s, key, id, token); e != nil {
		t.Fatal(e)
	}
	viewed, e := VisibleLink(ctx, s, key, id)
	if e != nil || viewed.Token != token {
		t.Fatal("admin received a different address", e)
	}
}
func TestRequestFormatNegotiation(t *testing.T) {
	for _, d := range []struct{ explicit, ua, accept, want string }{
		{"", "v2rayN/7.19.5", "", "base64"}, {"", "Clash-Verge/v2.4.4", "", "mihomo"}, {"", "clash-verge-rev/2.4.4", "", "mihomo"}, {"", "mihomo/1.19.32", "", "mihomo"}, {"", "Mozilla/5.0", "", ""}, {"auto", "v2rayN/7.0", "", "base64"}, {"base64", "clash-verge/2.4", "", "base64"}, {"mihomo", "v2rayN/7.0", "", "mihomo"}, {"", "curl/8.0", "text/yaml", "mihomo"}, {"invalid", "v2rayN/7.0", "", "invalid"},
	} {
		if got := RequestFormat(d.explicit, d.ua, d.accept); got != d.want {
			t.Fatal(d, got)
		}
	}
}
