package coremonitor

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func echoPort(t *testing.T) int {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer connection.Close(); io.Copy(connection, connection) }()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}
func socksConnection(t *testing.T, proxy, target int) net.Conn {
	t.Helper()
	var connection net.Conn
	var e error
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		connection, e = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxy), 100*time.Millisecond)
		if e == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e != nil {
		t.Fatal("official client SOCKS not ready", e)
	}
	t.Cleanup(func() { connection.Close() })
	connection.SetDeadline(time.Now().Add(8 * time.Second))
	connection.Write([]byte{5, 1, 0})
	response := make([]byte, 2)
	if _, e = io.ReadFull(connection, response); e != nil || response[1] != 0 {
		t.Fatal("SOCKS handshake", e)
	}
	request := []byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(request[8:], uint16(target))
	connection.Write(request)
	header := make([]byte, 4)
	if _, e = io.ReadFull(connection, header); e != nil || header[1] != 0 {
		t.Fatal("actual proxy connect failed", e)
	}
	n := 0
	switch header[3] {
	case 1:
		n = 4
	case 4:
		n = 16
	case 3:
		b := make([]byte, 1)
		io.ReadFull(connection, b)
		n = int(b[0])
	default:
		t.Fatal("invalid SOCKS address")
	}
	if _, e = io.ReadFull(connection, make([]byte, n+2)); e != nil {
		t.Fatal(e)
	}
	assertEcho(t, connection)
	return connection
}
func assertEcho(t *testing.T, connection net.Conn) {
	t.Helper()
	connection.SetDeadline(time.Now().Add(8 * time.Second))
	payload := []byte("real-official-proxy-flow-survives-read-only-diagnosis")
	if _, e := connection.Write(payload); e != nil {
		t.Fatal(e)
	}
	response := make([]byte, len(payload))
	if _, e := io.ReadFull(connection, response); e != nil || string(response) != string(payload) {
		t.Fatal("existing proxy connection was changed or dropped", e)
	}
	connection.SetDeadline(time.Time{})
}
func jsonFile(t *testing.T, path string, data any) {
	t.Helper()
	b, e := json.Marshal(data)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func singboxClients(t *testing.T, binary, dir string, server map[string]any) []int {
	t.Helper()
	ins := server["inbounds"].([]any)
	clientInbounds, clientOutbounds, rules := []any{}, []any{}, []any{}
	ports := []int{}
	for _, raw := range ins {
		in := raw.(map[string]any)
		protocol := in["type"].(string)
		var user map[string]string
		if protocol == "vless" {
			v := in["users"].([]map[string]string)
			user = v[0]
		} else {
			user = in["users"].([]any)[0].(map[string]string)
		}
		port := freePort(t)
		ports = append(ports, port)
		tag := "client-" + protocol
		clientInbounds = append(clientInbounds, map[string]any{"type": "socks", "tag": tag, "listen": "127.0.0.1", "listen_port": port})
		out := map[string]any{"type": protocol, "tag": tag, "server": "127.0.0.1", "server_port": in["listen_port"]}
		if protocol == "vless" {
			out["uuid"] = user["uuid"]
		} else {
			out["password"] = user["password"]
			out["tls"] = map[string]any{"enabled": true, "server_name": "localhost", "insecure": true}
		}
		clientOutbounds = append(clientOutbounds, out)
		rules = append(rules, map[string]any{"inbound": []string{tag}, "outbound": tag})
	}
	path := filepath.Join(dir, "singbox-client.json")
	jsonFile(t, path, map[string]any{"log": map[string]string{"level": "error"}, "inbounds": clientInbounds, "outbounds": clientOutbounds, "route": map[string]any{"rules": rules}})
	process(t, binary, "run", "-c", path)
	return ports
}
