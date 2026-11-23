//go:build linux

package systeminfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCgroupV2LimitsAndCounters(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "app")
	os.Mkdir(child, 0700)
	write := func(dir, name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(root, "memory.max", "1000")
	write(root, "cpu.max", "50000 100000")
	write(child, "memory.max", "2000")
	write(child, "memory.current", "600")
	write(child, "memory.stat", "inactive_file 100\n")
	write(child, "memory.events", "oom_kill 2\n")
	write(child, "cpu.max", "100000 100000")
	write(child, "cpu.stat", "usage_usec 2500000\nnr_throttled 7\n")
	write(child, "cpuset.cpus.effective", "0-3")
	mount := "1 0 0:1 / " + root + " rw - cgroup2 cgroup rw"
	out, seconds := readContainer("0::/app", mount)
	if out.Status != "available" || *out.MemoryLimit != 1000 || *out.MemoryWorkingSet != 500 || *out.CPUCores != 0.5 || *seconds != 2.5 || *out.OOMKills != 2 || *out.ThrottleCount != 7 {
		t.Fatalf("%+v %v", out, seconds)
	}
	write(child, "memory.stat", "inactive_file 9999")
	out, _ = readContainer("0::/app", mount)
	if *out.MemoryWorkingSet != 0 {
		t.Fatal("working set underflow")
	}
	os.Remove(filepath.Join(child, "memory.stat"))
	out, _ = readContainer("0::/app", mount)
	if out.MemoryWorkingSet != nil {
		t.Fatal("fabricated working set")
	}
	private := groupLocation("0::/", "1 0 0:1 /host/app "+root+" rw - cgroup2 cgroup rw", "")
	if private.dir != root {
		t.Fatal(private)
	}
	if groupLocation("0::/../../escape", mount, "").dir != "" {
		t.Fatal("escaped cgroup mount")
	}
}

func TestCgroupV1AndUnknown(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{"memory.usage_in_bytes": "600", "memory.limit_in_bytes": "9223372036854771712", "memory.stat": "total_inactive_file 50", "cpu.cfs_quota_us": "25000", "cpu.cfs_period_us": "100000", "cpuacct.usage": "3000000000", "cpu.stat": "nr_throttled 4"} {
		os.WriteFile(filepath.Join(root, name), []byte(value), 0600)
	}
	mounts := "1 0 0:1 /docker/app " + root + " rw - cgroup cgroup rw,memory,cpu,cpuacct"
	out, seconds := readContainer("1:memory,cpu,cpuacct:/docker/app", mounts)
	if out.Version != 1 || out.MemoryLimitState != "unlimited" || *out.MemoryWorkingSet != 550 || *out.CPUCores != 0.25 || *seconds != 3 {
		t.Fatalf("%+v %v", out, seconds)
	}
	unknown, s := readContainer("", "")
	if unknown.Status != "unavailable" || s != nil {
		t.Fatal(unknown)
	}
	for _, v := range []string{"", "-1", "4-2", "0-1000001", "oops"} {
		if _, ok := cpuSetSize(v); ok {
			t.Fatal(v)
		}
	}
	if n, ok := cpuSetSize("0-2,4,6-7"); !ok || n != 6 {
		t.Fatal(n, ok)
	}
}
