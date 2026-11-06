package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscription"
	"traffic-manager-lite/migrations"
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
	otherGroup, err := catalog.SaveGroup(ctx, s, catalog.Group{Name: "independent", Enabled: true, NodeIDs: []string{ids[30]}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 46; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("104.16.1.%d:443", i))
	}
	limit := 500
	if err = worker.EditSources(ctx, job.ID, catalog.SyncSourceEdit{Sources: job.Config.Sources, Minutes: 10, Revision: job.Revision, Limit: &limit, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	jobs, err := worker.List(ctx)
	if err != nil || jobs[0].Config.Limit == nil || *jobs[0].Config.Limit != 500 {
		t.Fatalf("limit not retained: %v %v", jobs, err)
	}
	updated, err := worker.Sync(ctx, job.ID)
	if err != nil || updated.Config.Count != 60 {
		t.Fatalf("candidate shortage must publish available nodes: %d %v", updated.Config.Count, err)
	}
	if updated.Config.Limit == nil || *updated.Config.Limit != 500 {
		t.Fatal("configured limit lost during sync")
	}
	newIDs, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(newIDs) != 60 {
		t.Fatal(len(newIDs), err)
	}
	for i := range ids {
		if ids[i] != newIDs[i] {
			t.Fatal("existing node identity changed")
		}
	}
	jobs, err = worker.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	limit = 20
	if err = worker.EditSources(ctx, job.ID, catalog.SyncSourceEdit{Sources: job.Config.Sources, Minutes: 10, Revision: jobs[0].Revision, Limit: &limit, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.Sync(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	var active int
	if err = s.DB.QueryRow("select count(*) from preferred_sync_slots where job_id=? and active=1", job.ID).Scan(&active); err != nil || active != 20 {
		t.Fatalf("lower limit: %d %v", active, err)
	}
	activeMembers, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(activeMembers) != 20 {
		t.Fatalf("managed group must shrink: %d %v", len(activeMembers), err)
	}
	otherMembers, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{otherGroup})
	if err != nil || len(otherMembers) != 1 || otherMembers[0] != ids[30] {
		t.Fatalf("independent group changed: %v %v", otherMembers, err)
	}
	jobs, err = worker.List(ctx)
	if err != nil || !jobs[0].LimitReached || jobs[0].CandidateCount != 60 || jobs[0].ActiveCount != 20 {
		t.Fatalf("limit status: %v %v", jobs, err)
	}
	limit = 0
	if err = worker.EditSources(ctx, job.ID, catalog.SyncSourceEdit{Sources: job.Config.Sources, Minutes: 10, Revision: jobs[0].Revision, Limit: &limit, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	jobs, err = worker.List(ctx)
	if err != nil || jobs[0].Config.Limit == nil || *jobs[0].Config.Limit != 0 {
		t.Fatalf("auto mode was not persisted: %v %v", jobs, err)
	}
	if _, err = worker.Sync(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(restored) != 60 {
		t.Fatalf("managed group must restore: %d %v", len(restored), err)
	}
	for i := range newIDs {
		if restored[i] != newIDs[i] {
			t.Fatal("restoration changed stable identity")
		}
	}
	lines = lines[:10]
	if current, syncErr := worker.Sync(ctx, job.ID); syncErr != nil || current.Status != "ok" {
		t.Fatalf("successful smaller source must be healthy: %s %v", current.Status, syncErr)
	}
	if err = s.DB.QueryRow("select count(*) from preferred_sync_slots where job_id=? and active=1", job.ID).Scan(&active); err != nil || active != 10 {
		t.Fatalf("candidate decrease: %d %v", active, err)
	}
	members, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(members) != 10 {
		t.Fatalf("candidate decrease must update group: %d %v", len(members), err)
	}
	var grants int
	if err = s.DB.QueryRow("select count(*) from subscriber_node_grants").Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("unexpected grants %d %v", grants, err)
	}
	max := 500
	short, err := worker.Create(ctx, catalog.SyncConfig{Name: "short-feed", Template: tpl.NodeID, Sources: []string{"https://a.example.com/a"}, Limit: &max, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err != nil || short.Config.Count != 10 || short.Config.Limit == nil || *short.Config.Limit != 500 {
		t.Fatalf("initial 500 limit with 10 candidates: %#v %v", short.Config, err)
	}
	_, err = worker.Create(ctx, catalog.SyncConfig{Name: "bad", Template: tpl.NodeID, Sources: []string{"https://a.example.com/a", "https://bad.example.com/a"}, Count: 0, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err == nil {
		t.Fatal("failed source must not silently create incomplete group")
	}
}

func TestPreferredSyncLongSourceUsesLimitWithoutRejecting(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	inbound, err := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "many-source", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/ws", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: inbound, Name: "template", Enabled: true, Profile: map[string]any{"address": "origin.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 510)
	for i := range lines {
		lines[i] = fmt.Sprintf("104.16.%d.%d:443", i/254+1, i%254+1)
	}
	w := &catalog.PreferredSync{Store: s}
	w.TestSetFetcher(func(context.Context, string, string) (string, error) { return strings.Join(lines, "\n"), nil })
	limit := 4
	j, err := w.Create(ctx, catalog.SyncConfig{Name: "long source", Template: tpl.NodeID, Sources: []string{"https://a.example.com/list"}, Limit: &limit, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err != nil || j.Config.Count != 4 {
		t.Fatalf("long source failed: %#v %v", j.Config, err)
	}
	jobs, err := w.List(ctx)
	if err != nil || len(jobs) != 1 || !jobs[0].LimitReached || jobs[0].Sources[0].Truncated != 10 || jobs[0].ActiveCount != 4 {
		t.Fatalf("missing truncation disclosure: %#v %v", jobs, err)
	}
}

func TestSyncActiveMembershipMigrationAndManualChange(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	in, err := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "membership", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/ws", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: in, Name: "template", Enabled: true, Profile: map[string]any{"address": "origin.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := &catalog.PreferredSync{Store: s}
	worker.TestSetFetcher(func(context.Context, string, string) (string, error) {
		return "104.16.1.1\n104.16.1.2\n104.16.1.3", nil
	})
	job, err := worker.Create(ctx, catalog.SyncConfig{Name: "migration", Template: tpl.NodeID, Sources: []string{"https://a.example.com/list"}, Count: 0, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(ids) != 3 {
		t.Fatal(ids, err)
	}
	other, err := catalog.SaveGroup(ctx, s, catalog.Group{Name: "other", Enabled: true, NodeIDs: []string{ids[2]}})
	if err != nil {
		t.Fatal(err)
	}
	user, err := subscription.SaveSubscriber(ctx, s, subscription.Subscriber{Name: "migration-user", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO subscriber_group_grants VALUES(?,?,?)", user, job.GroupID, "now"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE preferred_sync_slots SET active=0 WHERE node_id=?", ids[2]); err != nil {
		t.Fatal(err)
	}
	script, err := migrations.Files.ReadFile("011_sync_active_members.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	members, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(members) != 2 {
		t.Fatal(members, err)
	}
	assigned, err := subscription.NodeAssignments(ctx, s.DB, user)
	if err != nil || len(assigned) != 2 {
		t.Fatalf("subscriber must follow active group: %v %v", assigned, err)
	}
	otherMembers, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{other})
	if err != nil || len(otherMembers) != 1 || otherMembers[0] != ids[2] {
		t.Fatal(otherMembers, err)
	}
	var slots, grants int
	if err = s.DB.QueryRow("SELECT count(*) FROM preferred_sync_slots WHERE job_id=?", job.ID).Scan(&slots); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM subscriber_group_grants WHERE subscriber_id=?", user).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if slots != 3 || grants != 1 {
		t.Fatal(slots, grants)
	}
	if _, err = worker.Sync(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	members, err = subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(members) != 3 || members[2] != ids[2] {
		t.Fatal("reactivation identity", members, err)
	}
	assigned, err = subscription.NodeAssignments(ctx, s.DB, user)
	if err != nil || len(assigned) != 3 {
		t.Fatalf("subscriber did not recover group member: %v %v", assigned, err)
	}
	if _, err = s.DB.Exec("DELETE FROM node_group_members WHERE group_id=? AND node_id=?", job.GroupID, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.Sync(ctx, job.ID); err == nil {
		t.Fatal("manual group edit must block sync")
	}
	members, err = subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{job.GroupID})
	if err != nil || len(members) != 2 {
		t.Fatal("manual edit overwritten", members, err)
	}
}

func TestPreferredSyncHistoryPreservesPublishedOnSourceFailure(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	in, err := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "history", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/ws", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: in, Name: "history-template", Enabled: true, Profile: map[string]any{"address": "origin.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	a, b := "https://a.example.com/private/path", "https://b.example.com/secret/path"
	feeds := map[string]string{a: "104.16.1.1\n104.16.1.2", b: "104.16.1.3\n104.16.1.4"}
	w := &catalog.PreferredSync{Store: s}
	w.TestSetFetcher(func(_ context.Context, u, _ string) (string, error) {
		v, ok := feeds[u]
		if !ok {
			return "", fmt.Errorf("sensitive raw error")
		}
		return v, nil
	})
	j, err := w.Create(ctx, catalog.SyncConfig{Name: "history", Template: tpl.NodeID, Sources: []string{a, b}, Count: 0, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := w.History(ctx, j.ID)
	if err != nil || len(initial) != 1 || initial[0].Trigger != "initial" || initial[0].ActiveCount != 4 || len(initial[0].Sources) != 2 || initial[0].Sources[0].Valid != 2 {
		t.Fatalf("initial history %#v %v", initial, err)
	}
	ids, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{j.GroupID})
	if err != nil {
		t.Fatal(err)
	}
	before := []string{}
	for _, id := range ids {
		n, e := catalog.ResolveDefinition(ctx, s.DB, id)
		if e != nil {
			t.Fatal(e)
		}
		before = append(before, n.Profile.Address)
	}
	delete(feeds, a)
	feeds[b] = "104.16.10.1\n104.16.10.2\n104.16.10.3\n104.16.10.4\n104.16.10.5"
	got, err := w.Sync(ctx, j.ID)
	if err != nil || got.Status != "degraded" {
		t.Fatal(got, err)
	}
	afterIDs, err := subscription.ExpandNodeSelection(ctx, s.DB, nil, []string{j.GroupID})
	if err != nil || fmt.Sprint(ids) != fmt.Sprint(afterIDs) {
		t.Fatal("members changed on partial failure", afterIDs, err)
	}
	for i, id := range ids {
		n, e := catalog.ResolveDefinition(ctx, s.DB, id)
		if e != nil || n.Profile.Address != before[i] {
			t.Fatal("address changed on partial failure", i, e)
		}
	}
	history, err := w.History(ctx, j.ID)
	if err != nil || len(history) != 2 || history[0].Trigger != "manual" || history[0].Status != "degraded" || history[0].ActiveCount != 4 {
		t.Fatalf("history result %#v %v", history, err)
	}
	v := history[0].Sources
	if len(v) != 2 || v[0].Status != "cached" || v[0].ErrorCode != "fetch_failed" || v[0].Valid != 0 || v[0].CachedCount != 2 || v[1].Valid != 5 {
		t.Fatalf("per-source metrics %#v", v)
	}
	raw, _ := json.Marshal(history)
	if strings.Contains(string(raw), "private/path") || strings.Contains(string(raw), "secret/path") || strings.Contains(string(raw), "104.16.") || strings.Contains(string(raw), "sensitive raw error") {
		t.Fatal("history leaked data or secret")
	}
	delete(feeds, b)
	if got, err = w.Sync(ctx, j.ID); err != nil || got.Status != "stale" {
		t.Fatal(got, err)
	}
	history, err = w.History(ctx, j.ID)
	if err != nil || history[0].Status != "stale" || history[0].ActiveCount != 4 {
		t.Fatal(history, err)
	}
	// An explicit manual membership change is blocked and logged without leaking the underlying error.
	feeds[a] = "104.16.1.1\n104.16.1.2"
	feeds[b] = "104.16.1.3\n104.16.1.4"
	if _, err = s.DB.Exec("DELETE FROM node_group_members WHERE group_id=? AND node_id=?", j.GroupID, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Sync(ctx, j.ID); err == nil {
		t.Fatal("manual change should block sync")
	}
	history, err = w.History(ctx, j.ID)
	if err != nil || history[0].Status != "blocked" || history[0].ErrorCode != "group_changed" || history[0].ActiveCount != 4 {
		t.Fatal(history, err)
	}
	if _, err = s.DB.Exec("INSERT INTO node_group_members VALUES(?,?,?)", j.GroupID, ids[0], 0); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		_, err = tx.Exec(`INSERT INTO preferred_sync_runs(job_id,trigger,started_at,finished_at,duration_ms,status,candidate_count,active_count,slot_count,limit_value,sources_json,error_code) VALUES(?,'manual','now','now',0,'ok',0,0,0,500,'[]','')`, j.ID)
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Sync(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	history, err = w.History(ctx, j.ID)
	if err != nil || len(history) != 200 {
		t.Fatalf("history retention %d %v", len(history), err)
	}
	// The group grant and node history remain intact; individual source failures never reset counters.
	var n int
	if err = s.DB.QueryRow("select count(*) from preferred_sync_slots where job_id=? and active=1", j.ID).Scan(&n); err != nil || n != 4 {
		t.Fatal(n, err)
	}
}

func TestPreferredSyncScheduledRunRecordsTrigger(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	in, err := remote.SaveManual(ctx, s, remote.Inbound{InstanceID: 1, Tag: "scheduler", Profile: core.NodeProfile{Protocol: "vless", Transport: "ws", Path: "/ws", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: in, Name: "scheduler-template", Enabled: true, Profile: map[string]any{"address": "origin.example.com", "port": 443, "sni": "origin.example.com", "host": "origin.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	w := &catalog.PreferredSync{Store: s}
	w.TestSetFetcher(func(context.Context, string, string) (string, error) { return "104.16.1.1", nil })
	j, err := w.Create(ctx, catalog.SyncConfig{Name: "scheduler", Template: tpl.NodeID, Sources: []string{"https://a.example.com/list"}, Count: 0, Minutes: 10, Ports: []int{443}, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE preferred_sync_jobs SET next_run=0 WHERE id=?", j.ID); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); w.Run(workerCtx) }()
	deadline := time.Now().Add(19 * time.Second)
	for time.Now().Before(deadline) {
		h, e := w.History(ctx, j.ID)
		if e != nil {
			t.Fatal(e)
		}
		if len(h) >= 2 && h[0].Trigger == "scheduled" {
			cancel()
			<-done
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("scheduled synchronization not recorded")
}
