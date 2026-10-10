package coremonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"strconv"
	"strings"
	"time"
	"traffic-manager-lite/internal/adapters/stats"
	"traffic-manager-lite/internal/core"
	pb "traffic-manager-lite/internal/proto/singbox"
	native "traffic-manager-lite/internal/proto/singboxnative"
)

func auth(ctx context.Context, i core.Instance) context.Context {
	if i.APISecret != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+i.APISecret)
	}
	return ctx
}
func nativeEndpoint(i core.Instance) string {
	if i.ControlEndpoint != "" {
		return i.ControlEndpoint
	}
	return i.APIEndpoint
}
func (s Service) singbox(ctx context.Context, i core.Instance, o *observation, quick bool) {
	o.report.APITypes = []string{"sing-box V2Ray compatible gRPC", "sing-box native gRPC"}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, nativeEndpoint(i))
		if e == nil {
			defer conn.Close()
			v, err := native.NewStartedServiceClient(conn).GetVersion(auth(c, i), &emptypb.Empty{})
			e = err
			if e == nil {
				if !validVersion(v.Version) {
					e = fmt.Errorf("invalid version")
				} else {
					o.report.Version = v.Version
					o.report.Build = "API version " + strconv.Itoa(int(v.ApiVersion))
					o.report.VersionSource = "daemon.StartedService/GetVersion（实际原生 API）"
					o.report.VersionReason = ""
				}
			}
		}
		o.check("runtime", "原生 GetVersion", "/daemon.StartedService/GetVersion", "Bearer Secret（不记录）", "services.type=api；原生 API 与兼容 API 端口可不同", start, 0, e)
	})
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, i.APIEndpoint)
		n := 0
		if e == nil {
			defer conn.Close()
			r, err := pb.NewStatsServiceClient(conn).QueryStats(c, &pb.QueryStatsRequest{Reset_: false})
			e = err
			if e == nil {
				n = len(r.Stat)
				if n > 20000 {
					e = fmt.Errorf("counter limit")
				} else {
					cs := []stats.Counter{}
					for _, v := range r.Stat {
						cs = append(cs, stats.Counter{Name: v.Name, Value: v.Value})
					}
					o.records, e = stats.Records(i, cs)
					if e == nil {
						filtered := []core.TrafficRecord{}
						for _, record := range o.records {
							record.Source = "singbox-v2ray"
							if record.Scope == "user" {
								tag, ok := s.uniqueUserInbound(i, o, record.UserKey)
								if !ok {
									continue
								}
								record.InboundTag = tag
								record.Mapped = true
							}
							filtered = append(filtered, record)
						}
						o.records = filtered
					}
				}
			}
		}
		o.statsOK = e == nil
		o.userStatsOK = e == nil
		if e != nil {
			o.records = nil
		}
		o.check("stats", "V2Ray QueryStats", "/experimental.v2rayapi.StatsService/QueryStats", "reset=false", "with_v2ray_api 编译标签 + experimental.v2ray_api + stats.enabled/users/inbounds；用户粒度仅按实际计数器", start, n, e)
	})
	if quick {
		if i.ClashEndpoint != "" {
			s.clash(ctx, i, o, true)
		}
		return
	}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, nativeEndpoint(i))
		n := 0
		if e == nil {
			defer conn.Close()
			stream, err := native.NewStartedServiceClient(conn).SubscribeServiceStatus(auth(c, i), &emptypb.Empty{})
			e = err
			if e == nil {
				v, err := stream.Recv()
				e = err
				if e == nil {
					n = int(v.Status)
					o.providerSnapshots = append(o.providerSnapshots, core.ProviderSnapshot{Provider: "Native gRPC", Scope: "runtime", Status: "Available", Summary: map[string]any{"service_state": v.Status.String()}})
				}
			}
		}
		o.check("runtime", "原生 SubscribeServiceStatus", "/daemon.StartedService/SubscribeServiceStatus", "只读取首个生命周期状态；错误正文不保存", "1.14.3 官方状态枚举", start, n, e)
	})
	epoch := ""
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, nativeEndpoint(i))
		if e == nil {
			defer conn.Close()
			client := native.NewStartedServiceClient(conn)
			v, err := client.GetStartedAt(auth(c, i), &emptypb.Empty{})
			e = err
			if e == nil {
				if v.StartedAt <= 0 {
					e = fmt.Errorf("invalid startup evidence")
				} else {
					epoch = strconv.FormatInt(v.StartedAt, 10)
				}
			}
		}
		o.check("runtime", "原生 GetStartedAt", "/daemon.StartedService/GetStartedAt", "只读启动时间", "原生 API", start, 0, e)
	})
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, nativeEndpoint(i))
		n := 0
		available := false
		if e == nil {
			defer conn.Close()
			stream, err := native.NewStartedServiceClient(conn).SubscribeStatus(auth(c, i), &native.SubscribeStatusRequest{Interval: int64(time.Second)})
			e = err
			if e == nil {
				r, err := stream.Recv()
				e = err
				if e == nil {
					available = r.TrafficAvailable
					o.providerSnapshots = append(o.providerSnapshots, core.ProviderSnapshot{Provider: "Native gRPC", Scope: "instance", Status: "Available", Summary: map[string]any{"memory": r.Memory, "goroutines": r.Goroutines, "connections_in": r.ConnectionsIn, "connections_out": r.ConnectionsOut, "upload": r.UplinkTotal, "download": r.DownlinkTotal, "traffic_available": r.TrafficAvailable, "epoch": epoch}})
					if available {
						if r.UplinkTotal < 0 || r.DownlinkTotal < 0 {
							e = fmt.Errorf("invalid counter")
						} else {
							o.records = append(o.records, core.TrafficRecord{InstanceID: i.ID, ServerID: i.ServerID, Scope: "instance", UploadBytes: r.UplinkTotal, DownloadBytes: r.DownlinkTotal, CounterMode: "cumulative", EpochID: epoch, CollectedAt: time.Now(), Source: "singbox-native", UploadCounter: "uplinkTotal", DownloadCounter: "downlinkTotal"})
							n = 1
							o.statsOK = true
						}
					}
				}
			}
		}
		ccheck := o.check("stats", "原生 SubscribeStatus", "/daemon.StartedService/SubscribeStatus", "读取首个状态后关闭订阅", "原生 API；trafficAvailable=true 才代表可读取实例总量", start, n, e)
		if e == nil && !available {
			o.report.Checks[len(o.report.Checks)-1].Reason = "状态接口可用，但 trafficAvailable=false，不虚构流量总量"
		}
		_ = ccheck
	})
	for j := range o.records {
		if epoch != "" {
			o.records[j].EpochID = epoch
		}
	}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		conn, e := s.dial(c, i, nativeEndpoint(i))
		n := 0
		if e == nil {
			defer conn.Close()
			stream, err := native.NewStartedServiceClient(conn).SubscribeConnections(auth(c, i), &native.SubscribeConnectionsRequest{Interval: int64(time.Second)})
			e = err
			if e == nil {
				r, err := stream.Recv()
				e = err
				if e == nil {
					if !r.Reset_ || len(r.Events) > 20000 {
						e = fmt.Errorf("invalid snapshot")
					} else {
						seen := map[string]bool{}
						users := map[string]int64{}
						connectionCounters := []core.ConnectionCounter{}
						details := []core.ConnectionDetail{}
						for _, ev := range r.Events {
							connection := ev.Connection
							if connection == nil || seen[connection.Id] {
								continue
							}
							seen[connection.Id] = true
							details = append(details, core.ConnectionDetail{ID: connection.Id, Inbound: connection.Inbound, User: connection.User, Network: connection.Network, Source: connection.Source, Destination: connection.Destination, Domain: connection.Domain, Upload: connection.UplinkTotal, Download: connection.DownlinkTotal, Closed: connection.ClosedAt != 0})
							if connection.User != "" && len(connection.User) <= 512 && s.knownAsset(i, o, connection.Inbound, connection.User) {
								if connection.ClosedAt == 0 && ev.Type != native.ConnectionEventType_CONNECTION_EVENT_CLOSED {
									n++
									users[connection.Inbound+"\x00"+connection.User]++
								}
								if epoch != "" && connection.UplinkTotal >= 0 && connection.DownlinkTotal >= 0 {
									connectionCounters = append(connectionCounters, core.ConnectionCounter{ID: connection.Id, Epoch: epoch, Inbound: connection.Inbound, User: connection.User, Upload: connection.UplinkTotal, Download: connection.DownlinkTotal})
								}
							}
						}
						o.providerSnapshots = append(o.providerSnapshots, core.ProviderSnapshot{Provider: "Native gRPC", Scope: "connection", Status: "Available", Summary: map[string]any{"connections": len(details), "coverage": "bounded active/closed snapshot"}, Connections: details})
						for key, count := range users {
							parts := strings.SplitN(key, "\x00", 2)
							o.online = append(o.online, core.OnlineRecord{InboundTag: parts[0], UserKey: parts[1], Count: count, Kind: "session", Source: "singbox-native", CollectedAt: time.Now()})
						}
						if epoch != "" { // Native observed connection traffic takes precedence over compatibility counters.
							o.statsOK = true
							filtered := []core.TrafficRecord{}
							for _, v := range o.records {
								if v.Scope != "user" {
									filtered = append(filtered, v)
								}
							}
							o.records = append(filtered, core.TrafficRecord{InstanceID: i.ID, ServerID: i.ServerID, Scope: "user", Source: "singbox-native-connections", CounterMode: "cumulative", CollectedAt: time.Now(), Connections: connectionCounters, UploadCounter: "connection.uplinkTotal", DownloadCounter: "connection.downlinkTotal", Coverage: "observed_connection_deltas;first observation baseline;bounded closed history may miss traffic"})
						}
					}
				}
			}
		}
		o.onlineOK = e == nil
		o.onlineKind = "session"
		o.onlineBasis = "原生完整 reset 连接快照，按 inbound+user 统计连接 session；不是设备数；未具名或未验证配置身份不能映射用户"
		o.check("online", "原生 SubscribeConnections", "/daemon.StartedService/SubscribeConnections", "只读首个完整快照，不调用 CloseConnection", "原生 API；需要 reset=true，user 字段按协议实际返回", start, n, e)
	})
	if i.ClashEndpoint != "" {
		s.clash(ctx, i, o, false)
	} else {
		o.note("connections", "Clash API", "Unknown", "未配置 Clash 地址，不代表核心不支持 Clash API", "可选；优先使用原生 API")
	}
	o.note("stats", "原生用户连接流量", func() string {
		if o.onlineOK && epoch != "" {
			for _, r := range o.records {
				if r.Source == "singbox-native-connections" && len(r.Connections) > 0 {
					return "Available"
				}
			}
		}
		return "Unknown"
	}(), "仅累计已观测同一 connection ID 的差值；首次建基线，非完整用户总量，API 断连/历史淘汰期间可能遗漏", "inbound+user 必须唯一匹配配置资产；优先原生，不与兼容用户计数相加")
	o.note("online", "在线设备数 / 在线 IP 数", "Unknown", "原生 API 提供连接 session 和 source 元数据，不证明物理设备/完整在线 IP 语义；不由连接数猜测", "只报告经验证的 inbound+user session")
	o.note("clients", "运行时用户管理", "Unsupported", "1.14.3 原生 API 无服务端用户增删接口；使用受控配置校验及确认后的重启", "通过宿主配置代理，不能执行任意命令")
	o.note("clients", "API 完整配置用户枚举", "Unsupported", "已核实原生/V2Ray/Clash API 均不提供完整服务端用户库；连接列表不能代替 Clients", "只读配置 users 支持 VLESS/Trojan/Hysteria2/AnyTLS 等协议")
	o.note("online", "V2Ray compatible Online", "Unsupported", "sing-box experimental.v2rayapi.StatsService 不包含 Xray Online 方法", "使用原生连接快照，不调用独立 Hysteria2 HTTP API")
}

