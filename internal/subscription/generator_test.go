package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
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
		_, _, rejected, e = Generate(format, []core.Node{bad})
		if e != nil || len(rejected) != 1 {
			t.Fatal("unsupported XHTTP not filtered")
		}
	}
}
func TestTokenIsolationRotationAndRevocation(t *testing.T) {
	ctx := context.Background()
	s, e := storage.Open(filepath.Join(t.TempDir(), "traffic.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := storage.Stamp(time.Now())
	s.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now)
	s.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','xray','localhost:9999',?,?)", now, now)
	for id := 1; id <= 2; id++ {
		s.DB.Exec("INSERT INTO users(id,display_name,created_at,updated_at) VALUES(?,'user',?,?)", id, now, now)
		n := node("vless", "ws")
		n.UserID = int64(id)
		b, _ := json.Marshal(n.Profile)
		nodeID := string(rune('a' + id))
		if _, e = s.DB.Exec("INSERT INTO nodes(id,instance_id,inbound_tag,user_id,protocol,discovered_json,effective_json,source_hash,updated_at) VALUES(?,1,'in',?,'vless',?,?,'hash',?)", nodeID, id, string(b), string(b), now); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e = Save(ctx, s, 0, Definition{Name: "bad", Format: "singbox", UserID: 1, NodeIDs: []string{"c"}, Enabled: true}); e == nil {
		t.Fatal("cross user node allowed")
	}
	var cnt int
	s.DB.QueryRow("SELECT count(*) FROM subscriptions").Scan(&cnt)
	if cnt != 0 {
		t.Fatal("failed creation committed")
	}
	id, token, e := Save(ctx, s, 0, Definition{Name: "good", Format: "singbox", UserID: 1, NodeIDs: []string{"b"}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	var hash string
	s.DB.QueryRow("SELECT token_hash FROM subscriptions WHERE id=?", id).Scan(&hash)
	if hash == token || len(hash) != 64 {
		t.Fatal("plaintext token stored")
	}
	if _, _, _, e = Output(ctx, s, "singbox", token); e != nil {
		t.Fatal(e)
	}
	next, e := Rotate(ctx, s, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = Output(ctx, s, "singbox", token); e == nil {
		t.Fatal("old token valid")
	}
	s.DB.Exec("UPDATE subscriptions SET enabled=0 WHERE id=?", id)
	if _, _, _, e = Output(ctx, s, "singbox", next); e == nil {
		t.Fatal("revoked subscription valid")
	}
}
