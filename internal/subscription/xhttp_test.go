package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
)

func simpleXHTTP(t *testing.T, mode string) core.Node {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"tag": "xh", "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"email": "alice", "id": "11111111-1111-4111-8111-111111111111"}}}, "streamSettings": map[string]any{"method": "xhttp", "security": "tls", "xhttpSettings": map[string]any{"path": "/api/example", "mode": mode}}}}})
	rows, e := discovery.Parse("xray", b)
	if e != nil || len(rows) != 1 {
		t.Fatal(e)
	}
	p, e := discovery.Effective(rows[0].Profile, map[string]any{"address": "preferred.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com", "fingerprint": "chrome", "alpn": []string{"h3", "h2"}})
	if e != nil {
		t.Fatal(e)
	}
	return core.Node{ID: "xhttp", Present: true, Profile: p}
}

func TestSimpleXHTTPSharePreservesConnectionParameters(t *testing.T) {
	for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one"} {
		n := simpleXHTTP(t, mode)
		b, _, r, e := Generate("v2ray", []core.Node{n})
		if e != nil || len(r) != 0 {
			t.Fatal(r, e)
		}
		raw, e := base64.StdEncoding.DecodeString(string(b))
		if e != nil {
			t.Fatal(e)
		}
		u, e := url.Parse(string(raw))
		if e != nil {
			t.Fatal(e)
		}
		if u.Scheme != "vless" || u.Host != "preferred.example.com:443" || u.User.Username() != n.Profile.UUID {
			t.Fatal("connection/credential changed")
		}
		for k, want := range map[string]string{"type": "xhttp", "mode": mode, "path": "/api/example", "sni": "origin.example.com", "host": "origin.example.com", "fp": "chrome", "alpn": "h3,h2", "encryption": "none", "security": "tls"} {
			if got := u.Query().Get(k); got != want {
				t.Fatalf("%s=%q, want %q", k, got, want)
			}
		}
		for _, format := range []string{"singbox"} {
			if _, _, r, e = Generate(format, []core.Node{n}); e != nil || len(r) != 1 || !strings.Contains(r[0].Reason, "XHTTP") {
				t.Fatal("unsupported format fabricated", r, e)
			}
		}
	}
	bad := simpleXHTTP(t, "unsupported-mode")
	if _, _, r, e := Generate("v2ray", []core.Node{bad}); e != nil || len(r) != 1 {
		t.Fatal("unknown mode emitted", e)
	}
	bad = simpleXHTTP(t, "packet-up")
	bad.Profile.Limitations = []string{"XHTTP 扩展参数尚未验证，未输出"}
	if _, _, r, e := Generate("v2ray", []core.Node{bad}); e != nil || len(r) != 1 {
		t.Fatal("extra emitted", e)
	}
}

func TestDiscoveredXHTTPExtraCannotBePublishedAsSimple(t *testing.T) {
	b := []byte(`{"inbounds":[{"tag":"xh","protocol":"vless","settings":{"clients":[{"email":"alice","id":"11111111-1111-4111-8111-111111111111"}]},"streamSettings":{"method":"xhttp","security":"tls","xhttpSettings":{"path":"/api/example","mode":"packet-up","extra":{"downloadSettings":{"address":"other.example.com"}}}}}]}`)
	rows, err := discovery.Parse("xray", b)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	p, err := discovery.Effective(rows[0].Profile, map[string]any{"address": "preferred.example.com", "port": 443, "sni": "origin.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, rejected, err := Generate("v2ray", []core.Node{{ID: "extra", Present: true, Profile: p}})
	if err != nil || len(rejected) != 1 || !strings.Contains(rejected[0].Reason, "扩展参数") {
		t.Fatal("complex XHTTP settings were silently discarded", rejected, err)
	}
}

func TestRealXraySimpleXHTTPConfiguration(t *testing.T) {
	binary := os.Getenv("TML_TEST_XRAY")
	if binary == "" {
		t.Skip("set TML_TEST_XRAY for official Xray validation")
	}
	for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one"} {
		n := simpleXHTTP(t, mode)
		b, _, _, e := Generate("v2ray", []core.Node{n})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := base64.StdEncoding.DecodeString(string(b))
		u, _ := url.Parse(string(raw))
		q := u.Query()
		config := map[string]any{"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": 443, "users": []any{map[string]any{"id": u.User.Username(), "encryption": q.Get("encryption")}}}}}, "streamSettings": map[string]any{"network": q.Get("type"), "security": q.Get("security"), "tlsSettings": map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "alpn": strings.Split(q.Get("alpn"), ",")}, "xhttpSettings": map[string]any{"path": q.Get("path"), "host": q.Get("host"), "mode": q.Get("mode")}}}}}
		file := filepath.Join(t.TempDir(), "client.json")
		data, _ := json.Marshal(config)
		if e = os.WriteFile(file, data, 0600); e != nil {
			t.Fatal(e)
		}
		if out, e := exec.Command(binary, "run", "-test", "-config", file).CombinedOutput(); e != nil {
			t.Fatalf("official Xray mode %s: %v %s", mode, e, out)
		}
	}
}
