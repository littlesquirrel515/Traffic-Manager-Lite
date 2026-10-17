package baseline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"traffic-manager-lite/internal/storage"
)

func TestPrepareMakesVerifiedRecoveryArtifactsWithoutReset(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "owned.db")
	s, e := storage.Open(source, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'must remain','test','now','now')"); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	before, _ := fileHash(source)
	r, e := Prepare(context.Background(), source, filepath.Join(dir, "private-recovery"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	after, _ := fileHash(source)
	if before != after || r.ResetExecuted || r.Counts["servers"] != 1 || r.Ownership != "tml-v2" {
		t.Fatal(r)
	}
	if len(r.BackupSHA256) != 64 || len(r.CandidateSHA256) != 64 {
		t.Fatal("missing artifact hashes")
	}
	c, e := storage.Open(r.Candidate, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	var n int
	if e = c.DB.QueryRow("SELECT count(*) FROM servers").Scan(&n); e != nil || n != 0 {
		t.Fatal("candidate not empty", e)
	}
	if _, e = Prepare(context.Background(), source, filepath.Dir(r.Backup), "Asia/Shanghai"); e == nil {
		t.Fatal("overwrote recovery artifacts")
	}
}

func TestPrepareRefusesSharedDatabase(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "shared.db")
	s, e := storage.Open(source, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("CREATE TABLE another_application(secret TEXT)"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	out := filepath.Join(dir, "backup")
	if _, e = Prepare(context.Background(), source, out, "Asia/Shanghai"); e == nil {
		t.Fatal("shared database accepted")
	}
	if _, e = os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("wrote output before establishing ownership")
	}
}
