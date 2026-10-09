package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
)

type Rejection struct {
	NodeID string `json:"node_id"`
	Reason string `json:"reason"`
}

func Compatible(format string, p core.NodeProfile) string {
	if issues := discovery.Issues(p); len(issues) > 0 {
		return strings.Join(issues, "；")
	}
	if format != "v2ray" && format != "mihomo" && format != "singbox" {
		return "不支持的订阅格式"
	}
	if p.Protocol != "vless" && p.Protocol != "vmess" && p.Protocol != "trojan" && p.Protocol != "shadowsocks" && p.Protocol != "hysteria2" && p.Protocol != "anytls" {
		return "不支持的协议"
	}
	if p.Transport != "" && p.Transport != "tcp" && p.Transport != "ws" && p.Transport != "grpc" {
		return "当前生成器未验证 " + p.Transport + " 组合（含 XHTTP），已过滤"
	}
	if (p.Protocol == "hysteria2" || p.Protocol == "anytls" || p.Protocol == "shadowsocks") && p.Transport != "" && p.Transport != "tcp" {
		return "此协议不支持所选传输"
	}
	if p.Protocol == "anytls" && format == "v2ray" {
		return "AnyTLS 分享 URI 未纳入已验证的 v2rayN 兼容范围"
	}
	if p.Protocol == "shadowsocks" && p.TLS {
		return "Shadowsocks TLS/plugin 组合尚未验证，已过滤"
	}
	if (p.Protocol == "hysteria2" || p.Protocol == "anytls") && !p.TLS {
		return "此协议需要 TLS"
	}
	if p.PublicKey != "" && !p.TLS {
		return "Reality 必须启用 TLS"
	}
	if p.PublicKey != "" && (p.Protocol != "vless" || p.Transport != "tcp") {
		return "当前仅验证 VLESS + TCP + Reality"
	}
	if p.Flow != "" && (p.Protocol != "vless" || p.Transport != "tcp") {
		return "flow 仅支持 VLESS TCP"
	}
	return ""
}
func Generate(format string, nodes []core.Node) ([]byte, string, []Rejection, error) {
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Profile.Sort < nodes[j].Profile.Sort })
	rejected := []Rejection{}
	links := []string{}
	items := []map[string]any{}
	names := map[string]int{}
	for _, n := range nodes {
		p := n.Profile
		if !p.Enabled || !n.Present {
			rejected = append(rejected, Rejection{n.ID, "节点已禁用或从配置移除"})
			continue
		}
		if reason := Compatible(format, p); reason != "" {
			rejected = append(rejected, Rejection{n.ID, reason})
			continue
		}
		names[p.Name]++
		if names[p.Name] > 1 {
			p.Name += " · " + n.ID[:min(8, len(n.ID))]
		}
		if format == "v2ray" {
			links = append(links, share(p))
		} else if format == "mihomo" {
			items = append(items, mihomo(p))
		} else {
			items = append(items, singbox(p))
		}
	}
	if format == "v2ray" {
		return []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))), "text/plain; charset=utf-8", rejected, nil
	}
	if format == "mihomo" {
		b, e := yaml.Marshal(map[string]any{"proxies": items})
		return b, "application/yaml; charset=utf-8", rejected, e
	}
	if format == "singbox" {
		b, e := json.MarshalIndent(map[string]any{"outbounds": items}, "", "  ")
		return b, "application/json", rejected, e
	}
	return nil, "", rejected, fmt.Errorf("unsupported format")
}
func share(p core.NodeProfile) string {
	if p.Protocol == "vmess" {
		m := map[string]any{"v": "2", "ps": p.Name, "add": p.Address, "port": strconv.Itoa(p.Port), "id": p.UUID, "aid": "0", "scy": "auto", "net": p.Transport, "type": "none", "host": p.Host, "path": p.Path, "tls": "", "sni": p.SNI}
		if p.Transport == "grpc" {
			m["path"] = p.ServiceName
		}
		if p.TLS {
			m["tls"] = "tls"
		}
		b, _ := json.Marshal(m)
		return "vmess://" + base64.StdEncoding.EncodeToString(b)
	}
	scheme := p.Protocol
	credential := p.Password
	if scheme == "vless" {
		credential = p.UUID
	}
	if scheme == "shadowsocks" {
		scheme = "ss"
		credential = base64.RawURLEncoding.EncodeToString([]byte(p.Method + ":" + p.Password))
	}
	u := url.URL{Scheme: scheme, User: url.User(credential), Host: net.JoinHostPort(p.Address, strconv.Itoa(p.Port)), Fragment: p.Name}
	q := url.Values{}
	if p.Protocol == "vless" {
		q.Set("encryption", "none")
	}
	if p.Protocol == "vless" || p.Protocol == "trojan" {
		q.Set("type", first(p.Transport, "tcp"))
		if p.TLS {
			q.Set("security", "tls")
		}
		if p.PublicKey != "" {
			q.Set("security", "reality")
			q.Set("pbk", p.PublicKey)
			q.Set("sid", p.ShortID)
			q.Set("fp", first(p.Fingerprint, "chrome"))
		}
		if p.Flow != "" {
			q.Set("flow", p.Flow)
		}
	}
	if p.SNI != "" {
		q.Set("sni", p.SNI)
	}
	if p.Transport == "ws" {
		q.Set("path", p.Path)
		if p.Host != "" {
			q.Set("host", p.Host)
		}
	}
	if p.Transport == "grpc" {
		q.Set("serviceName", p.ServiceName)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func mihomo(p core.NodeProfile) map[string]any {
	kind := p.Protocol
	if kind == "shadowsocks" {
		kind = "ss"
	}
	m := map[string]any{"name": p.Name, "type": kind, "server": p.Address, "port": p.Port, "udp": true}
	switch p.Protocol {
	case "vless", "vmess":
		m["uuid"] = p.UUID
	case "shadowsocks":
		m["cipher"] = p.Method
		m["password"] = p.Password
	default:
		m["password"] = p.Password
	}
	if p.Protocol == "vmess" {
		m["alterId"] = 0
		m["cipher"] = "auto"
	}
	if p.Flow != "" {
		m["flow"] = p.Flow
	}
	if p.TLS {
		m["tls"] = true
		m["servername"] = p.SNI
		m["sni"] = p.SNI
	}
	if p.PublicKey != "" {
		m["reality-opts"] = map[string]any{"public-key": p.PublicKey, "short-id": p.ShortID}
		m["client-fingerprint"] = first(p.Fingerprint, "chrome")
	}
	if p.Transport == "ws" {
		m["network"] = "ws"
		ws := map[string]any{"path": p.Path}
		if p.Host != "" {
			ws["headers"] = map[string]string{"Host": p.Host}
		}
		m["ws-opts"] = ws
	}
	if p.Transport == "grpc" {
		m["network"] = "grpc"
		m["grpc-opts"] = map[string]string{"grpc-service-name": p.ServiceName}
	}
	return m
}
func singbox(p core.NodeProfile) map[string]any {
	m := map[string]any{"type": p.Protocol, "tag": p.Name, "server": p.Address, "server_port": p.Port}
	switch p.Protocol {
	case "vless", "vmess":
		m["uuid"] = p.UUID
	case "shadowsocks":
		m["method"] = p.Method
		m["password"] = p.Password
	default:
		m["password"] = p.Password
	}
	if p.Protocol == "vmess" {
		m["security"] = "auto"
	}
	if p.Flow != "" {
		m["flow"] = p.Flow
	}
	if p.TLS {
		tls := map[string]any{"enabled": true, "server_name": p.SNI}
		if p.PublicKey != "" {
			tls["reality"] = map[string]any{"enabled": true, "public_key": p.PublicKey, "short_id": p.ShortID}
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": first(p.Fingerprint, "chrome")}
		}
		m["tls"] = tls
	}
	if p.Transport == "ws" {
		tr := map[string]any{"type": "ws", "path": p.Path}
		if p.Host != "" {
			tr["headers"] = map[string]string{"Host": p.Host}
		}
		m["transport"] = tr
	}
	if p.Transport == "grpc" {
		m["transport"] = map[string]any{"type": "grpc", "service_name": p.ServiceName}
	}
	return m
}
