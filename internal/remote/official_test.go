package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	sp "traffic-manager-lite/internal/proto/v2fly"
)

func TestOfficialXrayRemoteInventoryAndRuntimeUsers(t *testing.T) {
	binary := os.Getenv("TML_TEST_XRAY")
	if binary == "" {
		t.Skip("set TML_TEST_XRAY to verified official binary; local temporary process only")
	}
	s, i, _ := fixture(t)
	port := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	apiPort, inboundPort := port(), port()
	i.APIEndpoint = fmt.Sprintf("127.0.0.1:%d", apiPort)
	if _, e := s.DB.Exec("UPDATE instances SET api_endpoint=? WHERE id=1", i.APIEndpoint); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	config := map[string]any{"log": map[string]any{"loglevel": "none"}, "api": map[string]any{"tag": "api", "listen": i.APIEndpoint, "services": []string{"HandlerService", "StatsService"}}, "stats": map[string]any{}, "inbounds": []any{map[string]any{"tag": "official-ws", "listen": "127.0.0.1", "port": inboundPort, "protocol": "vless", "settings": map[string]any{"clients": []any{}, "decryption": "none"}, "streamSettings": map[string]any{"network": "ws", "wsSettings": map[string]any{"path": "/official"}}}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}}
	config["inbounds"] = append(config["inbounds"].([]any), map[string]any{"tag": "official-xhttp", "listen": "127.0.0.1", "port": port(), "protocol": "vless", "settings": map[string]any{"clients": []any{}, "decryption": "none"}, "streamSettings": map[string]any{"network": "xhttp", "xhttpSettings": map[string]any{"path": "/packet", "mode": "packet-up"}}})
	b, _ := json.Marshal(config)
	path := filepath.Join(dir, "isolated.json")
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, binary, "run", "-config", path)
	if e := command.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); command.Wait() }()
	var err error
	var inventory InventoryResult
	for attempt := 0; attempt < 50; attempt++ {
		c, stop := context.WithTimeout(ctx, 200*time.Millisecond)
		inventory, err = SyncXray(c, s, i, nil)
		stop()
		if err == nil && inventory.Count > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || inventory.Count != 2 {
		t.Fatal("official serialized config enumeration", inventory, err)
	}
	var inbound, raw string
	if e := s.DB.QueryRow("SELECT id,profile_json FROM catalog_inbounds WHERE tag='official-ws'").Scan(&inbound, &raw); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(raw, `"path":"/official"`) || !strings.Contains(raw, `"transport":"ws"`) {
		t.Fatal("official transport projection failed", raw)
	}
	var xhttpID string
	if e := s.DB.QueryRow("SELECT id FROM catalog_inbounds WHERE tag='official-xhttp'").Scan(&xhttpID); e != nil {
		t.Fatal(e)
	}
	connection, e := catalog.CreateConnection(ctx, s, catalog.Connection{InboundID: xhttpID, Name: "Official packet-up", Enabled: true, Profile: map[string]any{"address": "preferred.example", "port": 443, "host": "origin.example", "sni": "origin.example"}})
	if e != nil || connection.ValidationStatus != "ready" {
		t.Fatalf("official XHTTP defaults blocked: %+v %v", connection, e)
	}
	key := filepath.Join(dir, "desired.key")
	if e := os.WriteFile(key, []byte(strings.Repeat("o", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	d := Mutation{Client: Client{InboundID: inbound, Reference: "isolated-runtime-user", UUID: fixtureUUID, Confirmed: true}, Action: "add", RestoreAuthorized: true}
	r, e := Mutate(ctx, s, key, nil, d)
	if e != nil || r.Status != "runtime_verified" {
		t.Fatal("official runtime add/readback", r, e)
	}
	d.Revision = r.Revision
	d.Action = "update"
	d.UUID = "22222222-2222-4222-8222-222222222222"
	r, e = Mutate(ctx, s, key, nil, d)
	if e != nil || r.Status != "runtime_verified" {
		t.Fatal("official runtime replace/readback", r, e)
	}
	d.Revision = r.Revision
	d.Action = "remove"
	r, e = Mutate(ctx, s, key, nil, d)
	if e != nil || r.Status != "runtime_verified" {
		t.Fatal("official runtime remove/readback", r, e)
	}
	after, e := os.ReadFile(path)
	if e != nil || string(after) != string(b) {
		t.Fatal("runtime API modified core config file", e)
	}
}

func TestOfficialV2FlyRuntimeAcknowledgementIsNotPersistence(t *testing.T) {
	binary := os.Getenv("TML_TEST_V2FLY")
	if binary == "" {
		t.Skip("set TML_TEST_V2FLY to verified official binary")
	}
	s, i, _ := fixture(t)
	i.CoreType = "v2fly"
	port := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	apiPort, inboundPort := port(), port()
	i.APIEndpoint = fmt.Sprintf("127.0.0.1:%d", apiPort)
	if _, e := s.DB.Exec("UPDATE instances SET core_type='v2fly',api_endpoint=? WHERE id=1", i.APIEndpoint); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	config := map[string]any{"log": map[string]any{"loglevel": "none"}, "api": map[string]any{"tag": "api", "services": []string{"HandlerService", "StatsService"}}, "stats": map[string]any{}, "inbounds": []any{map[string]any{"tag": "api-in", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}, map[string]any{"tag": "vmess-real", "listen": "127.0.0.1", "port": inboundPort, "protocol": "vmess", "settings": map[string]any{"clients": []any{}}}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}, "routing": map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"api-in"}, "outboundTag": "api"}}}}
	b, _ := json.Marshal(config)
	path := filepath.Join(dir, "isolated-v2fly.json")
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, binary, "run", "-c", path)
	if e := command.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); command.Wait() }()
	ready := false
	for attempt := 0; attempt < 50; attempt++ {
		c, stop := context.WithTimeout(ctx, 200*time.Millisecond)
		conn, e := dialRuntime(c, i, nil)
		if e == nil {
			_, e = sp.NewStatsServiceClient(conn).QueryStats(c, &sp.QueryStatsRequest{Reset_: false})
			conn.Close()
		}
		stop()
		if e == nil {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("official V2Fly API did not start")
	}
	inbound, e := SaveManual(ctx, s, Inbound{InstanceID: 1, Tag: "vmess-real", Profile: core.NodeProfile{Protocol: "vmess", Transport: "tcp"}})
	if e != nil {
		t.Fatal(e)
	}
	key := filepath.Join(dir, "desired.key")
	if e = os.WriteFile(key, []byte(strings.Repeat("v", 32)), 0600); e != nil {
		t.Fatal(e)
	}
	d := Mutation{Client: Client{InboundID: inbound, Reference: "isolated-v2fly-runtime", UUID: fixtureUUID, Confirmed: true}, Action: "add"}
	r, e := Mutate(ctx, s, key, nil, d)
	if e != nil || r.Status != "runtime_applied_unverified" || r.Persistence != "runtime_only" {
		t.Fatal("V2Fly acknowledgement claimed verified/persistent", r, e)
	}
	d.Revision = r.Revision
	d.Action = "update"
	if _, e = Mutate(ctx, s, key, nil, d); e == nil {
		t.Fatal("unsafe V2Fly update accepted")
	}
	d.Action = "remove"
	r, e = Mutate(ctx, s, key, nil, d)
	if e != nil || r.Status != "runtime_applied_unverified" {
		t.Fatal("official V2Fly remove", r, e)
	}
	after, e := os.ReadFile(path)
	if e != nil || string(after) != string(b) {
		t.Fatal("runtime CRUD changed core config", e)
	}
}
