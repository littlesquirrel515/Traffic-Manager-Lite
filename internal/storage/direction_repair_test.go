package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func TestHysteriaRepairBackupDryRunArchiveAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(filepath.Join(dir, "test.db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.DB.Exec("INSERT INTO servers(name,address,created_at,updated_at) VALUES('s','','now','now')")
	s.DB.Exec("INSERT INTO instances(server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,'hy','hysteria2','localhost:1','now','now'),(1,'x','xray','localhost:2','now','now')")
	ctx := context.Background()
	old := time.Now().AddDate(0, 0, -40)
	r := core.TrafficRecord{InstanceID: 1, ServerID: 1, Scope: "user", UserKey: "alice", Source: "hysteria2", CounterMode: "cumulative", CollectedAt: old}
	s.Apply(ctx, 1, []core.TrafficRecord{r})
	r.UploadBytes = 100
	r.DownloadBytes = 1000
	r.CollectedAt = old.Add(time.Second)
	s.Apply(ctx, 1, []core.TrafficRecord{r})
	if _, e = s.Archive(ctx, time.Now()); e != nil {
		t.Fatal(e)
	}
	r.UploadBytes = 120
	r.DownloadBytes = 1300
	r.CollectedAt = time.Now().Add(-time.Hour)
	s.Apply(ctx, 1, []core.TrafficRecord{r})
	cutoff := time.Now().Add(-time.Minute)
	p, e := s.DirectionPlan(ctx, 1, cutoff)
	if e != nil || p.Samples != 1 || p.ArchivedHourly != 1 {
		t.Fatal(p, e)
	}
	var u, d int64
	s.DB.QueryRow("SELECT SUM(upload_bytes),SUM(download_bytes) FROM traffic_daily WHERE instance_id=1").Scan(&u, &d)
	if u != 120 || d != 1300 {
		t.Fatal("dry run mutated history")
	}
	if _, e = s.RepairHysteriaDirection(ctx, 1, cutoff, "", filepath.Join(dir, "backups")); e == nil {
		t.Fatal("unverified source repaired")
	}
	p, e = s.RepairHysteriaDirection(ctx, 1, cutoff, "test source: known legacy TML adapter only", filepath.Join(dir, "backups"))
	if e != nil || p.Status != "Repaired" {
		t.Fatal(p, e)
	}
	if _, e = os.Stat(p.Backup); e != nil {
		t.Fatal("backup absent")
	}
	s.DB.QueryRow("SELECT SUM(upload_bytes),SUM(download_bytes) FROM traffic_daily WHERE instance_id=1").Scan(&u, &d)
	if u != 1300 || d != 120 {
		t.Fatal("wrong repair", u, d)
	}
	again, e := s.RepairHysteriaDirection(ctx, 1, cutoff, "same proof", filepath.Join(dir, "backups"))
	if e != nil || !again.AlreadyApplied {
		t.Fatal("repair not idempotent", again, e)
	}
	s.DB.QueryRow("SELECT SUM(upload_bytes),SUM(download_bytes) FROM traffic_daily WHERE instance_id=1").Scan(&u, &d)
	if u != 1300 || d != 120 {
		t.Fatal("swapped twice")
	}
	if _, e = s.Archive(ctx, time.Now()); e != nil {
		t.Fatal("post repair ledger damaged", e)
	}
	if _, e = s.DirectionPlan(ctx, 2, cutoff); e == nil {
		t.Fatal("non-HY repair allowed")
	}
}

func TestAuditReadOnlyDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.db")
	s, err := Open(path, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	ro, err := OpenReadOnly(path, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err = ro.DB.Exec("INSERT INTO settings VALUES('unexpected','write')"); err == nil {
		t.Fatal("dry-run database is writable")
	}
}

func TestRepairDoesNotSwapCorrectedRowsOrCurrentCursor(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(filepath.Join(dir, "mixed.db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.DB.Exec("INSERT INTO servers(name,address,created_at,updated_at) VALUES('s','','now','now')")
	s.DB.Exec("INSERT INTO instances(server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,'hy','hysteria2','localhost:1','now','now')")
	ctx := context.Background()
	now := time.Now().Add(-time.Minute)
	r := core.TrafficRecord{InstanceID: 1, ServerID: 1, Scope: "user", UserKey: "same", Source: "hysteria2", CounterMode: "cumulative", CollectedAt: now}
	s.Apply(ctx, 1, []core.TrafficRecord{r})
	r.UploadBytes = 10
	r.DownloadBytes = 20
	r.CollectedAt = now.Add(time.Second)
	s.Apply(ctx, 1, []core.TrafficRecord{r})
	cutoff := now.Add(2 * time.Second)
	r.EpochID = "hy-client-direction-v2"
	r.UploadBytes = 1000
	r.DownloadBytes = 100
	r.CollectedAt = now.Add(3 * time.Second)
	s.Apply(ctx, 1, []core.TrafficRecord{r})
	r.UploadBytes += 5
	r.DownloadBytes += 7
	r.CollectedAt = now.Add(4 * time.Second)
	s.Apply(ctx, 1, []core.TrafficRecord{r})
	if _, e = s.RepairHysteriaDirection(ctx, 1, now.Add(5*time.Second), "overbroad operator cutoff", filepath.Join(dir, "backups")); e == nil {
		t.Fatal("corrected observations accepted by overbroad repair")
	}
	if _, e = s.RepairHysteriaDirection(ctx, 1, cutoff, "known old TML data before selected cutoff", filepath.Join(dir, "backups")); e != nil {
		t.Fatal(e)
	}
	var u, d int64
	s.DB.QueryRow("SELECT SUM(upload_bytes),SUM(download_bytes) FROM traffic_daily").Scan(&u, &d)
	if u != 25 || d != 17 {
		t.Fatal("new direction contribution exchanged", u, d)
	}
	s.DB.QueryRow("SELECT raw_upload,raw_download FROM traffic_cursors").Scan(&u, &d)
	if u != 1005 || d != 107 {
		t.Fatal("current fixed cursor exchanged")
	}
}
