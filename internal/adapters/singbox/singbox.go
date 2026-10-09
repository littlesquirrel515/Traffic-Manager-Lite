package singbox

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"strconv"
	"time"
	"traffic-manager-lite/internal/adapters/stats"
	"traffic-manager-lite/internal/core"
	pb "traffic-manager-lite/internal/proto/singbox"
	native "traffic-manager-lite/internal/proto/singboxnative"
)

type Adapter struct {
	Instance        core.Instance
	Conn, Control   *grpc.ClientConn
	Caps            []core.Capability
	Detected        string
	NativeAvailable bool
	Epoch           string
}

func (a *Adapter) Name() string { return "singbox" }
func (a *Adapter) Capabilities() []core.Capability {
	if a.Caps == nil {
		return []core.Capability{{Metric: "user_traffic", Status: "unknown"}, {Metric: "online_sessions", Status: "unknown"}}
	}
	return a.Caps
}
func (a *Adapter) DetectedVersion() string { return a.Detected }
func (a *Adapter) auth(ctx context.Context) context.Context {
	if a.Instance.APISecret != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+a.Instance.APISecret)
	}
	return ctx
}
func (a *Adapter) CollectTraffic(ctx context.Context) ([]core.TrafficRecord, error) {
	a.Caps = []core.Capability{{Metric: "v2ray_api", Status: "unavailable"}, {Metric: "user_traffic", Status: "unknown", Reason: "未发现用户计数器，无法确认启用或协议支持"}, {Metric: "inbound_traffic", Status: "unknown"}, {Metric: "online_sessions", Status: "unknown", Reason: "等待原生 API 探测"}, {Metric: "version", Status: "unknown", Reason: "需要原生 API；管理员声明不等于探测版本"}, {Metric: "instance_traffic", Status: "unknown"}}
	out := []core.TrafficRecord{}
	var statsErr error
	if a.Conn != nil {
		statsContext, stop := context.WithTimeout(ctx, 3*time.Second)
		r, e := pb.NewStatsServiceClient(a.Conn).QueryStats(statsContext, &pb.QueryStatsRequest{Reset_: false})
		stop()
		statsErr = e
		if e == nil {
			a.Caps[0].Status = "supported"
			cs := []stats.Counter{}
			for _, s := range r.Stat {
				cs = append(cs, stats.Counter{Name: s.Name, Value: s.Value})
			}
			var e error
			out, e = stats.Records(a.Instance, cs)
			if e != nil {
				return nil, e
			}
			for _, v := range out {
				if v.Scope == "user" {
					a.Caps[1].Status = "supported"
					a.Caps[1].Reason = "已观察到用户计数器（不证明所有 inbound 支持）"
				}
				if v.Scope == "inbound" {
					a.Caps[2].Status = "supported"
				}
			}
		}
	}
	if a.Control != nil {
		probe, cancel := context.WithTimeout(a.auth(ctx), 2*time.Second)
		defer cancel()
		client := native.NewStartedServiceClient(a.Control)
		v, e := client.GetVersion(probe, &emptypb.Empty{})
		if e != nil {
			a.Caps[3].Status = "unavailable"
			if status.Code(e) == codes.Unimplemented {
				a.Caps[3].Status = "unsupported"
				a.Caps[3].Reason = "此端点未提供原生 API"
			}
		}
		if e == nil {
			a.Detected = v.Version
			a.NativeAvailable = true
			a.Caps[4] = core.Capability{Metric: "version", Status: "supported", Reason: v.Version}
			start, e := client.GetStartedAt(probe, &emptypb.Empty{})
			if e == nil {
				a.Epoch = strconv.FormatInt(start.StartedAt, 10)
			}
			stream, e := client.SubscribeStatus(probe, &native.SubscribeStatusRequest{Interval: int64(time.Second)})
			if e == nil {
				state, e := stream.Recv()
				if e == nil && state.TrafficAvailable {
					a.Caps[5].Status = "supported"
					out = append(out, core.TrafficRecord{InstanceID: a.Instance.ID, ServerID: a.Instance.ServerID, Scope: "instance", UploadBytes: state.UplinkTotal, DownloadBytes: state.DownlinkTotal, CounterMode: "cumulative", CollectedAt: time.Now(), EpochID: a.Epoch, Source: "singbox-native"})
				}
			}
		}
	}
	if statsErr != nil && !a.NativeAvailable {
		return nil, fmt.Errorf("sing-box API: %s", status.Code(statsErr))
	}
	if a.Epoch != "" {
		for i := range out {
			out[i].EpochID = a.Epoch
		}
	}
	return out, nil
}
func (a *Adapter) CollectOnline(ctx context.Context) ([]core.OnlineRecord, error) {
	if !a.NativeAvailable || a.Control == nil {
		return nil, core.ErrUnsupported
	}
	ctx, cancel := context.WithCancel(a.auth(ctx))
	defer cancel()
	stream, e := native.NewStartedServiceClient(a.Control).SubscribeConnections(ctx, &native.SubscribeConnectionsRequest{Interval: int64(time.Second)})
	if e != nil {
		a.Caps[3].Status = "unavailable"
		if status.Code(e) == codes.Unimplemented {
			a.Caps[3].Status = "unsupported"
		}
		return nil, fmt.Errorf("sing-box connection API: %s", status.Code(e))
	}
	snapshot, e := stream.Recv()
	if e != nil {
		a.Caps[3].Status = "unavailable"
		if status.Code(e) == codes.Unimplemented {
			a.Caps[3].Status = "unsupported"
		}
		return nil, fmt.Errorf("sing-box snapshot: %s", status.Code(e))
	}
	if !snapshot.Reset_ {
		return nil, fmt.Errorf("sing-box initial connection snapshot missing reset marker")
	}
	users := map[string]int64{}
	seen := map[string]bool{}
	for _, event := range snapshot.Events {
		c := event.Connection
		if c == nil || c.User == "" || c.ClosedAt != 0 || event.Type == native.ConnectionEventType_CONNECTION_EVENT_CLOSED || seen[c.Id] {
			continue
		}
		seen[c.Id] = true
		users[c.User]++
	}
	out := []core.OnlineRecord{}
	for user, count := range users {
		out = append(out, core.OnlineRecord{UserKey: user, Count: count, Kind: "session", CollectedAt: time.Now(), Source: "singbox-native"})
	}
	a.Caps[3] = core.Capability{Metric: "online_sessions", Status: "supported", Reason: "原生 API 当前连接快照"}
	return out, nil
}
func (a *Adapter) Close() error {
	if a.Control != nil && a.Control != a.Conn {
		a.Control.Close()
	}
	if a.Conn != nil {
		return a.Conn.Close()
	}
	return nil
}
