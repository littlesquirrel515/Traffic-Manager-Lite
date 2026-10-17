package subscription

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func TestQuotaDefaultsCalendarUsageResetAndStaleRevision(t *testing.T) {
	s, n, credentials := catalogFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Minute)
	apply := func(at time.Time, up, down int64) {
		t.Helper()
		if e := s.Apply(ctx, 1, []core.TrafficRecord{{Scope: "user", UserKey: "alice", Source: "xray", CounterMode: "cumulative", CollectedAt: at, UploadBytes: up, DownloadBytes: down}}); e != nil {
			t.Fatal(e)
		}
	}
	apply(now, 100, 200)
	id, e := SaveSubscriber(ctx, s, Subscriber{Name: "quota", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	q, e := ReadQuota(ctx, s.DB, id)
	if e != nil || q.Limit != 10737418240 || !q.Enabled || !q.ExpiryEnabled {
		t.Fatal(q, e)
	}
	activated, _ := time.Parse(time.RFC3339Nano, *q.ActivatedAt)
	end, _ := time.Parse(time.RFC3339Nano, *q.ExpiresAt)
	if !end.Equal(ThreeMonths(activated)) {
		t.Fatal("expiry moves or isn't calendar based")
	}
	if _, e = Bind(ctx, s, Binding{SubscriberID: id, InboundID: n.InboundID, CredentialID: credentials[0], Enabled: true}); e != nil {
		t.Fatal(e)
	}
	const gib = int64(1024 * 1024 * 1024)
	apply(now.Add(time.Second), 100+gib, 200+2*gib)
	// Both hourly and daily persist the deltas; the quota must use exactly one.
	u, e := SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Upload != gib || u.Download != 2*gib || u.Remaining == nil || *u.Remaining != 7*gib || u.Completeness != "complete" {
		t.Fatal(u, e)
	}
	if u.UserInfo() != "upload=1073741824; download=2147483648; total=10737418240; expire="+strings.Split(u.UserInfo(), "expire=")[1] {
		t.Fatal("header mapping")
	}
	update := QuotaUpdate{Quota: q}
	update.Limit = 20 * gib
	changed, e := UpdateQuota(ctx, s, id, update)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = UpdateQuota(ctx, s, id, update); !errors.Is(e, ErrQuotaConflict) {
		t.Fatal("stale revision accepted", e)
	}
	u, e = SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Used != 3*gib || *u.Remaining != 17*gib {
		t.Fatal(u, e)
	}
	var samples int
	s.DB.QueryRow("SELECT COUNT(*) FROM traffic_samples").Scan(&samples)
	changed, e = UpdateQuota(ctx, s, id, QuotaUpdate{Quota: changed, Reset: true, Confirmed: true})
	if e != nil {
		t.Fatal(e)
	}
	u, e = SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Used != 0 {
		t.Fatal(u, e)
	}
	var after int
	s.DB.QueryRow("SELECT COUNT(*) FROM traffic_samples").Scan(&after)
	if after != samples {
		t.Fatal("reset deleted facts")
	}
	apply(now.Add(2*time.Second), 100+2*gib, 200+3*gib)
	u, e = SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Used != 2*gib {
		t.Fatal(u, e)
	}
	changed.Limit = 2 * gib
	if _, e = UpdateQuota(ctx, s, id, QuotaUpdate{Quota: changed}); e != nil {
		t.Fatal(e)
	}
	u, e = SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Status != "quota_exhausted" || *u.Remaining != 0 || CheckQuotaAccess(u) == nil {
		t.Fatal(u, e)
	}
	for _, date := range []string{"2026-11-30T12:00:00Z", "2027-11-30T12:00:00Z"} {
		d, _ := time.Parse(time.RFC3339, date)
		r := ThreeMonths(d)
		if r.Month() != time.February || r.Day() != time.Date(r.Year(), 3, 0, 0, 0, 0, 0, time.UTC).Day() {
			t.Fatal(r)
		}
	}
}
func TestQuotaPartialUnavailableAndLegacyPreview(t *testing.T) {
	s, n, c := catalogFixture(t)
	ctx := context.Background()
	id, e := SaveSubscriber(ctx, s, Subscriber{Name: "partial", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	Bind(ctx, s, Binding{SubscriberID: id, InboundID: n.InboundID, CredentialID: c[0], Enabled: true})
	u, e := SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Completeness != "unavailable" || u.Remaining != nil || strings.Contains(u.UserInfo(), "upload=") {
		t.Fatal(u, e)
	}
	now := time.Now().Add(-time.Second)
	for pos := 0; pos < 2; pos++ {
		e = s.Apply(ctx, 1, []core.TrafficRecord{{Scope: "user", UserKey: "alice", Source: "singbox-native", Coverage: "observed_connection_deltas", CounterMode: "cumulative", CollectedAt: now.Add(time.Duration(pos) * time.Second), UploadBytes: int64(100 * pos), DownloadBytes: int64(200 * pos)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	u, e = SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Completeness != "partial" || u.Remaining != nil || u.Status != "quota_unknown" {
		t.Fatal(u, e)
	}
	_, e = s.DB.Exec("UPDATE subscribers SET quota_initialized=0,quota_enabled=0,expiry_enabled=0 WHERE id=?", id)
	if e != nil {
		t.Fatal(e)
	}
	u, e = SubscriberUsage(ctx, s.DB, id)
	if e != nil || u.Status != "migration_review" {
		t.Fatal(u, e)
	}
	_, e = UpdateQuota(ctx, s, id, QuotaUpdate{Quota: u.Quota})
	if e == nil {
		t.Fatal("legacy auto migration")
	}
}

func TestQuotaDeduplicatesSharedCounterAndFreezesRemovedBilling(t *testing.T) {
	s, n, c := catalogFixture(t)
	ctx := context.Background()
	now := time.Now().Add(-time.Minute)
	apply := func(pos int, up int64) {
		t.Helper()
		if e := s.Apply(ctx, 1, []core.TrafficRecord{{Scope: "user", UserKey: "alice", Source: "xray", CounterMode: "cumulative", CollectedAt: now.Add(time.Duration(pos) * time.Second), UploadBytes: up, DownloadBytes: up * 2}}); e != nil {
			t.Fatal(e)
		}
	}
	apply(0, 0)
	subscriber, e := SaveSubscriber(ctx, s, Subscriber{Name: "dedup", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.Exec(`INSERT INTO catalog_inbounds(id,instance_id,source_ref,tag,protocol,listen_port,profile_json,structure_hash,config_hash,discovered_at) SELECT 'second-inbound',instance_id,source_ref,'second',protocol,listen_port,profile_json,structure_hash,config_hash,discovered_at FROM catalog_inbounds WHERE id=?`, *n.InboundID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.Exec(`INSERT INTO credential_material(id,instance_id,inbound_tag,core_client_ref,user_id,auth_json,status,source,verified_at) SELECT 'second-credential',instance_id,'second',core_client_ref,user_id,auth_json,status,source,verified_at FROM credential_material WHERE id=?`, c[0])
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO inbound_client_keys VALUES('second-inbound','alice','second-credential')"); e != nil {
		t.Fatal(e)
	}
	first, e := Bind(ctx, s, Binding{SubscriberID: subscriber, InboundID: n.InboundID, CredentialID: c[0], Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	secondInbound := "second-inbound"
	second, e := Bind(ctx, s, Binding{SubscriberID: subscriber, InboundID: &secondInbound, CredentialID: "second-credential", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	apply(1, 100)
	u, e := SubscriberUsage(ctx, s.DB, subscriber)
	if e != nil || u.Used != 300 || len(u.Sources) != 1 {
		t.Fatal("duplicated shared user counter", u, e)
	}
	if e = Unbind(ctx, s, first); e != nil {
		t.Fatal(e)
	}
	if e = Unbind(ctx, s, second); e != nil {
		t.Fatal(e)
	}
	apply(2, 200)
	u, e = SubscriberUsage(ctx, s.DB, subscriber)
	if e != nil || u.Used != 300 {
		t.Fatal("unbinding lost usage or charged future traffic", u, e)
	}
	if _, e = Bind(ctx, s, Binding{SubscriberID: subscriber, InboundID: n.InboundID, CredentialID: c[0], Enabled: true}); e != nil {
		t.Fatal(e)
	}
	apply(3, 300)
	u, e = SubscriberUsage(ctx, s.DB, subscriber)
	if e != nil || u.Used != 600 {
		t.Fatal("rebinding charged paused traffic", u, e)
	}
}
