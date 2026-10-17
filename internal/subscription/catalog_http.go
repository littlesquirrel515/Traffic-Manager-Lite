package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
	"traffic-manager-lite/internal/storage"
)

type Settings struct {
	BaseURL        string `json:"base_url"`
	DefaultFormat  string `json:"default_format"`
	UpdateInterval int    `json:"update_interval"`
	SyncInterval   int    `json:"sync_interval_seconds"`
	RetentionDays  int    `json:"log_retention_days"`
	RatePerMinute  int    `json:"rate_per_minute"`
	DefaultSort    string `json:"default_sort"`
}

func LoadSettings(ctx context.Context, s *storage.Store) (Settings, error) {
	var d Settings
	var raw string
	e := s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='subscription_catalog'").Scan(&raw)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &d)
	}
	return d, e
}
func SaveSettings(ctx context.Context, s *storage.Store, d Settings) error {
	if !ValidFormat(d.DefaultFormat) || d.UpdateInterval < 1 || d.UpdateInterval > 168 || d.SyncInterval < 30 || d.SyncInterval > 86400 || d.RetentionDays < 1 || d.RetentionDays > 365 || d.RatePerMinute < 1 || d.RatePerMinute > 600 || d.DefaultSort != "explicit" {
		return fmt.Errorf("订阅设置超出范围")
	}
	if d.BaseURL != "" {
		u, e := url.Parse(d.BaseURL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
			return fmt.Errorf("外部订阅地址必须是 HTTPS 基础域名，不含凭据或路径")
		}
	}
	_, e := s.DB.ExecContext(ctx, "UPDATE settings SET value=? WHERE key='subscription_catalog'", jsonText(d))
	return e
}
func CatalogOutput(ctx context.Context, s *storage.Store, token, format string) (RenderResult, CatalogDefinition, error) {
	var empty RenderResult
	var d CatalogDefinition
	if len(token) != 43 {
		return empty, d, sql.ErrNoRows
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return empty, d, e
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRowContext(ctx, "SELECT id FROM catalog_subscriptions WHERE token_hash=? AND enabled=1", Hash(token)).Scan(&id); e != nil {
		return empty, d, e
	}
	d, e = CatalogSubscription(ctx, tx, id)
	if e != nil {
		return empty, d, e
	}
	usage, er := SubscriberUsage(ctx, tx, d.SubscriberID)
	if er != nil {
		return empty, d, er
	}
	d.Usage = &usage
	// A delivery may retain an earlier explicit expiry from before quota support.
	// Report the effective earliest expiry without changing either stored policy.
	if d.ExpiresAt != nil {
		childEnd, childErr := time.Parse(time.RFC3339, *d.ExpiresAt)
		parentEnd := time.Time{}
		if usage.ExpiresAt != nil {
			parentEnd, _ = time.Parse(time.RFC3339, *usage.ExpiresAt)
		}
		if childErr == nil && (!usage.ExpiryEnabled || parentEnd.IsZero() || childEnd.Before(parentEnd)) {
			usage.ExpiresAt = d.ExpiresAt
			usage.ExpiryEnabled = true
		}
	}
	if er = CheckQuotaAccess(usage); er != nil {
		return empty, d, er
	}
	if d.ExpiresAt != nil {
		end, e := time.Parse(time.RFC3339, *d.ExpiresAt)
		if e != nil || !time.Now().Before(end) {
			return empty, d, fmt.Errorf("expired")
		}
	}
	var enabled bool
	if e = tx.QueryRowContext(ctx, "SELECT enabled FROM subscribers WHERE id=?", d.SubscriberID).Scan(&enabled); e != nil || !enabled {
		return empty, d, fmt.Errorf("subscriber_disabled")
	}
	if format == "" {
		format = d.Format
	}
	if !ValidFormat(format) {
		return empty, d, fmt.Errorf("format_invalid")
	}
	nodes, missing, count, e := ResolveCatalog(ctx, tx, d)
	if e != nil {
		return empty, d, e
	}
	res, e := Render(ctx, format, nodes, d.Options)
	res.Rejected = append(missing, res.Rejected...)
	if e == nil && count == len(res.Rejected) {
		e = ErrNoUsableNodes
	}
	return res, d, e
}
