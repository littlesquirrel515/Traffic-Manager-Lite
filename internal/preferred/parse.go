package preferred

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxBytes = 512 * 1024

type Address struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Note    string `json:"note"`
}
type Parsed struct {
	Entries    []Address `json:"entries"`
	Invalid    int       `json:"invalid"`
	Duplicates int       `json:"duplicates"`
	Truncated  int       `json:"truncated"`
}

func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4"} {
		if netip.MustParsePrefix(s).Contains(ip) {
			return false
		}
	}
	return true
}
func validHost(h string) bool {
	if ip, e := netip.ParseAddr(h); e == nil {
		return ip.Zone() == "" && PublicIP(ip)
	}
	if len(h) > 253 || !strings.Contains(h, ".") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".localhost") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func parseAddress(s string, port int) (Address, error) {
	a := Address{Port: port}
	part, note, _ := strings.Cut(strings.TrimSpace(s), "#")
	a.Note = strings.TrimSpace(note)
	if strings.Contains(part, "://") {
		u, e := url.Parse(part)
		if e != nil || (u.Scheme != "vless" && u.Scheme != "trojan" && u.Scheme != "ss") {
			return a, fmt.Errorf("不支持的地址格式")
		}
		// Credentials, path, SNI and all query parameters from external subscriptions
		// are discarded. Only the address/port and display remark are imported.
		part = u.Host
	}
	host := part
	if h, p, e := net.SplitHostPort(part); e == nil {
		host = h
		var err error
		a.Port, err = strconv.Atoi(p)
		if err != nil {
			return a, err
		}
	} else if strings.Count(part, ":") == 1 {
		return a, fmt.Errorf("端口无效")
	}
	host = strings.Trim(host, "[]")
	host = strings.ToLower(host)
	if ip, e := netip.ParseAddr(host); e == nil {
		host = ip.Unmap().String()
	}
	if !validHost(host) || a.Port < 1 || a.Port > 65535 || len(a.Note) > 180 || strings.ContainsAny(a.Note, "\r\n\x00") {
		return a, fmt.Errorf("地址、端口或备注无效")
	}
	a.Address = host
	return a, nil
}
func Parse(text string, port, limit int) (Parsed, error) {
	p := Parsed{Entries: []Address{}}
	if len(text) > MaxBytes || !utf8.ValidString(text) || port < 1 || port > 65535 || limit < 1 || limit > 500 {
		return p, fmt.Errorf("输入须为 UTF-8、最多512KiB，数量1–500，端口1–65535")
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if !strings.ContainsAny(text, ":#,") {
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, e := encoding.DecodeString(strings.Join(strings.Fields(text), "")); e == nil && utf8.Valid(b) && strings.Contains(string(b), "://") {
				text = string(b)
				break
			}
		}
	}
	seen := map[string]bool{}
	add := func(s string) {
		a, e := parseAddress(s, port)
		if e != nil {
			p.Invalid++
			return
		}
		key := net.JoinHostPort(a.Address, strconv.Itoa(a.Port))
		if seen[key] {
			p.Duplicates++
			return
		}
		seen[key] = true
		if len(p.Entries) >= limit {
			p.Truncated++
			return
		}
		p.Entries = append(p.Entries, a)
	}
	first, _, _ := strings.Cut(text, "\n")
	if strings.Contains(first, ",") {
		r := csv.NewReader(strings.NewReader(text))
		r.FieldsPerRecord = -1
		rows, e := r.ReadAll()
		if e != nil || len(rows) == 0 || len(rows) > 10001 {
			return p, fmt.Errorf("CSV 格式无效或行数超过10000")
		}
		ipCol, portCol, noteCol, tlsCol := -1, -1, -1, -1
		for i, h := range rows[0] {
			h = strings.ToLower(strings.TrimSpace(h))
			switch {
			case strings.Contains(h, "ip") || h == "地址":
				ipCol = i
			case h == "port" || h == "端口":
				portCol = i
			case h == "tls":
				tlsCol = i
			case h == "备注" || h == "remark" || h == "国家" || h == "城市" || h == "数据中心":
				if noteCol < 0 {
					noteCol = i
				}
			}
		}
		if ipCol < 0 {
			return p, fmt.Errorf("CSV 缺少 IP/IP地址 列")
		}
		for _, row := range rows[1:] {
			get := func(i int) string {
				if i < 0 || i >= len(row) {
					return ""
				}
				return strings.TrimSpace(row[i])
			}
			if tlsCol >= 0 && strings.ToLower(get(tlsCol)) != "true" && get(tlsCol) != "1" {
				p.Invalid++
				continue
			}
			h := get(ipCol)
			v := get(portCol)
			if v != "" {
				h = net.JoinHostPort(strings.Trim(h, "[]"), v)
			}
			if n := get(noteCol); n != "" {
				h += "#" + n
			}
			add(h)
		}
	} else {
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		if len(lines) > 10000 {
			return p, fmt.Errorf("输入行数超过10000")
		}
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "vmess://") {
				b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "vmess://"))
				var v struct {
					Add  string          `json:"add"`
					Port json.RawMessage `json:"port"`
					Name string          `json:"ps"`
				}
				if e != nil || json.Unmarshal(b, &v) != nil {
					p.Invalid++
					continue
				}
				n := strings.Trim(string(v.Port), "\"")
				add(net.JoinHostPort(v.Add, n) + "#" + v.Name)
				continue
			}
			add(line)
		}
	}
	if len(p.Entries) == 0 {
		return p, fmt.Errorf("没有可用候选地址；仅支持公网 IP/域名、TXT、CSV 或 Base64 节点地址列表")
	}
	return p, nil
}
