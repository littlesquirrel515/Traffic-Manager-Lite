package discovery

import (
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"net"
	"strconv"
	"strings"
	"traffic-manager-lite/internal/core"
)

type Discovered struct {
	Tag     string
	Profile core.NodeProfile
}

func obj(v any) map[string]any              { m, _ := v.(map[string]any); return m }
func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func num(m map[string]any, k string) int {
	switch n := m[k].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}
func list(m map[string]any, k string) []any { l, _ := m[k].([]any); return l }
func truth(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }
func first(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
func Parse(kind string, b []byte) ([]Discovered, error) {
	root := map[string]any{}
	var e error
	if kind == "hysteria2" {
		e = yaml.Unmarshal(b, &root)
	} else {
		e = json.Unmarshal(b, &root)
	}
	if e != nil {
		return nil, fmt.Errorf("配置解析失败，请检查 JSON/YAML 格式")
	}
	out := []Discovered{}
	if kind == "hysteria2" {
		auth := obj(root["auth"])
		p := core.NodeProfile{Name: "Hysteria2", Protocol: "hysteria2", TLS: true, Enabled: true, Transport: "tcp", ListenPort: port(str(root, "listen"))}
		if str(obj(root["obfs"]), "type") != "" {
			p.Limitations = append(p.Limitations, "Hysteria2 obfs 参数尚未支持订阅输出")
		}
		switch str(auth, "type") {
		case "password":
			p.UserKey = "user"
			p.Password = str(auth, "password")
			out = append(out, Discovered{Tag: "hysteria2", Profile: p})
		case "userpass":
			for k, v := range obj(auth["userpass"]) {
				pass, ok := v.(string)
				if !ok {
					continue
				}
				n := p
				n.Name = k + " Hysteria2"
				n.UserKey = k
				n.Password = k + ":" + pass
				out = append(out, Discovered{Tag: "hysteria2", Profile: n})
			}
		default:
			return nil, fmt.Errorf("HTTP/command 认证无法从静态配置发现用户，请手动提供客户端身份")
		}
		return out, nil
	}
	for index, v := range list(root, "inbounds") {
		in := obj(v)
		tag := str(in, "tag")
		if tag == "" {
			tag = fmt.Sprintf("inbound-%d", index)
		}
		proto := first(str(in, "protocol"), str(in, "type"))
		if proto != "vless" && proto != "vmess" && proto != "trojan" && proto != "shadowsocks" && proto != "hysteria2" && proto != "anytls" {
			continue
		}
		p := core.NodeProfile{Name: tag, Protocol: proto, Enabled: true, Transport: "tcp", ListenPort: num(in, "port")}
		users := list(obj(in["settings"]), "clients")
		if kind == "singbox" {
			p.ListenPort = num(in, "listen_port")
			users = list(in, "users")
			tr := obj(in["transport"])
			p.Transport = first(str(tr, "type"), "tcp")
			p.Path = str(tr, "path")
			p.ServiceName = str(tr, "service_name")
			p.Host = str(obj(tr["headers"]), "Host")
			tls := obj(in["tls"])
			p.TLS = truth(tls, "enabled")
			if truth(obj(tls["ech"]), "enabled") {
				p.Limitations = append(p.Limitations, "ECH 参数尚未支持订阅输出")
			}
			if str(obj(in["obfs"]), "type") != "" {
				p.Limitations = append(p.Limitations, "obfs 参数尚未支持订阅输出")
			}
			p.SNI = str(tls, "server_name")
			r := obj(tls["reality"])
			if truth(r, "enabled") {
				p.Reality = true
				p.PublicKey = publicKey(str(r, "private_key"))
				if ids := list(r, "short_id"); len(ids) > 0 {
					p.ShortID, _ = ids[0].(string)
				}
			}
		} else {
			stream := obj(in["streamSettings"])
			p.Transport = first(str(stream, "network"), "tcp")
			p.TLS = str(stream, "security") == "tls" || str(stream, "security") == "reality"
			p.Reality = str(stream, "security") == "reality"
			tls := obj(stream["tlsSettings"])
			p.SNI = str(tls, "serverName")
			if str(tls, "echServerKeys") != "" {
				p.Limitations = append(p.Limitations, "ECH 参数尚未支持订阅输出")
			}
			ws := obj(stream["wsSettings"])
			p.Path = str(ws, "path")
			p.Host = str(obj(ws["headers"]), "Host")
			p.ServiceName = str(obj(stream["grpcSettings"]), "serviceName")
			r := obj(stream["realitySettings"])
			p.PublicKey = publicKey(str(r, "privateKey"))
			p.SNI = first(p.SNI, firstString(list(r, "serverNames")))
			p.ShortID = firstString(list(r, "shortIds"))
			if p.PublicKey != "" {
				p.Fingerprint = "chrome"
			}
		}
		if proto == "shadowsocks" && len(users) == 0 {
			settings := obj(in["settings"])
			if kind == "singbox" {
				settings = in
			}
			users = []any{map[string]any{"password": str(settings, "password"), "method": str(settings, "method"), "name": "default"}}
		}
		for _, uv := range users {
			u := obj(uv)
			n := p
			n.UUID = first(str(u, "id"), str(u, "uuid"))
			n.Password = str(u, "password")
			n.Method = first(str(u, "method"), str(in, "method"), str(obj(in["settings"]), "method"))
			if proto == "shadowsocks" && strings.HasPrefix(n.Method, "2022-") && len(list(in, "users")) > 0 {
				n.Limitations = append(n.Limitations, "Shadowsocks 2022 多用户密钥组合尚未验证")
			}
			n.Flow = str(u, "flow")
			n.UserKey = first(str(u, "email"), str(u, "name"), n.UUID)
			if n.UserKey == "" {
				continue
			}
			n.Name = tag + " · " + n.UserKey
			out = append(out, Discovered{Tag: tag, Profile: n})
		}
	}
	return out, nil
}
func port(v string) int {
	_, s, e := net.SplitHostPort(v)
	if e != nil {
		s = v
	}
	i, _ := strconv.Atoi(s)
	return i
}
func firstString(l []any) string {
	if len(l) > 0 {
		s, _ := l[0].(string)
		return s
	}
	return ""
}
func publicKey(s string) string {
	if s == "" {
		return ""
	}
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return ""
	}
	k, e := ecdh.X25519().NewPrivateKey(b)
	if e != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
}
func ID(instance int64, tag, user string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", instance, tag, user)))
	return hex.EncodeToString(h[:16])
}
