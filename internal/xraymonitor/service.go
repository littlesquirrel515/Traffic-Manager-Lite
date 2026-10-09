package xraymonitor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"os"
	"time"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
)

type Service struct {
	Store  *storage.Store
	Config config.Config
}

func (s Service) Observe(ctx context.Context, inst core.Instance, diagnostic bool, trigger string) (Report, error) {
	o := observation{report: Report{InstanceID: inst.ID, CheckedAt: storage.Stamp(time.Now()), Version: "Unknown", Build: "Unknown", VersionSource: "unavailable", ConfigSource: "runtime_api; config_file 未提供", Connected: "Unknown"}, completeTags: map[string]bool{}, listedTags: map[string]bool{}, states: map[string]*State{}}
	key, e := s.fingerprintKey(ctx)
	if e != nil {
		return o.report, e
	}
	o.fingerprintKey = key
	s.configAssets(inst, &o)
	s.versionEvidence(inst, &o.report)
	p := (security.Policy{Allowed: s.Config.AllowedTargets}).ForEndpoints(inst.APIEndpoint)
	var conn *grpc.ClientConn
	e = security.ValidateEndpoint(inst.APIEndpoint, false)
	if e == nil {
		conn, e = grpc.NewClient("passthrough:///"+inst.APIEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(p.Dial))
	}
	if e != nil {
		return o.report, e
	}
	defer conn.Close()
	// Each collector has its own deadline. One failed collector cannot cancel another.
	run := func(f func(context.Context)) {
		c, cancel := context.WithTimeout(ctx, s.Config.Timeout)
		defer cancel()
		f(c)
	}
	var targetError error
	blocked := func(group, api string) {
		o.report.Health = append(o.report.Health, Health{Collector: group, Status: "Error", Code: "TargetPolicy", Summary: targetError.Error()})
		o.report.Checks = append(o.report.Checks, Check{Group: group, API: api, Method: "未调用：地址策略或 DNS 验证失败", Status: "Error", Code: "TargetPolicy", Reason: targetError.Error(), Advice: "核对实例地址、Docker DNS 和部署访问策略", CheckedAt: o.report.CheckedAt})
	}
	run(func(c context.Context) {
		if e := p.Check(c, security.TargetAddress(inst.APIEndpoint)); e != nil {
			targetError = e
			blocked("clients", "ListInbounds")
			return
		}
		o.clientsRPC(c, conn)
	})
	if targetError != nil {
		blocked("stats", "QueryStats")
	} else {
		run(func(c context.Context) { o.statsRPC(c, conn, inst) })
	}
	// Saved assets retain the last known inventory if runtime enumeration failed.
	saved, _ := s.Store.Rows(ctx, "SELECT DISTINCT email FROM xray_clients WHERE instance_id=? AND present=1 AND email<>''", inst.ID)
	for _, row := range saved {
		email := row["email"].(string)
		if o.states[email] == nil {
			o.states[email] = &State{Email: email, Stats: "unknown", Online: "unknown"}
		}
	}
	statsOK := false
	for _, h := range o.report.Health {
		if h.Collector == "stats" && h.Status == "Healthy" {
			statsOK = true
		}
	}
	for _, asset := range append(append([]Client{}, o.clients...), o.configClients...) {
		if asset.Email != "" && o.states[asset.Email] == nil {
			o.states[asset.Email] = &State{Email: asset.Email, Stats: "unknown", Online: "unknown"}
		}
	}
	for _, state := range o.states {
		if statsOK && state.Stats == "unknown" {
			state.Stats = "not_generated"
		}
	}
	if targetError != nil {
		blocked("online", "GetAllOnlineUsers")
	} else {
		run(func(c context.Context) { o.onlineRPC(c, conn) })
	}
	for _, candidate := range []struct{ group, api string }{{"clients", "ListInbounds"}, {"clients", "GetInboundUsers"}, {"clients", "GetInboundUsersCount"}, {"stats", "QueryStats"}, {"stats", "GetStats"}, {"online", "GetAllOnlineUsers"}, {"online", "GetStatsOnline"}, {"online", "GetStatsOnlineIpList"}} {
		found := false
		for _, check := range o.report.Checks {
			if check.API == candidate.api {
				found = true
				break
			}
		}
		if !found {
			o.report.Checks = append(o.report.Checks, Check{Group: candidate.group, API: candidate.api, Status: "Unknown", Method: "未调用：缺少前置枚举/可验证用户或前置请求失败", Code: "NotChecked", Reason: "证据不足，不推断可用或离线", Advice: "先解决前置 API/用户枚举问题", CheckedAt: o.report.CheckedAt})
		}
	}
	usersStatsStatus := "Unknown"
	if o.report.Version == "26.3.27" {
		usersStatsStatus = "Unsupported"
	}
	o.report.Checks = append(o.report.Checks, Check{Group: "stats", API: "GetUsersStats", Status: usersStatsStatus, Method: "未调用（已核实正式版 schema 不存在此 RPC；其他版本/构建未知）", Params: "无", Code: "NotInOfficialSchema", Reason: "核实 v26.3.27 官方 StatsService 后未发现此方法，使用 QueryStats/GetStats", Required: "官方提供对应方法才可启用", Advice: "使用 QueryStats，reset=false", CheckedAt: o.report.CheckedAt})
	for _, c := range o.report.Checks {
		if c.Status == "Available" {
			o.report.Connected = "Available"
			break
		}
	}
	if o.report.Connected != "Available" {
		o.report.Connected = "Error"
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if !diagnostic {
		if statsOK {
			for _, record := range o.records {
				if record.Scope != "user" {
					continue
				}
				var up, down int64
				if e := s.Store.DB.QueryRowContext(persist, "SELECT c.raw_upload,c.raw_download FROM traffic_cursors c JOIN identities i ON i.id=c.identity_id WHERE i.instance_id=? AND i.core_user_key=? AND i.scope='user'", inst.ID, record.UserKey).Scan(&up, &down); e == nil && (record.UploadBytes < up || record.DownloadBytes < down) {
					o.states[record.UserKey].Stats = "reset_baseline"
				}
				if record.BootEstimate != nil {
					var oldBoot string
					if s.Store.DB.QueryRowContext(persist, "SELECT boot_estimate FROM instance_epochs WHERE instance_id=?", inst.ID).Scan(&oldBoot) == nil {
						old, _ := time.Parse(time.RFC3339Nano, oldBoot)
						drift := record.BootEstimate.Sub(old)
						if drift > 5*time.Second || drift < -5*time.Second {
							o.states[record.UserKey].Stats = "reset_baseline"
						}
					}
				}
			}
			if e := s.Store.Apply(persist, inst.ID, o.records); e != nil {
				for j := range o.report.Health {
					if o.report.Health[j].Collector == "stats" {
						o.report.Health[j].Status = "Error"
						o.report.Health[j].Summary = "统计事务写入失败"
					}
				}
			}
		}
		if e := s.saveObservation(persist, inst, o); e != nil {
			return o.report, e
		}
	}
	// Diagnostic history never creates identities, traffic cursors or online snapshots.
	reason := s.diagnosisTrigger(persist, inst, o.report)
	if diagnostic || reason != "" {
		if !diagnostic {
			trigger = reason
		}
		if e := s.saveReport(persist, inst, o.report, trigger); e != nil {
			return o.report, e
		}
	}
	return o.report, nil
}

func (s Service) configAssets(inst core.Instance, o *observation) {
	if inst.ConfigPath == "" {
		return
	}
	path, e := discovery.SafePath(s.Config.ConfigRoot, inst.ConfigPath)
	if e != nil {
		o.report.ConfigSource = "runtime_api; config_file 不可读取"
		return
	}
	info, e := os.Stat(path)
	if e != nil || info.Size() > 4<<20 {
		return
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	var file struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients  []json.RawMessage `json:"clients"`
				Accounts []json.RawMessage `json:"accounts"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if e := json.Unmarshal(b, &file); e != nil {
		o.report.ConfigSource = "runtime_api; config_file 解析失败"
		return
	}
	o.configOK = true
	o.report.ConfigSource = "runtime_api 与只读 config_file 分别保存；文件不保证等于已加载配置"
	for _, in := range file.Inbounds {
		for _, raw := range append(in.Settings.Clients, in.Settings.Accounts...) {
			if len(o.configClients) >= 10000 {
				o.configOK = false
				o.configClients = nil
				o.report.ConfigSource = "runtime_api; config_file 资产超过上限，保留旧文件资产"
				return
			}
			var user struct {
				Email    string `json:"email"`
				Level    uint32 `json:"level"`
				ID       string `json:"id"`
				Password string `json:"password"`
				Flow     string `json:"flow"`
			}
			if json.Unmarshal(raw, &user) != nil {
				o.configOK = false
				return
			}
			key := "email:" + user.Email
			if user.Email == "" {
				key = fmt.Sprintf("anonymous:%x", sha256.Sum256(raw))
			}
			credential := user.Password
			if in.Protocol == "vless" || in.Protocol == "vmess" {
				credential = user.ID
			}
			o.configClients = append(o.configClients, Client{Inbound: in.Tag, Key: key, Email: user.Email, Protocol: in.Protocol, Level: user.Level, Source: "config_file", Fingerprint: fingerprint(o.fingerprintKey, in.Protocol, credential, user.Flow)})
		}
	}
}

func (s Service) diagnosisTrigger(ctx context.Context, inst core.Instance, r Report) string {
	var previous, stamp string
	e := s.Store.DB.QueryRowContext(ctx, "SELECT report_json,checked_at FROM xray_diagnostics WHERE instance_id=? ORDER BY id DESC LIMIT 1", inst.ID).Scan(&previous, &stamp)
	if e != nil {
		return "first_observation"
	}
	var updated string
	s.Store.DB.QueryRowContext(ctx, "SELECT updated_at FROM instances WHERE id=?", inst.ID).Scan(&updated)
	if updated > stamp {
		return "instance_updated"
	}
	var old Report
	json.Unmarshal([]byte(previous), &old)
	if old.Version != r.Version {
		return "version_changed"
	}
	t, _ := time.Parse(time.RFC3339Nano, stamp)
	if time.Since(t) < 5*time.Minute {
		return ""
	}
	var failures int
	s.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(failures),0) FROM collector_health WHERE instance_id=?", inst.ID).Scan(&failures)
	if failures >= 3 {
		return "consecutive_failure"
	}
	// Refresh capabilities periodically to detect upgrades even when version is Unknown.
	if time.Since(t) >= 24*time.Hour {
		return "periodic"
	}
	return ""
}
func (s Service) saveReport(ctx context.Context, inst core.Instance, r Report, trigger string) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT INTO xray_diagnostics(instance_id,checked_at,trigger,report_json) VALUES(?,?,?,?)", inst.ID, r.CheckedAt, trigger, string(b)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM xray_diagnostics WHERE instance_id=? AND id NOT IN (SELECT id FROM xray_diagnostics WHERE instance_id=? ORDER BY id DESC LIMIT 200)", inst.ID, inst.ID); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO xray_runtime(instance_id,version,build_info,version_source,verified_at) VALUES(?,?,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET version=excluded.version,build_info=excluded.build_info,version_source=excluded.version_source,verified_at=excluded.verified_at", inst.ID, r.Version, r.Build, r.VersionSource, r.CheckedAt)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (s Service) Instance(ctx context.Context, id int64) (core.Instance, error) {
	list, e := s.Store.Instances(ctx)
	if e != nil {
		return core.Instance{}, e
	}
	for _, i := range list {
		if i.ID == id && i.CoreType == "xray" {
			return i, nil
		}
	}
	return core.Instance{}, fmt.Errorf("Xray instance not found")
}
