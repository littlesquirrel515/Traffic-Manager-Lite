package coremonitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	pb "traffic-manager-lite/internal/proto/xray"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/xraymonitor"
)

func (s Service) Instance(ctx context.Context, id int64) (core.Instance, error) {
	list, e := s.Store.Instances(ctx)
	if e != nil {
		return core.Instance{}, e
	}
	for _, i := range list {
		if i.ID == id {
			return i, nil
		}
	}
	return core.Instance{}, sql.ErrNoRows
}
func (s Service) quick(ctx context.Context, i core.Instance) (Report, error) {
	if i.CoreType == "xray" {
		r := (xraymonitor.Service{Store: s.Store, Config: s.Config}).VersionEvidence(i)
		r.InstanceID = i.ID
		r.CheckedAt = storage.Stamp(time.Now())
		r.APITypes = []string{"Xray gRPC"}
		r.Connected = "Unknown"
		r.Authentication = "Unknown"
		r.Health = []Health{}
		r.Checks = []Check{}
		o := observation{report: r}
		s.bounded(ctx, func(c context.Context) {
			start := time.Now()
			conn, e := s.dial(c, i, i.APIEndpoint)
			n := 0
			if e == nil {
				defer conn.Close()
				response, err := pb.NewStatsServiceClient(conn).QueryStats(c, &pb.QueryStatsRequest{Reset_: false})
				e = err
				if e == nil {
					n = len(response.Stat)
				}
			}
			check := o.check("stats", "QueryStats", "/xray.app.stats.command.StatsService/QueryStats", "reset=false", "StatsService", start, n, e)
			if check.Status == "Available" || check.Status == "AuthenticationFailed" || check.Code == "Unimplemented" {
				o.report.Connected = "Available"
			} else if check.Status == "Unreachable" {
				o.report.Connected = "Unreachable"
			}
			if check.Status == "Available" || check.Status == "AuthenticationFailed" {
				o.report.Authentication = check.Status
			}
		})
		return o.report, nil
	}
	return s.observe(ctx, i, true, "quick_check", true)
}
func (s Service) Observe(ctx context.Context, i core.Instance, diagnostic bool, trigger string) (Report, error) {
	if i.CoreType == "xray" {
		return (xraymonitor.Service{Store: s.Store, Config: s.Config}).Observe(ctx, i, diagnostic, trigger)
	}
	return s.observe(ctx, i, diagnostic, trigger, false)
}
func (s Service) observe(ctx context.Context, i core.Instance, diagnostic bool, trigger string, quick bool) (Report, error) {
	v := s.evidence(i)
	o := observation{report: Report{InstanceID: i.ID, CoreType: i.CoreType, Version: v.Version, Build: v.Build, VersionSource: v.Source, VersionReason: v.Reason, CheckedAt: storage.Stamp(time.Now()), Connected: "Unknown", Authentication: "Unknown", ConfigSource: "未提供完整资产来源"}, states: map[string]*xraymonitor.State{}}
	if !quick {
		s.clients(i, &o)
	}
	switch i.CoreType {
	case "hysteria2":
		s.hysteria2(ctx, i, &o, quick)
	case "v2fly":
		s.v2fly(ctx, i, &o, quick)
	case "singbox":
		s.singbox(ctx, i, &o, quick)
	default:
		return o.report, fmt.Errorf("unsupported core")
	}
	for j := range o.report.Checks {
		o.report.Checks[j].ApplicableVersion = o.report.Version
	}
	// Connectivity and authentication are observations, not claims that every optional API works.
	for _, c := range o.report.Checks {
		if c.Requests == 0 || c.Group == "clients" {
			continue
		}
		if c.Status == "Available" {
			o.report.Connected = "Available"
		}
		if c.Code == "Unimplemented" {
			o.report.Connected = "Available"
		}
		if c.Status == "Unreachable" && o.report.Connected != "Available" {
			o.report.Connected = "Unreachable"
		}
		if c.Status == "AuthenticationFailed" {
			o.report.Connected = "Available"
			o.report.Authentication = "AuthenticationFailed"
		} else if c.Status == "Available" && o.report.Authentication != "AuthenticationFailed" {
			o.report.Authentication = "Available"
		}
	}
	for j := range o.records {
		o.records[j].InstanceVersion = o.report.Version
		o.records[j].ConfigRevision = o.configRevision
		if i.CoreType == "hysteria2" {
			o.records[j].UploadCounter = "tx"
			o.records[j].DownloadCounter = "rx"
			o.records[j].Mapped = false
			o.records[j].Direction = "server_to_remote_tx=client_upload;remote_to_server_rx=client_download"
		}
	}
	for j := range o.report.Checks {
		c := &o.report.Checks[j]
		c.Scope = c.Group
		c.Provider = "Config"
		if c.Group != "clients" {
			switch i.CoreType {
			case "singbox":
				c.Provider = "Native gRPC"
				if strings.HasPrefix(c.API, "Clash") {
					c.Provider = "Clash"
				}
				if strings.HasPrefix(c.API, "V2Ray") {
					c.Provider = "V2Ray Stats"
				}
			case "hysteria2":
				c.Provider = "Hysteria2 Traffic Stats"
			default:
				c.Provider = i.CoreType
			}
		}
		if c.Group == "stats" {
			c.Scope = "instance"
			if c.Provider == "V2Ray Stats" || i.CoreType != "singbox" {
				c.Scope = "user/inbound"
			}
			c.Direction = "客户端上传/下载"
			if c.API == "原生用户连接流量" {
				c.Scope = "user/connection（已观测差值，非完整总量）"
			}
		}
	}
	if !quick {
		for _, h := range []struct {
			group string
			ok    bool
			n     int
		}{{"clients", o.clientsOK, len(o.clients)}, {"stats", o.statsOK, len(o.records)}, {"online", o.onlineOK, len(o.online)}} {
			state, reason := "Healthy", "采集成功，零结果也有效"
			code := "OK"
			if !h.ok {
				state = "Unknown"
				reason = "未取得可验证结果，保留资产及历史统计"
				for _, c := range o.report.Checks {
					if c.Group == h.group && c.Status != "Available" {
						state = c.Status
						reason = c.Reason
						code = c.Code
						break
					}
				}
			}
			o.report.Health = append(o.report.Health, Health{Collector: h.group, Status: state, Count: h.n, Code: code, Summary: reason})
		}
		if !diagnostic {
			// Apply is the existing atomic traffic cursor/sample/hour/day transaction.
			if o.statsOK {
				if e := s.Store.Apply(ctx, i.ID, o.records); e != nil {
					o.statsOK = false
					for j := range o.report.Health {
						if o.report.Health[j].Collector == "stats" {
							o.report.Health[j].Status = "Error"
							o.report.Health[j].Code = "PersistenceFailed"
							o.report.Health[j].Summary = "统计持久化失败；未推进流量基线"
						}
					}
				}
			}
			if e := s.saveObservation(ctx, i, o); e != nil {
				return o.report, e
			}
		}
	}
	// Manual/quick diagnostics never create identities, assets or traffic baselines.
	if !quick && (diagnostic || s.needsReport(ctx, i, o.report)) {
		if e := s.saveReport(ctx, i, o.report, trigger); e != nil {
			return o.report, e
		}
	}
	return o.report, nil
}
func (s Service) needsReport(ctx context.Context, i core.Instance, r Report) bool {
	var raw, stamp, updated string
	e := s.Store.DB.QueryRowContext(ctx, "SELECT report_json,checked_at FROM core_diagnostics WHERE instance_id=? ORDER BY id DESC LIMIT 1", i.ID).Scan(&raw, &stamp)
	if e != nil {
		return true
	}
	s.Store.DB.QueryRowContext(ctx, "SELECT updated_at FROM instances WHERE id=?", i.ID).Scan(&updated)
	if updated > stamp {
		return true
	}
	var old Report
	json.Unmarshal([]byte(raw), &old)
	if old.Version != r.Version {
		return true
	}
	at, _ := time.Parse(time.RFC3339Nano, stamp)
	var failures int
	s.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(failures),0) FROM collector_health WHERE instance_id=?", i.ID).Scan(&failures)
	return time.Since(at) >= 24*time.Hour || (failures >= 3 && time.Since(at) >= 5*time.Minute)
}
func (s Service) saveReport(ctx context.Context, i core.Instance, r Report, trigger string) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT INTO core_diagnostics(instance_id,checked_at,trigger,report_json) VALUES(?,?,?,?)", i.ID, r.CheckedAt, trigger, string(b)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM core_diagnostics WHERE instance_id=? AND id NOT IN (SELECT id FROM core_diagnostics WHERE instance_id=? ORDER BY id DESC LIMIT 200)", i.ID, i.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO core_runtime(instance_id,version,build_info,version_source,verified_at) VALUES(?,?,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET version=excluded.version,build_info=excluded.build_info,version_source=excluded.version_source,verified_at=excluded.verified_at", i.ID, r.Version, r.Build, r.VersionSource, r.CheckedAt); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE instances SET detected_version=? WHERE id=?", r.Version, i.ID); e != nil {
		return e
	}
	return tx.Commit()
}
