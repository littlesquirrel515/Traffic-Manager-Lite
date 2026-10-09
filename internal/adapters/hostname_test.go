package adapters

import (
	"context"
	"google.golang.org/grpc"
	"net"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
	xpb "traffic-manager-lite/internal/proto/xray"
	"traffic-manager-lite/internal/security"
)

func TestGRPCFactoryPreservesHostnameAuthorization(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer()
	xpb.RegisterStatsServiceServer(server, xserver{t: t})
	go server.Serve(listener)
	defer server.Stop()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	for _, p := range []security.Policy{{}, {Allowed: []string{"localhost"}}} {
		a, e := New(core.Instance{ID: 1, CoreType: "xray", APIEndpoint: net.JoinHostPort("localhost", port)}, p)
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		records, e := a.CollectTraffic(ctx)
		cancel()
		a.Close()
		if e != nil || len(records) != 1 {
			t.Fatalf("hostname authorization failed: %v %v", records, e)
		}
	}
}
