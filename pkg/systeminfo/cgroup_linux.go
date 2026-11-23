//go:build linux

package systeminfo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func inContainer() bool {
	for _, path := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	text, _ := readSmallFile("/proc/1/cgroup")
	return strings.Contains(text, "docker") || strings.Contains(text, "kubepods") || strings.Contains(text, "containerd") || strings.Contains(text, "/lxc/")
}

type groupMount struct{ dir, root string }

func groupLocation(membership, mounts, controller string) groupMount {
	group := ""
	for _, line := range strings.Split(membership, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if controller == "" && parts[1] == "" || containsController(parts[1], controller) {
			group = parts[2]
			break
		}
	}
	if group == "" {
		return groupMount{}
	}
	decode := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`)
	for _, line := range strings.Split(mounts, "\n") {
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		fields, fs := strings.Fields(left), strings.Fields(right)
		if len(fields) < 5 || len(fs) < 3 {
			continue
		}
		if controller == "" && fs[0] != "cgroup2" || controller != "" && (fs[0] != "cgroup" || !containsController(fs[2], controller)) {
			continue
		}
		mountRoot, mountPoint := decode.Replace(fields[3]), decode.Replace(fields[4])
		relative := group
		if group != "/" && mountRoot != "/" {
			if group != mountRoot && !strings.HasPrefix(group, strings.TrimSuffix(mountRoot, "/")+"/") {
				continue
			}
			relative = strings.TrimPrefix(group, mountRoot)
		}
		path := filepath.Join(mountPoint, strings.TrimPrefix(relative, "/"))
		if path != mountPoint && !strings.HasPrefix(path, mountPoint+string(filepath.Separator)) {
			continue
		}
		return groupMount{dir: path, root: mountPoint}
	}
	return groupMount{}
}

func containsController(list, key string) bool {
	if key == "" {
		return false
	}
	for _, value := range strings.Split(list, ",") {
		if value == key {
			return true
		}
	}
	return false
}

func ancestors(location groupMount) []string {
	out := []string{}
	if location.dir == "" {
		return out
	}
	for path := location.dir; len(out) < 64; path = filepath.Dir(path) {
		out = append(out, path)
		if path == location.root || path == filepath.Dir(path) {
			break
		}
	}
	return out
}

func memoryCeiling(location groupMount, filename string) (*uint64, string) {
	var limit *uint64
	seen := false
	for _, path := range ancestors(location) {
		text, err := readSmallFile(filepath.Join(path, filename))
		if err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "max" {
			seen = true
			continue
		}
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			continue
		}
		seen = true
		if value >= 1<<60 {
			continue
		}
		if limit == nil || value < *limit {
			copy := value
			limit = &copy
		}
	}
	if limit != nil {
		return limit, "limited"
	}
	if seen {
		return nil, "unlimited"
	}
	return nil, "unknown"
}

func cpuCeiling(location groupMount, version int) *float64 {
	var limit *float64
	for _, path := range ancestors(location) {
		var quota, period float64
		if version == 2 {
			text, err := readSmallFile(filepath.Join(path, "cpu.max"))
			if err != nil {
				continue
			}
			fields := strings.Fields(text)
			if len(fields) != 2 || fields[0] == "max" {
				continue
			}
			quota, _ = strconv.ParseFloat(fields[0], 64)
			period, _ = strconv.ParseFloat(fields[1], 64)
		} else {
			q, _ := readSmallFile(filepath.Join(path, "cpu.cfs_quota_us"))
			p, _ := readSmallFile(filepath.Join(path, "cpu.cfs_period_us"))
			quota, _ = strconv.ParseFloat(strings.TrimSpace(q), 64)
			period, _ = strconv.ParseFloat(strings.TrimSpace(p), 64)
		}
		if quota <= 0 || period <= 0 {
			continue
		}
		value := quota / period
		if limit == nil || value < *limit {
			copy := value
			limit = &copy
		}
	}
	return limit
}

func cpuSetSize(text string) (int, bool) {
	count := 0
	for _, item := range strings.Split(strings.TrimSpace(text), ",") {
		bounds := strings.Split(item, "-")
		if len(bounds) > 2 {
			return 0, false
		}
		first, err := strconv.Atoi(bounds[0])
		if err != nil || first < 0 {
			return 0, false
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.Atoi(bounds[1])
			if err != nil || last < first {
				return 0, false
			}
		}
		if last > 1_000_000 {
			return 0, false
		}
		count += last - first + 1
	}
	return count, count > 0
}

func collectContainer() (Container, *float64) {
	membership, _ := readSmallFile("/proc/self/cgroup")
	mounts, _ := readSmallFile("/proc/self/mountinfo")
	return readContainer(membership, mounts)
}

func readContainer(membership, mounts string) (Container, *float64) {
	out := Container{Status: "unavailable", MemoryLimitState: "unknown"}
	memory := groupLocation(membership, mounts, "")
	processor, cpuset, accounting := memory, memory, memory
	version := 2
	if memory.dir == "" {
		version = 1
		memory = groupLocation(membership, mounts, "memory")
		processor = groupLocation(membership, mounts, "cpu")
		cpuset = groupLocation(membership, mounts, "cpuset")
		accounting = groupLocation(membership, mounts, "cpuacct")
	}
	if memory.dir == "" && accounting.dir == "" {
		return out, nil
	}
	out.Version = version
	var seconds *float64
	if version == 2 {
		out.MemoryCurrent = uintFile(filepath.Join(memory.dir, "memory.current"))
		out.MemoryLimit, out.MemoryLimitState = memoryCeiling(memory, "memory.max")
		out.SwapCurrent = uintFile(filepath.Join(memory.dir, "memory.swap.current"))
		out.SwapLimit, _ = memoryCeiling(memory, "memory.swap.max")
		if count, ok := metricFields(filepath.Join(memory.dir, "memory.events"))["oom_kill"]; ok {
			out.OOMKills = &count
		}
		stats := metricFields(filepath.Join(processor.dir, "cpu.stat"))
		if value, ok := stats["usage_usec"]; ok {
			v := float64(value) / 1e6
			seconds = &v
		}
		if value, ok := stats["nr_throttled"]; ok {
			out.ThrottleCount = &value
		}
	} else {
		if memory.dir != "" {
			out.MemoryCurrent = uintFile(filepath.Join(memory.dir, "memory.usage_in_bytes"))
			out.MemoryLimit, out.MemoryLimitState = memoryCeiling(memory, "memory.limit_in_bytes")
			if total := uintFile(filepath.Join(memory.dir, "memory.memsw.usage_in_bytes")); total != nil && out.MemoryCurrent != nil {
				v := *total - min(*total, *out.MemoryCurrent)
				out.SwapCurrent = &v
			}
			if total, _ := memoryCeiling(memory, "memory.memsw.limit_in_bytes"); total != nil && out.MemoryLimit != nil {
				v := *total - min(*total, *out.MemoryLimit)
				out.SwapLimit = &v
			}
			if value, ok := metricFields(filepath.Join(memory.dir, "memory.oom_control"))["oom_kill"]; ok {
				out.OOMKills = &value
			}
		}
		if accounting.dir != "" {
			if value := uintFile(filepath.Join(accounting.dir, "cpuacct.usage")); value != nil {
				v := float64(*value) / 1e9
				seconds = &v
			}
		}
		if processor.dir != "" {
			if value, ok := metricFields(filepath.Join(processor.dir, "cpu.stat"))["nr_throttled"]; ok {
				out.ThrottleCount = &value
			}
		}
	}
	if out.MemoryCurrent != nil {
		stats := metricFields(filepath.Join(memory.dir, "memory.stat"))
		inactive, known := stats["inactive_file"]
		if version == 1 {
			if total, ok := stats["total_inactive_file"]; ok {
				inactive = total
				known = true
			}
		}
		if known {
			working := *out.MemoryCurrent - min(*out.MemoryCurrent, inactive)
			out.MemoryWorkingSet = &working
		}
	}
	if processor.dir != "" {
		out.CPUQuotaCores = cpuCeiling(processor, version)
	}
	out.CPUCores = out.CPUQuotaCores
	setFile := "cpuset.cpus.effective"
	if version == 1 {
		setFile = "cpuset.cpus"
	}
	for _, path := range ancestors(cpuset) {
		text, err := readSmallFile(filepath.Join(path, setFile))
		if err != nil {
			continue
		}
		if count, ok := cpuSetSize(text); ok && (out.CPUCores == nil || float64(count) < *out.CPUCores) {
			v := float64(count)
			out.CPUCores = &v
		}
	}
	if out.MemoryCurrent != nil || seconds != nil {
		out.Status = "available"
	}
	return out, seconds
}
