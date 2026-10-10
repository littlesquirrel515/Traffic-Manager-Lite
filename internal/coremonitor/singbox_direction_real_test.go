package coremonitor

import (
	"bufio"
	"context"
	"fmt"
	"golang.org/x/net/proxy"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
)

func verifyNativeDirections(t *testing.T, s Service, i core.Instance, ports []int, cfg map[string]any) {
	t.Helper()
	const size = 1 << 20
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download" {
			w.Header().Set("Content-Length", fmt.Sprint(size))
			io.CopyN(w, zeroReader{}, size)
		} else {
			io.Copy(io.Discard, r.Body)
			w.Write([]byte("OK"))
		}
	}))
	defer origin.Close()
	address := origin.Listener.Addr().String()
	inbounds := cfg["inbounds"].([]any)
	for n, port := range ports {
		tag := inbounds[n].(map[string]any)["tag"].(string)
		for _, path := range []string{"/download", "/upload"} {
			dialer, e := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, proxy.Direct)
			if e != nil {
				t.Fatal(e)
			}
			conn, e := dialer.Dial("tcp", address)
			if e != nil {
				t.Fatal(e)
			}
			conn.SetDeadline(time.Now().Add(15 * time.Second))
			if _, e = s.Observe(context.Background(), i, false, "direction_baseline"); e != nil {
				t.Fatal(e)
			}
			var up0, down0 int64
			s.Store.DB.QueryRow("SELECT COALESCE(SUM(d.upload_bytes),0),COALESCE(SUM(d.download_bytes),0) FROM traffic_daily d JOIN identities identity ON identity.id=d.identity_id WHERE identity.scope='user' AND identity.inbound_tag=?", tag).Scan(&up0, &down0)
			if path == "/download" {
				fmt.Fprintf(conn, "GET /download HTTP/1.1\r\nHost: %s\r\nConnection: keep-alive\r\n\r\n", address)
			} else {
				fmt.Fprintf(conn, "POST /upload HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\nConnection: keep-alive\r\n\r\n", address, size)
				io.CopyN(conn, zeroReader{}, size)
			}
			response, e := http.ReadResponse(bufio.NewReader(conn), nil)
			if e != nil {
				conn.Close()
				t.Fatal(e)
			}
			received, e := io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if e != nil || path == "/download" && received != size {
				conn.Close()
				t.Fatal("real native payload", received, e)
			}
			if _, e = s.Observe(context.Background(), i, false, "native_direction"); e != nil {
				conn.Close()
				t.Fatal(e)
			}
			conn.Close()
			var up, down int64
			s.Store.DB.QueryRow("SELECT COALESCE(SUM(d.upload_bytes),0),COALESCE(SUM(d.download_bytes),0) FROM traffic_daily d JOIN identities identity ON identity.id=d.identity_id WHERE identity.scope='user' AND identity.inbound_tag=?", tag).Scan(&up, &down)
			u, d := up-up0, down-down0
			if path == "/download" && (d < size || u > size/4) {
				t.Fatal("native download direction", tag, u, d)
			}
			if path == "/upload" && (u < size || d > size/4) {
				t.Fatal("native upload direction", tag, u, d)
			}
			t.Logf("native %s %s payload=%d user upload=%d download=%d; exact inbound attribution", tag, path, size, u, d)
		}
	}
}
