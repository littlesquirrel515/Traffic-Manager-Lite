//go:build linux

package systeminfo

import (
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func systemPressure() *Pressure {
	p := &Pressure{CPU: pressureAverage("cpu"), Memory: pressureAverage("memory"), IO: pressureAverage("io")}
	if p.CPU == nil && p.Memory == nil && p.IO == nil {
		return nil
	}
	return p
}

func pressureAverage(resource string) *float64 {
	content, err := readSmallFile("/proc/pressure/" + resource)
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			if raw, ok := strings.CutPrefix(field, "avg10="); ok {
				if value, err := strconv.ParseFloat(raw, 64); err == nil && value >= 0 && value <= 100 {
					return &value
				}
			}
		}
	}
	return nil
}

func fileDescriptorLimit() *uint64 {
	var limit unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_NOFILE, &limit) != nil || limit.Cur == unix.RLIM_INFINITY {
		return nil
	}
	return &limit.Cur
}
