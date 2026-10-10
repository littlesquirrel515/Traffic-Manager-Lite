package coremonitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

func freeUDPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()
	return port
}
func freePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}
func certificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	b, e := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cp, kp := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(cp, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b}), 0600)
	os.WriteFile(kp, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600)
	return cp, kp
}
func process(t *testing.T, binary string, args ...string) func() {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	time.Sleep(150 * time.Millisecond)
	select {
	case e := <-done:
		stopped = true
		t.Fatalf("official process failed: %v\n%s", e, output.String())
	default:
	}
	return stop
}
func writeEvidence(t *testing.T, binary string, i core.Instance) {
	t.Helper()
	out, e := exec.Command(binary, "version").Output()
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(i.ConfigPath)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(b)
	proof := map[string]string{"core_type": i.CoreType, "api_endpoint": i.APIEndpoint, "method": "controlled_core_version", "output": string(out), "observed_at": time.Now().UTC().Format(time.RFC3339), "config_sha256": hex.EncodeToString(sum[:])}
	data, _ := json.Marshal(proof)
	os.WriteFile(i.ConfigPath+".version.json", data, 0600)
}
func officialBinary(t *testing.T, env string) string {
	t.Helper()
	path := os.Getenv(env)
	if path == "" {
		t.Skip("set " + env + " to a verified official binary")
	}
	path, e := filepath.Abs(path)
	if e != nil {
		t.Fatal(e)
	}
	return path
}
func testService(t *testing.T, dir string, i core.Instance) Service {
	t.Helper()
	store, e := storage.Open(filepath.Join(dir, "traffic.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	now := storage.Stamp(time.Now())
	_, e = store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'isolated','localhost',?,?)", now, now)
	if e != nil {
		t.Fatal(e)
	}
	_, e = store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,control_endpoint,clash_endpoint,api_secret,config_path,enabled,created_at,updated_at) VALUES(?,1,'official',?,?,?,?,?, ?,1,?,?)", i.ID, i.CoreType, i.APIEndpoint, i.ControlEndpoint, i.ClashEndpoint, i.APISecret, i.ConfigPath, now, now)
	if e != nil {
		t.Fatal(e)
	}
	return Service{Store: store, Config: config.Config{ConfigRoot: dir, Timeout: 700 * time.Millisecond}}
}
func count(t *testing.T, s Service, query string) int {
	t.Helper()
	var n int
	if e := s.Store.DB.QueryRow(query).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func requireCheck(t *testing.T, r Report, api, state string) {
	t.Helper()
	for _, c := range r.Checks {
		if c.API == api {
			if c.Status != state {
				t.Fatalf("%s=%s (%s), expected %s", api, c.Status, c.Reason, state)
			}
			return
		}
	}
	t.Fatalf("missing check %s", api)
}
func observeReady(t *testing.T, s Service, i core.Instance) Report {
	t.Helper()
	var r Report
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		var e error
		r, e = s.Observe(context.Background(), i, false, "real_integration")
		if e != nil {
			t.Fatal(e)
		}
		ready := r.Connected == "Available"
		// Clash may start before the native gRPC listener. Wait for the actual
		// APIs asserted by this integration test rather than any responding API.
		if i.CoreType == "singbox" {
			for _, c := range r.Checks {
				if c.API == "原生 GetVersion" || c.API == "原生 SubscribeConnections" || c.API == "Clash /version" || c.API == "Clash /connections" {
					ready = ready && c.Status == "Available"
				}
			}
		}
		if ready {
			return r
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("official API never became ready: %+v", r.Checks)
	return r
}
func TestOfficialHysteria2Diagnostics(t *testing.T) {
	binary := officialBinary(t, "TML_TEST_HYSTERIA2")
	dir := t.TempDir()
	cert, key := certificate(t, dir)
	api, p := freePort(t), freeUDPPort(t)
	secret := "real-api-secret-for-isolated-test"
	i := core.Instance{ID: 1, ServerID: 1, CoreType: "hysteria2", APIEndpoint: fmt.Sprintf("http://localhost:%d", api), APISecret: secret, ConfigPath: filepath.Join(dir, "hysteria.yaml")}
	text := fmt.Sprintf("listen: 127.0.0.1:%d\ntls:\n  cert: %s\n  key: %s\nauth:\n  type: userpass\n  userpass:\n", p, filepath.ToSlash(cert), filepath.ToSlash(key))
	for n := 0; n < 10; n++ {
		text += fmt.Sprintf("    client-%02d: isolated-password-%d\n", n, n)
	}
	text += fmt.Sprintf("trafficStats:\n  listen: 127.0.0.1:%d\n  secret: %s\n", api, secret)
	os.WriteFile(i.ConfigPath, []byte(text), 0600)
	stop := process(t, binary, "server", "-c", i.ConfigPath)
	s := testService(t, dir, i)
	r := observeReady(t, s, i)
	if r.Version != "Unknown" {
		t.Fatal("invented API version")
	}
	for _, path := range []string{"/traffic", "/online", "/dump/streams"} {
		requireCheck(t, r, path, "Available")
	}
	requireCheck(t, r, "在线 IP", "Unsupported")
	if n := count(t, s, "SELECT count(*) FROM core_clients WHERE present=1"); n != 10 {
		t.Fatal("offline assets", n)
	}
	if count(t, s, "SELECT count(*) FROM traffic_cursors") != 0 {
		t.Fatal("empty traffic created baseline")
	}
	proxy := freePort(t)
	clientPath := filepath.Join(dir, "hy-client.yaml")
	clientConfig := fmt.Sprintf("server: 127.0.0.1:%d\nauth: client-00:isolated-password-0\ntls:\n  insecure: true\n  sni: localhost\nsocks5:\n  listen: 127.0.0.1:%d\n", p, proxy)
	os.WriteFile(clientPath, []byte(clientConfig), 0600)
	process(t, binary, "client", "-c", clientPath)
	connection := socksConnection(t, proxy, echoPort(t))
	r = observeReady(t, s, i)
	requireCheck(t, r, "/dump/streams", "Available")
	if count(t, s, "SELECT count(*) FROM core_user_states WHERE online_state='online'") != 1 {
		t.Fatal("actual Hysteria device was not observed")
	}
	writeEvidence(t, binary, i)
	r, e := s.Observe(context.Background(), i, true, "manual")
	if e != nil {
		t.Fatal(e)
	}
	if r.Version != "2.13.0" {
		t.Fatal("actual evidence not detected", r.Version)
	}
	b, _ := json.Marshal(r)
	if bytes.Contains(b, []byte(secret)) || bytes.Contains(b, []byte("isolated-password")) {
		t.Fatal("credential leaked")
	}
	assertEcho(t, connection)
	i.APISecret = "wrong-secret"
	r, e = s.Observe(context.Background(), i, false, "auth_failure")
	if e != nil {
		t.Fatal(e)
	}
	requireCheck(t, r, "/traffic", "AuthenticationFailed")
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 10 {
		t.Fatal("auth failure removed assets")
	}
	if r.Authentication != "AuthenticationFailed" {
		t.Fatal("auth state")
	}
	i.APISecret = secret
	stop()
	r, e = s.Observe(context.Background(), i, false, "disconnected")
	if e != nil {
		t.Fatal(e)
	}
	requireCheck(t, r, "/traffic", "Unreachable")
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 10 {
		t.Fatal("disconnected assets lost")
	}
	t.Log("PASS official Hysteria2: 10 offline Clients; three HTTP APIs empty Available; no clear/kick; auth/network distinction; actual fixed-command version evidence")
}
func TestOfficialSingBoxDiagnostics(t *testing.T) {
	binary := officialBinary(t, "TML_TEST_SINGBOX_DIAGNOSTICS")
	dir := t.TempDir()
	native, clash, inbound := freePort(t), freePort(t), freePort(t)
	cert, key := certificate(t, dir)
	secret := "real-singbox-secret"
	i := core.Instance{ID: 1, ServerID: 1, CoreType: "singbox", APIEndpoint: fmt.Sprintf("localhost:%d", native), ControlEndpoint: fmt.Sprintf("localhost:%d", native), ClashEndpoint: fmt.Sprintf("http://localhost:%d", clash), APISecret: secret, ConfigPath: filepath.Join(dir, "singbox.json")}
	users := []map[string]string{}
	for n := 0; n < 10; n++ {
		users = append(users, map[string]string{"name": fmt.Sprintf("client-%02d", n), "uuid": fmt.Sprintf("00000000-0000-4000-8000-%012d", n+1)})
	}
	cfg := map[string]any{"log": map[string]any{"level": "error"}, "inbounds": []any{map[string]any{"type": "vless", "tag": "vless-test", "listen": "127.0.0.1", "listen_port": inbound, "users": users}, map[string]any{"type": "trojan", "tag": "trojan-test", "listen": "127.0.0.1", "listen_port": freePort(t), "users": []any{map[string]string{"name": "trojan-user", "password": "private-trojan-password"}}, "tls": map[string]any{"enabled": true, "certificate_path": cert, "key_path": key}}, map[string]any{"type": "hysteria2", "tag": "hy-test", "listen": "127.0.0.1", "listen_port": freeUDPPort(t), "users": []any{map[string]string{"name": "hy-user", "password": "private-hy-password"}}, "tls": map[string]any{"enabled": true, "certificate_path": cert, "key_path": key}}, map[string]any{"type": "anytls", "tag": "anytls-test", "listen": "127.0.0.1", "listen_port": freePort(t), "users": []any{map[string]string{"name": "anytls-user", "password": "private-anytls-password"}}, "tls": map[string]any{"enabled": true, "certificate_path": cert, "key_path": key}}}, "outbounds": []any{map[string]string{"type": "direct", "tag": "direct"}}, "services": []any{map[string]any{"type": "api", "listen": "127.0.0.1", "listen_port": native, "secret": secret, "dashboard": false}}, "experimental": map[string]any{"clash_api": map[string]any{"external_controller": fmt.Sprintf("127.0.0.1:%d", clash), "secret": secret}}}
	b, _ := json.Marshal(cfg)
	os.WriteFile(i.ConfigPath, b, 0600)
	stop := process(t, binary, "run", "-c", i.ConfigPath)
	s := testService(t, dir, i)
	r := observeReady(t, s, i)
	if r.Version != "1.14.3" {
		t.Fatal("wrong actual version", r.Version)
	}
	requireCheck(t, r, "原生 GetVersion", "Available")
	requireCheck(t, r, "原生 SubscribeConnections", "Available")
	requireCheck(t, r, "Clash /version", "Available")
	requireCheck(t, r, "Clash /connections", "Available")
	requireCheck(t, r, "Clash 逐用户 Online", "Unsupported")
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 13 {
		t.Fatal("four protocol offline assets")
	}
	// The official release build may omit with_v2ray_api; this is not a reason to
	// lose native status or configuration assets. A compatibility error is explicit.
	if r.Checks[1].Status == "Unsupported" {
		t.Fatal("empty native result incorrectly unsupported")
	}
	proxies := singboxClients(t, binary, dir, cfg)
	target := echoPort(t)
	connections := []net.Conn{}
	for _, port := range proxies {
		connections = append(connections, socksConnection(t, port, target))
	}
	r = observeReady(t, s, i)
	if count(t, s, "SELECT count(*) FROM core_user_states WHERE online_state='online'") != 4 {
		t.Fatal("actual four-protocol user mappings not observed")
	}
	before := count(t, s, "SELECT count(*) FROM traffic_cursors")
	r, e := s.Observe(context.Background(), i, true, "manual")
	if e != nil {
		t.Fatal(e)
	}
	if count(t, s, "SELECT count(*) FROM traffic_cursors") != before || count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 13 {
		t.Fatal("diagnosis mutated traffic/assets")
	}
	b, _ = json.Marshal(r)
	for _, credential := range []string{secret, "private-trojan-password", "private-hy-password", "private-anytls-password", users[0]["uuid"]} {
		if bytes.Contains(b, []byte(credential)) {
			t.Fatal("secret leaked")
		}
	}
	for _, connection := range connections {
		assertEcho(t, connection)
	}
	i.APISecret = "wrong-secret"
	r, e = s.Observe(context.Background(), i, false, "wrong_auth")
	if e != nil {
		t.Fatal(e)
	}
	requireCheck(t, r, "原生 GetVersion", "AuthenticationFailed")
	requireCheck(t, r, "Clash /version", "AuthenticationFailed")
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 13 {
		t.Fatal("auth removed clients")
	}
	stop()
	t.Log("PASS official sing-box 1.14.3 native/Clash: actual version/auth; VLESS/Trojan/Hysteria2/AnyTLS offline Clients; compatibility failure isolated; no connection close or traffic reset")
}
func TestOfficialV2FlyDiagnostics(t *testing.T) {
	binary := officialBinary(t, "TML_TEST_V2FLY")
	dir := t.TempDir()
	api, inbound := freePort(t), freePort(t)
	i := core.Instance{ID: 1, ServerID: 1, CoreType: "v2fly", APIEndpoint: fmt.Sprintf("localhost:%d", api), ConfigPath: filepath.Join(dir, "v2ray.json")}
	users := []map[string]any{}
	for n := 0; n < 10; n++ {
		users = append(users, map[string]any{"email": fmt.Sprintf("client-%02d", n), "id": fmt.Sprintf("00000000-0000-4000-8000-%012d", n+1), "alterId": 0})
	}
	cfg := map[string]any{"log": map[string]string{"loglevel": "error"}, "api": map[string]any{"tag": "api", "services": []string{"StatsService", "HandlerService", "LoggerService", "ReflectionService"}}, "stats": map[string]any{}, "policy": map[string]any{"levels": map[string]any{"0": map[string]bool{"statsUserUplink": true, "statsUserDownlink": true}}}, "inbounds": []any{map[string]any{"tag": "api", "listen": "127.0.0.1", "port": api, "protocol": "dokodemo-door", "settings": map[string]string{"address": "127.0.0.1"}}, map[string]any{"tag": "vmess-test", "listen": "127.0.0.1", "port": inbound, "protocol": "vmess", "settings": map[string]any{"clients": users}}}, "outbounds": []any{map[string]string{"protocol": "freedom", "tag": "direct"}}, "routing": map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"}}}}
	b, _ := json.Marshal(cfg)
	os.WriteFile(i.ConfigPath, b, 0600)
	stop := process(t, binary, "run", "-c", i.ConfigPath)
	s := testService(t, dir, i)
	r := observeReady(t, s, i)
	requireCheck(t, r, "QueryStats", "Available")
	requireCheck(t, r, "v2ray.core.app.proxyman.command.HandlerService", "Available")
	requireCheck(t, r, "v2ray.core.app.log.command.LoggerService", "Available")
	requireCheck(t, r, "GetInboundUsers / ListInbounds", "Unsupported")
	requireCheck(t, r, "逐用户在线 / 在线 IP", "Unsupported")
	if r.Version != "Unknown" {
		t.Fatal("invented version")
	}
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 10 {
		t.Fatal("offline clients")
	}
	proxy := freePort(t)
	clientPath := filepath.Join(dir, "v2ray-client.json")
	jsonFile(t, clientPath, map[string]any{"log": map[string]string{"loglevel": "error"}, "inbounds": []any{map[string]any{"protocol": "socks", "listen": "127.0.0.1", "port": proxy, "settings": map[string]string{"auth": "noauth"}}}, "outbounds": []any{map[string]any{"protocol": "vmess", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": inbound, "users": []any{map[string]any{"id": users[0]["id"], "alterId": 0, "security": "none"}}}}}}}})
	process(t, binary, "run", "-c", clientPath)
	connection := socksConnection(t, proxy, echoPort(t))
	r = observeReady(t, s, i)
	if count(t, s, "SELECT count(*) FROM traffic_cursors") != 1 {
		t.Fatal("real VMess user traffic not collected")
	}
	writeEvidence(t, binary, i)
	r, e := s.Observe(context.Background(), i, true, "manual")
	if e != nil {
		t.Fatal(e)
	}
	if r.Version != "5.53.0" {
		t.Fatal("evidence version", r.Version)
	}
	if count(t, s, "SELECT count(*) FROM traffic_cursors") != 1 {
		t.Fatal("diagnosis changed actual baseline")
	}
	b, _ = json.Marshal(r)
	if strings.Contains(string(b), users[0]["id"].(string)) {
		t.Fatal("uuid leaked")
	}
	assertEcho(t, connection)
	stop()
	r, e = s.Observe(context.Background(), i, false, "disconnected")
	if e != nil {
		t.Fatal(e)
	}
	requireCheck(t, r, "QueryStats", "Unreachable")
	if count(t, s, "SELECT count(*) FROM core_clients WHERE present=1") != 10 {
		t.Fatal("stats failure removed clients")
	}
	t.Log("PASS official V2Fly: 10 offline config Clients; empty Stats Available; no Xray-only enumeration/Online; actual version evidence; disconnect preserves assets")
}
