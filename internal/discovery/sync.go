package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/inboundconfig"
	"traffic-manager-lite/internal/storage"
)

// ReadSource accepts only registered files/directories below the readonly root.
// A merged source is configuration evidence, never evidence of the running core.
func ReadSource(root, path, kind string) (string, []byte, error) {
	safe, e := SafePath(root, path)
	if e != nil {
		return "", nil, e
	}
	info, e := os.Stat(safe)
	if e != nil {
		return "", nil, fmt.Errorf("配置来源不可读")
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("配置来源必须为普通文件")
		}
		if info.Size() > 4<<20 {
			return "", nil, fmt.Errorf("配置超过 4 MiB")
		}
		b, e := os.ReadFile(safe)
		if e != nil {
			return "", nil, fmt.Errorf("配置读取失败")
		}
		return safe, b, nil
	}
	if kind == "hysteria2" {
		return "", nil, fmt.Errorf("Hysteria2 仅支持已登记的单个 YAML 配置")
	}
	entries, e := os.ReadDir(safe)
	if e != nil {
		return "", nil, fmt.Errorf("配置目录不可读")
	}
	files := []string{}
	for _, v := range entries {
		if !v.IsDir() && strings.HasSuffix(strings.ToLower(v.Name()), ".json") {
			files = append(files, filepath.Join(safe, v.Name()))
		}
	}
	sort.Strings(files)
	if len(files) == 0 || len(files) > 32 {
		return "", nil, fmt.Errorf("配置目录需包含 1–32 个 JSON 文件")
	}
	merged := map[string]any{}
	inbounds := []any{}
	size := 0
	for _, file := range files {
		resolved, e := SafePath(root, file)
		if e != nil {
			return "", nil, e
		}
		entry, e := os.Stat(resolved)
		if e != nil || !entry.Mode().IsRegular() {
			return "", nil, fmt.Errorf("目录只接受普通 JSON 文件，不递归加载目录链接")
		}
		_, b, e := ReadSource(root, file, kind)
		if e != nil {
			return "", nil, e
		}
		size += len(b)
		if size > 4<<20 {
			return "", nil, fmt.Errorf("合并配置超过 4 MiB")
		}
		var config map[string]any
		if e = json.Unmarshal(b, &config); e != nil {
			return "", nil, fmt.Errorf("目录配置 JSON 无效")
		}
		for k, v := range config {
			if k == "inbounds" {
				ins, ok := v.([]any)
				if !ok {
					return "", nil, fmt.Errorf("inbounds 结构无效")
				}
				inbounds = append(inbounds, ins...)
			} else {
				merged[k] = v
			}
		}
	}
	merged["inbounds"] = inbounds
	b, e := json.Marshal(merged)
	return safe, b, e
}

func markMissing(ctx context.Context, s *storage.Store, inst core.Instance, root string) {
	parent, e := SafePath(root, filepath.Dir(inst.ConfigPath))
	if e != nil {
		return
	}
	target := filepath.Join(parent, filepath.Base(inst.ConfigPath))
	if _, e = os.Stat(target); !os.IsNotExist(e) {
		return
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return
	}
	defer tx.Rollback()
	for _, q := range []string{"UPDATE catalog_inbounds SET present=0 WHERE instance_id=?", "UPDATE credential_material SET status='source_missing' WHERE instance_id=?"} {
		if _, e = tx.ExecContext(ctx, q, inst.ID); e != nil {
			return
		}
	}
	_ = tx.Commit()
}

type SyncPreview struct {
	InstanceID int64          `json:"instance_id"`
	Changed    bool           `json:"changed"`
	Hash       string         `json:"config_hash"`
	Clients    int            `json:"configured_clients"`
	Error      string         `json:"error,omitempty"`
	Runtime    string         `json:"runtime_state"`
	Changes    []SourceChange `json:"changes"`
}
type SourceChange struct {
	Tag    string   `json:"tag"`
	Action string   `json:"action"`
	Fields []string `json:"fields,omitempty"`
}