func (s Service) clash(ctx context.Context, i core.Instance, o *observation, quick bool) {
	o.report.APITypes = append(o.report.APITypes, "sing-box Clash HTTP")
	authorization := ""
	secret := i.ClashSecret
	if secret == "" {
		secret = i.APISecret
	}
	if secret != "" {
		authorization = "Bearer " + secret
	}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		var v struct {
			Version string `json:"version"`
		}
		e := s.get(c, i, i.ClashEndpoint, "/version", authorization, &v)
		if e == nil {
			version := v.Version
			if len(version) > 9 && version[:9] == "sing-box " {
				version = version[9:]
			}
			if !validVersion(version) {
				e = fmt.Errorf("invalid version")
			} else if o.report.Version == "Unknown" {
				o.report.Version = version
				o.report.VersionSource = "sing-box Clash GET /version"
				o.report.VersionReason = ""
			} else if o.report.Version != version {
				e = fmt.Errorf("version mismatch")
				o.report.Version = "Unknown"
				o.report.VersionReason = "原生与 Clash 版本不一致，检查地址是否指向同一实例"
			}
		}
		o.check("runtime", "Clash /version", "GET /version", "Bearer Secret（不记录）", "experimental.clash_api.external_controller", start, 0, e)
	})
	if quick {
		return
	}
	s.bounded(ctx, func(c context.Context) {
		start := time.Now()
		var r struct {
			Connections *[]json.RawMessage `json:"connections"`
			Upload      *int64             `json:"uploadTotal"`
			Download    *int64             `json:"downloadTotal"`
		}
		e := s.get(c, i, i.ClashEndpoint, "/connections", authorization, &r)
		n := 0
		if e == nil {
			if r.Connections == nil || len(*r.Connections) > 20000 || r.Upload == nil || r.Download == nil || *r.Upload < 0 || *r.Download < 0 {
				e = fmt.Errorf("invalid clash snapshot")
			} else {
				n = len(*r.Connections)
				details := []core.ConnectionDetail{}
				for _, raw := range *r.Connections {
					var v struct {
						ID               string `json:"id"`
						Upload, Download int64
						Metadata         struct {
							Network       string `json:"network"`
							SourceIP      string `json:"sourceIP"`
							DestinationIP string `json:"destinationIP"`
							Host          string `json:"host"`
						}
					}
					if json.Unmarshal(raw, &v) != nil {
						continue
					}
					details = append(details, core.ConnectionDetail{ID: v.ID, Network: v.Metadata.Network, Source: v.Metadata.SourceIP, Destination: v.Metadata.DestinationIP, Domain: v.Metadata.Host, Upload: v.Upload, Download: v.Download})
				}
				o.providerSnapshots = append(o.providerSnapshots, core.ProviderSnapshot{Provider: "Clash", Scope: "connection", Status: "Available", Summary: map[string]any{"connections": n, "attribution": "instance_only"}, Connections: details})
				o.providerSnapshots = append(o.providerSnapshots, core.ProviderSnapshot{Provider: "Clash", Scope: "instance", Status: "Available", Summary: map[string]any{"connections": n, "upload": *r.Upload, "download": *r.Download, "attribution": "instance only"}})
			}
		}
		o.check("connections", "Clash /connections", "GET /connections", "只读数量；连接明细独立快照，仅管理员可查，不进入诊断历史，不调用 DELETE", "连接观测可用，但官方元数据没有服务端用户身份，不能映射完整 Clients 或用户 Online", start, n, e)
		if e == nil {
			instanceAlreadyObserved := false
			for _, record := range o.records {
				if record.Scope == "instance" {
					instanceAlreadyObserved = true
				}
			}
			if !instanceAlreadyObserved {
				o.records = append(o.records, core.TrafficRecord{InstanceID: i.ID, ServerID: i.ServerID, Scope: "instance", UploadBytes: *r.Upload, DownloadBytes: *r.Download, CounterMode: "cumulative", CollectedAt: time.Now(), Source: "singbox-clash", UploadCounter: "uploadTotal", DownloadCounter: "downloadTotal"})
				o.statsOK = true
			}
			o.note("stats", "Clash 实例流量总量", "Available", "同一次只读 /connections 的 uploadTotal/downloadTotal 已校验；优先保留原生实例总量，仅持久化一份实例计数，不推导用户流量", "Clash 总量无启动标识；不可观测重启限制仍存在")
		}
	})
	o.note("online", "Clash 逐用户 Online", "Unsupported", "官方 Clash 连接元数据无服务端用户字段；不把连接数猜测成用户设备数", "使用原生 API 的 user 字段，按实际协议验证")
}
