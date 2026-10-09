package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Policy pins DNS answers at dial time and enforces instance or deployment authorization.
type Policy struct {
	Allowed []string
	// Endpoints is scoped to a single persisted instance, including exact ports.
	Endpoints []string
}

func safeIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && ip.String() != "fd00:ec2::254"
}

// ForEndpoints uses admin-saved endpoints only when deployment restrictions are absent.
func (p Policy) ForEndpoints(endpoints ...string) Policy {
	if len(p.Allowed) == 0 {
		p.Endpoints = nil
		for _, endpoint := range endpoints {
			if endpoint != "" {
				p.Endpoints = append(p.Endpoints, TargetAddress(endpoint))
			}
		}
	}
	return p
}

func TargetAddress(endpoint string) string {
	if u, e := url.Parse(endpoint); e == nil && (u.Scheme == "http" || u.Scheme == "https") {
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		return net.JoinHostPort(u.Hostname(), port)
	}
	return endpoint
}

func (p Policy) permitted(host string, ip netip.Addr) bool {
	if !safeIP(ip) {
		return false
	}
	for _, entry := range p.Allowed {
		entry = strings.TrimSpace(entry)
		if entry == host {
			return true
		}
		if prefix, e := netip.ParsePrefix(entry); e == nil && prefix.Contains(ip) {
			return true
		}
		if addr, e := netip.ParseAddr(entry); e == nil && addr == ip {
			return true
		}
	}
	return false
}

// Check rejects configuration errors before gRPC wraps dial failures as Unavailable.
func (p Policy) Check(ctx context.Context, address string) error {
	_, e := p.resolve(ctx, address)
	return e
}
func (p Policy) resolve(ctx context.Context, address string) ([]netip.Addr, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil || host == "" || port == "" {
		return nil, fmt.Errorf("API 地址必须包含主机和端口")
	}
	if len(p.Allowed) == 0 {
		approved := false
		for _, endpoint := range p.Endpoints {
			if endpoint == address {
				approved = true
				break
			}
		}
		if !approved {
			return nil, fmt.Errorf("API 地址未获当前实例授权")
		}
	} else {
		// Report unapproved DNS names before resolving them. CIDRs require DNS below.
		possible := false
		for _, entry := range p.Allowed {
			if strings.TrimSpace(entry) == host {
				possible = true
			}
			if _, err := netip.ParsePrefix(strings.TrimSpace(entry)); err == nil {
				possible = true
			}
			if _, err := netip.ParseAddr(strings.TrimSpace(entry)); err == nil {
				possible = true
			}
		}
		if !possible {
			return nil, fmt.Errorf("API 目标 %s 不在 TML_ALLOWED_TARGETS 允许范围内", host)
		}
	}
	ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if e != nil || len(ips) == 0 {
		return nil, fmt.Errorf("API 主机 %s DNS 解析失败", host)
	}
	for _, ip := range ips {
		if !safeIP(ip) {
			return nil, fmt.Errorf("API 地址禁止使用未指定、组播或链路本地/元数据地址")
		}
		if len(p.Allowed) > 0 && !p.permitted(host, ip.Unmap()) {
			return nil, fmt.Errorf("API 目标 %s 不在 TML_ALLOWED_TARGETS 允许范围内", host)
		}
	}
	return ips, nil
}
func (p Policy) Dial(ctx context.Context, address string) (net.Conn, error) {
	_, port, _ := net.SplitHostPort(address)
	ips, e := p.resolve(ctx, address)
	if e != nil {
		return nil, e
	}
	var last error
	for _, ip := range ips {
		c, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		if e == nil {
			return c, nil
		}
		last = e
	}
	_ = last
	return nil, fmt.Errorf("API connection failed")
}
func (p Policy) HTTP() *http.Client {
	return &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) { return p.Dial(ctx, address) }}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("API redirects are forbidden") }}
}
func ValidateEndpoint(endpoint string, httpAPI bool) error {
	if httpAPI {
		u, e := url.Parse(endpoint)
		if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("invalid HTTP API endpoint")
		}
		return validateAddress(TargetAddress(endpoint))
	}
	if strings.Contains(endpoint, "://") {
		return fmt.Errorf("gRPC endpoint must be host:port")
	}
	return validateAddress(endpoint)
}

func validateAddress(address string) error {
	host, port, e := net.SplitHostPort(address)
	n, pe := strconv.Atoi(port)
	if e != nil || host == "" || pe != nil || n < 1 || n > 65535 || strings.ContainsAny(host, " /\\\t\r\n") {
		return fmt.Errorf("API 地址必须是有效的主机与端口（1–65535）")
	}
	if ip, e := netip.ParseAddr(host); e == nil && !safeIP(ip) {
		return fmt.Errorf("API 地址禁止使用未指定、组播或链路本地/元数据地址")
	}
	return nil
}
