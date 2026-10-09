package xraymonitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
	"traffic-manager-lite/internal/storage"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// This is a real official Xray process, not a mocked gRPC server.
func TestRealXrayCollectorsAndDiagnostics(t *testing.T) {
	binary := os.Getenv("TML_TEST_XRAY")
	if binary == "" {
		t.Skip("set TML_TEST_XRAY to a verified official Xray binary")
	}
	dir := t.TempDir()
	store, e := storage.Open(filepath.Join(dir, "traffic.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	now := storage.Stamp(time.Now())
	store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'isolated-real-xray','localhost',?,?)", now, now)
	apiPort, port, port2 := freePort(t), freePort(t), freePort(t)
	endpoint := fmt.Sprintf("localhost:%d", apiPort)
	configPath := filepath.Join(dir, "xray.json")
	store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,config_path,created_at,updated_at) VALUES(1,1,'official-test','xray',?,?,?,?)", endpoint, configPath, now, now)
	inst := core.Instance{ID: 1, ServerID: 1, Name: "official-test", CoreType: "xray", APIEndpoint: endpoint, ConfigPath: configPath, Enabled: true}
	service := Service{Store: store, Config: config.Config{Timeout: time.Second, ConfigRoot: dir}}
	ctx := context.Background()
	clients := []map[string]any{}
	for j := 1; j <= 10; j++ {
		clients = append(clients, map[string]any{"id": fmt.Sprintf("%08x-0000-4000-8000-%012x", j, j), "email": fmt.Sprintf("user%d", j)})
	}
	start := func(services []string, online, duplicate bool) func() {
		in := []map[string]any{{"tag": "primary", "listen": "127.0.0.1", "port": port, "protocol": "vless", "settings": map[string]any{"clients": clients, "decryption": "none"}, "streamSettings": map[string]any{"network": "tcp"}}}
		if duplicate {
			in = append(in, map[string]any{"tag": "secondary", "listen": "127.0.0.1", "port": port2, "protocol": "vless", "settings": map[string]any{"clients": clients[:1], "decryption": "none"}, "streamSettings": map[string]any{"network": "tcp"}})
		}
		cfg := map[string]any{"log": map[string]any{"loglevel": "warning"}, "api": map[string]any{"tag": "api", "listen": fmt.Sprintf("127.0.0.1:%d", apiPort), "services": services}, "stats": map[string]any{}, "policy": map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true, "statsUserOnline": online, "connIdle": 60, "uplinkOnly": 1, "downlinkOnly": 1}}}, "inbounds": in, "outbounds": []map[string]any{{"protocol": "freedom", "tag": "direct"}}}
		data, _ := json.Marshal(cfg)
		os.WriteFile(configPath, data, 0600)
		log, e := os.Create(filepath.Join(dir, fmt.Sprintf("process-%d.log", time.Now().UnixNano())))
		if e != nil {
			t.Fatal(e)
		}
		command := exec.Command(binary, "run", "-config", configPath)
		command.Stdout = log
		command.Stderr = log
		if e = command.Start(); e != nil {
			t.Fatal(e)
		}
		var once sync.Once
		stop := func() { once.Do(func() { command.Process.Kill(); command.Wait(); log.Close() }) }
		t.Cleanup(stop)
		conn, _ := grpc.NewClient("passthrough:///"+endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
		defer conn.Close()
		for j := 0; j < 100; j++ {
			c, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			_, e := hp.NewHandlerServiceClient(conn).ListInbounds(c, &hp.ListInboundsRequest{IsOnlyTags: true})
			cancel()
			if e == nil {
				return stop
			}
			time.Sleep(30 * time.Millisecond)
		}
		stop()
		t.Fatal("official Xray did not start its HandlerService")
		return stop
	}
	observe := func(diagnostic bool) Report {
		t.Helper()
		r, e := service.Observe(ctx, inst, diagnostic, "real_integration")
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	assertCount := func(query string, want int) {
		t.Helper()
		var n int
		if e := store.DB.QueryRow(query).Scan(&n); e != nil || n != want {
			t.Fatalf("%s: got %d want %d (%v)", query, n, want, e)
		}
	}
	stop := start([]string{"HandlerService", "StatsService"}, false, false)
	r := observe(false)
	assertCount("SELECT COUNT(*) FROM xray_clients WHERE source='runtime_api' AND present=1", 10)
	assertCount("SELECT COUNT(*) FROM users", 10)
	assertCount("SELECT COUNT(*) FROM traffic_cursors", 0)
	assertCount("SELECT COUNT(*) FROM xray_user_states WHERE stats_state='not_generated' AND online_state='unknown'", 10)
	if r.Version != "Unknown" {
		t.Fatal("invented runtime version")
	}
	t.Log("PASS official process: 10 configured offline Clients; empty Stats; Online missing -> Unknown")
	// Actual executable output is bound to endpoint and config hash, never to image latest.
	output, e := exec.Command(binary, "version").Output()
	if e != nil {
		t.Fatal(e)
	}
	cb, _ := os.ReadFile(configPath)
	sum := sha256.Sum256(cb)
	evidence := map[string]any{"api_endpoint": endpoint, "method": "controlled_xray_version", "output": strings.Split(string(output), "\n")[0], "observed_at": time.Now().UTC().Format(time.RFC3339), "config_sha256": hex.EncodeToString(sum[:])}
	vb, _ := json.Marshal(evidence)
	os.WriteFile(configPath+".version.json", vb, 0600)
	r = observe(true)
	if r.Version != "26.3.27" {
		t.Fatalf("actual version detection: %s", r.Version)
	}
	assertCount("SELECT COUNT(*) FROM traffic_cursors", 0)
	// Runtime/file discrepancy remains visible as distinct sources, not a merged inventory.
	changed := map[string]any{}
	json.Unmarshal(cb, &changed)
	in := changed["inbounds"].([]any)
	settings := in[0].(map[string]any)["settings"].(map[string]any)
	fileClients := settings["clients"].([]any)[:9]
	fileClients[0].(map[string]any)["id"] = "11111111-1111-4111-8111-111111111111"
	settings["clients"] = fileClients
	changedBytes, _ := json.Marshal(changed)
	os.WriteFile(configPath, changedBytes, 0600)
	observe(false)
	assertCount("SELECT COUNT(*) FROM xray_clients WHERE source='runtime_api' AND present=1", 10)
	assertCount("SELECT COUNT(*) FROM xray_clients WHERE source='config_file' AND present=1", 9)
	assertCount("SELECT count(*) FROM xray_clients a JOIN xray_clients b ON a.instance_id=b.instance_id AND a.inbound_tag=b.inbound_tag AND a.email=b.email WHERE a.source='runtime_api' AND b.source='config_file' AND a.email='user1' AND a.credential_fingerprint<>'' AND b.credential_fingerprint<>'' AND a.credential_fingerprint<>b.credential_fingerprint", 1)
	t.Log("PASS runtime/config discrepancy independently retained; diagnostics created no traffic baseline")
	stop()
	stop = start([]string{"HandlerService"}, false, false)
	cb, _ = os.ReadFile(configPath)
	sum = sha256.Sum256(cb)
	evidence["config_sha256"] = hex.EncodeToString(sum[:])
	evidence["observed_at"] = time.Now().UTC().Format(time.RFC3339)
	vb, _ = json.Marshal(evidence)
	os.WriteFile(configPath+".version.json", vb, 0600)
	r = observe(false)
	disabled := false
	for _, check := range r.Checks {
		if check.API == "QueryStats" && check.Status == "Disabled" {
			disabled = true
		}
	}
	if !disabled {
		t.Fatal("actual known-version disabled StatsService not distinguished")
	}
	assertCount("SELECT COUNT(*) FROM xray_clients WHERE source='runtime_api' AND present=1", 10)
	assertCount("SELECT COUNT(*) FROM collector_health WHERE collector='stats' AND status<>'Healthy'", 1)
	t.Log("PASS actual Stats/Online service disabled: Clients unaffected")
	stop()
	stop = start([]string{"HandlerService", "StatsService"}, true, true)
	echo, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	traffic := func(serverPort int) net.Conn {
		t.Helper()
		conn, e := (&net.Dialer{Timeout: time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}).Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort))
		if e != nil {
			t.Fatal(e)
		}
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		id, _ := hex.DecodeString(strings.ReplaceAll(clients[0]["id"].(string), "-", ""))
		header := append([]byte{0}, id...)
		targetPort := echo.Addr().(*net.TCPAddr).Port
		header = append(header, 0, 1, byte(targetPort>>8), byte(targetPort), 1, 127, 0, 0, 1)
		payload := []byte(strings.Repeat("actual-vless-traffic", 100))
		conn.Write(append(header, payload...))
		response := make([]byte, 2)
		if _, e = io.ReadFull(conn, response); e != nil {
			t.Fatal(e)
		}
		if response[1] != 0 {
			t.Fatal("unexpected VLESS response addons")
		}
		got := make([]byte, len(payload))
		if _, e = io.ReadFull(conn, got); e != nil || string(got) != string(payload) {
			t.Fatal("actual VLESS echo failed", e)
		}
		conn.SetDeadline(time.Time{})
		return conn
	}
	connection := traffic(port)
	observe(false)
	assertCount("SELECT COUNT(*) FROM xray_clients WHERE source='runtime_api' AND present=1", 11)
	assertCount("SELECT COUNT(*) FROM identities WHERE core_user_key='user1' AND scope='user'", 1)
	assertCount("SELECT COUNT(*) FROM xray_user_states WHERE email='user1' AND online_state='online'", 1)
	connection2 := traffic(port2)
	time.Sleep(150 * time.Millisecond)
	observe(false)
	assertCount("SELECT COUNT(*) FROM traffic_cursors", 1)
	var delta int64
	if e := store.DB.QueryRow("SELECT COALESCE(SUM(upload_delta),0) FROM traffic_samples").Scan(&delta); e != nil {
		t.Fatal(e)
	}
	if delta <= 0 {
		var rawUp, rawDown int64
		store.DB.QueryRow("SELECT raw_upload,raw_download FROM traffic_cursors").Scan(&rawUp, &rawDown)
		t.Fatalf("real traffic delta not recorded (raw %d/%d)", rawUp, rawDown)
	}
	var before int64
	store.DB.QueryRow("SELECT raw_upload FROM traffic_cursors").Scan(&before)
	observe(true)
	var after int64
	store.DB.QueryRow("SELECT raw_upload FROM traffic_cursors").Scan(&after)
	if before != after {
		t.Fatal("diagnostic mutated cursor")
	}
	// A real VLESS header-only connection registers user counters without payload bytes.
	zeroConn, e := (&net.Dialer{Timeout: time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}).Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		t.Fatal(e)
	}
	defer zeroConn.Close()
	id2, _ := hex.DecodeString(strings.ReplaceAll(clients[1]["id"].(string), "-", ""))
	zeroHeader := append([]byte{0}, id2...)
	targetPort := echo.Addr().(*net.TCPAddr).Port
	zeroHeader = append(zeroHeader, 0, 1, byte(targetPort>>8), byte(targetPort), 1, 127, 0, 0, 1)
	zeroConn.Write(zeroHeader)
	for j := 0; j < 10; j++ {
		time.Sleep(50 * time.Millisecond)
		observe(false)
		var n int
		store.DB.QueryRow("SELECT count(*) FROM xray_user_states WHERE email='user2' AND stats_state='zero'").Scan(&n)
		if n == 1 {
			break
		}
	}
	assertCount("SELECT count(*) FROM xray_user_states WHERE email='user2' AND stats_state='zero'", 1)
	t.Log("PASS real registered zero-byte counters distinguished from not-generated counters")
	connection.Close()
	connection2.Close()
	for j := 0; j < 40; j++ {
		time.Sleep(100 * time.Millisecond)
		observe(false)
		var n int
		store.DB.QueryRow("SELECT COUNT(*) FROM xray_user_states WHERE email='user1' AND online_state='offline'").Scan(&n)
		if n == 1 {
			break
		}
	}
	assertCount("SELECT COUNT(*) FROM xray_user_states WHERE email='user1' AND online_state='offline'", 1)
	t.Log("PASS real VLESS traffic, Online positive/zero, shared Email across two inbounds without duplicate identity/cursor, read-only diagnostic")
	stop()
	observe(false)
	assertCount("SELECT COUNT(*) FROM xray_clients WHERE source='runtime_api' AND present=1", 11)
	assertCount("SELECT COUNT(*) FROM xray_user_states WHERE online_state='offline'", 0)
	t.Log("PASS API fully disconnected: retained Clients; states Unknown")
	if previous := os.Getenv("TML_TEST_XRAY_PREVIOUS"); previous != "" {
		latest := binary
		binary = previous
		stop = start([]string{"HandlerService", "StatsService"}, false, true)
		writeEvidence := func() {
			output, e := exec.Command(binary, "version").Output()
			if e != nil {
				t.Fatal(e)
			}
			cb, _ := os.ReadFile(configPath)
			sum := sha256.Sum256(cb)
			ev := map[string]any{"api_endpoint": endpoint, "method": "controlled_xray_version", "output": strings.Split(string(output), "\n")[0], "observed_at": time.Now().UTC().Format(time.RFC3339), "config_sha256": hex.EncodeToString(sum[:])}
			data, _ := json.Marshal(ev)
			os.WriteFile(configPath+".version.json", data, 0600)
		}
		writeEvidence()
		r = observe(false)
		if r.Version == "Unknown" || r.Version == "26.3.27" {
			t.Fatal("previous runtime version not detected", r.Version)
		}
		stop()
		binary = latest
		stop = start([]string{"HandlerService", "StatsService"}, true, true)
		writeEvidence()
		r = observe(false)
		if r.Version != "26.3.27" {
			t.Fatal("upgraded runtime version", r.Version)
		}
		var trigger string
		store.DB.QueryRow("SELECT trigger FROM xray_diagnostics ORDER BY id DESC LIMIT 1").Scan(&trigger)
		if trigger != "version_changed" {
			t.Fatal("upgrade did not re-diagnose", trigger)
		}
		var beforeReset int64
		store.DB.QueryRow("SELECT COALESCE(SUM(upload_delta),0) FROM traffic_samples").Scan(&beforeReset)
		fresh := traffic(port)
		observe(false)
		fresh.Close()
		assertCount("SELECT count(*) FROM xray_user_states WHERE email='user1' AND stats_state='reset_baseline'", 1)
		var afterReset int64
		store.DB.QueryRow("SELECT COALESCE(SUM(upload_delta),0) FROM traffic_samples").Scan(&afterReset)
		if beforeReset != afterReset {
			t.Fatal("restart baseline incorrectly counted old/unobserved traffic")
		}
		t.Log("PASS actual official previous release -> latest release upgrade; runtime version evidence and capability re-diagnosis")
	}
}
