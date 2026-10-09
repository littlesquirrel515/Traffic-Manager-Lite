package v2fly

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"time"
	"traffic-manager-lite/internal/adapters/stats"
	"traffic-manager-lite/internal/core"
	pb "traffic-manager-lite/internal/proto/v2fly"
)

type Adapter struct {
	Instance core.Instance
	Conn     *grpc.ClientConn
}

func (a *Adapter) Name() string { return "v2fly" }
func (a *Adapter) Capabilities() []core.Capability {
	return []core.Capability{{Metric: "user_traffic", Status: "supported"}, {Metric: "inbound_traffic", Status: "supported"}, {Metric: "online", Status: "unsupported", Reason: "V2Fly StatsService 未提供真实在线 API"}}
}
func (a *Adapter) CollectTraffic(ctx context.Context) ([]core.TrafficRecord, error) {
	r, e := pb.NewStatsServiceClient(a.Conn).QueryStats(ctx, &pb.QueryStatsRequest{Reset_: false})
	if e != nil {
		return nil, fmt.Errorf("V2Fly StatsService: %s", status.Code(e))
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
func (a *Adapter) CollectOnline(context.Context) ([]core.OnlineRecord, error) {
	return nil, core.ErrUnsupported
}
func (a *Adapter) Close() error { return a.Conn.Close() }
