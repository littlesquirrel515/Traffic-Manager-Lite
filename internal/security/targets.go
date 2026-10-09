package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Policy pins DNS answers at dial time. Only explicitly configured hosts/CIDRs are allowed.
type Policy struct{ Allowed []string }

func (p Policy) permitted(host string, ip netip.Addr) bool {
	if ip.IsUnspecified() || ip.IsMulticast() || ip.String() == "169.254.169.254" {
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
func (p Policy) Dial(ctx context.Context, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, fmt.Errorf("invalid API target")
	}
	if port == "" {
		return nil, fmt.Errorf("missing API port")
	}
	ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if e != nil {
		return nil, fmt.Errorf("API DNS lookup failed")
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("API target has no addresses")
	}
	for _, ip := range ips {
		if !p.permitted(host, ip.Unmap()) {
			return nil, fmt.Errorf("API target is not in TML_ALLOWED_TARGETS")
		}
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
		return nil
	}
	if strings.Contains(endpoint, "://") {
		return fmt.Errorf("gRPC endpoint must be host:port")
	}
	_, _, e := net.SplitHostPort(endpoint)
	return e
}
