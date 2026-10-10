package coremonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func TestOfficialHysteriaUploadDownloadDirection(t *testing.T) {
	binary := officialBinary(t, "TML_TEST_HYSTERIA2")
	dir := t.TempDir()
	cert, key := certificate(t, dir)
	api, inbound, socks := freePort(t), freeUDPPort(t), freePort(t)
	i := core.Instance{ID: 1, ServerID: 1, CoreType: "hysteria2", APIEndpoint: fmt.Sprintf("http://localhost:%d", api), APISecret: "isolated-direction-secret", ConfigPath: filepath.Join(dir, "server.yaml")}
	os.WriteFile(i.ConfigPath, []byte(fmt.Sprintf("listen: 127.0.0.1:%d\ntls:\n  cert: %s\n  key: %s\nauth:\n  type: userpass\n  userpass:\n    direction-user: direction-password\ntrafficStats:\n  listen: 127.0.0.1:%d\n  secret: %s\n", inbound, filepath.ToSlash(cert), filepath.ToSlash(key), api, i.APISecret)), 0600)
	process(t, binary, "server", "-c", i.ConfigPath)
	s := testService(t, dir, i)
	observeReady(t, s, i)
	cp := filepath.Join(dir, "client.yaml")
	os.WriteFile(cp, []byte(fmt.Sprintf("server: 127.0.0.1:%d\nauth: direction-user:direction-password\ntls:\n  insecure: true\n  sni: localhost\nsocks5:\n  listen: 127.0.0.1:%d\n", inbound, socks)), 0600)
	process(t, binary, "client", "-c", cp)
	const size = 4 << 20
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download" {
			w.Header().Set("Content-Length", fmt.Sprint(size))
			io.CopyN(w, zeroReader{}, size)
		} else if r.URL.Path == "/upload" {
			n, e := io.Copy(io.Discard, r.Body)
			if e != nil || n != size {
				t.Errorf("origin upload %d %v", n, e)
			}
			w.Write([]byte("OK"))
		} else {
			w.Write([]byte("baseline"))
		}
	}))
	defer origin.Close()
	dialer, e := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socks), nil, proxy.Direct)
	if e != nil {
		t.Fatal(e)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.(proxy.ContextDialer).DialContext(ctx, network, address)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	request := func(path string) {
		t.Helper()
		var response *http.Response
		var e error
		if path == "/upload" {
			response, e = client.Post(origin.URL+path, "application/octet-stream", io.LimitReader(zeroReader{}, size))
		} else {
			response, e = client.Get(origin.URL + path)
		}
		if e != nil {
			t.Fatal(e)
		}
		n, e := io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if e != nil || (path == "/download" && n != size) {
			t.Fatal("payload mismatch", n, e)
		}
	}
	request("/baseline")
	raw := func() (int64, int64) {
		var m map[string]struct{ TX, RX int64 }
		req, _ := http.NewRequest("GET", i.APIEndpoint+"/traffic", nil)
		req.Header.Set("Authorization", i.APISecret)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(&m)
		return m["direction-user"].TX, m["direction-user"].RX
	}
	time.Sleep(1100 * time.Millisecond)
	s.Observe(context.Background(), i, false, "baseline")
	tx0, rx0 := raw()
	var sumU, sumD int64
	for _, path := range []string{"/download", "/upload"} {
		request(path)
		var tx, rx int64
		for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); {
			tx, rx = raw()
			if path == "/download" && rx-rx0 >= size || path == "/upload" && tx-tx0 >= size {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if path == "/download" && (rx-rx0 < size || tx-tx0 > size/4) {
			t.Fatal("download direction", tx-tx0, rx-rx0)
		}
		if path == "/upload" && (tx-tx0 < size || rx-rx0 > size/4) {
			t.Fatal("upload direction", tx-tx0, rx-rx0)
		}
		if _, e = s.Observe(context.Background(), i, false, "direction_test"); e != nil {
			t.Fatal(e)
		}
		var u, d int64
		s.Store.DB.QueryRow("SELECT COALESCE(SUM(upload_bytes),0),COALESCE(SUM(download_bytes),0) FROM traffic_daily").Scan(&u, &d)
		if u-sumU != tx-tx0 || d-sumD != rx-rx0 {
			t.Fatal("raw/normalized/database mismatch", u-sumU, d-sumD, rx-rx0, tx-tx0)
		}
		var upName, downName, definition string
		s.Store.DB.QueryRow("SELECT source_upload_name,source_download_name,traffic_direction FROM traffic_provenance LIMIT 1").Scan(&upName, &downName, &definition)
		if upName != "tx" || downName != "rx" {
			t.Fatal("direction provenance lost")
		}
		t.Logf("%s payload=%d raw server tx=%d rx=%d client upload=%d download=%d stored daily upload=%d download=%d definition=%s", path, size, tx-tx0, rx-rx0, u-sumU, d-sumD, u, d, definition)
		sumU, sumD = u, d
		tx0, rx0 = tx, rx
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
