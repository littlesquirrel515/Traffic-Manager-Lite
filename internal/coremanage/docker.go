package coremanage

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	pb "traffic-manager-lite/internal/proto/singboxnative"
)

type Docker struct{}
type inspection struct {
	Mounts []struct {
		Type, Source, Destination string
		RW                        bool
	}
	Args   []string
	Path   string
	State  struct{ Running bool }
	Config struct{ Cmd, Entrypoint []string }
}

func command(ctx context.Context, args ...string) ([]byte, error) {
	b, e := exec.CommandContext(ctx, "docker", args...).Output()
	if e != nil {
		return nil, errors.New("controlled Docker operation failed")
	}
	return b, nil
}
func inspect(ctx context.Context, t Target) (inspection, error) {
	var items []inspection
	b, e := command(ctx, "inspect", t.Container)
	if e != nil {
		return inspection{}, e
	}
	if json.Unmarshal(b, &items) != nil || len(items) != 1 {
		return inspection{}, errors.New("invalid container inspection")
	}
	v := items[0]
	loaded := false
	count := 0
	args := append([]string{v.Path}, v.Args...)
	for n, a := range args {
		if a == "-c" || a == "--config" {
			count++
			if n+1 >= len(args) || args[n+1] != t.ContainerPath {
				return v, errors.New("multiple or mismatched configuration files rejected")
			}
			loaded = true
		}
		if a == "-C" || a == "--config-directory" || strings.HasPrefix(a, "--config=") {
			return v, errors.New("merged or implicit configuration is not managed")
		}
	}
	if !loaded || count != 1 {
		return v, errors.New("actual loaded configuration not verified")
	}
	mounted := false
	for _, m := range v.Mounts {
		if m.Type != "bind" {
			continue
		}
		if m.Destination == t.ContainerPath {
			return v, errors.New("single file bind mount cannot safely apply atomic replacement; mount its directory")
		}
		source, _ := filepath.EvalSymlinks(m.Source)
		target, _ := filepath.EvalSymlinks(t.Path)
		rel, e := filepath.Rel(source, target)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && strings.TrimRight(m.Destination, "/")+"/"+filepath.ToSlash(rel) == t.ContainerPath {
			mounted = true
		}
	}
	if !mounted {
		return v, errors.New("configuration directory mount not verified")
	}
	return v, nil
}
func (Docker) Check(ctx context.Context, t Target, candidate string) (string, error) {
	if _, e := inspect(ctx, t); e != nil {
		return "", e
	}
	if filepath.Dir(candidate) != filepath.Dir(t.Path) {
		return "", errors.New("candidate path rejected")
	}
	b, e := command(ctx, "exec", t.Container, "/usr/bin/sing-box", "version")
	if e != nil || !strings.HasPrefix(string(b), "sing-box version 1.14.3\n") {
		return "", errors.New("only verified sing-box 1.14.3 configuration management enabled")
	}
	path := strings.TrimSuffix(t.ContainerPath, filepath.Base(t.ContainerPath)) + filepath.Base(candidate)
	if _, e = command(ctx, "exec", t.Container, "/usr/bin/sing-box", "check", "-c", path); e != nil {
		return "", e
	}
	return "1.14.3", nil
}
func nativeStarted(ctx context.Context, t Target) (int64, error) {
	if t.NativeEndpoint == "" {
		return 0, errors.New("native endpoint required to verify restart")
	}
	conn, e := grpc.NewClient("passthrough:///"+t.NativeEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		return 0, e
	}
	defer conn.Close()
	if t.APISecret != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+t.APISecret)
	}
	client := pb.NewStartedServiceClient(conn)
	v, e := client.GetVersion(ctx, &emptypb.Empty{})
	if e != nil || v.Version != "1.14.3" {
		return 0, errors.New("native version not verified")
	}
	lifecycle, e := client.SubscribeServiceStatus(ctx, &emptypb.Empty{})
	if e != nil {
		return 0, e
	}
	state, e := lifecycle.Recv()
	if e != nil || state.Status != pb.ServiceStatus_STARTED {
		return 0, errors.New("native service not started")
	}
	status, e := client.SubscribeStatus(ctx, &pb.SubscribeStatusRequest{Interval: int64(time.Second)})
	if e != nil {
		return 0, e
	}
	if _, e = status.Recv(); e != nil {
		return 0, e
	}
	started, e := client.GetStartedAt(ctx, &emptypb.Empty{})
	if e != nil {
		return 0, e
	}
	return started.StartedAt, nil
}
func (Docker) Restart(ctx context.Context, t Target) error {
	if _, e := inspect(ctx, t); e != nil {
		return e
	}
	c, cancel := context.WithTimeout(ctx, time.Second)
	before, _ := nativeStarted(c, t)
	cancel()
	if _, e := command(ctx, "restart", "--time", "10", t.Container); e != nil {
		return e
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		v, e := inspect(ctx, t)
		if e == nil && v.State.Running {
			c, cancel := context.WithTimeout(ctx, time.Second)
			after, err := nativeStarted(c, t)
			cancel()
			if err == nil && after != 0 && (before == 0 || after != before) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return errors.New("restart health or epoch verification failed")
}

func (Docker) Verify(ctx context.Context, t Target) error {
	if _, e := inspect(ctx, t); e != nil {
		return e
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, e := nativeStarted(c, t)
	return e
}
