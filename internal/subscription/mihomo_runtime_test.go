package subscription

import (
	"context"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This isolated core only exposes a localhost controller; it never changes the
// user's running Clash profile or system proxy and never dials a fixture node.
func TestRealMihomoFullProfileExposesSelectableRuntimeGroup(t *testing.T) {
	binary := os.Getenv("TML_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set TML_TEST_MIHOMO")
	}
	n := node("vless", "ws")
	r, e := Render(context.Background(), "mihomo", []ResolvedClientNode{{ID: "fixture", Profile: n.Profile}}, RenderOptions{})
	if e != nil {
		t.Fatal(e)
	}
	var config map[string]any
	if e = yaml.Unmarshal(r.Content, &config); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	listener.Close()
	config["mixed-port"] = 0
	config["external-controller"] = address
	config["secret"] = "fixture-controller-secret"
	b, _ := yaml.Marshal(config)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	if e = os.WriteFile(file, b, 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-f", file, "-d", dir)
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	client := http.Client{Timeout: time.Second}
	for {
		if ctx.Err() != nil {
			t.Fatal("temporary Mihomo controller unavailable")
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/proxies", nil)
		req.Header.Set("Authorization", "Bearer fixture-controller-secret")
		res, e := client.Do(req)
		if e != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		var data struct {
			Proxies map[string]struct {
				Type string   `json:"type"`
				All  []string `json:"all"`
			} `json:"proxies"`
		}
		e = json.NewDecoder(res.Body).Decode(&data)
		res.Body.Close()
		if e != nil {
			t.Fatal(e)
		}
		group, ok := data.Proxies["节点选择"]
		if !ok || group.Type != "Selector" || len(group.All) != 2 {
			t.Fatal("rule mode would display no selectable group")
		}
		if _, ok = data.Proxies[n.Profile.Name]; !ok {
			t.Fatal("group node not loaded")
		}
		return
	}
}
