package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func TestProviderSwitchDoesNotAddCounterOffsets(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.DB.Exec("INSERT INTO servers(name,address,created_at,updated_at) VALUES('s','','now','now')")
	s.DB.Exec("INSERT INTO instances(server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,'s','singbox','localhost:1','now','now')")
	now := time.Now()
	r := core.TrafficRecord{InstanceID: 1, ServerID: 1, Scope: "instance", Source: "singbox-native", UploadBytes: 100, DownloadBytes: 200, CounterMode: "cumulative", CollectedAt: now}
	apply := func() {
		t.Helper()
		if e := s.Apply(context.Background(), 1, []core.TrafficRecord{r}); e != nil {
			t.Fatal(e)
		}
	}
	apply()
	r.UploadBytes = 110
	r.DownloadBytes = 220
	r.CollectedAt = now.Add(time.Second)
	apply()
	r.Source = "singbox-clash"
	r.UploadBytes = 50000
	r.DownloadBytes = 90000
	r.CollectedAt = now.Add(2 * time.Second)
	apply()
	r.UploadBytes += 5
	r.DownloadBytes += 7
	r.CollectedAt = now.Add(3 * time.Second)
	apply()
	var u, d int64
	s.DB.QueryRow("SELECT SUM(upload_bytes),SUM(download_bytes) FROM traffic_daily").Scan(&u, &d)
	if u != 15 || d != 27 {
		t.Fatal("provider offsets counted", u, d)
	}
	r.Source = "singbox-native"
	r.CollectedAt = now.Add(4 * time.Second)
	apply()
	r.InstanceVersion = "1.14.3"
	r.CollectedAt = now.Add(5 * time.Second)
	apply()
	r.InstanceVersion = "1.15.0"
	r.UploadBytes += 1000
	r.CollectedAt = now.Add(6 * time.Second)
	apply()
	s.DB.QueryRow("SELECT SUM(upload_bytes),SUM(download_bytes) FROM traffic_daily").Scan(&u, &d)
	if u != 15 || d != 27 {
		t.Fatal("version change fabricated delta", u, d)
	}
	// A pre-v1.4 cursor has no proven provider: the first named source is a baseline.
	s.DB.Exec("UPDATE traffic_cursors SET provider_type=''")
	r.CollectedAt = now.Add(7 * time.Second)
	r.UploadBytes += 9000
	apply()
	s.DB.QueryRow("SELECT SUM(upload_bytes),SUM(download_bytes) FROM traffic_daily").Scan(&u, &d)
	if u != 15 || d != 27 {
		t.Fatal("unproven migrated provider fabricated delta", u, d)
	}
	var observations int
	if e := s.DB.QueryRow("SELECT COUNT(*) FROM traffic_counter_observations").Scan(&observations); e != nil || observations != 8 {
		t.Fatal("missing baseline/source-change evidence", observations, e)
	}
	if _, e := s.Archive(context.Background(), now.AddDate(0, 0, 31)); e != nil {
		t.Fatal(e)
	}
	var count int
	var eu, ed int64
	if e := s.DB.QueryRow("SELECT SUM(observations),SUM(upload_delta),SUM(download_delta) FROM archived_counter_evidence").Scan(&count, &eu, &ed); e != nil || count != 8 || eu != u || ed != d {
		t.Fatal("archive lost or duplicated provider evidence", count, eu, ed, e)
	}
	if _, e := s.Archive(context.Background(), now.AddDate(0, 0, 31)); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT SUM(observations) FROM archived_counter_evidence").Scan(&count)
	if count != 8 {
		t.Fatal("archive evidence duplicated", count)
	}
}
