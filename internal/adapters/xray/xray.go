package xray

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
	"traffic-manager-lite/internal/adapters/stats"
	"traffic-manager-lite/internal/core"
	pb "traffic-manager-lite/internal/proto/xray"
)

type Adapter struct {
	Instance     core.Instance
	Conn         *grpc.ClientConn
	OnlineStatus string
}

func (a *Adapter) Name() string { return "xray" }
func (a *Adapter) Capabilities() []core.Capability {
	st := a.OnlineStatus
	if st == "" {
		st = "unknown"
	}
	return []core.Capability{{Metric: "user_traffic", Status: "supported"}, {Metric: "inbound_traffic", Status: "supported"}, {Metric: "online_ip", Status: st, Reason: "运行时探测 Xray 扩展 API"}}
}
func (a *Adapter) CollectTraffic(ctx context.Context) ([]core.TrafficRecord, error) {
	r, e := pb.NewStatsServiceClient(a.Conn).QueryStats(ctx, &pb.QueryStatsRequest{Reset_: false})
	if e != nil {
		return nil, fmt.Errorf("Xray StatsService: %s", status.Code(e))
	}
	cs := []stats.Counter{}
	for _, s := range r.Stat {
		cs = append(cs, stats.Counter{Name: s.Name, Value: s.Value})
	}
	rows, e := stats.Records(a.Instance, cs)
	if e != nil {
		return nil, e
	}
	if sys, e := pb.NewStatsServiceClient(a.Conn).GetSysStats(ctx, &pb.SysStatsRequest{}); e == nil {
		boot := time.Now().Add(-time.Duration(sys.Uptime) * time.Second)
		for i := range rows {
			rows[i].BootEstimate = &boot
		}
	}
	return rows, nil
}
func (a *Adapter) CollectOnline(ctx context.Context) ([]core.OnlineRecord, error) {
	client := pb.NewStatsServiceClient(a.Conn)
	r, e := client.GetAllOnlineUsers(ctx, &pb.GetAllOnlineUsersRequest{})
	if status.Code(e) == codes.Unimplemented {
		a.OnlineStatus = "unsupported"
		return nil, core.ErrUnsupported
	}
	if e != nil {
		return nil, fmt.Errorf("Xray online API: %s", status.Code(e))
	}
	out := []core.OnlineRecord{}
	for _, u := range r.Users {
		key := u
		if strings.HasPrefix(u, "user>>>") && strings.HasSuffix(u, ">>>online") {
			u = strings.TrimSuffix(strings.TrimPrefix(u, "user>>>"), ">>>online")
		} else {
			key = "user>>>" + u + ">>>online"
		}
		ip, e := client.GetStatsOnlineIpList(ctx, &pb.GetStatsRequest{Name: key, Reset_: false})
		if status.Code(e) == codes.Unimplemented {
			a.OnlineStatus = "unsupported"
			return nil, core.ErrUnsupported
		}
		if e != nil {
			return nil, fmt.Errorf("Xray IP API: %s", status.Code(e))
		}
		ips := []string{}
		for k := range ip.Ips {
			ips = append(ips, k)
		}
		out = append(out, core.OnlineRecord{UserKey: u, Count: int64(len(ips)), IPs: ips, Kind: "ip", CollectedAt: time.Now(), Source: a.Name()})
	}
	a.OnlineStatus = "supported"
	return out, nil
}
func (a *Adapter) Close() error { return a.Conn.Close() }
