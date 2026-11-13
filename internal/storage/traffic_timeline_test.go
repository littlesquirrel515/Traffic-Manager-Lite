package storage

import (
	"context"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func TestTrafficTimelineGrainsAndArchive(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, s.Location)
	for _, sample := range []struct {
		minute           int
		upload, download int64
	}{
		{1, 100, 200}, {10, 120, 230}, {40, 150, 270}, {70, 200, 330},
	} {
		apply(t, s, record(start.Add(time.Duration(sample.minute)*time.Minute), sample.upload, sample.download))
	}
	apply(t, s, core.TrafficRecord{InstanceID: 1, UserKey: "bob", Scope: "user", CounterMode: "cumulative", CollectedAt: start.Add(80 * time.Minute), UploadBytes: 100, DownloadBytes: 100})
	apply(t, s, core.TrafficRecord{InstanceID: 1, UserKey: "bob", Scope: "user", CounterMode: "cumulative", CollectedAt: start.Add(81 * time.Minute), UploadBytes: 180, DownloadBytes: 190})
	read := func(grain, subscriber string, user int64) TrafficTimeline {
		t.Helper()
		out, err := s.TrafficTimeline(ctx, HistoryFilter{From: "2026-10-01", Grain: grain, Subscriber: subscriber, UserID: user}, start)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var alice int64
	if err := s.DB.QueryRow("SELECT user_id FROM identities WHERE core_user_key='alice'").Scan(&alice); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"INSERT INTO subscribers(id,name,created_at,updated_at) VALUES('sub','Subscriber','now','now')",
		"INSERT INTO subscriber_usage_sources(subscriber_id,instance_id,inbound_id,client_ref,created_at) VALUES('sub',1,'','alice','now')",
	} {
		if _, err := s.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	half := read("half-hour", "", alice).Buckets
	if len(half) != 48 || half[0].Upload != 20 || half[1].Upload != 30 || half[2].Upload != 50 || half[3].State != "unobserved" {
		t.Fatal(half[:4])
	}
	if day := read("day", "sub", 0).Buckets[0]; day.Upload != 100 || day.Download != 130 {
		t.Fatal(day)
	}
	if day := read("day", "", 0).Buckets[0]; day.Upload != 180 || day.Download != 220 {
		t.Fatal(day)
	}
	if _, err := s.Archive(ctx, start.AddDate(0, 0, 31)); err != nil {
		t.Fatal(err)
	}
	if hour := read("hour", "sub", 0).Buckets; hour[0].Upload != 50 || hour[1].Upload != 50 {
		t.Fatal(hour[:2])
	}
	if half = read("half-hour", "sub", 0).Buckets; half[0].State != "archived" || half[1].State != "archived" || half[0].Upload != 0 {
		t.Fatal(half[:2])
	}
}
