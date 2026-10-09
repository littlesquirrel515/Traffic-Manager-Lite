package storage

import (
	"testing"
	"time"
)

func TestObservedRestartWithLargerCounterEstablishesBaseline(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	oldBoot := now.Add(-time.Hour)
	r := record(now, 100, 200)
	r.BootEstimate = &oldBoot
	apply(t, s, r)
	r = record(now.Add(time.Second), 120, 240)
	r.BootEstimate = &oldBoot
	apply(t, s, r)
	newBoot := now.Add(2 * time.Second)
	r = record(now.Add(5*time.Second), 1000, 2000)
	r.BootEstimate = &newBoot
	apply(t, s, r)
	u, d := totals(t, s)
	if u != 20 || d != 40 {
		t.Fatal("restart's existing bytes treated as observed delta")
	}
	r = record(now.Add(6*time.Second), 1010, 2020)
	r.BootEstimate = &newBoot
	apply(t, s, r)
	u, d = totals(t, s)
	if u != 30 || d != 60 {
		t.Fatal("post restart delta incorrect")
	}
}
