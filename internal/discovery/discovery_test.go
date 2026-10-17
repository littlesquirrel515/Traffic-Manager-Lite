package discovery

import (
	"encoding/json"
	"testing"
)

func TestParserWhitelistAndNoAssumedClientPort(t *testing.T) {
	b := []byte(`{"inbounds":[{"tag":"vless","protocol":"vless","port":8080,"settings":{"clients":[{"email":"alice","id":"uuid"}]},"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"example.com","privateKey":"forbidden"},"wsSettings":{"path":"/ws","headers":{"Host":"example.com"}}}}]}`)
	rows, e := Parse("xray", b)
	if e != nil || len(rows) != 1 {
		t.Fatal(e)
	}
	p := rows[0].Profile
	if p.Port != 0 || p.Address != "" || p.ListenPort != 8080 || p.Path != "/ws" || p.UserKey != "alice" {
		t.Fatalf("wrong discovery %+v", p)
	}
	safe, _ := json.Marshal(p)
	if string(safe) == "" || contains(string(safe), "forbidden") {
		t.Fatal("private key leaked")
	}
}
func TestXrayMethodPrecedenceAndTransportAliases(t *testing.T) {
	for _, tc := range []struct{ method, network, want string }{
		{"xhttp", "tcp", "xhttp"}, {"splithttp", "", "xhttp"},
		{"websocket", "", "ws"}, {"grpc", "", "grpc"},
		{"raw", "", "tcp"}, {"httpupgrade", "", "httpupgrade"},
		{"", "ws", "ws"}, {"", "", "tcp"},
	} {
		stream := map[string]any{"method": tc.method, "network": tc.network, "wsSettings": map[string]any{"path": "/ws"}, "grpcSettings": map[string]any{"serviceName": "service"}}
		b, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"tag": "test", "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"email": "alice", "id": "uuid"}}}, "streamSettings": stream}}})
		rows, e := Parse("xray", b)
		if e != nil || len(rows) != 1 || rows[0].Profile.Transport != tc.want {
			t.Fatalf("method=%q network=%q: %+v %v", tc.method, tc.network, rows, e)
		}
		if tc.want == "ws" && rows[0].Profile.Path != "/ws" {
			t.Fatal("WS path lost")
		}
		if tc.want == "grpc" && rows[0].Profile.ServiceName != "service" {
			t.Fatal("gRPC service lost")
		}
		// V2Fly retains its own network schema; Xray's method is not imported.
		rows, e = Parse("v2fly", b)
		if e != nil || len(rows) != 1 || rows[0].Profile.Transport != first(tc.network, "tcp") {
			t.Fatal("Xray method changed V2Fly discovery", e)
		}
	}
}
func contains(a, b string) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return true
		}
	}
	return false
}
func TestSingboxAndHysteria(t *testing.T) {
	rows, e := Parse("singbox", []byte(`{"inbounds":[{"type":"vless","tag":"ws","listen_port":8080,"users":[{"name":"alice","uuid":"uuid"}],"transport":{"type":"ws","path":"/ws"}},{"type":"anytls","tag":"at","listen_port":443,"users":[{"name":"alice","password":"pass"}],"tls":{"enabled":true}},{"type":"hysteria2","tag":"hy","users":[{"name":"alice","password":"pass"}]}]}`))
	if e != nil || len(rows) != 3 {
		t.Fatalf("singbox %v %v", rows, e)
	}
	rows, e = Parse("hysteria2", []byte("listen: :443\nauth:\n  type: userpass\n  userpass:\n    alice: secret\n"))
	if e != nil || len(rows) != 1 || rows[0].Profile.Password != "alice:secret" {
		t.Fatalf("Hysteria %v %v", rows, e)
	}
}
