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

func TestDockerXrayOverlayRuntimeAndRollback(t *testing.T) {
	image, version := os.Getenv("TML_TEST_DOCKER_XRAY_IMAGE"), os.Getenv("TML_TEST_DOCKER_XRAY_VERSION")
	if image == "" || version == "" {
		t.Skip("explicit isolated Xray image/version required")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	base := []byte(`{"log":{"loglevel":"error"},"api":{"tag":"api","listen":"0.0.0.0:10085","services":["HandlerService","StatsService"]},"stats":{},"inbounds":[{"tag":"v","listen":"127.0.0.1","port":21443,"protocol":"vless","settings":{"decryption":"none","clients":[{"email":"external","id":"00000000-0000-4000-8000-000000000001"}]}}],"outbounds":[{"protocol":"freedom"}]}`)
	if e := os.WriteFile(path, base, 0600); e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	name := fmt.Sprintf("tml-reconciliation-xray-%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, e = command(ctx, "run", "-d", "--name", name, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--mount", "type=bind,source="+dir+",target=/configs,readonly", "-p", fmt.Sprintf("127.0.0.1:%d:10085", port), image, "xray", "run", "-c", "/configs/config.json")
	if e != nil {
		t.Fatal(e)
	}
	target := Target{ID: 1, Container: name, CoreType: "xray", ExpectedVersion: version, Path: path, ContainerPath: "/configs/config.json", NativeEndpoint: fmt.Sprintf("127.0.0.1:%d", port)}
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
		t.Fatal("isolated Xray Handler/Stats API not ready")
	}
	m := &Manager{Targets: map[int64]Target{1: target}, StateDir: filepath.Join(dir, "state"), Check: (Docker{}).Check, Restart: (Docker{}).Restart, Verify: (Docker{}).Verify}
	r, e := m.Update(ctx, 1, Request{Operation: "overlay", Revision: Hash(base), Overlay: []ManagedUser{{Inbound: "v", Enabled: true, User: User{Name: "managed", UUID: "00000000-0000-4000-8000-000000000099"}}}})
	if e != nil {
		t.Fatal(e)
	}
	applied, e := m.Apply(ctx, 1, Request{Operation: "apply", Revision: r.Revision, ConfirmRestart: true})
	if e != nil || applied.Status != "Applied" {
		t.Fatal(applied, e)
	}
	v, e := m.Read(ctx, 1)
	if e != nil || v.Status != "Applied" {
		t.Fatal("runtime enumeration not verified", v, e)
	}
	rolled, e := m.Apply(ctx, 1, Request{Operation: "rollback", Revision: r.Revision, ConfirmRestart: true})
	if e != nil || rolled.Status != "RolledBack" {
		t.Fatal(rolled, e)
	}
	restored, _ := os.ReadFile(path)
	if Hash(restored) != Hash(base) {
		t.Fatal("Xray original not restored")
	}
	t.Log("PASS isolated Xray: official config check, persistent overlay, restart epoch, Handler account verification, encrypted backup and verified rollback")
}
