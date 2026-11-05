package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"traffic-manager-lite/internal/preferred"
	"traffic-manager-lite/internal/storage"
)

const sourceMaxAge = 24 * time.Hour

type SyncConfig struct {
	Name      string   `json:"name"`
	Template  string   `json:"template_node_id"`
	Sources   []string `json:"sources"`
	Count     int      `json:"count"`
	Minutes   int      `json:"minutes"`
	Ports     []int    `json:"ports"`
	Confirmed bool     `json:"confirmed"`
}
type sourceSnapshot struct {
	LastGood int64               `json:"last_good"`
	Entries  []preferred.Address `json:"entries"`
	Status   string              `json:"status"`
}
type SyncJob struct {
	ID          string             `json:"id"`
	GroupID     string             `json:"group_id"`
	Config      SyncConfig         `json:"config"`
	Enabled     bool               `json:"enabled"`
	Revision    int                `json:"revision"`
	LastAttempt string             `json:"last_attempt"`
	LastSuccess string             `json:"last_success"`
	Status      string             `json:"status"`
	Sources     []SyncSourceStatus `json:"sources"`
	snapshots   []sourceSnapshot
}
type SyncSourceStatus struct {
	Status   string `json:"status"`
	LastGood string `json:"last_good"`
	Count    int    `json:"count"`
}
type PreferredSync struct {
	Store *storage.Store
	mu    sync.Mutex
	// fetch can be replaced by a fixture in package tests.
	fetch func(context.Context, string, string) (string, error)
}

func validateSync(d *SyncConfig) error {
	d.Name = strings.TrimSpace(d.Name)
	if !d.Confirmed || !validName(d.Name) || d.Count < 0 || d.Count > 500 || d.Minutes < 5 || d.Minutes > 1440 || len(d.Sources) < 1 || len(d.Sources) > 8 || len(d.Ports) < 1 || len(d.Ports) > 2 {
		return fmt.Errorf("请确认同步计划：来源1–8个、入口0–500个（0表示首次有效候选全部创建）、间隔5–1440分钟")
	}
	seen := map[string]bool{}
	for i, raw := range d.Sources {
		u, e := preferred.ValidateSource(strings.TrimSpace(raw))
		if e != nil || u.RawQuery != "" || u.ForceQuery {
			return fmt.Errorf("来源 %d 必须为不含查询参数的公开 HTTPS 地址", i+1)
		}
		u.Host = strings.ToLower(u.Host)
		key := u.String()
		if seen[key] {
			return fmt.Errorf("来源 %d 重复", i+1)
		}
		seen[key] = true
		d.Sources[i] = key
	}
	for _, p := range d.Ports {
		if p != 443 && p != 8443 {
			return fmt.Errorf("同步仅允许端口443或8443")
		}
	}
	return nil
}
func (s *PreferredSync) collect(ctx context.Context, d SyncConfig, old []sourceSnapshot) []sourceSnapshot {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	fetch := s.fetch
	if fetch == nil {
		fetch = preferred.Fetch
	}
	out := make([]sourceSnapshot, len(d.Sources))
	var wg sync.WaitGroup
	for i, u := range d.Sources {
		wg.Go(func() {
			previous := sourceSnapshot{}
			if i < len(old) {
				previous = old[i]
			}
			text, e := fetch(ctx, u, "feed")
			if e == nil {
				var p preferred.Parsed
				p, e = preferred.Parse(text, 443, 500)
				if e == nil && p.Truncated > 0 {
					e = fmt.Errorf("来源超过500条有效候选")
				}
				if e == nil {
					entries := filterSyncAddresses(ctx, p.Entries, d.Ports)
					if len(entries) > 0 {
						out[i] = sourceSnapshot{time.Now().Unix(), entries, "ok"}
						return
					}
				}
			}
			previous.Status = "error"
			if previous.LastGood > 0 {
				previous.Status = "cached"
			}
			out[i] = previous
		})
	}
	wg.Wait()
	return out
}
func filterSyncAddresses(ctx context.Context, entries []preferred.Address, ports []int) []preferred.Address {
	out := make([]preferred.Address, 0, len(entries))
	allowed := map[int]bool{}
	for _, p := range ports {
		allowed[p] = true
	}
	// Bound DNS work and verify all answers for each domain before publication.
	for _, a := range entries {
		if !allowed[a.Port] {
			continue
		}
		if ip, e := netip.ParseAddr(a.Address); e == nil {
			if preferred.PublicIP(ip) {
				out = append(out, a)
			}
			continue
		}
		if ctx.Err() != nil {
			break
		}
		lookup, cancel := context.WithTimeout(ctx, time.Second)
		ips, e := net.DefaultResolver.LookupNetIP(lookup, "ip", a.Address)
		cancel()
		valid := e == nil && len(ips) > 0
		for _, ip := range ips {
			if !preferred.PublicIP(ip) {
				valid = false
				break
			}
		}
		if valid {
			out = append(out, a)
		}
	}
	return out
}
func addressKey(a preferred.Address) string { return fmt.Sprintf("%s/%d", a.Address, a.Port) }
func syncCandidates(states []sourceSnapshot, now time.Time) []preferred.Address {
	out := []preferred.Address{}
	seen := map[string]bool{}
	offsets := make([]int, len(states))
	for {
		added := false
		for i, s := range states {
			if s.LastGood == 0 || now.Sub(time.Unix(s.LastGood, 0)) >= sourceMaxAge {
				continue
			}
			for offsets[i] < len(s.Entries) {
				a := s.Entries[offsets[i]]
				offsets[i]++
				k := addressKey(a)
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, a)
				added = true
				break
			}
		}
		if !added {
			return out
		}
	}
}

