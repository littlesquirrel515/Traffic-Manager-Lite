package coremonitor

import (
	"context"
	"path/filepath"
	"testing"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/storage"
)

func TestRegisteredSingboxClientsMatchWithoutConfigOrTraffic(t *testing.T) {
	s, e := storage.Open(filepath.Join(t.TempDir(), "db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, `INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','singbox','localhost:1','now','now')`} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	svc := Service{Store: s}
	i := core.Instance{ID: 1, CoreType: "singbox"}
	makeClient := func(tag string) string {
		t.Helper()
		id, e := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: tag, Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Enabled: true}})
		if e != nil {
			t.Fatal(e)
		}
		for n := 0; n < 2; n++ {
			_, e = remote.RegisterClient(ctx, s, remote.Client{InboundID: id, Reference: "alice", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
			if e != nil {
				t.Fatal(e)
			}
		}
		return id
	}
	first := makeClient("ws")
	registered := observation{}
	svc.clients(i, &registered)
	if len(registered.clients) != 1 || registered.clientsOK {
		t.Fatal("manual inventory must be visible without claiming complete enumeration")
	}
	if !svc.knownAsset(i, &observation{}, "ws", "alice") {
		t.Fatal("manual asset filtered out of native snapshots")
	}
	if tag, ok := svc.uniqueUserInbound(i, &observation{}, "alice"); !ok || tag != "ws" {
		t.Fatal("manual identity unavailable to compatibility stats")
	}
	var n int
	for _, table := range []string{"core_clients", "users", "identities"} {
		if e = s.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 1 {
			t.Fatalf("idempotent offline asset %s: %d %v", table, n, e)
		}
	}
	second := makeClient("tls")
	if _, ok := svc.uniqueUserInbound(i, &observation{}, "alice"); ok {
		t.Fatal("ambiguous cross-inbound name attributed")
	}
	if !svc.knownAsset(i, &observation{}, "tls", "alice") {
		t.Fatal("native inbound+name cannot resolve duplicate names")
	}
	if _, e = s.DB.Exec("UPDATE catalog_inbounds SET retired=1 WHERE id=?", second); e != nil {
		t.Fatal(e)
	}
	if svc.knownAsset(i, &observation{}, "tls", "alice") {
		t.Fatal("retired asset used for new attribution")
	}
	// Successful empty config enumeration is authoritative for that observation;
	// it must not be repopulated from stale manual assertions.
	if svc.knownAsset(i, &observation{clientsOK: true}, "ws", "alice") {
		t.Fatal("complete empty source overridden")
	}
	if _, e = s.DB.Exec("UPDATE catalog_inbounds SET retired=1 WHERE id=?", first); e != nil {
		t.Fatal(e)
	}
	if _, ok := svc.uniqueUserInbound(i, &observation{}, "alice"); ok {
		t.Fatal("retired assets attributed")
	}
}
