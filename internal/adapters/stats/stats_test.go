package stats

import (
	"testing"
	"traffic-manager-lite/internal/core"
)

func TestIncompleteAndInvalidCounters(t *testing.T) {
	r, e := Records(core.Instance{}, []Counter{{"user>>>a>>>traffic>>>uplink", 10}})
	if e != nil || len(r) != 0 {
		t.Fatal("incomplete pair emitted")
	}
	if _, e = Records(core.Instance{}, []Counter{{"user>>>a>>>traffic>>>uplink", -1}}); e == nil {
		t.Fatal("negative accepted")
	}
	if _, e = Records(core.Instance{}, []Counter{{"user>>>a>>>traffic>>>uplink", 1}, {"user>>>a>>>traffic>>>uplink", 1}}); e == nil {
		t.Fatal("duplicate accepted")
	}
}
