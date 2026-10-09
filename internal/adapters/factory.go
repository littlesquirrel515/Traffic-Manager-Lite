package adapters

import (
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"traffic-manager-lite/internal/adapters/hysteria2"
	"traffic-manager-lite/internal/adapters/singbox"
	"traffic-manager-lite/internal/adapters/v2fly"
	"traffic-manager-lite/internal/adapters/xray"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/security"
)

func New(i core.Instance, p security.Policy) (core.Collector, error) {
	p = p.ForEndpoints(i.APIEndpoint, i.ControlEndpoint)
	if e := security.ValidateEndpoint(i.APIEndpoint, i.CoreType == "hysteria2"); e != nil {
		return nil, e
	}
	if i.CoreType == "hysteria2" {
		return &hysteria2.Adapter{Instance: i, Client: p.HTTP()}, nil
	}
	// Keep the original hostname for our dialer's authorization and pinned DNS resolution.
	conn, e := grpc.NewClient("passthrough:///"+i.APIEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(p.Dial))
	if e != nil {
		return nil, fmt.Errorf("invalid gRPC target")
	}
	switch i.CoreType {
	case "xray":
		return &xray.Adapter{Instance: i, Conn: conn}, nil
	case "v2fly":
		return &v2fly.Adapter{Instance: i, Conn: conn}, nil
	case "singbox":
		control := conn
		if i.ControlEndpoint != "" {
			if e := security.ValidateEndpoint(i.ControlEndpoint, false); e != nil {
				conn.Close()
				return nil, e
			}
			control, e = grpc.NewClient("passthrough:///"+i.ControlEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(p.Dial))
			if e != nil {
				conn.Close()
				return nil, fmt.Errorf("invalid control target")
			}
		}
		return &singbox.Adapter{Instance: i, Conn: conn, Control: control}, nil
	}
	conn.Close()
	return nil, fmt.Errorf("unsupported core")
}
