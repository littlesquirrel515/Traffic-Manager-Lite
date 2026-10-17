package coremanage

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	xp "traffic-manager-lite/internal/proto/xray"
	"traffic-manager-lite/internal/security"
)

func checkAdditional(ctx context.Context, t Target, candidate string) (string, error) {
	if _, e := inspect(ctx, t); e != nil {
		return "", e
	}
	if filepath.Dir(candidate) != filepath.Dir(t.Path) {
		return "", errors.New("candidate directory rejected")
	}
	var binary string
	switch t.CoreType {
	case "xray":
		binary = "xray"
	case "hysteria2":
		binary = "hysteria"
	default:
		return "", errors.New("unsupported apply adapter")
	}
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(t.ExpectedVersion) {
		return "", errors.New("an exact authorized core version is required")
	}
	version, e := command(ctx, "exec", t.Container, binary, "version")
	if e != nil || !regexp.MustCompile(`(?:^|[^0-9.])`+regexp.QuoteMeta(t.ExpectedVersion)+`(?:$|[^0-9.])`).Match(version) {
		return "", errors.New("authorized core version mismatch")
	}
	path := strings.TrimSuffix(t.ContainerPath, filepath.Base(t.ContainerPath)) + filepath.Base(candidate)
	if t.CoreType == "xray" {
		_, e = command(ctx, "exec", t.Container, "xray", "run", "-test", "-c", path)
		return t.ExpectedVersion, e
	}
	// HY has no official offline check command. Only auth-only patches are allowed;
	// never invent a check flag or treat a generic YAML parser as a full core check.
	before, e := read(t.Path)
	if e != nil {
		return "", e
	}
	after, e := os.ReadFile(candidate)
	if e != nil {
		return "", e
	}
	var a, b map[string]any
	if yaml.Unmarshal(before, &a) != nil || yaml.Unmarshal(after, &b) != nil {
		return "", errors.New("invalid Hysteria2 YAML")
	}
	auth, _ := b["auth"].(map[string]any)
	if auth["type"] != "userpass" {
		return "", errors.New("only static userpass is managed")
	}
	users, ok := auth["userpass"].(map[string]any)
	if !ok || len(users) == 0 {
		return "", errors.New("invalid userpass mapping")
	}
	for name, password := range users {
		v, ok := password.(string)
		if !ok || v == "" || name != strings.ToLower(name) {
			return "", errors.New("invalid normalized HY userpass")
		}
	}
	delete(a, "auth")
	delete(b, "auth")
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if Hash(aa) != Hash(bb) {
		return "", errors.New("HY non-auth settings changed; full external validation required")
	}
	if e = verifyAdditional(ctx, t); e != nil {
		return "", e
	}
	return t.ExpectedVersion, nil
}
func verifyAdditional(ctx context.Context, t Target) error {
	v, e := inspect(ctx, t)
	if e != nil || !v.State.Running {
		return errors.New("core is not running")
	}
	if t.NativeEndpoint == "" {
		return errors.New("controlled API health endpoint required")
	}
	if t.CoreType == "xray" {
		conn, e := grpc.NewClient("passthrough:///"+t.NativeEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if e != nil {
			return e
		}
		defer conn.Close()
		_, e = xp.NewStatsServiceClient(conn).GetSysStats(ctx, &xp.SysStatsRequest{})
		if e != nil {
			return e
		}
		return verifyXrayUsers(ctx, conn, t)
	}
	if t.CoreType != "hysteria2" {
		return errors.New("unsupported health adapter")
	}
	client := (security.Policy{}).ForEndpoints(t.NativeEndpoint).HTTP()
	defer client.CloseIdleConnections()
	client.Timeout = 3 * time.Second
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(t.NativeEndpoint, "/")+"/traffic", nil)
	if e != nil {
		return e
	}
	if t.APISecret != "" {
		req.Header.Set("Authorization", t.APISecret)
	}
	r, e := client.Do(req)
	if e != nil {
		return errors.New("HY health unreachable")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return errors.New("HY health authentication/status failed")
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	var values map[string]json.RawMessage
	if e != nil || json.Unmarshal(b, &values) != nil {
		return errors.New("HY health payload invalid")
	}
	return nil
}
func restartAdditional(ctx context.Context, t Target) error {
	before, e := inspect(ctx, t)
	if e != nil {
		return e
	}
	if _, e = command(ctx, "restart", "--time", "10", t.Container); e != nil {
		return e
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		v, e := inspect(ctx, t)
		if e == nil && v.State.StartedAt != before.State.StartedAt && v.State.Running {
			c, cancel := context.WithTimeout(ctx, 3*time.Second)
			e = verifyAdditional(c, t)
			cancel()
			if e == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return errors.New("controlled core restart health failed")
}
