package coremonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"traffic-manager-lite/internal/core"
)

func (s Service) hysteria2(ctx context.Context, i core.Instance, o *observation, quick bool) {
	o.report.APITypes = []string{"Hysteria2 Traffic Stats HTTP"}
	// Separate request budgets: a failed /traffic cannot starve /online or /dump/streams.
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		var response map[string]struct {
			TX *int64 `json:"tx"`
			RX *int64 `json:"rx"`
		}
		e := s.get(c, i, i.APIEndpoint, "/traffic", i.APISecret, &response)
		if e == nil && (response == nil || len(response) > 10000) {
			e = fmt.Errorf("invalid traffic map")
		}
		if e == nil {
			for key, v := range response {
				if key == "" || len(key) > 512 || v.TX == nil || v.RX == nil || *v.TX < 0 || *v.RX < 0 {
					e = fmt.Errorf("invalid counter")
					break
				}
				o.records = append(o.records, core.TrafficRecord{InstanceID: i.ID, ServerID: i.ServerID, Scope: "user", UserKey: key, UploadBytes: *v.RX, DownloadBytes: *v.TX, CounterMode: "cumulative", CollectedAt: time.Now(), Source: "hysteria2"})
			}
		}
		o.statsOK = e == nil
		o.userStatsOK = e == nil
		if e != nil {
			o.records = nil
		}
		o.check("stats", "/traffic", "GET /traffic", "无 clear 参数；只读累计值", "trafficStats.listen / secret", start, len(response), e)
	})
	if quick {
		return
	}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		var response map[string]int64
		e := s.get(c, i, i.APIEndpoint, "/online", i.APISecret, &response)
		if e == nil && (response == nil || len(response) > 10000) {
			e = fmt.Errorf("invalid online map")
		}
		if e == nil {
			for key, count := range response {
				if key == "" || len(key) > 512 || count < 0 {
					e = fmt.Errorf("invalid count")
					break
				}
				o.online = append(o.online, core.OnlineRecord{UserKey: key, Count: count, Kind: "device", Source: "hysteria2", CollectedAt: time.Now()})
			}
		}
		o.onlineOK = e == nil
		if e != nil {
			o.online = nil
		}
		o.onlineKind = "device"
		o.onlineBasis = "/online 完整设备快照；设备数不是活动代理流数或 IP 数"
		o.check("online", "/online", "GET /online", "不调用 kick", "trafficStats.listen / secret", start, len(response), e)
	})
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		var response struct {
			Streams *[]json.RawMessage `json:"streams"`
		}
		e := s.get(c, i, i.APIEndpoint, "/dump/streams", i.APISecret, &response)
		n := 0
		if e == nil {
			if response.Streams == nil || len(*response.Streams) > 10000 {
				e = fmt.Errorf("invalid streams")
			} else {
				n = len(*response.Streams)
			}
		}
		o.check("connections", "/dump/streams", "GET /dump/streams", "仅保存数量，不存地址、auth、连接内容", "活动 TCP 代理 QUIC 流；不代表完整用户或 UDP 会话", start, n, e)
	})
	o.note("online", "在线 IP", "Unsupported", "官方 Traffic Stats API 没有逐用户在线 IP 接口", "设备统计与 IP 统计不同")
	o.note("clients", "运行时完整用户枚举", "Unsupported", "Traffic Stats API 不枚举认证库全部用户；/traffic 与 /online 仅为已观测用户", "从配置或认证后端提供完整资产")
}