func PreviewSync(ctx context.Context, s *storage.Store, inst core.Instance, root string) SyncPreview {
	p := SyncPreview{InstanceID: inst.ID, Runtime: "unknown"}
	_, b, e := ReadSource(root, inst.ConfigPath, inst.CoreType)
	if e != nil {
		p.Error = e.Error()
		return p
	}
	h := sha256.Sum256(b)
	p.Hash = hex.EncodeToString(h[:])
	nodes, e := Parse(inst.CoreType, b)
	if e != nil {
		if inst.CoreType != "hysteria2" || !strings.Contains(e.Error(), "HTTP/command") {
			p.Error = e.Error()
			return p
		}
	}
	p.Clients = len(nodes)
	var hash string
	_ = s.DB.QueryRowContext(ctx, "SELECT config_hash FROM catalog_inbounds WHERE instance_id=? LIMIT 1", inst.ID).Scan(&hash)
	p.Changed = hash != p.Hash
	sources, e := inboundconfig.Sources(inst.CoreType, b)
	if e != nil {
		p.Error = e.Error()
		return p
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT tag,profile_json FROM catalog_inbounds WHERE instance_id=? AND present=1", inst.ID)
	if e != nil {
		p.Error = "来源查询失败"
		return p
	}
	old := map[string]string{}
	for rows.Next() {
		var tag, profile string
		if e = rows.Scan(&tag, &profile); e != nil {
			rows.Close()
			p.Error = "来源查询失败"
			return p
		}
		old[tag] = profile
	}
	rows.Close()
	p.Changes = []SourceChange{}
	for _, source := range sources {
		raw, exists := old[source.Tag]
		if !exists {
			p.Changes = append(p.Changes, SourceChange{Tag: source.Tag, Action: "added"})
			continue
		}
		delete(old, source.Tag)
		var a core.NodeProfile
		if json.Unmarshal([]byte(raw), &a) != nil {
			p.Error = "来源配置无效"
			return p
		}
		ab, _ := json.Marshal(a)
		bb, _ := json.Marshal(source.Profile)
		var before, after map[string]any
		json.Unmarshal(ab, &before)
		json.Unmarshal(bb, &after)
		fields := []string{}
		keys := map[string]bool{}
		for k := range before {
			keys[k] = true
		}
		for k := range after {
			keys[k] = true
		}
		for k := range keys {
			v := after[k]
			x, _ := json.Marshal(v)
			y, _ := json.Marshal(before[k])
			if string(x) != string(y) {
				fields = append(fields, k)
			}
		}
		sort.Strings(fields)
		if len(fields) > 0 {
			p.Changes = append(p.Changes, SourceChange{Tag: source.Tag, Action: "changed", Fields: fields})
		}
	}
	for tag := range old {
		p.Changes = append(p.Changes, SourceChange{Tag: tag, Action: "missing"})
	}
	sort.Slice(p.Changes, func(i, j int) bool { return p.Changes[i].Tag < p.Changes[j].Tag })
	return p
}

// RunSync reacts to persisted settings without restarting TML. It never writes
// proxy configuration or invokes a command; failure leaves the previous snapshot.
func RunSync(ctx context.Context, s *storage.Store, root string) {
	next := time.Time{}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var settings struct {
			SyncInterval  int `json:"sync_interval_seconds"`
			RetentionDays int `json:"log_retention_days"`
		}
		var raw string
		e := s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='subscription_catalog'").Scan(&raw)
		if e != nil || json.Unmarshal([]byte(raw), &settings) != nil || settings.SyncInterval < 30 {
			continue
		}
		if time.Now().Before(next) {
			continue
		}
		next = time.Now().Add(time.Duration(settings.SyncInterval) * time.Second)
		instances, e := s.Instances(ctx)
		if e != nil {
			continue
		}
		for _, i := range instances {
			if !i.Enabled || !i.ServerEnabled || i.ConfigPath == "" {
				continue
			}
			p := PreviewSync(ctx, s, i, root)
			if p.Error == "" && !p.Changed {
				continue
			}
			syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, e = Scan(syncCtx, s, i, root)
			if e != nil {
				_, _ = s.DB.ExecContext(syncCtx, "INSERT INTO catalog_sync_runs(instance_id,status,summary_json,created_at) VALUES(?,'failed',?,?)", i.ID, `{"error":"config_unavailable_or_invalid","previous_snapshot_retained":true}`, storage.Stamp(time.Now()))
			}
			cancel()
		}
		_, _ = s.DB.ExecContext(ctx, "DELETE FROM subscription_access_logs WHERE accessed_at<?", storage.Stamp(time.Now().AddDate(0, 0, -settings.RetentionDays)))
		_, _ = s.DB.ExecContext(ctx, "DELETE FROM catalog_sync_runs WHERE id NOT IN (SELECT id FROM catalog_sync_runs ORDER BY id DESC LIMIT 200)")
	}
}
