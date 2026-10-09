package stats

import (
	"fmt"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
)

type Counter struct {
	Name  string
	Value int64
}

func Records(i core.Instance, counters []Counter) ([]core.TrafficRecord, error) {
	m := map[string]*core.TrafficRecord{}
	directions := map[string]bool{}
	now := time.Now()
	for _, c := range counters {
		p := strings.Split(c.Name, ">>>")
		if len(p) != 4 || p[2] != "traffic" || (p[0] != "user" && p[0] != "inbound") || (p[3] != "uplink" && p[3] != "downlink") {
			continue
		}
		if c.Value < 0 {
			return nil, fmt.Errorf("negative stats counter")
		}
		if directions[c.Name] {
			return nil, fmt.Errorf("duplicate stats counter")
		}
		directions[c.Name] = true
		key := p[0] + ">>>" + p[1]
		r := m[key]
		if r == nil {
			r = &core.TrafficRecord{ServerID: i.ServerID, InstanceID: i.ID, Scope: p[0], CounterMode: "cumulative", CollectedAt: now, Source: i.CoreType}
			if p[0] == "user" {
				r.UserKey = p[1]
			} else {
				r.InboundTag = p[1]
			}
			m[key] = r
		}
		if p[3] == "uplink" {
			r.UploadBytes = c.Value
		} else {
			r.DownloadBytes = c.Value
		}
	}
	out := []core.TrafficRecord{}
	for key, r := range m {
		if !directions[key+">>>traffic>>>uplink"] || !directions[key+">>>traffic>>>downlink"] {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}
