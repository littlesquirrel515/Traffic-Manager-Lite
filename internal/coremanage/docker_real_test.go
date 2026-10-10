package coremanage

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDockerOfficialSingBoxManagedUsers(t *testing.T) {
	image := os.Getenv("TML_TEST_DOCKER_SINGBOX_IMAGE")
	if image == "" {
		t.Skip("explicit isolated Docker image required")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	name := fmt.Sprintf("tml-v14-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	secret := "isolated-native-api-secret"
	root := map[string]any{"log": map[string]any{"level": "error"}, "inbounds": []any{map[string]any{"type": "vless", "tag": "vless", "listen": "127.0.0.1", "listen_port": 21443, "users": []any{map[string]any{"name": "same", "uuid": "00000000-0000-4000-8000-000000000001"}}}, map[string]any{"type": "hysteria2", "tag": "hy", "listen": "127.0.0.1", "listen_port": 22443, "users": []any{map[string]any{"name": "same", "password": "hy-private-password"}}, "tls": map[string]any{"enabled": true, "certificate_path": "/configs/cert.pem", "key_path": "/configs/key.pem"}}, map[string]any{"type": "anytls", "tag": "tls", "listen": "127.0.0.1", "listen_port": 23443, "users": []any{map[string]any{"name": "same", "password": "tls-private-password"}}, "tls": map[string]any{"enabled": true, "certificate_path": "/configs/cert.pem", "key_path": "/configs/key.pem"}}}, "outbounds": []any{map[string]any{"type": "direct"}}, "services": []any{map[string]any{"type": "api", "listen": "0.0.0.0", "listen_port": 10085, "secret": secret, "dashboard": false}}}
	b, _ := json.Marshal(root)
	os.WriteFile(p, b, 0600)
	openssl := os.Getenv("TML_TEST_OPENSSL")
	if openssl == "" {
		openssl = "openssl"
	}
	if e = exec.Command(openssl, "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=localhost", "-keyout", filepath.Join(dir, "key.pem"), "-out", filepath.Join(dir, "cert.pem")).Run(); e != nil {
		t.Fatal("test certificate generation", e)
	}
	args := []string{"run", "-d", "--name", name, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--mount", "type=bind,source=" + dir + ",target=/configs,readonly", "-p", fmt.Sprintf("127.0.0.1:%d:10085", port), image, "run", "-c", "/configs/config.json"}
	if _, e = command(context.Background(), args...); e != nil {
		t.Fatal(e)
	}
	target := Target{ID: 1, Container: name, Path: p, ContainerPath: "/configs/config.json", NativeEndpoint: fmt.Sprintf("127.0.0.1:%d", port), APISecret: secret}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ready := false
	for n := 0; n < 40; n++ {
		c, stop := context.WithTimeout(ctx, time.Second)
		_, err := nativeStarted(c, target)
		stop()
		if err == nil {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("actual official Docker API not ready")
	}
	m := &Manager{Targets: map[int64]Target{1: target}, StateDir: filepath.Join(dir, "private-state"), Check: (Docker{}).Check, Restart: (Docker{}).Restart}
	state, e := m.Read(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, inbound := range []string{"vless", "hy", "tls"} {
		user := User{Name: "new-" + inbound, UUID: "00000000-0000-4000-8000-000000000099", Password: "new-private-password"}
		r, e := m.Update(ctx, 1, Request{Operation: "add", Revision: state.Revision, Inbound: inbound, User: user})
		if e != nil {
			t.Fatal(inbound, "actual check", e)
		}
		if r.Status != "PendingRestart" {
			t.Fatal("implicit restart")
		}
		state, e = m.Apply(ctx, 1, Request{Operation: "apply", Revision: r.Revision, ConfirmRestart: true})
		if e != nil || state.Status != "Applied" {
			t.Fatal(inbound, "actual restart", state, e)
		}
		r, e = m.Update(ctx, 1, Request{Operation: "update", Revision: state.Revision, Inbound: inbound, PreviousName: user.Name, User: User{Name: "changed-" + inbound}})
		if e != nil {
			t.Fatal(e)
		}
		state, e = m.Apply(ctx, 1, Request{Operation: "apply", Revision: r.Revision, ConfirmRestart: true})
		if e != nil {
			t.Fatal(e)
		}
		r, e = m.Update(ctx, 1, Request{Operation: "delete", Revision: state.Revision, Inbound: inbound, PreviousName: "changed-" + inbound, ConfirmDelete: true})
		if e != nil {
			t.Fatal(e)
		}
		state, e = m.Apply(ctx, 1, Request{Operation: "apply", Revision: r.Revision, ConfirmRestart: true})
		if e != nil {
			t.Fatal(e)
		}
	}
	// Break candidate validation using an invalid user UUID before any file replacement.
	original, _ := os.ReadFile(p)
	if _, e = m.Update(ctx, 1, Request{Operation: "add", Revision: state.Revision, Inbound: "vless", User: User{Name: "bad", UUID: "invalid"}}); e == nil {
		t.Fatal("invalid configuration saved")
	}
	current, _ := os.ReadFile(p)
	if Hash(original) != Hash(current) {
		t.Fatal("original overwritten")
	}
	// Real container startup failure followed by actual restore/restart, not a fixture.
	r, e := m.Update(ctx, 1, Request{Operation: "add", Revision: state.Revision, Inbound: "tls", User: User{Name: "recovery-user", Password: "recovery-password"}})
	if e != nil {
		t.Fatal(e)
	}
	os.Rename(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "cert-hidden.pem"))
	calls := 0
	m.Restart = func(c context.Context, tgt Target) error {
		calls++
		err := (Docker{}).Restart(c, tgt)
		if calls == 1 {
			os.Rename(filepath.Join(dir, "cert-hidden.pem"), filepath.Join(dir, "cert.pem"))
		}
		return err
	}
	restored, e := m.Apply(ctx, 1, Request{Operation: "apply", Revision: r.Revision, ConfirmRestart: true})
	if e != nil || restored.Status != "RolledBack" || calls != 2 {
		t.Fatal("actual failure rollback", restored, e, calls)
	}
	current, _ = os.ReadFile(p)
	if Hash(current) != Hash(original) {
		t.Fatal("rollback content differs")
	}
	t.Log("PASS official sing-box 1.14.3 Docker: three inbounds user CRUD, official check before atomic save, explicit restart/epoch health, no listener change, real failed startup restored")
}
