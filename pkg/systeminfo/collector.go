package systeminfo

import (
	"context"
	"net"
	"net/netip"
	"os"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// Collector samples on demand and shares a five-second cache across callers.
// CPU rates are deltas between samples, so the first sample has null rates.
type Collector struct {
	mu      sync.Mutex
	options Options
	cached  Snapshot
	prior   counters
	static  CPU
}

type counters struct {
	at       time.Time
	host     cpu.TimesStat
	hostOK   bool
	process  *float64
	group    *float64
	throttle *uint64
	io       ioCounters
}

func New(options Options) *Collector {
	options.Disks = append([]DiskPath(nil), options.Disks...)
	options.Files = append([]DiskPath(nil), options.Files...)
	options.PublicIPs = append([]string(nil), options.PublicIPs...)
	return &Collector{options: options}
}

func (c *Collector) Snapshot(ctx context.Context) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.cached.ObservedAt.IsZero() && time.Since(c.cached.ObservedAt) < 5*time.Second {
		return c.cached
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	start := time.Now()
	out := Snapshot{
		ObservedAt: start, Disks: []Disk{}, Files: []File{}, Interfaces: []NIC{},
		HostIPs: []Address{}, PublicIPs: append([]string{}, c.options.PublicIPs...), Issues: []Issue{},
	}
	issue := func(component string) { out.Issues = append(out.Issues, Issue{component, "unavailable"}) }
	out.System = System{OS: runtime.GOOS, Architecture: runtime.GOARCH, NetworkScope: "local"}
	out.System.Hostname, _ = os.Hostname()
	if platform, _, version, err := host.PlatformInformationWithContext(ctx); err == nil {
		out.System.Platform = strings.TrimSpace(platform + " " + version)
	} else {
		issue("platform")
	}
	out.System.Kernel, _ = host.KernelVersionWithContext(ctx)
	if uptime, err := host.UptimeWithContext(ctx); err == nil {
		out.System.Uptime = &uptime
	}
	if c.static.Logical == 0 {
		c.static.Logical, _ = cpu.CountsWithContext(ctx, true)
		c.static.Physical, _ = cpu.CountsWithContext(ctx, false)
		if info, err := cpu.InfoWithContext(ctx); err == nil && len(info) > 0 {
			c.static.Model, c.static.MHz = info[0].ModelName, info[0].Mhz
		} else {
			issue("cpu_info")
		}
	}
	out.CPU = c.static
	current := counters{at: start}
	if times, err := cpu.TimesWithContext(ctx, false); err == nil && len(times) > 0 {
		current.host, current.hostOK = times[0], true
	} else {
		issue("cpu_usage")
	}
	if averages, err := load.AvgWithContext(ctx); err == nil {
		out.CPU.Load1, out.CPU.Load5, out.CPU.Load15 = &averages.Load1, &averages.Load5, &averages.Load15
	} else {
		issue("load")
	}
	if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		used := memory.Total - min(memory.Total, memory.Available)
		out.Memory = &Memory{Total: memory.Total, Used: used, Available: memory.Available, Free: memory.Free, Cache: memory.Cached + memory.Buffers, Percent: percent(used, memory.Total)}
	} else {
		issue("memory")
	}
	if swap, err := mem.SwapMemoryWithContext(ctx); err == nil {
		out.Swap = &Swap{Total: swap.Total, Used: swap.Used, Free: swap.Free, Percent: swap.UsedPercent, Devices: []SwapDevice{}}
		if devices, err := mem.SwapDevicesWithContext(ctx); err == nil {
			for _, device := range devices {
				out.Swap.Devices = append(out.Swap.Devices, SwapDevice{device.Name, device.UsedBytes, device.FreeBytes})
			}
		}
	} else {
		issue("swap")
	}
	collectProcess(ctx, &out, &current)
	out.Container, current.group = collectContainer()
	if out.Container.Status == "available" && c.static.Logical > 0 && (out.Container.CPUCores == nil || *out.Container.CPUCores > float64(c.static.Logical)) {
		cores := float64(c.static.Logical)
		out.Container.CPUCores = &cores
	}
	current.throttle = out.Container.ThrottleCount
	out.System.Containerized = inContainer()
	if out.System.Containerized {
		out.System.NetworkScope = "container"
	}
	if !out.System.Containerized && out.Container.Status == "unavailable" {
		out.Container.Status = "not_applicable"
	}
	if c.options.HostOSFile != "" {
		if platform, err := hostPlatform(c.options.HostOSFile); err == nil {
			out.System.HostPlatform = platform
		} else {
			issue("host_platform")
		}
	} else if !out.System.Containerized {
		out.System.HostPlatform = out.System.Platform
	}
	if c.options.HostNetworkDir != "" {
		addresses, err := hostAddresses(c.options.HostNetworkDir)
		out.HostIPs = addresses
		if err != nil {
			issue("host_network")
		}
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		issue("network")
	}
	for _, network := range interfaces {
		if network.Flags&net.FlagLoopback != 0 {
			continue
		}
		nic := NIC{Name: network.Name, MTU: network.MTU, Up: network.Flags&net.FlagUp != 0, Addresses: []Address{}}
		addresses, err := network.Addrs()
		if err != nil {
			issue("network_addresses")
			continue
		}
		for _, value := range addresses {
			prefix, err := netip.ParsePrefix(value.String())
			if err != nil {
				continue
			}
			address := classifyAddress(prefix.Addr())
			nic.Addresses = append(nic.Addresses, address)
			if !out.System.Containerized && c.options.HostNetworkDir == "" {
				out.HostIPs = append(out.HostIPs, address)
			}
		}
		out.Interfaces = append(out.Interfaces, nic)
	}
	for _, target := range c.options.Disks {
		entry := Disk{Name: target.Name, Path: target.Path, Status: "unavailable"}
		if usage, err := disk.UsageWithContext(ctx, target.Path); err == nil {
			entry.Status, entry.Total, entry.Used, entry.Available = "available", usage.Total, usage.Used, usage.Free
			entry.Percent, entry.InodesTotal, entry.InodesFree = usage.UsedPercent, usage.InodesTotal, usage.InodesFree
		}
		out.Disks = append(out.Disks, entry)
	}
	for _, target := range c.options.Files {
		entry := File{Name: target.Name, Status: "unavailable"}
		if info, err := os.Stat(target.Path); err == nil && info.Mode().IsRegular() {
			size := uint64(info.Size())
			entry.Size, entry.Status = &size, "available"
		} else if os.IsNotExist(err) {
			entry.Status = "absent"
		}
		out.Files = append(out.Files, entry)
	}
	applyRates(&out, c.prior, current)
	current.io = collectIO(ctx, &out, c.prior.io)
	out.Pressure = systemPressure()
	out.DurationMS = time.Since(start).Milliseconds()
	c.prior, c.cached = current, out
	return out
}

