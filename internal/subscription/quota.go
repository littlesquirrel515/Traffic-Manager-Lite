package subscription

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/storage"
)

const DefaultQuota int64 = 10 * 1024 * 1024 * 1024

func CheckQuotaAccess(u Usage) error {
	if u.Status == "expired" || u.Status == "quota_exhausted" || u.Status == "disabled" {
		return errors.New(u.Status)
	}
	if u.Enabled && u.Policy == "require_complete" && u.Completeness != "complete" {
		return errors.New("usage_unavailable")
	}
	return nil
}

var ErrQuotaConflict = errors.New("配额修订已改变，请重新读取")

type Quota struct {
	SubscriberID  string  `json:"subscriber_id"`
	Limit         int64   `json:"traffic_limit_bytes"`
	ExpiresAt     *string `json:"expires_at"`
	ActivatedAt   *string `json:"activated_at"`
	Enabled       bool    `json:"quota_enabled"`
	ExpiryEnabled bool    `json:"expiry_enabled"`
	Revision      int64   `json:"quota_revision"`
	Initialized   bool    `json:"quota_initialized"`
	ResetAt       *string `json:"quota_reset_at"`
	Policy        string  `json:"quota_policy"`
}
type Usage struct {
	Quota
	Upload       int64         `json:"traffic_used_upload_bytes"`
	Download     int64         `json:"traffic_used_download_bytes"`
	Used         int64         `json:"traffic_used_total_bytes"`
	Remaining    *int64        `json:"traffic_remaining_bytes"`
	Completeness string        `json:"completeness"`
	Status       string        `json:"status"`
	Sources      []UsageSource `json:"sources"`
}
type UsageSource struct {
	Instance         int64  `json:"instance_id"`
	Inbound          string `json:"inbound_id"`
	Client           string `json:"client_ref"`
	Upload           int64  `json:"upload_bytes"`
	Download         int64  `json:"download_bytes"`
	Completeness     string `json:"completeness"`
	Coverage         string `json:"coverage"`
	BaselineUpload   int64  `json:"-"`
	BaselineDownload int64  `json:"-"`
	Known            bool   `json:"-"`
	DesiredActive    bool   `json:"-"`
	Persisted        bool   `json:"-"`
	Active           bool   `json:"-"`
	FrozenUpload     *int64 `json:"-"`
	FrozenDownload   *int64 `json:"-"`
	FrozenState      string `json:"-"`
}

