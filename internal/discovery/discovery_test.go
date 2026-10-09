package discovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
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
func TestReconcilePreservesOverridesAndStableID(t *testing.T) {
	root := t.TempDir()
	s, e := storage.Open(filepath.Join(root, "traffic.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := storage.Stamp(time.Now())
	s.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now)
	s.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','singbox','localhost:9999',?,?)", now, now)
	path := filepath.Join(root, "core.json")
	os.WriteFile(path, []byte(`{"inbounds":[{"type":"vless","tag":"ws","listen_port":8080,"users":[{"name":"alice","uuid":"uuid"}],"transport":{"type":"ws","path":"/old"}}]}`), 0600)
	inst := core.Instance{ID: 1, CoreType: "singbox", ConfigPath: path}
	ctx := context.Background()
	if _, e = Scan(ctx, s, inst, root); e != nil {
		t.Fatal(e)
	}
	nodes, e := Nodes(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	id := nodes[0].ID
	if e = Patch(ctx, s, id, map[string]any{"address": "example.com", "port": 443, "path": "/manual"}); e != nil {
		t.Fatal(e)
	}
	if _, e = Scan(ctx, s, inst, root); e != nil {
		t.Fatal(e)
	}
	nodes, e = Nodes(ctx, s)
	if e != nil || nodes[0].ID != id || nodes[0].Profile.Path != "/manual" || !nodes[0].Complete {
		t.Fatalf("overrides lost %v %v", nodes, e)
	}
	if e = Patch(ctx, s, id, map[string]any{"private_key": "secret"}); e == nil {
		t.Fatal("non-whitelist accepted")
	}
	if _, e = SafePath(root, filepath.Join(root, "..", "secret.json")); e == nil {
		t.Fatal("outside root accepted")
	}
	os.WriteFile(path, []byte(`{"inbounds":[]}`), 0600)
	Scan(ctx, s, inst, root)
	nodes, _ = Nodes(ctx, s)
	if nodes[0].Present {
		t.Fatal("removed node still published")
	}
}
