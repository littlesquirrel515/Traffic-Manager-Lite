package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

func TestDirectoryDiscoveryMissingSourceAndSafePreview(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "core")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(dir, "01.json"), []byte(`{"inbounds":[{"tag":"empty","port":10000,"protocol":"vless","settings":{"clients":[]},"streamSettings":{"network":"ws","wsSettings":{"path":"/ws"}}}]}`), 0600)
	os.WriteFile(filepath.Join(dir, "02.json"), []byte(`{"inbounds":[{"tag":"with-client","port":10001,"protocol":"trojan","settings":{"clients":[{"email":"alice","password":"secret-fixture"}]}}]}`), 0600)
	s, e := storage.Open(filepath.Join(root, "db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','xray','localhost:10085','now','now')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	inst := core.Instance{ID: 1, CoreType: "xray", ConfigPath: dir}
	ctx := context.Background()
	p := PreviewSync(ctx, s, inst, root)
	if p.Error != "" || len(p.Changes) != 2 {
		t.Fatal(p)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM catalog_inbounds").Scan(&count)
	if count != 0 {
		t.Fatal("preview wrote inbounds")
	}
	if _, e = Scan(ctx, s, inst, root); e != nil {
		t.Fatal(e)
	}
	nodes, e := catalog.Nodes(ctx, s.DB)
	if e != nil || len(nodes) != 2 {
		t.Fatal(nodes, e)
	}
	os.WriteFile(filepath.Join(dir, "02.json"), []byte(`{"inbounds":[]}`), 0600)
	p = PreviewSync(ctx, s, inst, root)
	if !p.Changed || len(p.Changes) != 1 || p.Changes[0].Action != "missing" {
		t.Fatal(p)
	}
	if _, e = Scan(ctx, s, inst, root); e != nil {
		t.Fatal(e)
	}
	nodes, e = catalog.Nodes(ctx, s.DB)
	if e != nil || len(nodes) != 2 {
		t.Fatal("source deletion removed catalog nodes", e)
	}
	single := filepath.Join(root, "single.json")
	os.WriteFile(single, []byte(`{"inbounds":[]}`), 0600)
	inst.ConfigPath = single
	if _, e = Scan(ctx, s, inst, root); e != nil {
		t.Fatal(e)
	}
	os.Remove(single)
	if _, e = Scan(ctx, s, inst, root); e == nil {
		t.Fatal("missing source accepted")
	}
}
func TestExternalHysteriaAuthDiscoversInboundWithoutInventingClients(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hy.yaml")
	os.WriteFile(path, []byte("listen: :443\nauth:\n  type: http\n  http:\n    url: http://auth.invalid\n"), 0600)
	s, e := storage.Open(filepath.Join(root, "db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.DB.Exec(`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost','now','now')`)
	s.DB.Exec(`INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','hysteria2','localhost:9999','now','now')`)
	if n, e := Scan(context.Background(), s, core.Instance{ID: 1, CoreType: "hysteria2", ConfigPath: path}, root); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	var clients int
	s.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&clients)
	if clients != 0 {
		t.Fatal("fake credentials")
	}
	var auth string
	if e = s.DB.QueryRow("SELECT authentication_type FROM catalog_inbounds").Scan(&auth); e != nil || auth != "http" {
		t.Fatal(auth, e)
	}
}