func syncScan(row interface{ Scan(...any) error }) (SyncJob, error) {
	var j SyncJob
	var config, state string
	e := row.Scan(&j.ID, &j.GroupID, &config, &state, &j.Enabled, &j.Revision, &j.LastAttempt, &j.LastSuccess, &j.Status)
	if e != nil {
		return j, e
	}
	if e = json.Unmarshal([]byte(config), &j.Config); e != nil {
		return j, e
	}
	if e = json.Unmarshal([]byte(state), &j.snapshots); e != nil {
		return j, e
	}
	j.Sources = []SyncSourceStatus{}
	for _, s := range j.snapshots {
		v := SyncSourceStatus{Status: s.Status, Count: len(s.Entries)}
		if s.LastGood > 0 {
			v.LastGood = storage.Stamp(time.Unix(s.LastGood, 0))
			if time.Since(time.Unix(s.LastGood, 0)) >= sourceMaxAge {
				v.Status = "expired"
			}
		}
		j.Sources = append(j.Sources, v)
	}
	return j, nil
}

const syncColumns = "id,group_id,config_json,state_json,enabled,revision,last_attempt,last_success,status"

func (s *PreferredSync) List(ctx context.Context) ([]SyncJob, error) {
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT "+syncColumns+" FROM preferred_sync_jobs ORDER BY name,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SyncJob{}
	for rows.Next() {
		j, e := syncScan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (s *PreferredSync) Create(ctx context.Context, d SyncConfig) (SyncJob, error) {
	var empty SyncJob
	if e := validateSync(&d); e != nil {
		return empty, e
	}
	template, e := ResolveDefinition(ctx, s.Store.DB, d.Template)
	if e != nil || !PreferredTemplate(template) {
		return empty, fmt.Errorf("请选择可用 TLS 节点模板")
	}
	// Synchronized slots must never be used as templates for further jobs.
	var linked int
	if e = s.Store.DB.QueryRowContext(ctx, "SELECT count(*) FROM preferred_sync_slots WHERE node_id=?", d.Template).Scan(&linked); e != nil {
		return empty, e
	}
	if linked != 0 {
		return empty, fmt.Errorf("请选择原始节点作为模板")
	}
	states := s.collect(ctx, d, nil)
	now := time.Now()
	candidates := syncCandidates(states, now)
	if len(candidates) == 0 {
		return empty, fmt.Errorf("首次同步没有有效候选，请检查来源和端口")
	}
	if d.Count == 0 {
		for i, source := range states {
			if source.Status != "ok" {
				return empty, fmt.Errorf("来源 %d 未完整读取（可能超出500条），无法创建全部候选", i+1)
			}
		}
		if len(candidates) > 500 {
			return empty, fmt.Errorf("去重后有效候选 %d 个，超过单组上限500；请拆分来源", len(candidates))
		}
		d.Count = len(candidates)
	}
	if len(candidates) < d.Count {
		return empty, fmt.Errorf("首次同步只有 %d 个有效候选，请减少入口数量或检查来源", len(candidates))
	}
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	current, e := ResolveDefinition(ctx, tx, d.Template)
	if e != nil || current.Revision != template.Revision {
		return empty, fmt.Errorf("模板已变化，请重新确认")
	}
	var total int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM preferred_sync_jobs").Scan(&total); e != nil {
		return empty, e
	}
	if total >= 20 {
		return empty, fmt.Errorf("最多创建20个同步任务")
	}
	id, group := ID(), ID()
	if _, e = tx.ExecContext(ctx, "INSERT INTO node_groups VALUES(?,?,?,1,0,?,?)", group, d.Name, "自动优选；授权固定入口，网络未验证", stamp(), stamp()); e != nil {
		return empty, e
	}
	d.Confirmed = false
	if _, e = tx.ExecContext(ctx, `INSERT INTO preferred_sync_jobs VALUES(?,?,?,?,?, ?,1,1,?,?,?,?)`, id, d.Name, d.Template, group, encode(d), encode(states), now.Add(time.Duration(d.Minutes)*time.Minute).Unix(), storage.Stamp(now), storage.Stamp(now), syncStatus(states)); e != nil {
		return empty, e
	}
	for i, a := range candidates[:d.Count] {
		c, e := createPreferredVariant(ctx, tx, template, a, clipped(fmt.Sprintf("%s-%02d", clipped(d.Name, 180), i+1), 190))
		if e != nil {
			return empty, e
		}
		n, e := ResolveDefinition(ctx, tx, c.NodeID)
		if e != nil {
			return empty, e
		}
		var profile string
		if e = tx.QueryRowContext(ctx, "SELECT profile_json FROM node_endpoints WHERE id=?", c.EndpointID).Scan(&profile); e != nil {
			return empty, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO preferred_sync_slots VALUES(?,?,?,?,?,?,?,1)", id, i, c.NodeID, c.EndpointID, c.BindingID, slotRevision(n), profile); e != nil {
			return empty, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO node_group_members VALUES(?,?,?)", group, c.NodeID, i); e != nil {
			return empty, e
		}
	}
	j, e := syncScan(tx.QueryRowContext(ctx, "SELECT "+syncColumns+" FROM preferred_sync_jobs WHERE id=?", id))
	if e != nil {
		return empty, e
	}
	return j, tx.Commit()
}
func syncStatus(states []sourceSnapshot) string {
	for _, s := range states {
		if s.Status != "ok" {
			return "degraded"
		}
	}
	return "ok"
}
func (s *PreferredSync) Enable(ctx context.Context, id string, enabled bool) error {
	res, e := s.Store.DB.ExecContext(ctx, "UPDATE preferred_sync_jobs SET enabled=?,revision=revision+1,next_run=? WHERE id=?", enabled, time.Now().Unix(), id)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n == 0 {
		return sql.ErrNoRows
	}
	return e
}

type syncSlot struct {
	node, endpoint, binding, revision, profile string
	current                                    preferred.Address
	active                                     bool
}

func chooseSlots(slots []syncSlot, candidates []preferred.Address) []preferred.Address {
	out := make([]preferred.Address, len(slots))
	available := map[string]preferred.Address{}
	used := map[string]bool{}
	for _, a := range candidates {
		available[addressKey(a)] = a
	}
	for _, active := range []bool{true, false} {
		for i, s := range slots {
			if s.active != active {
				continue
			}
			k := addressKey(s.current)
			if a, ok := available[k]; ok && !used[k] {
				out[i] = a
				used[k] = true
			}
		}
	}
	cursor := 0
	for i := range out {
		if out[i].Address != "" {
			continue
		}
		for cursor < len(candidates) && used[addressKey(candidates[cursor])] {
			cursor++
		}
		if cursor < len(candidates) {
			out[i] = candidates[cursor]
			used[addressKey(out[i])] = true
			cursor++
		}
	}
	return out
}
func (s *PreferredSync) Sync(ctx context.Context, id string) (SyncJob, error) {
	if !s.mu.TryLock() {
		return SyncJob{}, fmt.Errorf("已有同步正在执行，请稍后重试")
	}
	defer s.mu.Unlock()
	j, e := syncScan(s.Store.DB.QueryRowContext(ctx, "SELECT "+syncColumns+" FROM preferred_sync_jobs WHERE id=?", id))
	if e != nil {
		return j, e
	}
	if !j.Enabled {
		return j, fmt.Errorf("任务已暂停")
	}
	now := time.Now()
	res, e := s.Store.DB.ExecContext(ctx, "UPDATE preferred_sync_jobs SET next_run=?,last_attempt=? WHERE id=? AND revision=? AND enabled=1", now.Add(time.Duration(j.Config.Minutes)*time.Minute).Unix(), storage.Stamp(now), id, j.Revision)
	if e != nil {
		return j, e
	}
	changed, e := res.RowsAffected()
	if e != nil {
		return j, e
	}
	if changed != 1 {
		return j, fmt.Errorf("任务状态已变化")
	}
	states := s.collect(ctx, j.Config, j.snapshots)
	if ctx.Err() != nil {
		return j, ctx.Err()
	}
	if e = s.apply(ctx, j, states); e != nil {
		// Keep the previously committed slots intact on any write or validation failure.
		_, _ = s.Store.DB.ExecContext(ctx, "UPDATE preferred_sync_jobs SET state_json=?,status='blocked' WHERE id=? AND revision=? AND enabled=1", encode(states), id, j.Revision)
		return j, e
	}
	return syncScan(s.Store.DB.QueryRowContext(ctx, "SELECT "+syncColumns+" FROM preferred_sync_jobs WHERE id=?", id))
}
func (s *PreferredSync) apply(ctx context.Context, j SyncJob, states []sourceSnapshot) error {
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var revision int
	var enabled bool
	if e = tx.QueryRowContext(ctx, "SELECT revision,enabled FROM preferred_sync_jobs WHERE id=?", j.ID).Scan(&revision, &enabled); e != nil {
		return e
	}
	if !enabled || revision != j.Revision {
		return fmt.Errorf("任务已暂停或已变化")
	}
	now := time.Now()
	candidates := syncCandidates(states, now)
	status := syncStatus(states)
	success := j.LastSuccess
	fresh := false
	for _, st := range states {
		fresh = fresh || st.Status == "ok"
	}
	if fresh && len(candidates) > 0 {
		rows, e := tx.QueryContext(ctx, `SELECT node_id,endpoint_id,binding_id,expected_revision,expected_profile,active FROM preferred_sync_slots WHERE job_id=? ORDER BY position`, j.ID)
		if e != nil {
			return e
		}
		slots := []syncSlot{}
		for rows.Next() {
			var slot syncSlot
			if e = rows.Scan(&slot.node, &slot.endpoint, &slot.binding, &slot.revision, &slot.profile, &slot.active); e != nil {
				rows.Close()
				return e
			}
			slots = append(slots, slot)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(slots) != j.Config.Count {
			return fmt.Errorf("同步入口数量已变化")
		}
		for i := range slots {
			v := &slots[i]
			n, e := ResolveDefinition(ctx, tx, v.node)
			if e != nil || !PreferredTemplate(n) || *n.EndpointID != v.endpoint || *n.BindingID != v.binding || slotRevision(n) != v.revision {
				return fmt.Errorf("同步入口已被修改、停用或需要复核；保留原发布")
			}
			var raw string
			var refs, variants int
			if e = tx.QueryRowContext(ctx, `SELECT profile_json,(SELECT count(*) FROM inbound_endpoint_bindings WHERE endpoint_id=?),(SELECT count(*) FROM node_profiles WHERE endpoint_id=?) FROM node_endpoints WHERE id=?`, v.endpoint, v.endpoint, v.endpoint).Scan(&raw, &refs, &variants); e != nil {
				return e
			}
			if raw != v.profile || refs != 1 || variants != 1 {
				return fmt.Errorf("同步入口已修改或共享；保留原发布")
			}
			v.current = preferred.Address{Address: n.Profile.Address, Port: n.Profile.Port}
		}
		next := chooseSlots(slots, candidates)
		for i, v := range slots {
			a := next[i]
			active := a.Address != ""
			if active && addressKey(a) != addressKey(v.current) {
				var profile map[string]any
				if e = json.Unmarshal([]byte(v.profile), &profile); e != nil {
					return e
				}
				profile["address"] = a.Address
				profile["port"] = a.Port
				v.profile = encode(profile)
				if _, e = tx.ExecContext(ctx, "UPDATE node_endpoints SET profile_json=?,updated_at=? WHERE id=?", v.profile, stamp(), v.endpoint); e != nil {
					return e
				}
				n, e := ResolveDefinition(ctx, tx, v.node)
				if e != nil || !PreferredTemplate(n) || n.Profile.Address != a.Address || n.Profile.Port != a.Port {
					return fmt.Errorf("同步入口参数验证失败")
				}
				v.revision = slotRevision(n)
			}
			if _, e = tx.ExecContext(ctx, "UPDATE preferred_sync_slots SET expected_revision=?,expected_profile=?,active=? WHERE node_id=?", v.revision, v.profile, active, v.node); e != nil {
				return e
			}
		}
		success = storage.Stamp(now)
		if len(candidates) < len(slots) {
			status = "degraded"
		}
	} else {
		status = "stale"
	}
	if _, e = tx.ExecContext(ctx, "UPDATE preferred_sync_jobs SET state_json=?,last_success=?,status=? WHERE id=?", encode(states), success, status, j.ID); e != nil {
		return e
	}
	return tx.Commit()
}

// SyncSlotAvailable affects delivery only. The slot remains assignable even
// when a source temporarily has too few candidates.
func SyncSlotAvailable(ctx context.Context, q Query, node string) (bool, error) {
	var active bool
	e := q.QueryRowContext(ctx, "SELECT active FROM preferred_sync_slots WHERE node_id=?", node).Scan(&active)
	if e == sql.ErrNoRows {
		return true, nil
	}
	return active, e
}
func (s *PreferredSync) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var id string
		if e := s.Store.DB.QueryRowContext(ctx, "SELECT id FROM preferred_sync_jobs WHERE enabled=1 AND next_run<=? ORDER BY next_run,id LIMIT 1", time.Now().Unix()).Scan(&id); e == nil {
			_, _ = s.Sync(ctx, id)
		}
	}
}

