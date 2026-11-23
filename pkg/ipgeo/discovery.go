package ipgeo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

const (
	traceURL = "https://www.cloudflare.com/cdn-cgi/trace"
	ipifyURL = "https://api64.ipify.org/"
)

type Options struct {
	Declared      []netip.Addr
	DisableOnline bool
	DatabasePath  string
}

type Address struct {
	IP        netip.Addr `json:"ip"`
	Source    string     `json:"source"`
	Location  Location   `json:"location"`
	GeoSource string     `json:"geo_source,omitempty"`
}

type Snapshot struct {
	Status         string     `json:"status"`
	Addresses      []Address  `json:"addresses"`
	CheckedAt      *time.Time `json:"checked_at,omitempty"`
	DatabaseStatus string     `json:"database_status,omitempty"`
}

type Resolver struct {
	mu       sync.Mutex
	options  Options
	client   *http.Client
	snapshot Snapshot
	next     time.Time
	running  bool
}

func New(options Options) *Resolver {
	options.Declared = append([]netip.Addr(nil), options.Declared...)
	transport := &http.Transport{DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10}
	return &Resolver{
		options: options,
		client: &http.Client{
			Transport: transport,
			Timeout:   2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		snapshot: Snapshot{Status: "loading", Addresses: []Address{}},
	}
}

// Current is nonblocking. It shares at most one refresh across all callers and
// returns the most recent successful observation during transient failures.
// Local addresses are candidates read from the host network, in caller order.
func (r *Resolver) Current(local []netip.Addr) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running && !time.Now().Before(r.next) {
		r.running = true
		go r.refresh(append([]netip.Addr(nil), local...))
	}
	result := r.snapshot
	result.Addresses = append([]Address{}, result.Addresses...)
	if result.CheckedAt != nil {
		checked := *result.CheckedAt
		result.CheckedAt = &checked
	}
	return result
}

func (r *Resolver) refresh(local []netip.Addr) {
	result := r.resolve(local)
	r.mu.Lock()
	defer r.mu.Unlock()
	delay := time.Hour
	if result.Status == "unavailable" {
		delay = 5 * time.Minute
		if len(r.snapshot.Addresses) > 0 && (r.snapshot.Status == "available" || r.snapshot.Status == "stale") {
			result = r.snapshot
			result.Status = "stale"
		}
	}
	r.snapshot = result
	r.next = time.Now().Add(delay)
	r.running = false
}

func (r *Resolver) resolve(local []netip.Addr) Snapshot {
	checked := time.Now()
	out := Snapshot{Status: "available", Addresses: []Address{}, CheckedAt: &checked}
	source := "configured"
	selected := r.options.Declared
	if len(selected) == 0 {
		source = "local"
		for _, ip := range local {
			if public(ip) {
				selected = append(selected, ip.Unmap())
			}
		}
	}
	if len(selected) == 0 && !r.options.DisableOnline {
		if address, err := r.discover(); err == nil {
			out.Addresses = append(out.Addresses, address)
		}
	}
	seen := map[netip.Addr]bool{}
	for _, ip := range selected {
		ip = ip.Unmap()
		if !seen[ip] {
			out.Addresses = append(out.Addresses, Address{IP: ip, Source: source})
			seen[ip] = true
		}
	}
	if len(out.Addresses) == 0 {
		out.Status = "unavailable"
		if r.options.DisableOnline {
			out.Status = "disabled"
		}
	}
	r.enrich(&out)
	return out
}

func (r *Resolver) enrich(out *Snapshot) {
	if r.options.DatabasePath == "" || len(out.Addresses) == 0 {
		return
	}
	reader, err := maxminddb.Open(r.options.DatabasePath)
	if err != nil {
		out.DatabaseStatus = "unavailable"
		return
	}
	defer reader.Close()
	out.DatabaseStatus = "available"
	for i := range out.Addresses {
		address := &out.Addresses[i]
		location, err := lookup(reader, address.IP)
		if err != nil {
			out.DatabaseStatus = "unavailable"
		} else if location != (Location{}) {
			address.Location, address.GeoSource = location, "mmdb"
		}
	}
}

func (r *Resolver) discover() (Address, error) {
	if body, err := r.fetch(traceURL); err == nil {
		var ipText, country string
		for _, line := range strings.Split(body, "\n") {
			key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
			switch key {
			case "ip":
				ipText = value
			case "loc":
				country = value
			}
		}
		if ip, err := netip.ParseAddr(ipText); err == nil && public(ip) {
			address := Address{IP: ip.Unmap(), Source: "cloudflare"}
			if len(country) == 2 && country != "XX" && country[0] >= 'A' && country[0] <= 'Z' && country[1] >= 'A' && country[1] <= 'Z' {
				address.Location.CountryCode, address.GeoSource = country, "cloudflare"
			}
			return address, nil
		}
	}
	if body, err := r.fetch(ipifyURL); err == nil {
		if ip, err := netip.ParseAddr(strings.TrimSpace(body)); err == nil && public(ip) {
			return Address{IP: ip.Unmap(), Source: "ipify"}, nil
		}
	}
	return Address{}, errors.New("public IP discovery failed")
}

func (r *Resolver) fetch(endpoint string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	res, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", errors.New("unexpected IP discovery status")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4097))
	if len(body) > 4096 {
		return "", errors.New("IP discovery response too large")
	}
	return string(body), err
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range nonPublic {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
