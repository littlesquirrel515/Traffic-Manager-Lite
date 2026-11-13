package storage_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

func TestTrafficUserNamesUseSavedOwnershipWithoutChangingAccounting(t *testing.T) {
	ctx := context.Background()
	s, e := storage.Open(filepath.Join(t.TempDir(), "traffic.db"), "UTC")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'server','display','now','now')")
	for id, kind := range []string{"xray", "singbox", "xray"} {
		exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(?,1,?,?,?,'now','now')", id+1, fmt.Sprintf("core-%d", id+1), kind, "localhost:1")
	}
	alice, e := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "Alice", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	bob, e := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "Bob", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	inbound := func(instance int64, tag string) string {
		t.Helper()
		id, err := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: instance, Tag: tag, Profile: core.NodeProfile{Protocol: "vless", Transport: "ws"}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	x, sa, sb, x2 := inbound(1, "ws"), inbound(2, "A"), inbound(2, "B"), inbound(3, "ws")
	sharedInbound := inbound(2, "Shared")
	sequence := 0
	bind := func(in, ref, owner, mode string, enabled int) {
		t.Helper()
		cid, err := remote.RegisterClient(ctx, s, remote.Client{InboundID: in, Reference: ref, UUID: "11111111-2222-4333-8444-555555555555", Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		sequence++
		exec("INSERT INTO subscriber_credentials(id,subscriber_id,inbound_id,credential_id,enabled,billable,usage_mode,created_at) VALUES(?,?,?,?,?,0,?,'now')", fmt.Sprintf("b-%d", sequence), owner, in, cid, enabled, mode)
	}
	counter := func(instance int64, tag, ref string) {
		t.Helper()
		now := time.Now()
		for n := 0; n < 2; n++ {
			if err := s.Apply(ctx, instance, []core.TrafficRecord{{InstanceID: instance, UserKey: ref, InboundTag: tag, Scope: "user", CounterMode: "cumulative", CollectedAt: now.Add(time.Duration(n) * time.Second), UploadBytes: int64(100 + n*20), DownloadBytes: int64(200 + n*30)}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	bind(x, "tml-node-known", alice, "exclusive", 1)
	counter(1, "", "tml-node-known")
	counter(1, "", "tml-node-unregistered")
	counter(1, "", "native-jane")
	exec("UPDATE users SET display_name='Jane <Operations>' WHERE display_name='native-jane'")
	bind(sa, "same", alice, "exclusive", 1)
	bind(sb, "same", bob, "exclusive", 1)
	counter(2, "A", "same")
	counter(2, "B", "same")
	counter(2, "", "same")
	bind(sharedInbound, "shared", alice, "shared_native", 1)
	bind(sharedInbound, "shared", bob, "shared_native", 1)
	counter(2, "Shared", "shared")
	bind(x2, "tml-node-known", bob, "exclusive", 1)
	counter(3, "", "tml-node-known")
	counter(1, "", "old-client")
	exec("INSERT INTO subscriber_usage_sources(subscriber_id,instance_id,inbound_id,client_ref,active,created_at) VALUES(?,1,'','old-client',0,'now')", alice)
	find := func(instance int64, tag, ref string) map[string]any {
		t.Helper()
		rows, err := s.TrafficUsers(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			for _, i := range row["identity_refs"].([]storage.TrafficIdentityRef) {
				if i.InstanceID == instance && i.InboundTag == tag && i.CoreUserKey == ref {
					return row
				}
			}
		}
		t.Fatal("missing traffic user", instance, tag, ref)
		return nil
	}
	totalsBefore, e := s.Traffic(ctx, storage.Filter{Scope: "user", Dimension: "user"}, false)
	if e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		instance       int64
		tag, ref, want string
	}{{1, "", "tml-node-known", "Alice"}, {1, "", "tml-node-unregistered", "tml-node-unregistered"}, {1, "", "native-jane", "Jane <Operations>"}, {2, "A", "same", "Alice"}, {2, "B", "same", "Bob"}, {2, "", "same", "same"}, {3, "", "tml-node-known", "Bob"}} {
		row := find(test.instance, test.tag, test.ref)
		if row["operator_name"] != test.want {
			t.Fatalf("%s: %v", test.ref, row["operator_name"])
		}
	}
	shared := find(2, "Shared", "shared")
	owners := shared["subscriber_owners"].([]storage.TrafficSubscriberName)
	if len(owners) != 2 || !owners[0].SharedNative || !owners[1].SharedNative {
		t.Fatal("shared account guessed as exclusive", owners)
	}
	old := find(1, "", "old-client")
	if !old["subscriber_owners"].([]storage.TrafficSubscriberName)[0].Historical {
		t.Fatal("historical ownership not labeled")
	}
	exec("INSERT INTO subscriber_usage_sources(subscriber_id,instance_id,inbound_id,client_ref,active,created_at) VALUES(?,1,'','old-client',1,'now')", bob)
	if row := find(1, "", "old-client"); row["operator_name"] != "Bob" || len(row["subscriber_owners"].([]storage.TrafficSubscriberName)) != 1 {
		t.Fatal("historical owner overrode current")
	}
	rows, e := s.TrafficUsers(ctx, "alice")
	if e != nil || len(rows) < 2 {
		t.Fatal("subscriber-name search failed", len(rows), e)
	}
	if rows, e = s.TrafficUsers(ctx, "native-jane"); e != nil || len(rows) != 1 || rows[0]["operator_name"] != "Jane <Operations>" {
		t.Fatal("raw identity search failed", e)
	}
	managed := find(1, "", "tml-node-known")
	exec("UPDATE users SET display_name='Custom alias' WHERE id=?", managed["id"])
	if row := find(1, "", "tml-node-known"); row["operator_name"] != "Custom alias" || row["name_source"] != "alias" {
		t.Fatal("manual alias overwritten")
	}
	var unknown int64
	s.DB.QueryRow("SELECT user_id FROM identities WHERE instance_id=1 AND core_user_key='tml-node-unregistered'").Scan(&unknown)
	exec("UPDATE identities SET user_id=? WHERE user_id=?", managed["id"], unknown)
	if row := find(1, "", "tml-node-known"); row["partial_ownership"] != true {
		t.Fatal("merged unresolved identity claimed as entirely owned")
	}
	// Only the explicit fixture merge changes grouping; name projection does not.
	exec("UPDATE identities SET user_id=? WHERE instance_id=1 AND core_user_key='tml-node-unregistered'", unknown)
	totalsAfter, e := s.Traffic(ctx, storage.Filter{Scope: "user", Dimension: "user"}, false)
	if e != nil || !reflect.DeepEqual(totalsBefore, totalsAfter) {
		t.Fatal("display lookup changed traffic totals", e)
	}
}
