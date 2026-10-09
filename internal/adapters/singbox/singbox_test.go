package singbox

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"net"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
	pb "traffic-manager-lite/internal/proto/singboxnative"
)

type nativeServer struct {
	pb.UnimplementedStartedServiceServer
	t *testing.T
}

func (s nativeServer) GetVersion(ctx context.Context, _ *emptypb.Empty) (*pb.Version, error) {
	m, _ := metadata.FromIncomingContext(ctx)
	if v := m.Get("authorization"); len(v) != 1 || v[0] != "Bearer secret" {
		s.t.Error("missing native API auth")
	}
	return &pb.Version{Version: "1.14.0", ApiVersion: 1}, nil
}
func (s nativeServer) GetStartedAt(context.Context, *emptypb.Empty) (*pb.StartedAt, error) {
	return &pb.StartedAt{StartedAt: 12345}, nil
}
func (s nativeServer) SubscribeStatus(_ *pb.SubscribeStatusRequest, stream grpc.ServerStreamingServer[pb.Status]) error {
	return stream.Send(&pb.Status{TrafficAvailable: true, UplinkTotal: 100, DownlinkTotal: 200})
}
func (s nativeServer) SubscribeConnections(_ *pb.SubscribeConnectionsRequest, stream grpc.ServerStreamingServer[pb.ConnectionEvents]) error {
	return stream.Send(&pb.ConnectionEvents{Reset_: true, Events: []*pb.ConnectionEvent{{Connection: &pb.Connection{Id: "1", User: "alice"}}, {Connection: &pb.Connection{Id: "2", User: "alice"}}, {Connection: &pb.Connection{Id: "3", User: "bob", ClosedAt: 1}}, {Connection: &pb.Connection{Id: "4"}}}})
}
func TestNativeVersionInstanceTrafficAndRealSessions(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := grpc.NewServer()
	pb.RegisterStartedServiceServer(s, nativeServer{t: t})
	go s.Serve(l)
	defer s.Stop()
	conn, e := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	a := &Adapter{Instance: core.Instance{ID: 1, CoreType: "singbox", APISecret: "secret"}, Conn: conn, Control: conn}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, e := a.CollectTraffic(ctx)
	if e != nil || len(rows) != 1 || rows[0].Scope != "instance" || rows[0].EpochID != "12345" || a.DetectedVersion() != "1.14.0" {
		t.Fatalf("native stats %v %v", rows, e)
	}
	online, e := a.CollectOnline(ctx)
	if e != nil || len(online) != 1 || online[0].Count != 2 || online[0].Kind != "session" {
		t.Fatalf("connections %v %v", online, e)
	}
	if a.Caps[1].Status == "supported" {
		t.Fatal("instance totals mistaken for user traffic")
	}
}
