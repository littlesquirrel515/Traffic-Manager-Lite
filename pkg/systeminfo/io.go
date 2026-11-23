package systeminfo

import (
	"context"
	"slices"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	psnet "github.com/shirou/gopsutil/v4/net"
)

type ioCounters struct {
	disk map[string]disk.IOCountersStat
	net  map[string]psnet.IOCountersStat
}

func throughput(previous, current uint64, seconds float64) *float64 {
	if seconds <= 0 || current < previous {
		return nil
	}
	rate := float64(current-previous) / seconds
	return &rate
}

func collectIO(ctx context.Context, out *Snapshot, prior ioCounters) ioCounters {
	out.DiskIO = []DeviceIO{}
	next := ioCounters{net: make(map[string]psnet.IOCountersStat)}
	if counters, err := psnet.IOCountersWithContext(ctx, true); err == nil {
		for _, entry := range counters {
			next.net[entry.Name] = entry
		}
		for i := range out.Interfaces {
			nic := &out.Interfaces[i]
			entry, ok := next.net[nic.Name]
			if !ok {
				continue
			}
			io := &NetworkIO{
				Received: entry.BytesRecv, Sent: entry.BytesSent,
				ReceiveErrors: entry.Errin, SendErrors: entry.Errout,
				ReceiveDrops: entry.Dropin, SendDrops: entry.Dropout,
			}
			if previous, ok := prior.net[nic.Name]; ok {
				io.ReceiveRate = throughput(previous.BytesRecv, entry.BytesRecv, out.Window)
				io.SendRate = throughput(previous.BytesSent, entry.BytesSent, out.Window)
			}
			nic.IO = io
		}
	} else {
		out.Issues = append(out.Issues, Issue{"network_io", "unavailable"})
	}

	devices, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		out.Issues = append(out.Issues, Issue{"disk_io", "unavailable"})
		return next
	}
	next.disk = devices
	names := make([]string, 0, len(devices))
	for name := range devices {
		if !strings.HasPrefix(name, "loop") && !strings.HasPrefix(name, "ram") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		entry := devices[name]
		device := DeviceIO{Name: name}
		if previous, ok := prior.disk[name]; ok {
			device.ReadRate = throughput(previous.ReadBytes, entry.ReadBytes, out.Window)
			device.WriteRate = throughput(previous.WriteBytes, entry.WriteBytes, out.Window)
			device.ReadIOPS = throughput(previous.ReadCount, entry.ReadCount, out.Window)
			device.WriteIOPS = throughput(previous.WriteCount, entry.WriteCount, out.Window)
		}
		out.DiskIO = append(out.DiskIO, device)
	}
	return next
}
