package discovery

import (
	"testing"
)

func TestMissingCriticalParametersAreExplicit(t *testing.T) {
	nodes, e := Parse("hysteria2", []byte("listen: '[::]:443'\nauth:\n  type: password\n  password: secret\nobfs:\n  type: salamander\n  salamander:\n    password: obfs-secret\n"))
	if e != nil || len(nodes) != 1 || nodes[0].Profile.UserKey != "user" || nodes[0].Profile.ListenPort != 443 {
		t.Fatalf("Hysteria identity/listen %v %v", nodes, e)
	}
	p := nodes[0].Profile
	p.Address = "example.com"
	p.Port = 443
	p.SNI = "example.com"
	if len(Issues(p)) == 0 {
		t.Fatal("missing obfs emitted as connectable")
	}
	nodes, e = Parse("singbox", []byte(`{"inbounds":[{"type":"vless","tag":"reality","users":[{"name":"alice","uuid":"uuid"}],"tls":{"enabled":true,"server_name":"example.com","reality":{"enabled":true,"private_key":"invalid"}}}]}`))
	if e != nil {
		t.Fatal(e)
	}
	p = nodes[0].Profile
	p.Address = "example.com"
	p.Port = 443
	if len(Issues(p)) == 0 {
		t.Fatal("Reality misrepresented as TLS")
	}
}
