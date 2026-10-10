// Opt-in host agent. Run on the Docker host; never mount its socket into TML.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"traffic-manager-lite/internal/coremanage"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8091", "private host address")
	targets := flag.String("targets", "", "0600 target mapping JSON")
	tokenFile := flag.String("token-file", "", "0600 bearer token file")
	state := flag.String("state-dir", "", "private backup/journal directory")
	flag.Parse()
	if e := run(*listen, *targets, *tokenFile, *state); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(listen, targets, tokenFile, state string) error {
	host, _, e := net.SplitHostPort(listen)
	ip := net.ParseIP(host)
	if e != nil || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return fmt.Errorf("agent must listen on a private or loopback IP")
	}
	if state == "" {
		return fmt.Errorf("state directory required")
	}
	if !filepath.IsAbs(state) {
		return fmt.Errorf("absolute private state directory required")
	}
	if e := os.MkdirAll(state, 0700); e != nil {
		return e
	}
	unlock, e := coremanage.AcquireHostLock(filepath.Join(state, "agent.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	b, e := os.ReadFile(tokenFile)
	if e != nil {
		return fmt.Errorf("token file unavailable")
	}
	token := strings.TrimSpace(string(b))
	if len(token) < 32 {
		return fmt.Errorf("token must contain at least 32 characters")
	}
	b, e = os.ReadFile(targets)
	if e != nil {
		return fmt.Errorf("target mapping unavailable")
	}
	var list []coremanage.Target
	if json.Unmarshal(b, &list) != nil {
		return fmt.Errorf("invalid target mapping")
	}
	m := &coremanage.Manager{Targets: map[int64]coremanage.Target{}, StateDir: state, Check: (coremanage.Docker{}).Check, Restart: (coremanage.Docker{}).Restart, Verify: (coremanage.Docker{}).Verify}
	containers := map[string]bool{}
	paths := map[string]bool{}
	for _, t := range list {
		if t.ID <= 0 || m.Targets[t.ID].ID != 0 || containers[t.Container] || paths[t.Path] {
			return fmt.Errorf("invalid or duplicate instance mapping")
		}
		containers[t.Container] = true
		paths[t.Path] = true
		m.Targets[t.ID] = t
	}
	server := &http.Server{Addr: listen, Handler: m.Handler(token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 30 * time.Second}
	return server.ListenAndServe()
}
