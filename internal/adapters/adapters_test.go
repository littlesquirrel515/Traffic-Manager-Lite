package adapters

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net"
	"testing"
	"time"
	"traffic-manager-lite/internal/adapters/singbox"
	"traffic-manager-lite/internal/adapters/v2fly"
	"traffic-manager-lite/internal/adapters/xray"
	"traffic-manager-lite/internal/core"
	spb "traffic-manager-lite/internal/proto/singbox"
	vpb "traffic-manager-lite/internal/proto/v2fly"
	xpb "traffic-manager-lite/internal/proto/xray"
)

type xserver struct {
	xpb.UnimplementedStatsServiceServer
	t *testing.T
}

func (s xserver) QueryStats(ctx context.Context, r *xpb.QueryStatsRequest) (*xpb.QueryStatsResponse, error) {
	if r.Reset_ {
		s.t.Error("Xray reset requested")
	}
	return &xpb.QueryStatsResponse{Stat: []*xpb.Stat{{Name: "user>>>alice>>>traffic>>>uplink", Value: 100}, {Name: "user>>>alice>>>traffic>>>downlink", Value: 200}}}, nil
}

type vserver struct {
	vpb.UnimplementedStatsServiceServer
	t *testing.T
}
type sserver struct {
	spb.UnimplementedStatsServiceServer
	t *testing.T
}

func (s sserver) QueryStats(ctx context.Context, r *spb.QueryStatsRequest) (*spb.QueryStatsResponse, error) {
	if r.Reset_ {
		s.t.Error("sing-box reset requested")
	}
	return &spb.QueryStatsResponse{Stat: []*spb.Stat{{Name: "user>>>bob>>>traffic>>>uplink", Value: 300}, {Name: "user>>>bob>>>traffic>>>downlink", Value: 400}}}, nil
}

func (s vserver) QueryStats(ctx context.Context, r *vpb.QueryStatsRequest) (*vpb.QueryStatsResponse, error) {
	if r.Reset_ {
		s.t.Error("V2Fly reset requested")
	}
	return &vpb.QueryStatsResponse{Stat: []*vpb.Stat{{Name: "user>>>bob>>>traffic>>>uplink", Value: 300}, {Name: "user>>>bob>>>traffic>>>downlink", Value: 400}}}, nil
}
func TestOfficialGRPCAndCapabilities(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer()
	xpb.RegisterStatsServiceServer(server, xserver{t: t})
	vpb.RegisterStatsServiceServer(server, vserver{t: t})
	spb.RegisterStatsServiceServer(server, sserver{t: t})
	go server.Serve(listener)
	defer server.Stop()
	conn, e := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	x := &xray.Adapter{Instance: core.Instance{ID: 1, CoreType: "xray"}, Conn: conn}
	v := &v2fly.Adapter{Instance: core.Instance{ID: 2, CoreType: "v2fly"}, Conn: conn}
	for _, a := range []core.Collector{x, v} {
		r, e := a.CollectTraffic(ctx)
		if e != nil || len(r) != 1 {
			t.Fatalf("%s: %v %v", a.Name(), r, e)
		}
		if _, e = a.CollectOnline(ctx); e != core.ErrUnsupported {
			t.Fatalf("unsupported online %v", e)
		}
	}
	s := &singbox.Adapter{Instance: core.Instance{ID: 3, CoreType: "singbox"}, Conn: conn}
	r, e := s.CollectTraffic(ctx)
	if e != nil || len(r) != 1 || s.Capabilities()[1].Status != "supported" {
		t.Fatal("sing-box compatibility API not detected")
	}
}
