package systeminfo

import (
	"context"
	"encoding/json"
	"github.com/shirou/gopsutil/v4/cpu"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRatesAndResets(t *testing.T) {
	a, b, cores := 2.0, 4.0, 0.5
	old := counters{at: time.Unix(100, 0), hostOK: true, host: cpu.TimesStat{User: 10, Idle: 20, Iowait: 5, Guest: 99}, process: &a, group: &a}
	now := counters{at: old.at.Add(4 * time.Second), hostOK: true, host: cpu.TimesStat{User: 12, Idle: 25, Iowait: 6, Guest: 199}, process: &b, group: &b}
	var out Snapshot
	out.Container.CPUCores = &cores
	applyRates(&out, old, now)
	if out.CPU.Percent == nil || *out.CPU.Percent != 25 || *out.CPU.IOWaitPercent != 12.5 || *out.Process.CPUPercent != 50 || *out.Container.CPUCapacityPercent != 100 {
		t.Fatalf("incorrect rates: %+v %+v", out.CPU, out.Container)
	}
	applyRates(&Snapshot{}, counters{}, now)
	if counterRate(&b, &a, 4) != nil || counterRate(nil, &b, 4) != nil || counterRate(&a, &b, 0) != nil {
		t.Fatal("counter reset or missing sample fabricated rate")
	}
}

func TestIndependentSnapshotCacheAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Disks: []DiskPath{{Name: "test", Path: dir}}, Files: []DiskPath{{Name: "present", Path: file}, {Name: "absent", Path: file + "-missing"}}})
	first := c.Snapshot(context.Background())
	second := c.Snapshot(context.Background())
	if !first.ObservedAt.Equal(second.ObservedAt) || first.Process.PID != int32(os.Getpid()) || first.Process.Goroutines == 0 || first.Process.CPUPercent != nil {
		t.Fatal("cache/process/initial sample invalid")
	}
	if *first.Files[0].Size != 3 || first.Files[1].Status != "absent" {
		t.Fatal(first.Files)
	}
	if _, err := json.Marshal(first); err != nil {
		t.Fatal(err)
	}
}

func TestHostAddressFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fib_trie"), []byte(" |-- 10.0.0.2\n /32 host LOCAL\n |-- 127.0.0.1\n /32 host LOCAL\n |-- 10.0.0.2\n /32 host LOCAL\n"), 0600)
	os.WriteFile(filepath.Join(dir, "if_inet6"), []byte("20010db8000000000000000000000001 02 40 00 80 eth0\n"), 0600)
	addresses, err := hostAddresses(dir)
	if err != nil || len(addresses) != 2 || addresses[0].IP != "10.0.0.2" || addresses[0].Scope != "private" {
		t.Fatalf("%+v %v", addresses, err)
	}
}
