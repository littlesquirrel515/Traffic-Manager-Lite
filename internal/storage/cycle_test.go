package storage

import (
	"context"
	"testing"
	"time"
)

func TestCycleUsesOnlyLatestCollectionBatch(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	apply(t, s, record(now, 100, 200))
	apply(t, s, record(now.Add(time.Second), 130, 270))
	v, e := s.Traffic(context.Background(), Filter{Cycle: true}, false)
	if e != nil || v[0]["upload"].(int64) != 30 {
		t.Fatalf("cycle %v %v", v, e)
	}
	apply(t, s, record(now.Add(2*time.Second), 150, 300))
	v, e = s.Traffic(context.Background(), Filter{Cycle: true}, false)
	if e != nil || v[0]["upload"].(int64) != 20 {
		t.Fatal("cycle includes earlier batch")
	}
	apply(t, s, record(now.Add(3*time.Second), 150, 300))
	v, e = s.Traffic(context.Background(), Filter{Cycle: true}, false)
	if e != nil || len(v) != 0 {
		t.Fatal("unchanged counters show previous traffic")
	}
}