// References change as groups and advanced subscriptions are edited; they are
// not an edit to the slot's connection configuration.
func slotRevision(n Node) string {
	n.References = 0
	n.Revision = ""
	return fmt.Sprintf("%x", sha256.Sum256([]byte(encode(n))))
}

type SyncSourceEdit struct {
	Sources   []string `json:"sources"`
	Minutes   int      `json:"minutes"`
	Revision  int      `json:"revision"`
	Count     *int     `json:"count"`
	Confirmed bool     `json:"confirmed"`
}

func (s *PreferredSync) EditSources(ctx context.Context, id string, d SyncSourceEdit) error {
	if !s.mu.TryLock() {
		return fmt.Errorf("已有同步正在执行，请稍后重试")
	}
	defer s.mu.Unlock()
	j, e := syncScan(s.Store.DB.QueryRowContext(ctx, "SELECT "+syncColumns+" FROM preferred_sync_jobs WHERE id=?", id))
	if e != nil {
		return e
	}
	if j.Revision != d.Revision {
		return fmt.Errorf("任务已变化，请重新打开编辑")
	}
	config := j.Config
	config.Sources = d.Sources
	config.Minutes = d.Minutes
	config.Confirmed = d.Confirmed
	desired := config.Count
	if d.Count != nil {
		desired = *d.Count
	}
	config.Count = desired
	if e = validateSync(&config); e != nil {
		return e
	}
	var states []sourceSnapshot
	var candidates []preferred.Address
	if desired == 0 || desired > j.Config.Count {
		if !j.Enabled {
			return fmt.Errorf("同步组已暂停，请恢复后再扩容")
		}
		states = s.collect(ctx, config, nil)
		for i, v := range states {
			if v.Status != "ok" {
				return fmt.Errorf("来源 %d 未完整读取，无法扩容", i+1)
			}
		}
		candidates = syncCandidates(states, time.Now())
		if desired == 0 {
			desired = len(candidates)
		}
		if desired > 500 {
			return fmt.Errorf("有效候选超过500，请拆分来源")
		}
		if desired < j.Config.Count {
			return fmt.Errorf("当前有效候选不足原有入口数，不支持缩容")
		}
		if len(candidates) < desired {
			return fmt.Errorf("只有 %d 个有效候选，无法扩容至 %d", len(candidates), desired)
		}
	}
	if desired < j.Config.Count {
		return fmt.Errorf("暂不支持缩容，以免删除已授权节点")
	}
	config.Count = desired
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	current, e := syncScan(tx.QueryRowContext(ctx, "SELECT "+syncColumns+" FROM preferred_sync_jobs WHERE id=?", id))
	if e != nil {
		return e
	}
	if current.Revision != j.Revision {
		return fmt.Errorf("任务已变化，请重新打开编辑")
	}
	snapshots := make([]sourceSnapshot, len(config.Sources))
	for i, u := range config.Sources {
		for k, old := range j.Config.Sources {
			if old == u && k < len(j.snapshots) {
				snapshots[i] = j.snapshots[k]
				break
			}
		}
	}
	if desired > j.Config.Count {
		template, err := ResolveDefinition(ctx, tx, j.Config.Template)
		if err != nil || !PreferredTemplate(template) {
			return fmt.Errorf("原节点模板不可用，请检查模板")
		}
		rows, err := tx.QueryContext(ctx, "SELECT node_id,endpoint_id,binding_id,expected_revision,expected_profile FROM preferred_sync_slots WHERE job_id=? ORDER BY position", id)
		if err != nil {
			return err
		}
		slots := []syncSlot{}
		for rows.Next() {
			var v syncSlot
			if err = rows.Scan(&v.node, &v.endpoint, &v.binding, &v.revision, &v.profile); err != nil {
				rows.Close()
				return err
			}
			slots = append(slots, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(slots) != j.Config.Count {
			return fmt.Errorf("同步入口数量已变化")
		}
		used := map[string]bool{}
		for _, v := range slots {
			n, err := ResolveDefinition(ctx, tx, v.node)
			if err != nil || !PreferredTemplate(n) || n.EndpointID == nil || n.BindingID == nil || *n.EndpointID != v.endpoint || *n.BindingID != v.binding || slotRevision(n) != v.revision {
				return fmt.Errorf("同步入口已被修改或停用，请先处理")
			}
			var profile string
			var refs, variants int
			if err = tx.QueryRowContext(ctx, `SELECT profile_json,(SELECT count(*) FROM inbound_endpoint_bindings WHERE endpoint_id=?),(SELECT count(*) FROM node_profiles WHERE endpoint_id=?) FROM node_endpoints WHERE id=?`, v.endpoint, v.endpoint, v.endpoint).Scan(&profile, &refs, &variants); err != nil {
				return err
			}
			if profile != v.profile || refs != 1 || variants != 1 {
				return fmt.Errorf("同步入口已修改或共享，请先处理")
			}
			used[addressKey(preferred.Address{Address: n.Profile.Address, Port: n.Profile.Port})] = true
		}
		pos := j.Config.Count
		for _, a := range candidates {
			if pos >= desired {
				break
			}
			if used[addressKey(a)] {
				continue
			}
			created, err := createPreferredVariant(ctx, tx, template, a, clipped(fmt.Sprintf("%s-%02d", clipped(config.Name, 180), pos+1), 190))
			if err != nil {
				return err
			}
			n, err := ResolveDefinition(ctx, tx, created.NodeID)
			if err != nil {
				return err
			}
			var profile string
			if err = tx.QueryRowContext(ctx, "SELECT profile_json FROM node_endpoints WHERE id=?", created.EndpointID).Scan(&profile); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO preferred_sync_slots VALUES(?,?,?,?,?,?,?,1)", id, pos, created.NodeID, created.EndpointID, created.BindingID, slotRevision(n), profile); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO node_group_members VALUES(?,?,?)", j.GroupID, created.NodeID, pos); err != nil {
				return err
			}
			used[addressKey(a)] = true
			pos++
		}
		if pos < desired {
			return fmt.Errorf("可用新候选不足，无法安全扩容到 %d", desired)
		}
		snapshots = states
	}
	config.Confirmed = false
	if _, e = tx.ExecContext(ctx, "UPDATE preferred_sync_jobs SET config_json=?,state_json=?,revision=revision+1,next_run=?,status='pending' WHERE id=?", encode(config), encode(snapshots), time.Now().Unix(), id); e != nil {
		return e
	}
	return tx.Commit()
}
