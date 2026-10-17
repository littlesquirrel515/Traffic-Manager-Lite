package subscription

import (
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"traffic-manager-lite/internal/core"
)

func TestMihomoXHTTPModesTLSRealityAndOfficialValidation(t *testing.T) {
	for _, mode := range []string{"auto", "stream-one", "stream-up", "packet-up"} {
		for _, reality := range []bool{false, true} {
			n := simpleXHTTP(t, mode)
			n.Profile.ALPN = []string{"h2"}
			n.Profile.Host = "http-host.example.com"
			n.Profile.SNI = "tls-sni.example.com"
			no := false
			n.Profile.XHTTPNoGRPCHeader = &no
			n.Profile.XHTTPPadding = "100-1000"
			n.Profile.XHTTPHeaders = map[string]string{"X-Test": "fixture"}
			if reality {
				n.Profile.Reality = true
				n.Profile.PublicKey = "KjD1RnwGsoXDi_p6AW9VKBN50Y1GYqULb7TkpnTdSVQ"
				n.Profile.ShortID = "abcdef"
			}
			b, _, rejected, e := Generate("mihomo", []core.Node{n})
			if e != nil || len(rejected) > 0 {
				t.Fatal(rejected, e)
			}
			var config struct {
				Proxies []map[string]any `yaml:"proxies"`
			}
			if e = yaml.Unmarshal(b, &config); e != nil || len(config.Proxies) != 1 {
				t.Fatal(e)
			}
			p := config.Proxies[0]
			opts, ok := p["xhttp-opts"].(map[string]any)
			if !ok {
				t.Fatal("missing structured xhttp-opts")
			}
			if p["network"] != "xhttp" || p["type"] != "vless" || p["server"] != "preferred.example.com" || p["servername"] != "tls-sni.example.com" || opts["host"] != "http-host.example.com" || opts["path"] != "/api/example" || opts["mode"] != mode || opts["no-grpc-header"] != false || opts["x-padding-bytes"] != "100-1000" || p["skip-cert-verify"] != nil {
				t.Fatal("incorrect parameter mapping", p)
			}
			if reality {
				r := p["reality-opts"].(map[string]any)
				if r["public-key"] != n.Profile.PublicKey || r["short-id"] != "abcdef" {
					t.Fatal("REALITY mapping")
				}
			}
			if binary := os.Getenv("TML_TEST_MIHOMO"); binary != "" {
				dir := t.TempDir()
				file := filepath.Join(dir, "config.yaml")
				os.WriteFile(file, b, 0600)
				cmd := exec.Command(binary, "-t", "-f", file, "-d", dir)
				if out, e := cmd.CombinedOutput(); e != nil {
					t.Fatalf("official Mihomo rejects %s reality=%v: %s %v", mode, reality, out, e)
				}
			}
		}
	}
}
