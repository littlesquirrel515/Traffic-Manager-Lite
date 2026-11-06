package catalog_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscription"
)

func TestPreferredSyncKeepsAuthorizedSlotsAndExistingUnifiedURL(t *testing.T) {
	s, dir := fixture(t)
	ctx := context.Background()
	inbound, e := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "sync", Profile: core.NodeProfile{Protocol: "vless", Transport: "xhttp", Path: "/fixed", XHTTPMode: "packet-up", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	tpl, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "template", Enabled: true, Profile: map[string]any{"address": "origin.example.com", "port": 443, "sni": "origin.example.com", "host": "http.example.com"}})
	if e != nil {
		t.Fatal(e)
	}
	a, b := "https://a.example.com/us.txt", "https://b.example.com/us.txt"
	feeds := map[string]string{a: "104.16.1.1\n104.16.1.2", b: "104.16.1.1\n104.16.1.3"}
	worker := &catalog.PreferredSync{Store: s}
	worker.TestSetFetcher(func(_ context.Context, u, _ string) (string, error) {
		if x := feeds[u]; x != "" {
			return x, nil
		}
		return "", fmt.Errorf("fixture failure")
	})
	job, e := worker.Create(ctx, catalog.SyncConfig{Name: "US", Template: tpl.NodeID, Sources: []string{a, b}, Count: 2, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	ids, e := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if e != nil || len(ids) != 2 {
		t.Fatal(ids, e)
	}
	n, e := catalog.ResolveDefinition(ctx, s.DB, ids[1])
	if e != nil || n.Profile.Address != "104.16.1.3" {
		t.Fatal("sources did not alternate/dedupe", n, e)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := s.DB.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	user, e := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "Alice", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	exec("UPDATE subscribers SET node_authorization_required=1 WHERE id=?", user)
	cred, e := remote.RegisterClient(ctx, s, remote.Client{InboundID: inbound, Reference: "alice", UUID: "11111111-1111-4111-8111-111111111111", Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = subscription.Bind(ctx, s, subscription.Binding{SubscriberID: user, InboundID: &inbound, CredentialID: cred, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	for _, id := range ids {
		exec("INSERT INTO subscriber_node_grants VALUES(?,?,?,'before')", user, id, inbound)
	}
	key := filepath.Join(dir, "managed.key")
	if e = os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	link, e := subscription.UnifiedLink(ctx, s, key, user)
	if e != nil {
		t.Fatal(e)
	}
	check := func(format string, present, absent []string) {
		t.Helper()
		res, _, e := subscription.CatalogOutput(ctx, s, link.Token, format)
		if e != nil {
			t.Fatal(e)
		}
		output := string(res.Content)
		for _, v := range present {
			if !strings.Contains(output, v) {
				t.Fatalf("missing expected %s in %s", v, format)
			}
		}
		for _, v := range absent {
			if strings.Contains(output, v) {
				t.Fatalf("unexpected %s in %s", v, format)
			}
		}
	}
	check("raw", []string{"104.16.1.1", "104.16.1.3", "origin.example.com", "packet-up"}, nil)
	feeds[a] = "104.16.1.4\n104.16.1.2:8443"
	feeds[b] = "104.16.1.3"
	j, e := worker.Sync(ctx, job.ID)
	if e != nil || j.Status != "ok" {
		t.Fatal(j, e)
	}
	// The surviving address remains in its original slot, the other slot rotates.
	check("mihomo", []string{"104.16.1.4", "104.16.1.3", "origin.example.com", "/fixed"}, []string{"104.16.1.1", "104.16.1.2"})
	if got, e := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID}); e != nil || fmt.Sprint(got) != fmt.Sprint(ids) {
		t.Fatal("slot IDs changed", got, e)
	}
	exec("INSERT INTO subscriber_node_revocations(subscriber_id,node_id,created_at) VALUES(?,?,'revoked')", user, ids[1])
	delete(feeds, a)
	if j, e = worker.Sync(ctx, job.ID); e != nil || j.Status != "degraded" {
		t.Fatal(j, e)
	}
	delete(feeds, b)
	if j, e = worker.Sync(ctx, job.ID); e != nil || j.Status != "stale" {
		t.Fatal(j, e)
	}
	check("raw", []string{"104.16.1.4"}, []string{"104.16.1.3"})
	feeds[a] = "104.16.1.5"
	feeds[b] = "104.16.1.5"
	if _, e = worker.Sync(ctx, job.ID); e != nil {
		t.Fatal(e)
	}
	check("raw", []string{"104.16.1.5"}, []string{"104.16.1.4", "104.16.1.3"})
	if active, e := catalog.SyncSlotAvailable(ctx, s.DB, ids[1]); e != nil || active {
		t.Fatal("empty slot published", active, e)
	}
	exec("UPDATE node_profiles SET enabled=0 WHERE id=?", ids[1])
	feeds[a] = "104.16.1.6\n104.16.1.7"
	if _, e = worker.Sync(ctx, job.ID); e == nil {
		t.Fatal("manual change was overwritten")
	}
	check("raw", []string{"104.16.1.5"}, []string{"104.16.1.6"})
	if e = worker.Enable(ctx, job.ID, false); e != nil {
		t.Fatal(e)
	}
	if _, e = worker.Sync(ctx, job.ID); e == nil {
		t.Fatal("paused job ran")
	}
	again, e := subscription.UnifiedLink(ctx, s, key, user)
	if e != nil || again.Token != link.Token {
		t.Fatal("subscription token changed", e)
	}
	var count int
	if e = s.DB.QueryRow("SELECT count(*) FROM credential_material").Scan(&count); e != nil || count != 1 {
		t.Fatal("sync touched credentials", count, e)
	}
}

func TestPreferredSyncAllSourcesCreatesMoreThanThirtyWithoutGrant(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	inbound, err := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "many", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/ws", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "template", Enabled: true, Profile: map[string]any{"address": "origin.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 1; i <= 45; i++ {
		lines = append(lines, fmt.Sprintf("104.16.1.%d:443", i))
	}
	worker := &catalog.PreferredSync{Store: s}
	worker.TestSetFetcher(func(_ context.Context, u, _ string) (string, error) {
		if strings.Contains(u, "bad") {
			return "", fmt.Errorf("failed")
		}
		return strings.Join(lines, "\n"), nil
	})
	job, err := worker.Create(ctx, catalog.SyncConfig{Name: "SG", Template: tpl.NodeID, Sources: []string{"https://a.example.com/a", "https://b.example.com/b"}, Count: 0, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(ids) != 45 || job.Config.Count != 45 {
		t.Fatalf("count=%d job=%d error=%v", len(ids), job.Config.Count, err)
	}
	for i := 46; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("104.16.1.%d:443", i))
	}
	newCount := 55
	if err = worker.EditSources(ctx, job.ID, catalog.SyncSourceEdit{Sources: job.Config.Sources, Minutes: 10, Revision: job.Revision, Count: &newCount, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	newIDs, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(newIDs) != 55 {
		t.Fatalf("expanded %d %v", len(newIDs), err)
	}
	for i := range ids {
		if ids[i] != newIDs[i] {
			t.Fatal("existing node identity changed")
		}
	}
	jobs, err := worker.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	shrink := 20
	if err = worker.EditSources(ctx, job.ID, catalog.SyncSourceEdit{Sources: job.Config.Sources, Minutes: 10, Revision: jobs[0].Revision, Count: &shrink, Confirmed: true}); err == nil {
		t.Fatal("unexpected shrink")
	}
	updated, err := worker.Sync(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config.Count != 60 {
		t.Fatalf("automatic growth: %d", updated.Config.Count)
	}
	grown, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(grown) != 60 {
		t.Fatalf("automatic growth nodes: %d %v", len(grown), err)
	}
	lines = lines[:10]
	if _, err = worker.Sync(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	var active int
	if err = s.DB.QueryRow("select count(*) from preferred_sync_slots where job_id=? and active=1", job.ID).Scan(&active); err != nil || active != 10 {
		t.Fatalf("automatic shrink publication %d %v", active, err)
	}
	var grants int
	if err = s.DB.QueryRow("select count(*) from subscriber_node_grants").Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("unexpected grants %d %v", grants, err)
	}
	_, err = worker.Create(ctx, catalog.SyncConfig{Name: "bad", Template: tpl.NodeID, Sources: []string{"https://a.example.com/a", "https://bad.example.com/a"}, Count: 0, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err == nil {
		t.Fatal("failed source must not silently create incomplete group")
	}
}
