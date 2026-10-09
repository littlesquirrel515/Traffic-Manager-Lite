package security

import (
	"context"
	"strings"
	"testing"
)

func TestInstanceAuthorizationScopesPortsAndTargets(t *testing.T) {
	p := (Policy{}).ForEndpoints("127.0.0.1:12345", "http://localhost:23456")
	for _, address := range []string{"127.0.0.1:12345", "localhost:23456"} {
		if e := p.Check(context.Background(), address); e != nil {
			t.Fatal(e)
		}
	}
	for _, address := range []string{"127.0.0.1:23456", "localhost:12345", "127.0.0.1:80"} {
		if e := p.Check(context.Background(), address); e == nil {
			t.Fatalf("unapproved target %s", address)
		}
	}
	if e := (Policy{}).Check(context.Background(), "127.0.0.1:12345"); e == nil {
		t.Fatal("empty policy must not authorize arbitrary targets")
	}
	p = (Policy{Allowed: []string{"xray"}}).ForEndpoints("xray-multi:10085")
	if e := p.Check(context.Background(), "xray-multi:10085"); e == nil || !strings.Contains(e.Error(), "TML_ALLOWED_TARGETS") {
		t.Fatalf("strict restrictions lost: %v", e)
	}
	p = (Policy{Allowed: []string{"127.0.0.0/8"}}).ForEndpoints("127.0.0.1:10085")
	if e := p.Check(context.Background(), "127.0.0.1:10085"); e != nil {
		t.Fatal(e)
	}
	p = Policy{Allowed: []string{"127.0.0.1", "::1"}}
	if e := p.Check(context.Background(), "localhost:10085"); e != nil {
		t.Fatal(e)
	}
}

func TestInstanceAuthorizationRejectsUnsafeAddresses(t *testing.T) {
	for _, endpoint := range []string{"0.0.0.0:80", "[::]:80", "169.254.169.254:80", "[::ffff:169.254.169.254]:80", "[fe80::1]:80", "224.0.0.1:80", "localhost:0", "localhost:65536", ":80", "localhost:http"} {
		if e := ValidateEndpoint(endpoint, false); e == nil {
			t.Fatalf("unsafe address %s", endpoint)
		}
	}
	p := (Policy{}).ForEndpoints("169.254.169.254:80")
	if e := p.Check(context.Background(), "169.254.169.254:80"); e == nil {
		t.Fatal("metadata allowed")
	}
	if TargetAddress("https://localhost") != "localhost:443" {
		t.Fatal("HTTP default port")
	}
}
