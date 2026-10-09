package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "traffic.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	now := Stamp(time.Now())
	if _, e = s.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'test','hysteria2','http://localhost:9999',?,?)", now, now); e != nil {
		t.Fatal(e)
	}
	return s
}
func record(t time.Time, u, d int64) core.TrafficRecord {
	return core.TrafficRecord{InstanceID: 1, UserKey: "alice", Scope: "user", CounterMode: "cumulative", CollectedAt: t, UploadBytes: u, DownloadBytes: d}
}
func apply(t *testing.T, s *Store, r core.TrafficRecord) {
	t.Helper()
	if e := s.Apply(context.Background(), 1, []core.TrafficRecord{r}); e != nil {
		t.Fatal(e)
	}
}
func totals(t *testing.T, s *Store) (int64, int64) {
	t.Helper()
	var u, d int64
	if e := s.DB.QueryRow("SELECT COALESCE(SUM(upload_bytes),0),COALESCE(SUM(download_bytes),0) FROM traffic_daily").Scan(&u, &d); e != nil {
		t.Fatal(e)
	}
	return u, d
}
func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if e := s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestBaselineResetDuplicatesAndRestart(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	apply(t, s, record(now, 100, 200))
	if count(t, s, "traffic_samples") != 0 {
		t.Fatal("baseline counted")
	}
	r := record(now.Add(time.Second), 130, 270)
	apply(t, s, r)
	apply(t, s, r)
	apply(t, s, record(now.Add(2*time.Second), 5, 8))
	apply(t, s, record(now.Add(3*time.Second), 15, 28))
	u, d := totals(t, s)
	if u != 40 || d != 90 {
		t.Fatalf("reset/duplicate got %d/%d", u, d)
	}
	if e := s.Apply(ctx, 1, []core.TrafficRecord{record(now.Add(4*time.Second), -1, 100)}); e == nil {
		t.Fatal("negative accepted")
	}
	apply(t, s, record(now.Add(-time.Second), 500, 500))
	u, d = totals(t, s)
	if u != 40 || d != 90 {
		t.Fatal("older sample counted")
	}
	// Reopen the same SQLite file: a cursor survives process restart.
	var path string
	rows, e := s.DB.Query("PRAGMA database_list")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var seq int
		var name string
		if e = rows.Scan(&seq, &name, &path); e != nil {
			t.Fatal(e)
		}
	}
	rows.Close()
	s.Close()
	next, e := Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	apply(t, next, record(now.Add(4*time.Second), 20, 38))
	u, d = totals(t, next)
	if u != 45 || d != 100 {
		t.Fatalf("restart got %d/%d", u, d)
	}
}
func TestArchiveBoundariesAndIdempotence(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	baseline := now.AddDate(0, 0, -32)
	apply(t, s, record(baseline, 0, 0))
	apply(t, s, record(now.AddDate(0, 0, -31), 10, 20))
	apply(t, s, record(now.AddDate(0, 0, -30), 30, 60))
	apply(t, s, record(now.AddDate(0, 0, -29), 60, 120))
	n, e := s.Archive(ctx, now)
	if e != nil || n != 1 {
		t.Fatalf("first archive %d %v", n, e)
	}
	if count(t, s, "traffic_samples") != 2 {
		t.Fatal("day 30 was deleted")
	}
	n, e = s.Archive(ctx, now)
	if e != nil || n != 0 {
		t.Fatalf("repeat archive %d %v", n, e)
	}
	n, e = s.Archive(ctx, now.Add(24*time.Hour))
	if e != nil || n != 1 {
		t.Fatalf("day31 archive %d %v", n, e)
	}
	u, d := totals(t, s)
	if u != 60 || d != 120 {
		t.Fatal("historical totals lost")
	}
	n, e = s.Archive(ctx, now.Add(40*24*time.Hour))
	if e != nil || n != 1 {
		t.Fatalf("full archive %d %v", n, e)
	}
	n, e = s.Archive(ctx, now.Add(40*24*time.Hour))
	if e != nil || n != 0 {
		t.Fatalf("all repeat %d %v", n, e)
	}
}
func TestArchiveRollbackOnCorruptionMissingSummaryAndInterruption(t *testing.T) {
	for _, mode := range []string{"corrupt", "missing", "delete_failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			ctx := context.Background()
			now := time.Now().UTC()
			apply(t, s, record(now.AddDate(0, 0, -32), 0, 0))
			apply(t, s, record(now.AddDate(0, 0, -31), 10, 20))
			switch mode {
			case "corrupt":
				s.DB.Exec("UPDATE traffic_daily SET upload_bytes=999")
			case "missing":
				s.DB.Exec("DELETE FROM traffic_hourly")
			case "delete_failure":
				s.DB.Exec("CREATE TRIGGER fail_archive BEFORE DELETE ON traffic_samples BEGIN SELECT RAISE(ABORT,'injected failure'); END")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, e := s.Archive(ctx, now); e == nil {
				t.Fatal("failure accepted")
			}
			if count(t, s, "traffic_samples") != 1 || count(t, s, "archive_ledger") != 0 {
				t.Fatal("partial archive committed")
			}
		})
	}
}
func TestCollectionTransactionFailureDoesNotAdvanceCursor(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	apply(t, s, record(now, 100, 200))
	if _, e := s.DB.Exec("CREATE TRIGGER fail_sample BEFORE INSERT ON traffic_samples BEGIN SELECT RAISE(ABORT,'injected failure'); END"); e != nil {
		t.Fatal(e)
	}
	if e := s.Apply(context.Background(), 1, []core.TrafficRecord{record(now.Add(time.Second), 150, 300)}); e == nil {
		t.Fatal("failure not returned")
	}
	var raw int
	if e := s.DB.QueryRow("SELECT raw_upload FROM traffic_cursors").Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if raw != 100 || count(t, s, "traffic_hourly") != 0 {
		t.Fatal("partial transaction committed")
	}
	s.DB.Exec("DROP TRIGGER fail_sample")
	apply(t, s, record(now.Add(2*time.Second), 150, 300))
	u, d := totals(t, s)
	if u != 50 || d != 100 {
		t.Fatal("retry lost bytes")
	}
}
func TestCrossYearMonthAndArchivedRange(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	apply(t, s, record(time.Date(2025, 12, 31, 15, 59, 0, 0, time.UTC), 0, 0))
	apply(t, s, record(time.Date(2025, 12, 31, 15, 59, 30, 0, time.UTC), 10, 20))
	apply(t, s, record(time.Date(2025, 12, 31, 16, 0, 0, 0, time.UTC), 30, 60))
	apply(t, s, record(time.Date(2026, 1, 31, 16, 0, 0, 0, time.UTC), 60, 120))
	if _, e := s.Archive(ctx, time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)); e != nil {
		t.Fatal(e)
	}
	all, e := s.Traffic(ctx, Filter{From: "2025-12-31", To: "2026-02-01"}, false)
	if e != nil {
		t.Fatal(e)
	}
	if all[0]["upload"].(int64) != 60 {
		t.Fatal("cross range double counting")
	}
	jan, e := s.Traffic(ctx, Filter{From: "2026-01-01", To: "2026-01-31"}, false)
	if e != nil {
		t.Fatal(e)
	}
	if jan[0]["upload"].(int64) != 20 {
		t.Fatal("timezone/month boundary incorrect")
	}
	history, e := s.Traffic(ctx, Filter{}, true)
	if e != nil || len(history) != 3 {
		t.Fatalf("history %v %v", history, e)
	}
}
func TestTimezoneLockedAndBackup(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	apply(t, s, record(now, 0, 0))
	apply(t, s, record(now.Add(time.Second), 10, 20))
	path, e := s.Backup(context.Background(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	copy, e := Open(path, "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	u, d := totals(t, copy)
	copy.Close()
	if u != 10 || d != 20 {
		t.Fatal("backup wrong")
	}
	if _, e = Open(path, "UTC"); e == nil {
		t.Fatal("timezone reinterpretation allowed")
	}
}
func TestScopesAreNotAdded(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	u := record(now, 0, 0)
	in := core.TrafficRecord{Scope: "inbound", InboundTag: "in", CounterMode: "cumulative", CollectedAt: now}
	if e := s.Apply(context.Background(), 1, []core.TrafficRecord{u, in}); e != nil {
		t.Fatal(e)
	}
	u.CollectedAt = now.Add(time.Second)
	u.UploadBytes = 10
	in.CollectedAt = u.CollectedAt
	in.UploadBytes = 10
	if e := s.Apply(context.Background(), 1, []core.TrafficRecord{u, in}); e != nil {
		t.Fatal(e)
	}
	v, e := s.Traffic(context.Background(), Filter{}, false)
	if e != nil || v[0]["upload"].(int64) != 10 {
		t.Fatal("double counted scopes")
	}
}
