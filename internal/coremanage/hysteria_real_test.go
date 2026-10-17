package coremanage

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDockerHysteriaStaticUserpassOverlay(t *testing.T) {
	image, version := os.Getenv("TML_TEST_DOCKER_HYSTERIA_IMAGE"), os.Getenv("TML_TEST_DOCKER_HYSTERIA_VERSION")
	if image == "" || version == "" {
		t.Skip("explicit isolated Hysteria image/version required")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if e := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=localhost", "-keyout", filepath.Join(dir, "key.pem"), "-out", filepath.Join(dir, "cert.pem")).Run(); e != nil {
		t.Fatal("certificate fixture generation", e)
	}
	base := []byte("listen: 127.0.0.1:22443\ntls:\n  cert: /configs/cert.pem\n  key: /configs/key.pem\nauth:\n  type: userpass\n  userpass:\n    external: fixture-external-password\ntrafficStats:\n  listen: 0.0.0.0:10085\n  secret: isolated-traffic-api-secret\n")
	os.WriteFile(path, base, 0600)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	name := fmt.Sprintf("tml-reconciliation-hy-%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, e = command(ctx, "run", "-d", "--name", name, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--mount", "type=bind,source="+dir+",target=/configs,readonly", "-p", fmt.Sprintf("127.0.0.1:%d:10085", port), image, "server", "--config", "/configs/config.yaml")
	if e != nil {
		t.Fatal(e)
	}
	target := Target{ID: 1, Container: name, CoreType: "hysteria2", ExpectedVersion: version, Path: path, ContainerPath: "/configs/config.yaml", NativeEndpoint: fmt.Sprintf("http://127.0.0.1:%d", port), APISecret: "isolated-traffic-api-secret"}
	ready := false
	for n := 0; n < 40; n++ {
		c, stop := context.WithTimeout(ctx, time.Second)
		err := verifyAdditional(c, target)
		stop()
		if err == nil {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("isolated HY traffic API not ready")
	}
	m := &Manager{Targets: map[int64]Target{1: target}, StateDir: filepath.Join(dir, "state"), Check: (Docker{}).Check, Restart: (Docker{}).Restart, Verify: (Docker{}).Verify}
	r, e := m.Update(ctx, 1, Request{Operation: "overlay", Revision: Hash(base), Overlay: []ManagedUser{{Inbound: "hysteria2", Enabled: true, User: User{Name: "managed", Password: "fixture-managed-password"}}}})
	if e != nil {
		t.Fatal(e)
	}
	applied, e := m.Apply(ctx, 1, Request{Operation: "apply", Revision: r.Revision, ConfirmRestart: true})
	if e != nil || applied.Status != "Applied" {
		t.Fatal(applied, e)
	}
	v, e := m.Read(ctx, 1)
	if e != nil || v.Status != "Applied" {
		t.Fatal(v, e)
	}
	rolled, e := m.Apply(ctx, 1, Request{Operation: "rollback", Revision: r.Revision, ConfirmRestart: true})
	if e != nil || rolled.Status != "RolledBack" {
		t.Fatal(rolled, e)
	}
	restored, _ := os.ReadFile(path)
	if Hash(restored) != Hash(base) {
		t.Fatal("HY base not restored")
	}
	t.Log("PASS isolated native HY userpass: auth-only semantic patch, exact version, actual restart epoch, readonly traffic API health and rollback; no client-session authentication claim")
}