func collectProcess(ctx context.Context, out *Snapshot, current *counters) {
	out.Process = Process{PID: int32(os.Getpid()), GoVersion: runtime.Version(), GOMAXPROCS: runtime.GOMAXPROCS(0)}
	samples := []metrics.Sample{{Name: "/sched/goroutines:goroutines"}, {Name: "/memory/classes/heap/objects:bytes"}, {Name: "/memory/classes/total:bytes"}, {Name: "/gc/cycles/total:gc-cycles"}}
	metrics.Read(samples)
	out.Process.Goroutines, out.Process.GoHeap = samples[0].Value.Uint64(), samples[1].Value.Uint64()
	out.Process.GoRuntimeMemory, out.Process.GCCycles = samples[2].Value.Uint64(), samples[3].Value.Uint64()
	p, err := process.NewProcessWithContext(ctx, out.Process.PID)
	if err != nil {
		out.Issues = append(out.Issues, Issue{"process", "unavailable"})
		return
	}
	if memory, err := p.MemoryInfoWithContext(ctx); err == nil {
		out.Process.RSS, out.Process.VirtualMemory = &memory.RSS, &memory.VMS
	}
	if times, err := p.TimesWithContext(ctx); err == nil {
		seconds := times.User + times.System
		current.process = &seconds
	}
	if created, err := p.CreateTimeWithContext(ctx); err == nil {
		uptime := uint64(max(0, time.Now().UnixMilli()-created) / 1000)
		out.Process.Uptime = &uptime
	}
	if threads, err := p.NumThreadsWithContext(ctx); err == nil {
		out.Process.Threads = &threads
	}
	if fds, err := p.NumFDsWithContext(ctx); err == nil {
		out.Process.FileDescriptors = &fds
	}
	out.Process.FileDescriptorLimit = fileDescriptorLimit()
	if io, err := p.IOCountersWithContext(ctx); err == nil {
		out.Process.ReadBytes, out.Process.WriteBytes = &io.ReadBytes, &io.WriteBytes
	}
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

func counterRate(previous, current *float64, seconds float64) *float64 {
	if previous == nil || current == nil || seconds <= 0 || *current < *previous {
		return nil
	}
	value := (*current - *previous) / seconds * 100
	return &value
}

func applyRates(out *Snapshot, previous, current counters) {
	if previous.at.IsZero() {
		return
	}
	seconds := current.at.Sub(previous.at).Seconds()
	if seconds <= 0 {
		return
	}
	out.Window = seconds
	if previous.hostOK && current.hostOK {
		total := func(t cpu.TimesStat) float64 {
			return t.User + t.System + t.Nice + t.Idle + t.Iowait + t.Irq + t.Softirq + t.Steal
		}
		delta := total(current.host) - total(previous.host)
		idle := current.host.Idle - previous.host.Idle
		wait := current.host.Iowait - previous.host.Iowait
		steal := current.host.Steal - previous.host.Steal
		if delta > 0 && idle >= 0 && wait >= 0 && steal >= 0 && idle+wait+steal <= delta {
			busy, io := (delta-idle-wait-steal)/delta*100, wait/delta*100
			out.CPU.Percent, out.CPU.IOWaitPercent = &busy, &io
			if runtime.GOOS == "linux" {
				value := steal / delta * 100
				out.CPU.StealPercent = &value
			}
		}
	}
	out.Process.CPUPercent = counterRate(previous.process, current.process, seconds)
	out.Container.CPUPercent = counterRate(previous.group, current.group, seconds)
	if capacity := out.Container.CPUCores; capacity != nil && *capacity > 0 {
		if out.Process.CPUPercent != nil {
			value := *out.Process.CPUPercent / *capacity
			out.Process.CPUCapacityPercent = &value
		}
		if out.Container.CPUPercent != nil {
			value := *out.Container.CPUPercent / *capacity
			out.Container.CPUCapacityPercent = &value
		}
	}
	if previous.throttle != nil && current.throttle != nil && *current.throttle >= *previous.throttle {
		delta := *current.throttle - *previous.throttle
		out.Container.ThrottleDelta = &delta
	}
}

func classifyAddress(address netip.Addr) Address {
	address = address.Unmap()
	scope := "other"
	switch {
	case address.IsLoopback():
		scope = "loopback"
	case address.IsLinkLocalUnicast():
		scope = "link_local"
	case address.IsPrivate():
		scope = "private"
	case address.IsGlobalUnicast():
		scope = "global"
	}
	return Address{IP: address.String(), Scope: scope}
}
