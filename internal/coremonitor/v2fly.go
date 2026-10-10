package coremonitor

import (
	"context"
	"fmt"
	"google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"time"
	"traffic-manager-lite/internal/adapters/stats"
	"traffic-manager-lite/internal/core"
	pb "traffic-manager-lite/internal/proto/v2fly"
	observe "traffic-manager-lite/internal/proto/v2flyobserve"
)

func (s Service) v2fly(ctx context.Context, i core.Instance, o *observation, quick bool) {
	o.report.APITypes = []string{"V2Fly gRPC"}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, i.APIEndpoint)
		n := 0
		if e == nil {
			defer conn.Close()
			response, err := pb.NewStatsServiceClient(conn).QueryStats(c, &pb.QueryStatsRequest{Reset_: false})
			e = err
			if e == nil {
				n = len(response.Stat)
				if n > 20000 {
					e = fmt.Errorf("counter limit")
				} else {
					counters := []stats.Counter{}
					for _, counter := range response.Stat {
						counters = append(counters, stats.Counter{Name: counter.Name, Value: counter.Value})
					}
					o.records, e = stats.Records(i, counters)
				}
			}
		}
		o.statsOK = e == nil
		o.userStatsOK = e == nil
		if e != nil {
			o.records = nil
		}
		o.check("stats", "QueryStats", "/v2ray.core.app.stats.command.StatsService/QueryStats", "reset=false; pattern为空", "StatsService、stats:{}、policy 用户上下行开关", start, n, e)
	})
	if quick {
		return
	}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, i.APIEndpoint)
		if e == nil {
			defer conn.Close()
			r, err := pb.NewStatsServiceClient(conn).GetSysStats(c, &pb.SysStatsRequest{})
			e = err
			if e == nil {
				boot := time.Now().Add(-time.Duration(r.Uptime) * time.Second)
				for j := range o.records {
					o.records[j].BootEstimate = &boot
				}
			}
		}
		o.check("runtime", "GetSysStats", "/v2ray.core.app.stats.command.StatsService/GetSysStats", "只读 uptime；不含版本", "StatsService", start, 0, e)
	})
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, i.APIEndpoint)
		n := 0
		if e == nil {
			defer conn.Close()
			r, err := observe.NewObservatoryServiceClient(conn).GetOutboundStatus(c, &observe.GetOutboundStatusRequest{})
			e = err
			if e == nil && r.Status != nil {
				n = len(r.Status.Status)
			}
		}
		o.check("observatory", "GetOutboundStatus", "/v2ray.core.app.observatory.command.ObservatoryService/GetOutboundStatus", "Tag为空；仅保存状态条数", "ObservatoryService + observatory；出口健康，不是用户在线", start, n, e)
	})
	// Reflection is read-only. Never call AlterInbound, AddUser or RestartLogger as a probe.
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, i.APIEndpoint)
		present := map[string]bool{}
		if e == nil {
			defer conn.Close()
			stream, err := grpc_reflection_v1alpha.NewServerReflectionClient(conn).ServerReflectionInfo(c)
			e = err
			if e == nil {
				e = stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_ListServices{ListServices: ""}})
				if e == nil {
					r, err := stream.Recv()
					e = err
					if e == nil {
						if list := r.GetListServicesResponse(); list != nil {
							for _, service := range list.Service {
								present[service.Name] = true
							}
						} else {
							e = fmt.Errorf("reflection failed")
						}
					}
				}
				stream.CloseSend()
			}
		}
		o.check("runtime", "服务注册枚举", "grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo", "ListServices；不读取日志或修改用户", "反射不是必需 API；不可用时服务状态 Unknown", start, len(present), e)
		for _, service := range []struct{ group, name string }{{"management", "v2ray.core.app.proxyman.command.HandlerService"}, {"logger", "v2ray.core.app.log.command.LoggerService"}} {
			state, reason := "Unknown", "无法只读确认服务注册；禁止执行用户变更或 RestartLogger"
			if e == nil {
				if present[service.name] {
					state = "Available"
					reason = "通过反射验证服务已注册；没有执行任何用户管理/日志变更"
				} else if o.report.Version != "Unknown" {
					state = "Disabled"
					reason = "反射列表未注册此正式版服务"
				}
			}
			o.note(service.group, service.name, state, reason, "HandlerService 只有增删改；Logger 的 FollowLog 是不稳定日志流，本工具不采集日志")
			if e == nil {
				check := &o.report.Checks[len(o.report.Checks)-1]
				check.Method = "grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo（复用同一 ListServices 响应）"
				check.Evidence = "实际只读服务注册列表；不调用用户变更或日志重启"
				check.Response = "服务未注册"
				if present[service.name] {
					check.Count = 1
					check.Response = "服务已注册"
				}
			}
		}
	})
	o.note("clients", "GetInboundUsers / ListInbounds", "Unsupported", "V2Fly v5.53.0 HandlerService 没有完整用户或 inbound 枚举接口；不能套用 Xray 方法", "使用只读配置文件，文件不保证是运行时配置")
	o.note("online", "逐用户在线 / 在线 IP", "Unsupported", "官方 StatsService 只有累计流量，没有 Xray Online 方法；Observatory 是出口健康", "页面显示 Unsupported，保留 Clients 与流量")
	o.onlineBasis = "V2Fly 正式版无逐用户在线 API"
	o.onlineKind = "unsupported"
}
