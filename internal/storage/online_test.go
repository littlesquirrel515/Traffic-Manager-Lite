package storage

import (
	"context"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func TestOnlineOnlyUserHasIdentityWithoutFakeTraffic(t *testing.T) {
	s := fixture(t)
	if e := s.SaveOnline(context.Background(), 1, []core.OnlineRecord{{UserKey: "alice", Count: 2, Kind: "session", CollectedAt: time.Now()}}); e != nil {
		t.Fatal(e)
	}
	if count(t, s, "users") != 1 || count(t, s, "identities") != 1 || count(t, s, "traffic_cursors") != 0 || count(t, s, "traffic_samples") != 0 {
		t.Fatal("online snapshot fabricated traffic or lost user")
	}
	if e := s.SaveOnline(context.Background(), 1, []core.OnlineRecord{{UserKey: "alice", Count: 1, Kind: "session"}}); e != nil {
		t.Fatal(e)
	}
	if count(t, s, "users") != 1 {
		t.Fatal("duplicate online identity")
	}
	apply(t, s, record(time.Now(), 100, 200))
	if count(t, s, "traffic_samples") != 0 {
		t.Fatal("first observed traffic did not establish baseline")
	}
}
