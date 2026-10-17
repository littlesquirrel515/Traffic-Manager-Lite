package inboundconfig

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"net"
	"strconv"
	"strings"
	"traffic-manager-lite/internal/core"
)

type Source struct {
	Tag                string           `json:"tag"`
	Profile            core.NodeProfile `json:"profile"`
	ListenAddress      string           `json:"listen_address"`
	AuthenticationType string           `json:"authentication_type"`
}

// Sources reuses protocol normalization without persisting placeholder users.
// The input bytes are never changed, and secrets never enter the result.
func Sources(kind string, b []byte) ([]Source, error) {
	root := map[string]any{}
	var e error
	if kind == "hysteria2" {
		e = yaml.Unmarshal(b, &root)
	} else {
		e = json.Unmarshal(b, &root)
	}
	if e != nil {
		return nil, fmt.Errorf("配置结构无效")
	}
	metadata := map[string]Source{}
	if kind == "hysteria2" {
		listen := str(root, "listen")
		host, _, _ := net.SplitHostPort(listen)
		metadata["hysteria2"] = Source{ListenAddress: host, AuthenticationType: str(obj(root["auth"]), "type")}
		root["auth"] = map[string]any{"type": "password", "password": "discovery-placeholder"}
	} else {
		for index, v := range list(root, "inbounds") {
			in := obj(v)
			if in == nil {
				return nil, fmt.Errorf("入站结构无效")
			}
			tag := str(in, "tag")
			if tag == "" {
				tag = fmt.Sprintf("inbound-%d", index)
			}
			protocol := first(str(in, "protocol"), str(in, "type"))
			auth := "password"
			if protocol == "vless" || protocol == "vmess" {
				auth = "uuid"
			}
			metadata[tag] = Source{ListenAddress: str(in, "listen"), AuthenticationType: auth}
			if kind == "xray" || kind == "v2fly" {
				ports, err := listenPorts(in["port"])
				if err != nil {
					return nil, err
				}
				entry := metadata[tag]
				entry.Profile.ListenPorts = ports
				metadata[tag] = entry
			}
			if kind == "singbox" {
				in["users"] = []any{map[string]any{"name": "discovery"}}
			} else {
				settings := obj(in["settings"])
				if settings == nil {
					settings = map[string]any{}
					in["settings"] = settings
				}
				settings["clients"] = []any{map[string]any{"email": "discovery"}}
			}
		}
	}
	normalized, e := json.Marshal(root)
	if e != nil {
		return nil, e
	}
	if kind == "hysteria2" {
		normalized, e = yaml.Marshal(root)
		if e != nil {
			return nil, e
		}
	}
	clients, e := Parse(kind, normalized)
	if e != nil {
		return nil, e
	}
	if len(clients) > 256 {
		return nil, fmt.Errorf("入站超过 256 上限")
	}
	out := []Source{}
	seen := map[string]bool{}
	for _, c := range clients {
		if seen[c.Tag] {
			return nil, fmt.Errorf("入站 tag 重复，无法安全匹配来源")
		}
		seen[c.Tag] = true
		p := c.Profile
		p.UUID, p.Password, p.UserKey, p.Name = "", "", "", ""
		s := metadata[c.Tag]
		if len(s.Profile.ListenPorts) > 0 {
			p.ListenPorts = s.Profile.ListenPorts
		} else if p.ListenPort > 0 {
			p.ListenPorts = []string{strconv.Itoa(p.ListenPort)}
		}
		s.Tag = c.Tag
		s.Profile = p
		out = append(out, s)
	}
	return out, nil
}

// Xray's official port syntax accepts a number, comma-separated spans, or a
// core-side env reference. Never expand a range or evaluate the TML host env.
func listenPorts(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	text := ""
	switch v := value.(type) {
	case float64:
		if v != float64(int(v)) {
			return nil, fmt.Errorf("监听端口必须为整数")
		}
		text = strconv.Itoa(int(v))
	case string:
		text = v
	default:
		return nil, fmt.Errorf("监听端口格式无效")
	}
	if strings.HasPrefix(text, "env:") {
		if len(text) > 200 || strings.ContainsAny(text, "\r\n\x00") {
			return nil, fmt.Errorf("监听端口环境引用无效")
		}
		return []string{text}, nil
	}
	parts := strings.Split(text, ",")
	if len(parts) > 128 {
		return nil, fmt.Errorf("监听端口分段过多")
	}
	out := []string{}
	for _, span := range parts {
		bounds := strings.Split(strings.TrimSpace(span), "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("监听端口范围无效")
		}
		lo, e := strconv.Atoi(bounds[0])
		if e != nil || lo < 1 || lo > 65535 {
			return nil, fmt.Errorf("监听端口无效")
		}
		hi := lo
		if len(bounds) == 2 {
			hi, e = strconv.Atoi(bounds[1])
			if e != nil || hi < lo || hi > 65535 {
				return nil, fmt.Errorf("监听端口范围无效")
			}
		}
		if hi == lo {
			out = append(out, strconv.Itoa(lo))
		} else {
			out = append(out, fmt.Sprintf("%d-%d", lo, hi))
		}
	}
	return out, nil
}
