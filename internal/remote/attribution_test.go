package remote

import (
	"context"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

func TestTrafficAttributionNeverDuplicatesAcrossEndpoints(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	inbound, e := SaveManual(ctx, s, Inbound{InstanceID: 1, Tag: "alice-in", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = RegisterClient(ctx, s, Client{InboundID: inbound, Reference: "alice", UUID: fixtureUUID, Confirmed: true}); e != nil {
		t.Fatal(e)
	}
	other, e := SaveManual(ctx, s, Inbound{InstanceID: 1, Tag: "bob-in", Profile: core.NodeProfile{Protocol: "trojan", Transport: "tcp"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = RegisterClient(ctx, s, Client{InboundID: other, Reference: "bob", Password: "fixture-only", Confirmed: true}); e != nil {
		t.Fatal(e)
	}
	one, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "only", Enabled: true, Profile: map[string]any{"address": "edge.example", "port": 443}})
	if e != nil {
		t.Fatal(e)
	}
	r := core.TrafficRecord{InstanceID: 1, UserKey: "alice", Scope: "user", CounterMode: "cumulative", CollectedAt: time.Now(), UploadBytes: 1000, DownloadBytes: 2000}
	if e = s.Apply(ctx, 1, []core.TrafficRecord{r}); e != nil {
		t.Fatal(e)
	}
	r.CollectedAt = r.CollectedAt.Add(time.Second)
	r.UploadBytes += 10
	r.DownloadBytes += 20
	if e = s.Apply(ctx, 1, []core.TrafficRecord{r}); e != nil {
		t.Fatal(e)
	}
	rows, e := s.Traffic(ctx, storage.Filter{Dimension: "protocol"}, false)
	if e != nil || len(rows) != 1 || rows[0]["dimension"] != "vless" {
		t.Fatal("unrelated protocol changed attribution", rows, e)
	}
	rows, e = s.Traffic(ctx, storage.Filter{Dimension: "node"}, false)
	if e != nil || len(rows) != 1 || rows[0]["dimension"] != one.NodeID {
		t.Fatal("not a Proxy Node identity", rows, e)
	}
	if _, e = catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "second", Enabled: true, Profile: map[string]any{"address": "second.example", "port": 8443}}); e != nil {
		t.Fatal(e)
	}
	rows, e = s.Traffic(ctx, storage.Filter{Dimension: "node"}, false)
	if e != nil || len(rows) != 1 || rows[0]["dimension"] != "未归属" || rows[0]["upload"] != int64(10) || rows[0]["download"] != int64(20) {
		t.Fatal("shared meter duplicated or arbitrarily assigned", rows, e)
	}
}
