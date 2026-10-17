package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"golang.org/x/net/proxy"
	"gopkg.in/yaml.v3"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/hysteriaauth"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/subscriberprovision"
	"traffic-manager-lite/internal/subscription"
)

// All processes/configurations and generated credentials are isolated fixtures.
// A real official client authenticates a newly created account on an already
// running server; the official Stats API must use the backend's stable ID.
func TestOfficialHysteriaHTTPAuthDynamicUser(t *testing.T) {
	binary := os.Getenv("TML_TEST_HYSTERIA2")
	if binary == "" {
		t.Skip("set TML_TEST_HYSTERIA2 to official binary for isolated handshake")
	}
	dir := t.TempDir()
	a, _ := testAPI(t)
	ctx := context.Background()
	keyfile := filepath.Join(dir, "desired.key")
	if e := os.WriteFile(keyfile, []byte(strings.Repeat("k", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	a.Config.ManagedKeyFile = keyfile
	backend := httptest.NewServer(a.Handler())
	defer backend.Close()
	port := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	udp, e := net.ListenPacket("udp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	inboundPort := udp.LocalAddr().(*net.UDPAddr).Port
	udp.Close()
	statsPort, socksPort := port(), port()
	for _, q := range []string{`INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','fixture','now','now')`, fmt.Sprintf(`INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','hysteria2','http://127.0.0.1:%d','now','now')`, statsPort)} {
		if _, e = a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	id, e := remote.SaveManual(ctx, a.Store, remote.Inbound{InstanceID: 1, Tag: "http-hy", Profile: core.NodeProfile{Protocol: "hysteria2", Enabled: true}})
	if e != nil {
		t.Fatal(e)
	}
	p, e := hysteriaauth.Prepare(ctx, a.Store, id, hysteriaauth.Setup{Confirmed: true, AllowedCIDRs: []string{"127.0.0.0/8"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = hysteriaauth.SetState(ctx, a.Store, id, p.Revision, true, true); e != nil {
		t.Fatal(e)
	}
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &private.PublicKey, private)
	if e != nil {
		t.Fatal(e)
	}
	pk, e := x509.MarshalPKCS8PrivateKey(private)
	if e != nil {
		t.Fatal(e)
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), 0600)
	write := func(name string, v any) string {
		t.Helper()
		raw, err := yaml.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	start := func(args ...string) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		stopped := false
		t.Cleanup(func() {
			if !stopped {
				cmd.Process.Kill()
				<-done
			}
		})
		time.Sleep(500 * time.Millisecond)
		select {
		case err := <-done:
			stopped = true
			t.Fatal("official isolated process exited", err, regexp.MustCompile(`[a-f0-9]{32,64}`).ReplaceAllString(output.String(), "[redacted]"))
		default:
		}
	}
	config := write("server.yaml", map[string]any{"listen": fmt.Sprintf("127.0.0.1:%d", inboundPort), "tls": map[string]string{"cert": certPath, "key": keyPath}, "auth": map[string]any{"type": "http", "http": map[string]any{"url": backend.URL + p.CallbackPath, "insecure": false}}, "trafficStats": map[string]any{"listen": fmt.Sprintf("127.0.0.1:%d", statsPort), "secret": "fixture-stats-secret"}})
	start("server", "-c", config)
	r, e := (subscriberprovision.Service{Store: a.Store, Config: a.Config}).Create(ctx, subscriberprovision.Request{RequestID: "official-http-dynamic-001", Mode: "managed", InboundID: id, Confirmed: true, Subscriber: subscription.Subscriber{Name: "Official managed fixture", Enabled: true}})
	if e != nil || r.Status != "ready" {
		t.Fatal("dynamic DB provisioning", e)
	}
	var raw, ref string
	a.Store.DB.QueryRow(`SELECT c.auth_json,c.core_client_ref FROM credential_material c JOIN subscriber_credentials b ON b.credential_id=c.id WHERE b.subscriber_id=?`, r.SubscriberID).Scan(&raw, &ref)
	var auth map[string]string
	json.Unmarshal([]byte(raw), &auth)
	clientPath := write("client.yaml", map[string]any{"server": fmt.Sprintf("127.0.0.1:%d", inboundPort), "auth": auth["password"], "tls": map[string]any{"insecure": true, "sni": "localhost"}, "socks5": map[string]any{"listen": fmt.Sprintf("127.0.0.1:%d", socksPort)}})
	start("client", "-c", clientPath)
	payload := strings.Repeat("z", 65536)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) }))
	defer origin.Close()
	dialer, e := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, proxy.Direct)
	if e != nil {
		t.Fatal(e)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, n, addr string) (net.Conn, error) {
		return dialer.(proxy.ContextDialer).DialContext(ctx, n, addr)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, e := client.Get(origin.URL)
	if e != nil {
		t.Fatal("official HY2 HTTP-auth connection failed", e)
	}
	got, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || string(got) != payload {
		t.Fatal("proxy payload mismatch", e)
	}
	observed := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/traffic", statsPort), nil)
		req.Header.Set("Authorization", "fixture-stats-secret")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var users map[string]struct {
			RX uint64 `json:"rx"`
		}
		err = json.NewDecoder(res.Body).Decode(&users)
		res.Body.Close()
		if err == nil && users[ref].RX >= uint64(len(payload)) {
			observed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !observed {
		t.Fatal("official traffic did not use TML authentication ID")
	}
	var seen string
	if e = a.Store.DB.QueryRow("SELECT last_success_at FROM hysteria_auth_backends WHERE inbound_id=?", id).Scan(&seen); e != nil || seen == "" {
		t.Fatal("actual authentication evidence missing")
	}
	after, _ := os.ReadFile(config)
	var server map[string]any
	yaml.Unmarshal(after, &server)
	if server["auth"].(map[string]any)["type"] != "http" || server["auth"].(map[string]any)["userpass"] != nil {
		t.Fatal("provisioning wrote static users")
	}
}
