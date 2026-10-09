package hysteria2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
)

type Adapter struct {
	Instance core.Instance
	Client   *http.Client
}

func (a *Adapter) Name() string { return "hysteria2" }
func (a *Adapter) Capabilities() []core.Capability {
	return []core.Capability{{Metric: "user_traffic", Status: "supported"}, {Metric: "online_devices", Status: "supported"}, {Metric: "online_ip", Status: "unsupported", Reason: "Traffic Stats API 不提供 IP"}}
}
func (a *Adapter) get(ctx context.Context, path string, v any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(a.Instance.APIEndpoint, "/")+path, nil)
	if e != nil {
		return e
	}
	if a.Instance.APISecret != "" {
		req.Header.Set("Authorization", a.Instance.APISecret)
	}
	resp, e := a.Client.Do(req)
	if e != nil {
		return fmt.Errorf("Hysteria2 API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Hysteria2 API status %d", resp.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v); e != nil {
		return fmt.Errorf("invalid Hysteria2 API response")
	}
	return nil
}
func (a *Adapter) CollectTraffic(ctx context.Context) ([]core.TrafficRecord, error) {
	m := map[string]struct {
		TX *int64 `json:"tx"`
		RX *int64 `json:"rx"`
	}{}
	if e := a.get(ctx, "/traffic", &m); e != nil {
		return nil, e
	}
	now := time.Now()
	out := []core.TrafficRecord{}
	for k, v := range m {
		if v.TX == nil || v.RX == nil || *v.TX < 0 || *v.RX < 0 {
			return nil, fmt.Errorf("negative Hysteria2 counter")
		}
		out = append(out, core.TrafficRecord{ServerID: a.Instance.ServerID, InstanceID: a.Instance.ID, UserKey: k, Scope: "user", UploadBytes: *v.RX, DownloadBytes: *v.TX, CounterMode: "cumulative", CollectedAt: now, Source: "hysteria2"})
	}
	return out, nil
}
func (a *Adapter) CollectOnline(ctx context.Context) ([]core.OnlineRecord, error) {
	m := map[string]int64{}
	if e := a.get(ctx, "/online", &m); e != nil {
		return nil, e
	}
	out := []core.OnlineRecord{}
	for k, n := range m {
		if n < 0 {
			return nil, fmt.Errorf("invalid online count")
		}
		out = append(out, core.OnlineRecord{UserKey: k, Count: n, Kind: "device", CollectedAt: time.Now(), Source: a.Name()})
	}
	return out, nil
}
func (a *Adapter) Close() error { a.Client.CloseIdleConnections(); return nil }