// Clamp month ends: October 31 + three months is January 31; November 30 +
// three months is the last day of February. The persisted instant is UTC.
func ThreeMonths(t time.Time) time.Time {
	t = t.UTC()
	first := time.Date(t.Year(), t.Month()+3, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := t.Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}
func ReadQuota(ctx context.Context, q catalog.Query, id string) (Quota, error) {
	var v Quota
	v.SubscriberID = id
	e := q.QueryRowContext(ctx, `SELECT traffic_limit_bytes,expires_at,activated_at,quota_enabled,expiry_enabled,quota_revision,quota_initialized,quota_reset_at,quota_policy FROM subscribers WHERE id=?`, id).Scan(&v.Limit, &v.ExpiresAt, &v.ActivatedAt, &v.Enabled, &v.ExpiryEnabled, &v.Revision, &v.Initialized, &v.ResetAt, &v.Policy)
	return v, e
}
func sourceKey(v UsageSource) string {
	return fmt.Sprintf("%d\x00%s\x00%s", v.Instance, v.Inbound, v.Client)
}
func sources(ctx context.Context, q catalog.Query, id string) ([]UsageSource, error) {
	rows, e := q.QueryContext(ctx, `SELECT c.instance_id,CASE WHEN x.core_type IN ('xray','v2fly','hysteria2') THEN '' ELSE COALESCE(b.inbound_id,'') END,c.core_client_ref FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id JOIN instances x ON x.id=c.instance_id WHERE b.subscriber_id=? AND b.enabled=1 AND b.billable=1`, id)
	if e != nil {
		return nil, e
	}
	m := map[string]UsageSource{}
	for rows.Next() {
		var v UsageSource
		if e = rows.Scan(&v.Instance, &v.Inbound, &v.Client); e != nil {
			rows.Close()
			return nil, e
		}
		v.DesiredActive = true
		m[sourceKey(v)] = v
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = q.QueryContext(ctx, `SELECT instance_id,inbound_id,client_ref,baseline_upload,baseline_download,baseline_known,active,frozen_upload,frozen_download,frozen_state FROM subscriber_usage_sources WHERE subscriber_id=?`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var v UsageSource
		if e = rows.Scan(&v.Instance, &v.Inbound, &v.Client, &v.BaselineUpload, &v.BaselineDownload, &v.Known, &v.Active, &v.FrozenUpload, &v.FrozenDownload, &v.FrozenState); e != nil {
			rows.Close()
			return nil, e
		}
		v.Persisted = true
		v.DesiredActive = m[sourceKey(v)].DesiredActive
		m[sourceKey(v)] = v
	}
	e = rows.Err()
	rows.Close()
	out := []UsageSource{}
	for _, v := range m {
		out = append(out, v)
	}
	return out, e
}
func sourceTotals(ctx context.Context, q catalog.Query, v UsageSource) (int64, int64, string, string, error) {
	// Sum the authoritative daily ledger once. Never add raw, hourly, instance,
	// inbound or archive copies. Native sing-box requires the stable inbound.
	ids := `SELECT i.id FROM identities i WHERE i.instance_id=? AND i.core_user_key=? AND i.scope='user' AND ((?='' AND i.inbound_tag='') OR (?<>'' AND i.inbound_tag IN (SELECT tag FROM inbound_identity_aliases WHERE inbound_id=? UNION SELECT tag FROM catalog_inbounds WHERE id=?)))`
	args := []any{v.Instance, v.Client, v.Inbound, v.Inbound, v.Inbound, v.Inbound}
	var up, down int64
	e := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(upload_bytes),0),COALESCE(SUM(download_bytes),0) FROM traffic_daily WHERE identity_id IN (`+ids+`)`, args...).Scan(&up, &down)
	if e != nil {
		return 0, 0, "", "", e
	}
	var seen int
	var at sql.NullString
	var partial int
	var coverage sql.NullString
	e = q.QueryRowContext(ctx, `SELECT COUNT(*),MAX(collected_at),COALESCE(MAX(CASE WHEN coverage<>'cumulative_counter' THEN 1 ELSE 0 END),0),MAX(coverage) FROM traffic_provenance WHERE identity_id IN (`+ids+`)`, args...).Scan(&seen, &at, &partial, &coverage)
	if e != nil {
		return 0, 0, "", "", e
	}
	state := "complete"
	if seen == 0 {
		state = "unavailable"
	} else if partial > 0 {
		state = "partial"
	}
	if at.Valid {
		t, er := time.Parse(time.RFC3339Nano, at.String)
		if er != nil || time.Since(t) > 5*time.Minute {
			state = "stale"
		}
	}
	return up, down, state, coverage.String, nil
}

// Capture binding-time baselines without changing any traffic facts. Sources
// survive unbinding: removing credentials must not erase already billed usage.
func RememberUsageSources(ctx context.Context, q catalog.Query, id string, reset bool) error {
	values, e := sources(ctx, q, id)
	if e != nil {
		return e
	}
	for _, v := range values {
		if v.DesiredActive {
			var conflicts int
			if e = q.QueryRowContext(ctx, `SELECT count(*) FROM subscriber_usage_sources WHERE instance_id=? AND inbound_id=? AND client_ref=? AND active=1 AND subscriber_id<>?`, v.Instance, v.Inbound, v.Client, id).Scan(&conflicts); e != nil {
				return e
			}
			if conflicts > 0 {
				return fmt.Errorf("该流量来源已有其他订阅用户的计费归属，请先解除原绑定；不能重复计费")
			}
		}
		up, down, state, _, er := sourceTotals(ctx, q, v)
		if er != nil {
			return er
		}
		baselineUp, baselineDown, known := v.BaselineUpload, v.BaselineDownload, v.Known
		if !v.Persisted {
			baselineUp, baselineDown = up, down
			// The clean v2 ledger contains no imported v1 history. A never-observed
			// zero ledger has a known TML baseline, while source completeness stays
			// unavailable until actual counters have been observed.
			known = state == "complete" || state == "partial" || up == 0 && down == 0
		}
		if v.Persisted && !v.Active && v.DesiredActive && v.FrozenUpload != nil && v.FrozenDownload != nil {
			baselineUp += max(up-*v.FrozenUpload, 0)
			baselineDown += max(down-*v.FrozenDownload, 0)
		}
		if reset {
			if !v.DesiredActive && v.FrozenUpload != nil && v.FrozenDownload != nil {
				up, down = *v.FrozenUpload, *v.FrozenDownload
				state = v.FrozenState
			}
			baselineUp, baselineDown = up, down
			known = state == "complete" || state == "partial"
		}
		var frozenUp, frozenDown any
		frozenState := ""
		if !v.DesiredActive {
			if v.Persisted && !v.Active && v.FrozenUpload != nil && v.FrozenDownload != nil {
				up, down = *v.FrozenUpload, *v.FrozenDownload
				state = v.FrozenState
			}
			frozenUp, frozenDown, frozenState = up, down, state
		}
		_, e = q.ExecContext(ctx, `INSERT INTO subscriber_usage_sources(subscriber_id,instance_id,inbound_id,client_ref,baseline_upload,baseline_download,baseline_known,active,frozen_upload,frozen_download,frozen_state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(subscriber_id,instance_id,inbound_id,client_ref) DO UPDATE SET baseline_upload=excluded.baseline_upload,baseline_download=excluded.baseline_download,baseline_known=excluded.baseline_known,active=excluded.active,frozen_upload=excluded.frozen_upload,frozen_download=excluded.frozen_download,frozen_state=excluded.frozen_state`, id, v.Instance, v.Inbound, v.Client, baselineUp, baselineDown, known, v.DesiredActive, frozenUp, frozenDown, frozenState, storage.Stamp(time.Now()))
		if e != nil {
			return e
		}
	}
	return nil
}

func SubscriberUsage(ctx context.Context, q catalog.Query, id string) (Usage, error) {
	var u Usage
	v, e := ReadQuota(ctx, q, id)
	u.Quota = v
	if e != nil {
		return u, e
	}
	u.Completeness = "complete"
	u.Status = "active"
	values, e := sources(ctx, q, id)
	if e != nil {
		return u, e
	}
	u.Sources = []UsageSource{}
	if len(values) == 0 {
		u.Completeness = "unavailable"
	}
	rank := map[string]int{"complete": 0, "partial": 1, "stale": 2, "unavailable": 3}
	for _, s := range values {
		up, down, state, coverage, er := sourceTotals(ctx, q, s)
		if er != nil {
			return u, er
		}
		if s.Persisted && !s.Active && s.FrozenUpload != nil && s.FrozenDownload != nil {
			up, down, state = *s.FrozenUpload, *s.FrozenDownload, s.FrozenState
		}
		s.Coverage = coverage
		if !s.Known && state == "complete" {
			state = "partial"
		}
		s.Completeness = state
		s.Upload = max(up-s.BaselineUpload, 0)
		s.Download = max(down-s.BaselineDownload, 0)
		if u.Upload > math.MaxInt64-s.Upload || u.Download > math.MaxInt64-s.Download {
			return u, fmt.Errorf("流量超过整数范围")
		}
		u.Upload += s.Upload
		u.Download += s.Download
		if rank[state] > rank[u.Completeness] {
			u.Completeness = state
		}
		u.Sources = append(u.Sources, s)
	}
	if u.Upload > math.MaxInt64-u.Download {
		return u, fmt.Errorf("流量超过整数范围")
	}
	if u.Completeness == "unavailable" {
		for _, source := range u.Sources {
			if source.Completeness != "unavailable" {
				u.Completeness = "partial"
				break
			}
		}
	}
	u.Used = u.Upload + u.Download
	var enabled bool
	if e = q.QueryRowContext(ctx, "SELECT enabled FROM subscribers WHERE id=?", id).Scan(&enabled); e != nil {
		return u, e
	}
	if u.Enabled {
		if u.Completeness == "complete" {
			r := max(u.Limit-u.Used, 0)
			u.Remaining = &r
		}
		if u.Used >= u.Limit {
			u.Status = "quota_exhausted"
		} else if u.Completeness != "complete" {
			u.Status = "quota_unknown"
		} else if u.Limit-u.Used <= u.Limit/10 {
			u.Status = "low_quota"
		}
	}
	if u.ExpiryEnabled && u.ExpiresAt != nil {
		end, er := time.Parse(time.RFC3339, *u.ExpiresAt)
		if er != nil {
			return u, er
		}
		if !time.Now().Before(end) {
			u.Status = "expired"
		} else if time.Until(end) <= 7*24*time.Hour && u.Status == "active" {
			u.Status = "expiring"
		}
	}
	if !enabled {
		u.Status = "disabled"
	}
	if !u.Initialized && enabled {
		u.Status = "migration_review"
	}
	return u, nil
}

type QuotaUpdate struct {
	Quota
	Reset     bool  `json:"reset_usage"`
	Confirmed bool  `json:"confirmed"`
	AddBytes  int64 `json:"add_bytes"`
}

func UpdateQuota(ctx context.Context, s *storage.Store, id string, d QuotaUpdate) (Quota, error) {
	var empty Quota
	if d.Limit < 0 || d.AddBytes < 0 || d.Limit > (1<<50)-d.AddBytes || d.Revision < 1 || d.Policy != "report_partial" && d.Policy != "require_complete" {
		return empty, fmt.Errorf("配额字段无效")
	}
	if d.Reset && !d.Confirmed {
		return empty, fmt.Errorf("重置周期需要确认")
	}
	if d.ExpiryEnabled && d.ExpiresAt == nil {
		return empty, fmt.Errorf("请指定到期时间")
	}
	if d.ExpiresAt != nil {
		t, e := time.Parse(time.RFC3339, *d.ExpiresAt)
		if e != nil || t.Year() < 2000 || t.Year() > 2200 {
			return empty, fmt.Errorf("到期时间无效")
		}
		v := storage.Stamp(t)
		d.ExpiresAt = &v
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	before, e := ReadQuota(ctx, tx, id)
	if e != nil {
		return empty, e
	}
	if before.Revision != d.Revision {
		return empty, ErrQuotaConflict
	}
	if !before.Initialized && !d.Confirmed {
		return empty, fmt.Errorf("旧用户配额迁移须先预览并确认，不自动清零")
	}
	// Old users retain all known historical consumption; no new baseline unless
	// the administrator explicitly asks to reset the usage period.
	if !before.Initialized {
		values, er := sources(ctx, tx, id)
		if er != nil {
			return empty, er
		}
		for _, v := range values {
			_, e = tx.ExecContext(ctx, `INSERT INTO subscriber_usage_sources(subscriber_id,instance_id,inbound_id,client_ref,baseline_upload,baseline_download,baseline_known,created_at) VALUES(?,?,?,?,0,0,0,?) ON CONFLICT DO NOTHING`, id, v.Instance, v.Inbound, v.Client, storage.Stamp(time.Now()))
			if e != nil {
				return empty, e
			}
		}
	}
	if d.Reset {
		if e = RememberUsageSources(ctx, tx, id, true); e != nil {
			return empty, e
		}
	}
	reset := before.ResetAt
	if d.Reset {
		v := storage.Stamp(time.Now())
		reset = &v
	}
	now := storage.Stamp(time.Now())
	_, e = tx.ExecContext(ctx, `UPDATE subscribers SET traffic_limit_bytes=?,expires_at=?,quota_enabled=?,expiry_enabled=?,quota_policy=?,quota_revision=quota_revision+1,quota_initialized=1,activated_at=COALESCE(activated_at,created_at),quota_reset_at=?,updated_at=? WHERE id=?`, d.Limit+d.AddBytes, d.ExpiresAt, d.Enabled, d.ExpiryEnabled, d.Policy, reset, now, id)
	if e != nil {
		return empty, e
	}
	after, e := ReadQuota(ctx, tx, id)
	if e != nil {
		return empty, e
	}
	action := "update"
	if d.Reset {
		action = "reset"
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at) VALUES(?,?,?,?,?)`, id, action, jsonText(before), jsonText(after), now)
	if e != nil {
		return empty, e
	}
	return after, tx.Commit()
}
func (u Usage) UserInfo() string {
	// Clash Verge substitutes zero for omitted numeric keys. An expiry-only
	// standard header would therefore fabricate 0/0 billing when usage is absent.
	if u.Completeness == "unavailable" || !u.Enabled || !u.Initialized {
		return ""
	}
	fields := []string{}
	if u.Completeness != "unavailable" {
		fields = append(fields, fmt.Sprintf("upload=%d; download=%d", u.Upload, u.Download))
		if u.Enabled {
			fields = append(fields, fmt.Sprintf("total=%d", u.Limit))
		}
	}
	if u.ExpiryEnabled && u.ExpiresAt != nil {
		if t, e := time.Parse(time.RFC3339, *u.ExpiresAt); e == nil {
			fields = append(fields, fmt.Sprintf("expire=%d", t.Unix()))
		}
	}
	return strings.Join(fields, "; ")
}
