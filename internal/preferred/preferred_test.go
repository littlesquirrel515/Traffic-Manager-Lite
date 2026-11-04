package preferred

import (
	"context"
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"
)

func TestTXTCSVAndSubscriptionAddressExtraction(t *testing.T) {
	input := "104.16.1.1:443#A\n104.16.1.1:443#duplicate\n[2606:4700::1]:8443#IPv6\nedge.example.com#域名\n127.0.0.1:443\nhttp://metadata.google.internal/"
	p, e := Parse(input, 443, 10)
	if e != nil || len(p.Entries) != 3 || p.Duplicates != 1 || p.Invalid != 2 {
		t.Fatal(p, e)
	}
	csv := "\ufeffIP地址,端口,TLS,国家\n104.16.2.1,8443,true,\"SG,线路\"\n104.16.2.2,80,false,skip"
	p, e = Parse(csv, 443, 10)
	if e != nil || len(p.Entries) != 1 || p.Entries[0].Note != "SG,线路" || p.Entries[0].Port != 8443 || p.Invalid != 1 {
		t.Fatal(p, e)
	}
	raw := "vless://private-uuid@104.16.1.9:443?host=evil.example&path=/secret&security=none#Remark"
	p, e = Parse(base64.StdEncoding.EncodeToString([]byte(raw)), 443, 10)
	if e != nil || len(p.Entries) != 1 || p.Entries[0].Address != "104.16.1.9" || strings.Contains(p.Entries[0].Note, "uuid") {
		t.Fatal(p, e)
	}
	p, e = Parse("104.16.1.1\n104.16.1.2\n104.16.1.3", 443, 2)
	if e != nil || len(p.Entries) != 2 || p.Truncated != 1 {
		t.Fatal(p, e)
	}
}
func TestBoundsSourcePolicyAndRandomNotMeasured(t *testing.T) {
	for _, u := range []string{"http://example.com", "https://user:pass@example.com", "https://127.0.0.1", "https://169.254.169.254", "https://[::1]", "https://100.64.1.1", "https://example.com:22"} {
		if _, e := ValidateSource(u); e == nil {
			t.Fatal("accepted restricted source", u)
		}
	}
	for _, ip := range []string{"10.1.1.1", "::ffff:127.0.0.1", "fe80::1", "fd00:ec2::254", "100.64.1.1", "198.18.1.1", "240.1.1.1"} {
		if PublicIP(netip.MustParseAddr(ip)) {
			t.Fatal("unsafe DNS result", ip)
		}
	}
	if _, e := Fetch(context.Background(), "https://127.0.0.1", "feed"); e == nil {
		t.Fatal("private fetch allowed")
	}
	if _, e := Parse(strings.Repeat("x", MaxBytes+1), 443, 10); e == nil {
		t.Fatal("oversized feed accepted")
	}
	text, e := Random(100, 443)
	if e != nil {
		t.Fatal(e)
	}
	p, e := Parse(text, 443, 100)
	if e != nil || len(p.Entries) != 100 {
		t.Fatal(p, e)
	}
	for _, a := range p.Entries {
		ip := netip.MustParseAddr(a.Address)
		found := false
		for _, n := range cloudflarePool {
			if netip.MustParsePrefix(n).Contains(ip) {
				found = true
			}
		}
		if !found || !strings.Contains(a.Note, "未测速") {
			t.Fatal(a)
		}
	}
}
