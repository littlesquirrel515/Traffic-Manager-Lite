package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
	"traffic-manager-lite/internal/core"
)

type RenderOptions struct {
	Template      string `json:"template"`
	LocalPort     int    `json:"local_port"`
	GroupName     string `json:"group_name"`
	DefaultRoute  string `json:"default_route"`
	ClientVersion string `json:"client_version,omitempty"`
}
type RenderResult struct {
	Content     []byte
	ContentType string
	Rejected    []Rejection
}
type SubscriptionRenderer interface {
	Format() string
	Render(context.Context, []ResolvedClientNode, RenderOptions) (RenderResult, error)
}
type renderer struct{ format string }

func (r renderer) Format() string { return r.format }
func ValidFormat(f string) bool {
	switch f {
	case "raw", "base64", "v2ray", "mihomo", "singbox":
		return true
	}
	return false
}
func Renderer(f string) (SubscriptionRenderer, error) {
	if !ValidFormat(f) {
		return nil, fmt.Errorf("不支持的订阅格式")
	}
	return renderer{f}, nil
}
func Render(ctx context.Context, f string, n []ResolvedClientNode, o RenderOptions) (RenderResult, error) {
	r, e := Renderer(f)
	if e != nil {
		return RenderResult{}, e
	}
	return r.Render(ctx, n, o)
}
func (r renderer) Render(ctx context.Context, input []ResolvedClientNode, o RenderOptions) (RenderResult, error) {
	if e := ctx.Err(); e != nil {
		return RenderResult{}, e
	}
	nodes := make([]core.Node, len(input))
	for i, n := range input {
		if r.format == "mihomo" && n.Profile.Transport == "xhttp" && o.ClientVersion != "" && o.ClientVersion != "1.19.32" {
			n.Profile.Limitations = append(append([]string{}, n.Profile.Limitations...), "此目标版本的 XHTTP 未验证，请使用已验证的 Mihomo 1.19.32")
		}
		nodes[i] = core.Node{ID: n.ID, Present: true, Profile: n.Profile}
	}
	f := r.format
	if f == "raw" || f == "base64" {
		f = "v2ray"
	}
	b, ct, rejected, e := Generate(f, nodes)
	res := RenderResult{Content: b, ContentType: ct, Rejected: rejected}
	if e != nil {
		return res, e
	}
	if r.format == "raw" {
		res.Content, e = base64.StdEncoding.DecodeString(string(b))
		return res, e
	}
	if o.Template == "" {
		o.Template = "full"
	}
	if o.Template != "full" && o.Template != "collection" {
		return res, fmt.Errorf("未知输出模板")
	}
	if o.LocalPort == 0 {
		o.LocalPort = 7890
	}
	if o.LocalPort < 1024 || o.LocalPort > 65535 {
		return res, fmt.Errorf("本地端口应为 1024–65535")
	}
	if o.DefaultRoute == "" {
		o.DefaultRoute = "proxy"
	}
	if o.DefaultRoute != "proxy" && o.DefaultRoute != "direct" {
		return res, fmt.Errorf("默认路由无效")
	}
	if o.GroupName == "" {
		o.GroupName = "节点选择"
	}
	if len(o.GroupName) > 200 || strings.ContainsAny(o.GroupName, ",\r\n\x00") || o.GroupName == "DIRECT" || o.GroupName == "REJECT" || o.GroupName == "GLOBAL" {
		return res, fmt.Errorf("代理组名称无效")
	}
	if o.Template == "collection" || f == "v2ray" {
		return res, nil
	}
	if f == "mihomo" {
		res.Content, e = FullMihomo(b, o)
	} else if f == "singbox" {
		res.Content, e = FullSingBox(b, o)
	}
	return res, e
}
func FullMihomo(b []byte, o RenderOptions) ([]byte, error) {
	config := map[string]any{}
	if e := yaml.Unmarshal(b, &config); e != nil {
		return nil, e
	}
	list, _ := config["proxies"].([]any)
	names := []string{}
	seen := map[string]bool{"DIRECT": true, "REJECT": true, "GLOBAL": true, o.GroupName: true}
	for i, item := range list {
		p, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("节点结构无效")
		}
		name, _ := p["name"].(string)
		if seen[name] {
			name = fmt.Sprintf("%s · %d", name, i+1)
			for seen[name] {
				name += "_"
			}
			p["name"] = name
		}
		seen[name] = true
		names = append(names, name)
	}
	config["proxies"] = list
	members := append(names, "DIRECT")
	config["proxy-groups"] = []any{map[string]any{"name": o.GroupName, "type": "select", "proxies": members}}
	route := o.GroupName
	if o.DefaultRoute == "direct" {
		route = "DIRECT"
	}
	config["rules"] = []string{"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,172.16.0.0/12,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "IP-CIDR6,::1/128,DIRECT,no-resolve", "MATCH," + route}
	config["mode"] = "rule"
	config["mixed-port"] = o.LocalPort
	config["allow-lan"] = false
	config["log-level"] = "info"
	return yaml.Marshal(config)
}
func FullSingBox(b []byte, o RenderOptions) ([]byte, error) {
	var config map[string]any
	if e := json.Unmarshal(b, &config); e != nil {
		return nil, e
	}
	list, _ := config["outbounds"].([]any)
	tags := []string{}
	seen := map[string]bool{"tml-selector": true, "tml-direct": true}
	for i, item := range list {
		p, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("节点结构无效")
		}
		tag, _ := p["tag"].(string)
		if seen[tag] {
			tag = fmt.Sprintf("%s-%d", tag, i+1)
			for seen[tag] {
				tag += "_"
			}
			p["tag"] = tag
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	tags = append(tags, "tml-direct")
	config["outbounds"] = append([]any{map[string]any{"type": "selector", "tag": "tml-selector", "outbounds": tags}, map[string]any{"type": "direct", "tag": "tml-direct"}}, list...)
	final := "tml-selector"
	if o.DefaultRoute == "direct" {
		final = "tml-direct"
	}
	config["inbounds"] = []any{map[string]any{"type": "mixed", "tag": "tml-local", "listen": "127.0.0.1", "listen_port": o.LocalPort}}
	config["route"] = map[string]any{"final": final, "rules": []any{map[string]any{"ip_is_private": true, "outbound": "tml-direct"}}}
	return json.MarshalIndent(config, "", "  ")
}
