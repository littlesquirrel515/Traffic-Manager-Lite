// Package coremanage is an opt-in host boundary. The monitoring container never
// receives a Docker Socket or permission to execute arbitrary commands.
package coremanage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrConflict = errors.New("configuration revision conflict")

type Target struct {
	ID             int64  `json:"instance_id"`
	Container      string `json:"container"`
	Path           string `json:"host_config"`
	ContainerPath  string `json:"container_config"`
	NativeEndpoint string `json:"native_endpoint"`
	APISecret      string `json:"api_secret"`
}
type User struct {
	Name     string `json:"name"`
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Flow     string `json:"flow,omitempty"`
}
type Request struct {
	Operation      string `json:"operation"`
	Revision       string `json:"revision"`
	Inbound        string `json:"inbound"`
	PreviousName   string `json:"previous_name"`
	User           User   `json:"user"`
	ConfirmDelete  bool   `json:"confirm_delete"`
	ConfirmRestart bool   `json:"confirm_restart"`
}
type Result struct {
	Revision         string           `json:"revision"`
	PreviousRevision string           `json:"previous_revision,omitempty"`
	Status           string           `json:"apply_status"`
	Version          string           `json:"version"`
	Inbounds         []map[string]any `json:"inbounds,omitempty"`
	ObservedAt       string           `json:"observed_at"`
	Reason           string           `json:"reason,omitempty"`
}
type journal struct {
	Old, New, Backup, Status, Version         string
	Operation, Inbound, BeforeName, AfterName string
	Updated                                   time.Time
}
type Manager struct {
	Targets  map[int64]Target
	StateDir string
	Check    func(context.Context, Target, string) (string, error)
	Restart  func(context.Context, Target) error
	Verify   func(context.Context, Target) error
	mu       sync.Mutex
}

func Hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func (m *Manager) target(id int64) (Target, error) {
	t, ok := m.Targets[id]
	if !ok || t.ID != id {
		return t, errors.New("target is not authorized")
	}
	if !filepath.IsAbs(t.Path) || !strings.HasPrefix(t.ContainerPath, "/") || t.Container == "" || strings.HasPrefix(t.Container, "-") {
		return t, errors.New("invalid controlled target")
	}
	e := rejectLinks(t.Path)
	if e != nil {
		return t, errors.New("symlink or missing configuration rejected")
	}
	return t, nil
}
func read(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, errors.New("invalid configuration file")
	}
	return os.ReadFile(path)
}
func (m *Manager) journalPath(id int64) string {
	return filepath.Join(m.StateDir, fmt.Sprintf("instance-%d.json", id))
}
func (m *Manager) load(id int64) journal {
	var j journal
	b, e := os.ReadFile(m.journalPath(id))
	if e == nil {
		json.Unmarshal(b, &j)
	}
	return j
}
func atomic(path string, b []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".tml-atomic-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = preserveOwner(path, tmp); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e == nil {
		dir.Sync()
		dir.Close()
	}
	return nil
}
func (m *Manager) save(id int64, j journal) error {
	if e := os.MkdirAll(m.StateDir, 0700); e != nil {
		return e
	}
	if e := os.Chmod(m.StateDir, 0700); e != nil {
		return e
	}
	j.Updated = time.Now().UTC()
	b, _ := json.Marshal(j)
	if e := atomic(m.journalPath(id), b, 0600); e != nil {
		return e
	}
	audit, err := os.OpenFile(filepath.Join(m.StateDir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer audit.Close()
	entry, _ := json.Marshal(map[string]any{"instance_id": id, "old_revision": j.Old, "revision": j.New, "apply_status": j.Status, "time": j.Updated, "version": j.Version, "operation": j.Operation, "inbound": j.Inbound, "user_before": j.BeforeName, "user_after": j.AfterName})
	if _, err = audit.Write(append(entry, '\n')); err != nil {
		return err
	}
	return audit.Sync()
}
func parse(b []byte) (map[string]any, []any, error) {
	var root map[string]any
	if json.Unmarshal(b, &root) != nil || root == nil {
		return nil, nil, errors.New("invalid JSON configuration")
	}
	in, ok := root["inbounds"].([]any)
	if !ok || len(in) > 256 {
		return nil, nil, errors.New("inbounds unavailable")
	}
	return root, in, nil
}
func public(in []any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range in {
		v, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		users := []map[string]any{}
		if a, ok := v["users"].([]any); ok {
			for _, raw := range a {
				u, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				users = append(users, map[string]any{"name": u["name"], "has_uuid": u["uuid"] != nil, "has_password": u["password"] != nil, "flow": u["flow"]})
			}
		}
		out = append(out, map[string]any{"tag": v["tag"], "protocol": v["type"], "user_count": len(users), "users": users})
	}
	return out
}
func (m *Manager) Read(ctx context.Context, id int64) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, e := m.target(id)
	if e != nil {
		return Result{}, e
	}
	b, e := read(t.Path)
	if e != nil {
		return Result{}, e
	}
	_, in, e := parse(b)
	if e != nil {
		return Result{}, e
	}
	j := m.load(id)
	status := j.Status
	if status == "" {
		status = "Unknown"
	}
	if j.New != "" && j.New != Hash(b) {
		status = "ExternalChange"
		if j.Status == "Saved" && j.Old == Hash(b) {
			status = "SaveFailedOriginalPreserved"
		}
	}
	if status == "Applied" && m.Verify != nil {
		if err := m.Verify(ctx, t); err != nil {
			status = "RuntimeUnverified"
		}
	}
	return Result{Revision: Hash(b), Status: status, Version: j.Version, Inbounds: public(in), ObservedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}
func mutate(root map[string]any, in []any, r Request) error {
	var target map[string]any
	matches := 0
	for _, raw := range in {
		v, ok := raw.(map[string]any)
		if !ok {
			return errors.New("invalid inbound")
		}
		if v["tag"] == r.Inbound {
			target = v
			matches++
		}
	}
	if matches != 1 {
		return errors.New("inbound must be uniquely tagged")
	}
	protocol, _ := target["type"].(string)
	if protocol != "vless" && protocol != "hysteria2" && protocol != "anytls" {
		return errors.New("protocol is not managed")
	}
	users, _ := target["users"].([]any)
	found := -1
	for n, raw := range users {
		u, ok := raw.(map[string]any)
		if !ok {
			return errors.New("invalid user")
		}
		if u["name"] == r.PreviousName {
			if found != -1 {
				return errors.New("ambiguous user name")
			}
			found = n
		}
	}
	if r.Operation == "delete" {
		if !r.ConfirmDelete {
			return errors.New("delete requires second confirmation")
		}
		if found < 0 {
			return errors.New("user not found")
		}
		target["users"] = append(users[:found], users[found+1:]...)
		return nil
	}
	if r.Operation != "add" && r.Operation != "update" {
		return errors.New("invalid user operation")
	}
	if r.User.Name == "" || len(r.User.Name) > 512 || strings.ContainsAny(r.User.Name, "\x00\r\n") {
		return errors.New("invalid user name")
	}
	for n, raw := range users {
		if raw.(map[string]any)["name"] == r.User.Name && (r.Operation == "add" || n != found) {
			return errors.New("duplicate user in inbound")
		}
	}
	var u map[string]any
	if r.Operation == "update" {
		if found < 0 {
			return errors.New("user not found")
		}
		u = users[found].(map[string]any)
	} else {
		u = map[string]any{}
	}
	u["name"] = r.User.Name
	if protocol == "vless" {
		if r.User.UUID != "" {
			if !validUUID(r.User.UUID) {
				return errors.New("invalid UUID")
			}
			u["uuid"] = r.User.UUID
		}
		if u["uuid"] == nil {
			return errors.New("UUID required")
		}
		if r.User.Flow != "" && r.User.Flow != "xtls-rprx-vision" {
			return errors.New("invalid flow")
		}
		u["flow"] = r.User.Flow
	} else {
		if r.User.Password != "" {
			if len(r.User.Password) > 1024 {
				return errors.New("password too long")
			}
			u["password"] = r.User.Password
		}
		if u["password"] == nil {
			return errors.New("password required")
		}
	}
	if r.Operation == "add" {
		users = append(users, u)
	}
	if len(users) > 10000 {
		return errors.New("user limit")
	}
	target["users"] = users
	return nil
}
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for n, c := range s {
		if n == 8 || n == 13 || n == 18 || n == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
func (m *Manager) Update(ctx context.Context, id int64, r Request) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, e := m.target(id)
	if e != nil {
		return Result{}, e
	}
	b, e := read(t.Path)
	if e != nil {
		return Result{}, e
	}
	old := Hash(b)
	if r.Revision != old {
		return Result{}, ErrConflict
	}
	j := m.load(id)
	if j.Status == "Saved" && j.Old == old {
		j.Status = "SaveFailedOriginalPreserved"
	}
	if j.Status == "PendingRestart" || j.Status == "Saved" {
		return Result{}, errors.New("apply or roll back pending revision before next change")
	}
	root, in, e := parse(b)
	if e != nil {
		return Result{}, e
	}
	if e = mutate(root, in, r); e != nil {
		return Result{}, e
	}
	candidate, e := json.MarshalIndent(root, "", "  ")
	if e != nil {
		return Result{}, e
	}
	info, e := os.Stat(t.Path)
	if e != nil {
		return Result{}, e
	}
	f, e := os.CreateTemp(filepath.Dir(t.Path), ".tml-check-*.json")
	if e != nil {
		return Result{}, e
	}
	path := f.Name()
	defer os.Remove(path)
	f.Chmod(info.Mode().Perm())
	_, e = f.Write(candidate)
	f.Close()
	if e != nil {
		return Result{}, e
	}
	if e = preserveOwner(t.Path, path); e != nil {
		return Result{}, e
	}
	version, e := m.Check(ctx, t, path)
	if e != nil {
		return Result{}, errors.New("matching core configuration check failed; original preserved")
	}
	current, e := read(t.Path)
	if e != nil || Hash(current) != old {
		return Result{}, ErrConflict
	}
	if e = os.MkdirAll(m.StateDir, 0700); e != nil {
		return Result{}, e
	}
	backup := filepath.Join(m.StateDir, fmt.Sprintf("instance-%d-%s.json", id, old))
	if _, e = os.Stat(backup); os.IsNotExist(e) {
		if e = atomic(backup, b, 0600); e != nil {
			return Result{}, e
		}
	}
	j = journal{Old: old, New: Hash(candidate), Backup: backup, Status: "Saved", Version: version, Operation: r.Operation, Inbound: r.Inbound, BeforeName: r.PreviousName, AfterName: r.User.Name}
	if e = m.save(id, j); e != nil {
		return Result{}, e
	}
	if e = atomic(t.Path, candidate, info.Mode().Perm()); e != nil {
		j.Status = "SaveFailedOriginalPreserved"
		m.save(id, j)
		return Result{}, e
	}
	j.Status = "PendingRestart"
	if e = m.save(id, j); e != nil {
		return Result{}, e
	}
	return Result{Revision: j.New, PreviousRevision: old, Status: j.Status, Version: version, Inbounds: public(in), ObservedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}
func (m *Manager) Apply(ctx context.Context, id int64, r Request) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, e := m.target(id)
	if e != nil {
		return Result{}, e
	}
	b, e := read(t.Path)
	if e != nil {
		return Result{}, e
	}
	if Hash(b) != r.Revision {
		return Result{}, ErrConflict
	}
	j := m.load(id)
	if j.New != r.Revision || j.Old == "" {
		return Result{}, errors.New("no controlled revision to apply")
	}
	if j.Status == "RolledBack" {
		if r.Operation == "rollback" {
			return Result{Revision: j.New, Status: j.Status, Version: j.Version}, nil
		}
		return Result{}, errors.New("no pending revision after rollback")
	}
	if j.Status == "Applied" && r.Operation != "rollback" {
		return Result{Revision: j.New, Status: "Applied", Version: j.Version}, nil
	}
	if !r.ConfirmRestart {
		return Result{}, errors.New("restart requires second confirmation")
	}
	if r.Operation == "rollback" {
		e = errors.New("requested rollback")
	} else {
		e = m.Restart(ctx, t)
	}
	if e == nil {
		j.Status = "Applied"
	} else {
		backup, err := read(j.Backup)
		if err != nil || Hash(backup) != j.Old {
			return Result{}, errors.New("rollback backup unavailable; manual recovery required")
		}
		info, err := os.Stat(t.Path)
		if err != nil {
			return Result{}, err
		}
		current, err := read(t.Path)
		if err != nil || Hash(current) != j.New {
			return Result{}, ErrConflict
		}
		if err = atomic(t.Path, backup, info.Mode().Perm()); err != nil {
			return Result{}, err
		}
		j.New = j.Old
		j.Status = "RolledBack"
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = m.Restart(recoveryCtx, t)
		cancel()
		if err != nil {
			j.Status = "RecoveryFailed"
		}
	}
	if e = m.save(id, j); e != nil {
		return Result{}, e
	}
	return Result{Revision: j.New, PreviousRevision: r.Revision, Status: j.Status, Version: j.Version, ObservedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}

func rejectLinks(path string) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink path rejected")
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}
