package subscription

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"traffic-manager-lite/internal/core"
)

// Optional real-client checks are read-only and never start a proxy service.
func TestRealClientConfigurationValidation(t *testing.T) {
	sing, mihomo := os.Getenv("TML_TEST_SINGBOX"), os.Getenv("TML_TEST_MIHOMO")
	if sing == "" || mihomo == "" {
		t.Skip("set TML_TEST_SINGBOX and TML_TEST_MIHOMO to enable official client checks")
	}
	nodes := []core.Node{}
	for _, p := range []string{"vless", "vmess", "trojan", "hysteria2", "anytls", "shadowsocks"} {
		n := node(p, "tcp")
		n.ID = p
		n.Profile.Name = p
		nodes = append(nodes, n)
	}
	ws := node("vless", "ws")
	ws.ID = "ws"
	ws.Profile.Name = "vless-ws"
	nodes = append(nodes, ws)
	grpc := node("trojan", "grpc")
	grpc.ID = "grpc"
	grpc.Profile.Name = "trojan-grpc"
	nodes = append(nodes, grpc)
	reality := node("vless", "tcp")
	reality.ID = "reality"
	reality.Profile.Name = "vless-reality"
	reality.Profile.PublicKey = "KjD1RnwGsoXDi_p6AW9VKBN50Y1GYqULb7TkpnTdSVQ"
	reality.Profile.ShortID = "abcdef"
	nodes = append(nodes, reality)
	for _, format := range []string{"singbox", "mihomo"} {
		t.Run(format, func(t *testing.T) {
			b, _, rejected, e := Generate(format, nodes)
			if e != nil || len(rejected) > 0 {
				t.Fatalf("generate %v %v", rejected, e)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "config."+map[string]string{"singbox": "json", "mihomo": "yaml"}[format])
			if e = os.WriteFile(file, b, 0600); e != nil {
				t.Fatal(e)
			}
			var cmd *exec.Cmd
			if format == "singbox" {
				cmd = exec.Command(sing, "check", "-c", file)
			} else {
				cmd = exec.Command(mihomo, "-t", "-f", file, "-d", dir)
			}
			cmd.Dir = dir
			out, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatalf("official client rejected config: %s %v", out, e)
			}
			t.Logf("official %s configuration validated: %s", format, out)
		})
	}
}
