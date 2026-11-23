package ipgeo

import (
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDiscoveryUsesCountryNotEdgeLocation(t *testing.T) {
	r := New(Options{})
	r.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != traceURL || req.Method != http.MethodGet || len(req.Header) != 0 {
			t.Errorf("unexpected discovery request")
		}
		return response(200, "ip=8.8.4.4\nloc=US\ncolo=SIN\n"), nil
	})
	got := r.resolve([]netip.Addr{netip.MustParseAddr("100.64.0.5"), netip.MustParseAddr("10.0.0.1")})
	if len(got.Addresses) != 1 || got.Addresses[0].Location.CountryCode != "US" || got.Addresses[0].Source != "cloudflare" {
		t.Fatalf("%+v", got)
	}
}

func TestFallbackAndStaleRefresh(t *testing.T) {
	r := New(Options{})
	r.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == traceURL {
			return response(200, "ip=::ffff:127.0.0.1\nloc=US"), nil
		}
		return response(200, "2606:4700:4700::1111\n"), nil
	})
	r.refresh(nil)
	first := r.Current(nil)
	if len(first.Addresses) != 1 || first.Addresses[0].Source != "ipify" || first.Addresses[0].Location != (Location{}) {
		t.Fatalf("%+v", first)
	}
	r.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})
	r.refresh(nil)
	stale := r.Current(nil)
	if stale.Status != "stale" || !stale.CheckedAt.Equal(*first.CheckedAt) || stale.Addresses[0] != first.Addresses[0] {
		t.Fatalf("%+v", stale)
	}
}

func TestResolverReturnsWhileDiscoveryIsPending(t *testing.T) {
	r := New(Options{})
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	r.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		started <- struct{}{}
		select {
		case <-release:
			return response(200, "ip=8.8.8.8\nloc=US"), nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	})
	if got := r.Current(nil); got.Status != "loading" {
		t.Fatal(got)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	for range 10 {
		r.Current(nil)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := r.Current(nil); got.Status == "available" {
			if len(started) != 0 {
				t.Fatal("concurrent discovery requests")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("refresh result was not published")
}

func TestLocalAndConfiguredDoNotContactProvider(t *testing.T) {
	hostIP := netip.MustParseAddr("8.8.8.8")
	declaredIP := netip.MustParseAddr("1.1.1.1")
	for _, tt := range []struct {
		declared []netip.Addr
		want     netip.Addr
		source   string
	}{
		{nil, hostIP, "local"},
		{[]netip.Addr{declaredIP}, declaredIP, "configured"},
	} {
		r := New(Options{Declared: tt.declared, DatabasePath: t.TempDir() + "/missing.mmdb"})
		r.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("local or configured address caused an external request")
			return nil, nil
		})
		result := r.resolve([]netip.Addr{hostIP})
		if len(result.Addresses) != 1 || result.Addresses[0].IP != tt.want || result.Addresses[0].Source != tt.source || result.DatabaseStatus != "unavailable" {
			t.Fatalf("%+v", result)
		}
		r.options.DisableOnline = true
		r.options.Declared = nil
		if got := r.resolve(nil); got.Status != "disabled" || len(got.Addresses) != 0 {
			t.Fatal(got)
		}
	}
}
