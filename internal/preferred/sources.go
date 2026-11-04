package preferred

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func ValidateSource(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 2048 || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443" && u.Port() != "8443") {
		return nil, fmt.Errorf("优选源仅允许不含账号的 HTTPS 公网地址，端口443或8443")
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !PublicIP(ip) {
		return nil, fmt.Errorf("优选源不能访问内网或元数据地址")
	}
	return u, nil
}
func Fetch(ctx context.Context, raw, mode string) (string, error) {
	u, e := ValidateSource(raw)
	if e != nil {
		return "", e
	}
	if mode != "feed" && mode != "edgetunnel" {
		return "", fmt.Errorf("优选源类型无效")
	}
	if mode == "edgetunnel" {
		// Public BEST_SUB protocol: never forward a real TML UUID, token or hostname.
		u.Path = "/sub"
		u.RawPath = ""
		u.RawQuery = url.Values{"host": {"example.com"}, "uuid": {"00000000-0000-4000-8000-000000000000"}}.Encode()
	}
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16384, MaxConnsPerHost: 2,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(address)
			if e != nil {
				return nil, fmt.Errorf("优选源地址无效")
			}
			ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if e != nil || len(ips) == 0 {
				return nil, fmt.Errorf("优选源 DNS 解析失败")
			}
			// Validate the entire answer set, then dial those exact addresses. A second
			// DNS lookup must never bypass the public-only boundary.
			for _, ip := range ips {
				if !PublicIP(ip) || ip.Zone() != "" {
					return nil, fmt.Errorf("优选源 DNS 指向受限地址")
				}
			}
			for _, ip := range ips {
				c, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if e == nil {
					return c, nil
				}
			}
			return nil, fmt.Errorf("优选源连接失败")
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("优选源不允许重定向") }}
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return "", fmt.Errorf("优选源地址无效")
	}
	req.Header.Set("User-Agent", "TML-Preferred/2.3")
	if mode == "edgetunnel" {
		req.Header.Set("User-Agent", "edgetunnel (https://github.com/cmliu/edgetunnel)")
	}
	res, e := client.Do(req)
	if e != nil {
		return "", fmt.Errorf("获取优选源失败：检查公网地址、TLS、重定向或超时")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("优选源返回 HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, MaxBytes+1))
	if e != nil || len(b) > MaxBytes {
		return "", fmt.Errorf("优选源响应无效或超过512KiB")
	}
	return string(b), nil
}

// Sampling is explicitly unmeasured. The pool is the official Cloudflare IPv4
// list checked 2026-10-31; imported measured lists remain administrator-controlled.
var cloudflarePool = []string{"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22"}

func Random(count, port int) (string, error) {
	if count < 1 || count > 100 || port < 1 || port > 65535 {
		return "", fmt.Errorf("随机候选数量1–100，端口1–65535")
	}
	lines := []string{}
	seen := map[string]bool{}
	for len(lines) < count {
		index, e := rand.Int(rand.Reader, big.NewInt(int64(len(cloudflarePool))))
		if e != nil {
			return "", e
		}
		prefix := netip.MustParsePrefix(cloudflarePool[index.Int64()])
		b := prefix.Addr().As4()
		span := int64(1) << (32 - prefix.Bits())
		offset, e := rand.Int(rand.Reader, big.NewInt(span-2))
		if e != nil {
			return "", e
		}
		n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		n += uint32(offset.Int64() + 1)
		ip := netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}).String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		lines = append(lines, net.JoinHostPort(ip, strconv.Itoa(port))+"#随机候选-未测速")
	}
	return strings.Join(lines, "\n"), nil
}
