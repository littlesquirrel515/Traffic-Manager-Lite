package subscription

import (
	"encoding/base64"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
	"traffic-manager-lite/internal/core"
)

func node(protocol, transport string) core.Node {
	return core.Node{ID: "12345678", Present: true, UserID: 1, Profile: core.NodeProfile{Name: "测试节点", Protocol: protocol, Transport: transport, Address: "example.com", Port: 443, UUID: "11111111-1111-4111-8111-111111111111", Password: "user-password", TLS: protocol != "shadowsocks", SNI: "example.com", Enabled: true, Path: "/ws", ServiceName: "grpc", Method: "aes-128-gcm"}}
}
func TestProtocolFormats(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "trojan", "hysteria2", "anytls", "shadowsocks"} {
		for _, format := range []string{"v2ray", "mihomo", "singbox"} {
			t.Run(protocol+"/"+format, func(t *testing.T) {
				n := node(protocol, "tcp")
				b, _, rejected, e := Generate(format, []core.Node{n})
				if e != nil {
					t.Fatal(e)
				}
				if protocol == "anytls" && format == "v2ray" {
					if len(rejected) != 1 {
						t.Fatal("unsupported AnyTLS URI emitted")
					}
					return
				}
				if len(rejected) != 0 {
					t.Fatal(rejected)
				}
				switch format {
				case "v2ray":
					decoded, e := base64.StdEncoding.DecodeString(string(b))
					if e != nil || !strings.Contains(string(decoded), "://") {
						t.Fatalf("share %s %v", decoded, e)
					}
				case "mihomo":
					var config struct {
						Proxies []map[string]any `yaml:"proxies"`
					}
					if e = yaml.Unmarshal(b, &config); e != nil || len(config.Proxies) != 1 {
						t.Fatalf("YAML %s %v", b, e)
					}
				case "singbox":
					var config struct {
						Outbounds []map[string]any `json:"outbounds"`
					}
					if e = json.Unmarshal(b, &config); e != nil || len(config.Outbounds) != 1 {
						t.Fatal(e)
					}
					o := config.Outbounds[0]
					if o["server"] != "example.com" || o["type"] != protocol || o["server_port"].(float64) != 443 {
						t.Fatal("invalid outbound fields")
					}
				}
			})
		}
	}
}
func TestWSRealityAndFiltering(t *testing.T) {
	for _, format := range []string{"v2ray", "mihomo", "singbox"} {
		ws := node("vless", "ws")
		reality := node("vless", "tcp")
		reality.ID = "abcdef12"
		reality.Profile.PublicKey = "reality-public-key"
		reality.Profile.ShortID = "abcdef"
		b, _, rejected, e := Generate(format, []core.Node{ws, reality})
		if e != nil || len(rejected) != 0 {
			t.Fatalf("WS/Reality %v %v", rejected, e)
		}
		if format == "v2ray" {
			raw, _ := base64.StdEncoding.DecodeString(string(b))
			b = raw
		}
		if !strings.Contains(string(b), "reality") || !strings.Contains(string(b), "ws") {
			t.Fatal("transport/security omitted")
		}
		bad := node("vless", "xhttp")
		bad.Profile.XHTTPMode = "unverified-mode"
		_, _, rejected, e = Generate(format, []core.Node{bad})
		if e != nil || len(rejected) != 1 {
			t.Fatal("unsupported XHTTP not filtered")
		}
	}
}
