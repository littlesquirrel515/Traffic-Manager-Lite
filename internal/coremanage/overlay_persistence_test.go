package coremanage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeparateBaseEffectiveAndEncryptedLegacyBackup(t *testing.T) {
	m, target := fixture(t)
	ctx := context.Background()
	original, _ := os.ReadFile(target.Path)
	base := filepath.Join(filepath.Dir(target.Path), "base.json")
	os.WriteFile(base, original, 0600)
	target.BasePath = base
	m.Targets[1] = target
	req := Request{Operation: "overlay", Revision: Hash(original), Overlay: []ManagedUser{{Inbound: "vless", Enabled: true, User: User{Name: "new", UUID: "00000000-0000-4000-8000-000000000099"}}}}
	result, e := m.Update(ctx, 1, req)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(base)
	if string(before) != string(original) {
		t.Fatal("base overwritten")
	}
	if _, e = m.Apply(ctx, 1, Request{Operation: "apply", Revision: result.Revision, ConfirmRestart: true}); e != nil {
		t.Fatal(e)
	}
	v, e := m.Read(ctx, 1)
	if e != nil || v.DeployMode != "managed" || v.Status != "Applied" {
		t.Fatal(v, e)
	}
	j := m.load(1)
	encrypted, _ := os.ReadFile(j.Backup)
	if !strings.HasPrefix(string(encrypted), "tmlenc1:") {
		t.Fatal("plaintext host backup")
	}
	// A legacy plaintext archive must match both journal path and old hash.
	os.WriteFile(j.Backup, original, 0600)
	if _, e = m.readBackup(j.Backup); e != nil {
		t.Fatal("legacy rollback incompatible", e)
	}
	encrypted, _ = os.ReadFile(j.Backup)
	if !strings.HasPrefix(string(encrypted), "tmlenc1:") {
		t.Fatal("legacy archive not converted")
	}
	arbitrary := filepath.Join(m.StateDir, "untrusted.json")
	os.WriteFile(arbitrary, original, 0600)
	if _, e = m.readBackup(arbitrary); e == nil {
		t.Fatal("unknown plaintext archive accepted")
	}
}
