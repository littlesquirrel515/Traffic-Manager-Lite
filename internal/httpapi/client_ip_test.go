package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestSubscriptionClientIPTrustedChain(t *testing.T) {
	a := &API{}
	a.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.18.0.3/32"), netip.MustParsePrefix("10.0.0.0/24")}
	for _, tt := range []struct{ peer, xff, want, source string }{
		{"198.51.100.3:4000", "1.1.1.1", "198.51.100.3", "socket"},
		{"172.18.0.3:4000", "203.0.113.7", "203.0.113.7", "trusted_x_forwarded_for"},
		{"172.18.0.3:4000", "1.1.1.1, 203.0.113.7", "203.0.113.7", "trusted_x_forwarded_for"},
		{"172.18.0.3:4000", "203.0.113.7, 10.0.0.2", "203.0.113.7", "trusted_x_forwarded_for"},
		{"172.18.0.3:4000", "credential-not-ip, 10.0.0.2", "172.18.0.3", "socket"},
		{"172.18.0.3:4000", "::", "172.18.0.3", "socket"},
		{"[::ffff:172.18.0.3]:4000", "2001:db8::5", "2001:db8::5", "trusted_x_forwarded_for"},
	} {
		r := httptest.NewRequest("GET", "http://example.com/sub/redacted", nil)
		r.RemoteAddr = tt.peer
		r.Header.Set("X-Forwarded-For", tt.xff)
		r.Header.Set("CF-Connecting-IP", "8.8.8.8")
		got, peer, source := a.subscriptionClientIP(r)
		if got != tt.want || source != tt.source || peer == "" {
			t.Fatalf("%+v: %s %s %s", tt, got, peer, source)
		}
	}
}

func TestSubscriptionLogsResolvedIPAndRateLimitsByIt(t *testing.T) {
	a, _ := testAPI(t)
	a.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.18.0.3/32")}
	h := a.Handler()
	for _, ip := range []string{"203.0.113.7", "203.0.113.8"} {
		r := httptest.NewRequest("GET", "http://example.com/sub/invalid-token", nil)
		r.RemoteAddr = "172.18.0.3:1234"
		r.Header.Set("X-Forwarded-For", ip)
		r.Header.Set("User-Agent", "v2rayN/7.19.5")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
	var n int
	if err := a.Store.DB.QueryRow("SELECT count(DISTINCT client_ip_hash) FROM subscription_access_logs WHERE peer_ip='172.18.0.3' AND ip_source='trusted_x_forwarded_for' AND client_ip IN ('203.0.113.7','203.0.113.8')").Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
