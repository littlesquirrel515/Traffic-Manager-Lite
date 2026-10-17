package subscription

import "traffic-manager-lite/internal/core"

type ClientCapability struct {
	Protocol           string `json:"protocol"`
	Transport          string `json:"transport"`
	Security           string `json:"security"`
	Renderer           string `json:"renderer"`
	ClientVersionRange string `json:"client_version_range"`
	SupportStatus      string `json:"support_status"`
	Reason             string `json:"reason"`
}

func CapabilityMatrix() []ClientCapability {
	out := []ClientCapability{}
	for _, f := range []string{"raw", "base64", "mihomo", "singbox"} {
		for _, protocol := range []string{"vless", "vmess", "trojan", "hysteria2", "anytls", "shadowsocks"} {
			for _, transport := range []string{"tcp", "ws", "grpc", "xhttp"} {
				p := core.NodeProfile{Name: "example", Protocol: protocol, Transport: transport, TLS: protocol != "shadowsocks", Address: "example.com", Port: 443, SNI: "example.com", UUID: "11111111-1111-4111-8111-111111111111", Password: "example", Method: "aes-128-gcm", Path: "/example", XHTTPMode: "packet-up", Enabled: true}
				format := f
				if f == "raw" || f == "base64" {
					format = "v2ray"
				}
				reason := Compatible(format, p)
				status := "Available"
				if reason != "" {
					status = "Unsupported"
				}
				version := map[string]string{"mihomo": "已验证 Mihomo 1.19.32", "singbox": "已验证 sing-box 1.14.3", "raw": "Xray 26.3.27/26.9.9 配置及 v2rayN 分享解析器", "base64": "Xray 26.3.27/26.9.9 配置及 v2rayN 分享解析器"}[f]
				security := "tls"
				if !p.TLS {
					security = "none"
				}
				out = append(out, ClientCapability{protocol, transport, security, f, version, status, reason})
			}
		}
	}
	return out
}
