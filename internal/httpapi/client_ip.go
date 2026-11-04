package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Forwarding headers are meaningful only across explicitly trusted hops.
// Walk from the socket peer towards the client; never trust the leftmost
// address supplied by an arbitrary Internet caller.
func (a *API) subscriptionClientIP(r *http.Request) (client, peer, source string) {
	raw, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		raw = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.Zone() != "" {
		return "", "", "unknown"
	}
	ip = ip.Unmap()
	peer = ip.String()
	client = peer
	source = "socket"
	trusted := func(ip netip.Addr) bool {
		for _, p := range a.Config.TrustedProxies {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(ip) {
		return
	}
	header := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if len(header) == 0 || len(header) > 4096 {
		return
	}
	parts := strings.Split(header, ",")
	if len(parts) > 32 {
		return
	}
	hops := make([]netip.Addr, len(parts))
	for i, part := range parts {
		next, e := netip.ParseAddr(strings.TrimSpace(part))
		if e != nil || next.Zone() != "" || next.IsUnspecified() || next.IsMulticast() {
			return
		}
		hops[i] = next.Unmap()
	}
	for i := len(hops) - 1; i >= 0 && trusted(ip); i-- {
		ip = hops[i]
	}
	return ip.String(), peer, "trusted_x_forwarded_for"
}
