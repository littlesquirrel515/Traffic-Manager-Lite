package remote

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protowire"
	"strings"
	"testing"
	"traffic-manager-lite/internal/catalog"

	hp "traffic-manager-lite/internal/proto/xrayhandler"
)

func defaultXHTTP() *hp.XHTTPConfig {
	return &hp.XHTTPConfig{Path: "/packet", Mode: "packet-up", XPaddingBytes: &hp.XHTTPRange{}, ScMaxEachPostBytes: &hp.XHTTPRange{}, ScMinPostsIntervalMs: &hp.XHTTPRange{}, ScStreamUpServerSecs: &hp.XHTTPRange{}, UplinkChunkSize: &hp.XHTTPRange{}, Xmux: &hp.XHTTPXmux{MaxConcurrency: &hp.XHTTPRange{From: 1, To: 1}, MaxConnections: &hp.XHTTPRange{}, CMaxReuseTimes: &hp.XHTTPRange{}, HMaxRequestTimes: &hp.XHTTPRange{From: 600, To: 900}, HMaxReusableSecs: &hp.XHTTPRange{From: 1800, To: 3000}}, XPaddingKey: "x_padding", XPaddingHeader: "X-Padding", XPaddingPlacement: "queryInHeader", XPaddingMethod: "repeat-x", UplinkHttpMethod: "POST", SessionPlacement: "path", SeqPlacement: "path", UplinkDataPlacement: "auto", UplinkDataKey: "X-Data"}
}
func xhttpInbound(t *testing.T, v *hp.XHTTPConfig) *hp.Inbound {
	return &hp.Inbound{Tag: "packet", ProxySettings: &hp.TypedAccount{Type: "xray.proxy.vless.inbound.Config"}, ReceiverSettings: &hp.TypedAccount{Type: "xray.app.proxyman.ReceiverConfig", Value: wire(t, &hp.ReceiverConfig{PortList: &hp.PortList{Range: []*hp.PortRange{{From: 10443, To: 10443}}}, StreamSettings: &hp.StreamConfig{ProtocolName: "splithttp", TransportSettings: []*hp.TransportConfig{{ProtocolName: "splithttp", Settings: &hp.TypedAccount{Type: "xray.transport.internet.splithttp.Config", Value: wire(t, v)}}}}})}}
}
func TestXHTTPRuntimeDefaultsAndFourFieldConnection(t *testing.T) {
	s, i, f := fixture(t)
	f.inbounds = []*hp.Inbound{xhttpInbound(t, defaultXHTTP())}
	if _, e := SyncXray(context.Background(), s, i, nil); e != nil {
		t.Fatal(e)
	}
	var id string
	if e := s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='packet'").Scan(&id); e != nil {
		t.Fatal(e)
	}
	r, e := catalog.CreateConnection(context.Background(), s, catalog.Connection{InboundID: id, Name: "Preferred address", Enabled: true, Profile: map[string]any{"address": "preferred.example", "port": 443, "host": "origin.example", "sni": "origin.example"}})
	if e != nil || r.ValidationStatus != "ready" {
		t.Fatalf("default XHTTP should publish with four endpoint fields: %+v %v", r, e)
	}
	resolved, e := catalog.ResolveDefinition(context.Background(), s.DB, r.NodeID)
	if e != nil {
		t.Fatal(e)
	}
	p := resolved.Profile
	if p.Path != "/packet" || p.XHTTPMode != "packet-up" || p.Transport != "xhttp" || p.ListenPort != 10443 || p.Address != "preferred.example" || p.Port != 443 || p.Host != "origin.example" || p.SNI != "origin.example" || !p.TLS {
		t.Fatal("inherited transport or endpoint parameters changed")
	}
	if f.mutations != 0 {
		t.Fatal("connection entry mutated core")
	}
}
func TestXHTTPExtraClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    func(*hp.XHTTPConfig)
		blocked bool
	}{
		{"defaults", func(v *hp.XHTTPConfig) {}, false},
		{"26.9.9 defaults", func(v *hp.XHTTPConfig) {
			v.Xmux.MaxConcurrency = &hp.XHTTPRange{}
			v.Xmux.MaxConnections = &hp.XHTTPRange{From: 3, To: 3}
			v.SessionIdLength = &hp.XHTTPRange{}
		}, false},
		{"custom session ID", func(v *hp.XHTTPConfig) {
			v.SessionIdTable = "abc"
			v.SessionIdLength = &hp.XHTTPRange{From: 20, To: 20}
		}, true},
		{"server only", func(v *hp.XHTTPConfig) {
			v.NoSseHeader = true
			v.ServerMaxHeaderBytes = 16384
			v.ScMaxBufferedPosts = 80
		}, false},
		{"supported padding", func(v *hp.XHTTPConfig) { v.XPaddingBytes = &hp.XHTTPRange{From: 100, To: 200}; v.NoGrpcHeader = true }, false},
		{"unknown field", func(v *hp.XHTTPConfig) {
			v.ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 99, protowire.VarintType), 1))
		}, true},
		{"obfuscation", func(v *hp.XHTTPConfig) { v.XPaddingObfsMode = true }, true},
		{"custom routing", func(v *hp.XHTTPConfig) { v.SessionPlacement = "header" }, true},
		{"download", func(v *hp.XHTTPConfig) { v.DownloadSettings = &hp.StreamConfig{} }, true},
		{"sensitive header", func(v *hp.XHTTPConfig) { v.Headers = map[string]string{"Authorization": "never-store-secret"} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := defaultXHTTP()
			tc.edit(v)
			p, e := DecodeXray(xhttpInbound(t, v))
			if e != nil {
				t.Fatal(e)
			}
			if (len(p.Limitations) > 0) != tc.blocked {
				t.Fatalf("unexpected classification: %v", p.Limitations)
			}
			b, _ := json.Marshal(p)
			if strings.Contains(string(b), "never-store-secret") {
				t.Fatal("secret entered inventory")
			}
			if tc.name == "supported padding" && (p.XHTTPPadding != "100-200" || p.XHTTPNoGRPCHeader == nil || !*p.XHTTPNoGRPCHeader) {
				t.Fatal("supported parameters lost")
			}
		})
	}

}
