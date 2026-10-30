package subscription

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/xraymonitor"
)

type Subscriber struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	ExternalRef  string       `json:"external_ref"`
	Enabled      bool         `json:"enabled"`
	InitialQuota *QuotaUpdate `json:"quota,omitempty"`
}
type Binding struct {
	ID           string  `json:"id"`
	SubscriberID string  `json:"subscriber_id"`
	InboundID    *string `json:"inbound_id"`
	CredentialID string  `json:"credential_id"`
	Enabled      bool    `json:"enabled"`
	Billable     *bool   `json:"billable,omitempty"`
}
type CatalogDefinition struct {
	ID           string        `json:"id"`
	SubscriberID string        `json:"subscriber_id"`
	Name         string        `json:"name"`
	Format       string        `json:"format"`
	NodeIDs      []string      `json:"node_ids"`
	GroupIDs     []string      `json:"group_ids"`
	Enabled      bool          `json:"enabled"`
	ExpiresAt    *string       `json:"expires_at"`
	Options      RenderOptions `json:"options"`
	Usage        *Usage        `json:"-"`
}

func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }
func SaveSubscriber(ctx context.Context, s *storage.Store, d Subscriber) (string, error) {
	return SaveSubscriberWith(ctx, s, d, nil)
}

// SaveSubscriberWith atomically registers a provisioning operation with its
// subscriber. No remote operation may run inside beforeCommit.
func SaveSubscriberWith(ctx context.Context, s *storage.Store, d Subscriber, beforeCommit func(*sql.Tx, string) error) (string, error) {
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 200 || len(d.ExternalRef) > 200 || strings.ContainsAny(d.Name+d.ExternalRef, "\r\n\x00") {
		return "", fmt.Errorf("订阅用户无效")
	}
	creating := d.ID == ""
	if creating {
		d.ID = catalog.ID()
	}
	now := storage.Stamp(time.Now())
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	beforeProfile := map[string]any{}
	if !creating {
		var name, ref string
		var enabled bool
		if e = tx.QueryRowContext(ctx, "SELECT name,external_ref,enabled FROM subscribers WHERE id=?", d.ID).Scan(&name, &ref, &enabled); e != nil {
			return "", e
		}
		beforeProfile = map[string]any{"name": name, "external_ref": ref, "enabled": enabled}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO subscribers(id,name,external_ref,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,external_ref=excluded.external_ref,enabled=excluded.enabled,updated_at=excluded.updated_at`, d.ID, d.Name, d.ExternalRef, d.Enabled, now, now)
	if e != nil {
		return "", e
	}
	if creating {
		limit := DefaultQuota
		activation, _ := time.Parse(time.RFC3339Nano, now)
		end := storage.Stamp(ThreeMonths(activation))
		qe, ee := true, true
		policy := "report_partial"
		if d.InitialQuota != nil {
			v := d.InitialQuota
			if v.Limit < 0 || v.Limit > (1<<50) || v.Policy != "report_partial" && v.Policy != "require_complete" {
				return "", fmt.Errorf("配额字段无效")
			}
			limit = v.Limit
			qe = v.Enabled
			ee = v.ExpiryEnabled
			policy = v.Policy
			if v.ExpiresAt != nil {
				t, er := time.Parse(time.RFC3339, *v.ExpiresAt)
				if er != nil || t.Year() < 2000 || t.Year() > 2200 {
					return "", fmt.Errorf("到期时间无效")
				}
				end = storage.Stamp(t)
			}
		}
		_, e = tx.ExecContext(ctx, `UPDATE subscribers SET traffic_limit_bytes=?,expires_at=?,activated_at=?,quota_enabled=?,expiry_enabled=?,quota_initialized=1,quota_policy=? WHERE id=?`, limit, end, now, qe, ee, policy, d.ID)
		if e != nil {
			return "", e
		}
		quota, er := ReadQuota(ctx, tx, d.ID)
		if er != nil {
			return "", er
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at) VALUES(?,'create','{}',?,?)`, d.ID, jsonText(quota), now)
		if e != nil {
			return "", e
		}
	}
	if !creating {
		_, e = tx.ExecContext(ctx, "UPDATE subscribers SET quota_revision=quota_revision+1 WHERE id=?", d.ID)
		if e != nil {
			return "", e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at) VALUES(?,'profile_update',?,?,?)`, d.ID, jsonText(beforeProfile), jsonText(map[string]any{"name": d.Name, "external_ref": d.ExternalRef, "enabled": d.Enabled}), now)
		if e != nil {
			return "", e
		}
	}
	if beforeCommit != nil {
		if e = beforeCommit(tx, d.ID); e != nil {
			return "", e
		}
	}
	return d.ID, tx.Commit()
}
func Bind(ctx context.Context, s *storage.Store, d Binding) (string, error) {
	if d.InboundID == nil || *d.InboundID == "" {
		return "", fmt.Errorf("绑定必须指定已登记入站")
	}
	var matches int
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if d.InboundID != nil {
		e := tx.QueryRowContext(ctx, `SELECT count(*) FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id WHERE c.id=? AND k.inbound_id=?`, d.CredentialID, *d.InboundID).Scan(&matches)
		if e != nil {
			return "", e
		}

	}
	if matches != 1 {
		return "", fmt.Errorf("凭据不属于所选入站/节点")
	}
	if d.ID == "" {
		d.ID = catalog.ID()
	}
	_, e = tx.ExecContext(ctx, "DELETE FROM subscriber_credentials WHERE subscriber_id=? AND inbound_id=?", d.SubscriberID, d.InboundID)
	if e != nil {
		return "", e
	}
	billable := true
	if d.Billable != nil {
		billable = *d.Billable
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO subscriber_credentials(id,subscriber_id,inbound_id,credential_id,enabled,created_at,billable) VALUES(?,?,?,?,?,?,?)", d.ID, d.SubscriberID, d.InboundID, d.CredentialID, d.Enabled, storage.Stamp(time.Now()), billable)
	if e != nil {
		return "", e
	}
	if e = RememberUsageSources(ctx, tx, d.SubscriberID, false); e != nil {
		return "", e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at) VALUES(?,'binding_update','{}',?,?)`, d.SubscriberID, jsonText(map[string]any{"credential_id": d.CredentialID, "billable": billable, "enabled": d.Enabled}), storage.Stamp(time.Now()))
	if e != nil {
		return "", e
	}
	return d.ID, tx.Commit()
}

func Unbind(ctx context.Context, s *storage.Store, id string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var subscriber string
	if e = tx.QueryRowContext(ctx, "SELECT subscriber_id FROM subscriber_credentials WHERE id=?", id).Scan(&subscriber); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM subscriber_credentials WHERE id=?", id); e != nil {
		return e
	}
	if e = RememberUsageSources(ctx, tx, subscriber, false); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at) VALUES(?,'binding_remove','{}',?,?)`, subscriber, jsonText(map[string]string{"binding_id": id}), storage.Stamp(time.Now())); e != nil {
		return e
	}
	return tx.Commit()
}

type ResolvedClientNode struct {
	ID           string            `json:"id"`
	Profile      core.NodeProfile  `json:"profile"`
	Tags         []string          `json:"tags"`
	FieldSources map[string]string `json:"field_sources"`
	CredentialID string            `json:"-"`
}
type CatalogPreview struct {
	Selected    int                  `json:"selected"`
	Usable      int                  `json:"usable"`
	Rejected    []Rejection          `json:"rejected"`
	Nodes       []ResolvedClientNode `json:"nodes"`
	Content     string               `json:"content"`
	ContentType string               `json:"content_type"`
}

func expand(ctx context.Context, q catalog.Query, d CatalogDefinition) ([]string, error) {
	if len(d.NodeIDs)+len(d.GroupIDs) > 500 {
		return nil, fmt.Errorf("分配上限 500")
	}
	ids := []string{}
	seen := map[string]bool{}
	appendID := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range d.NodeIDs {
		appendID(id)
	}
	for _, group := range d.GroupIDs {
		var enabled bool
		if e := q.QueryRowContext(ctx, "SELECT enabled FROM node_groups WHERE id=?", group).Scan(&enabled); e != nil {
			return nil, e
		}
		if !enabled {
			continue
		}
		rows, e := q.QueryContext(ctx, "SELECT node_id FROM node_group_members WHERE group_id=? ORDER BY sort_order,node_id", group)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return nil, e
			}
			appendID(id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	if len(ids) > 500 {
		return nil, fmt.Errorf("分组展开超过 500 个节点")
	}
	return ids, nil
}
func ResolveCatalog(ctx context.Context, q catalog.Query, d CatalogDefinition) ([]ResolvedClientNode, []Rejection, int, error) {
	ids, e := expand(ctx, q, d)
	if e != nil {
		return nil, nil, 0, e
	}
	var subscriberEnabled, nodeAuthorizationRequired bool
	if e = q.QueryRowContext(ctx, "SELECT enabled,node_authorization_required FROM subscribers WHERE id=?", d.SubscriberID).Scan(&subscriberEnabled, &nodeAuthorizationRequired); e != nil {
		return nil, nil, 0, e
	}
	out := []ResolvedClientNode{}
	rejected := []Rejection{}
	for pos, id := range ids {
		n, e := catalog.ResolveDefinition(ctx, q, id)
		if e != nil {
			if e == sql.ErrNoRows {
				rejected = append(rejected, Rejection{id, "source_missing：节点不存在"})
				continue
			}
			return nil, nil, 0, e
		}
		if !subscriberEnabled {
			rejected = append(rejected, Rejection{id, "disabled：订阅用户已停用"})
			continue
		}
		if nodeAuthorizationRequired {
			var granted int
			if e = q.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_node_grants WHERE subscriber_id=? AND node_id=? AND inbound_id=?", d.SubscriberID, id, n.InboundID).Scan(&granted); e != nil {
				return nil, nil, 0, e
			}
			if granted != 1 {
				rejected = append(rejected, Rejection{id, "node_not_authorized：节点未分配给此订阅用户"})
				continue
			}
		}
		if n.Status != "ready" && !(n.Source == "manual" && n.Status == "needs_review") {
			rejected = append(rejected, Rejection{id, n.Status})
			continue
		}
		var auth, status, credentialID string
		var bindingEnabled bool
		e = q.QueryRowContext(ctx, `SELECT c.auth_json,c.status,c.id,b.enabled FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id WHERE b.subscriber_id=? AND b.inbound_id=?`, d.SubscriberID, n.InboundID).Scan(&auth, &status, &credentialID, &bindingEnabled)
		if e == sql.ErrNoRows || e == nil && (!bindingEnabled || status != "available") {
			rejected = append(rejected, Rejection{id, "credential_missing：未绑定、凭据已禁用或来源缺失"})
			continue
		}
		if e != nil {
			return nil, nil, 0, e
		}
		if e = json.Unmarshal([]byte(auth), &n.Profile); e != nil {
			return nil, nil, 0, fmt.Errorf("凭据来源无法解析")
		}
		var authBlocked int
		if e = q.QueryRowContext(ctx, `SELECT count(*) FROM hysteria_auth_accounts a JOIN hysteria_auth_backends h ON h.inbound_id=a.inbound_id JOIN catalog_inbounds b ON b.id=h.inbound_id JOIN instances i ON i.id=b.instance_id WHERE a.credential_id=? AND (a.origin='managed' OR h.configured_at IS NOT NULL) AND (h.enabled=0 OR h.confirmed=0 OR h.target_endpoint<>i.api_endpoint OR h.target_tag<>b.tag)`, credentialID).Scan(&authBlocked); e != nil {
			return nil, nil, 0, e
		}
		if authBlocked > 0 {
			rejected = append(rejected, Rejection{id, "auth_backend_unavailable：HTTP 认证后端未确认、停用或目标变化"})
			continue
		}
		n.Profile.Sort = pos
		n.Sources["authentication"] = "subscriber_binding"
		// A fresh complete runtime client snapshot is authoritative for an explicit
		// removal. Stats/Online absence must never invalidate credentials.
		if n.InboundID != nil {
			var instance int64
			var tag string
			if e = q.QueryRowContext(ctx, "SELECT instance_id,tag FROM catalog_inbounds WHERE id=?", *n.InboundID).Scan(&instance, &tag); e != nil {
				return nil, nil, 0, e
			}
			var name string
			if e = q.QueryRowContext(ctx, "SELECT core_client_ref FROM credential_material WHERE id=?", credentialID).Scan(&name); e != nil {
				return nil, nil, 0, e
			}
			var removed int
			_ = q.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT instance_id,inbound_tag,email,present,source FROM xray_clients UNION ALL SELECT instance_id,inbound_tag,email,present,source FROM core_clients) WHERE instance_id=? AND inbound_tag=? AND email=? AND present=0 AND source='runtime_api'`, instance, tag, name).Scan(&removed)
			if removed > 0 {
				rejected = append(rejected, Rejection{id, "credential_missing：运行资产已明确移除；配置与运行状态需复核"})
				continue
			}
			var keyHex string
			if q.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='xray_asset_fingerprint_key'").Scan(&keyHex) == nil {
				key, _ := hex.DecodeString(keyHex)
				credential := n.Profile.UUID
				if credential == "" {
					credential = n.Profile.Password
				}
				fp := xraymonitor.CredentialFingerprint(key, n.Profile.Protocol, credential, n.Profile.Flow)
				var total, matched int
				if fp != "" && len(key) == 32 {
					_ = q.QueryRowContext(ctx, "SELECT count(*),COALESCE(sum(CASE WHEN credential_fingerprint=? THEN 1 ELSE 0 END),0) FROM xray_clients WHERE instance_id=? AND inbound_tag=? AND email=? AND source='runtime_api' AND present=1 AND credential_fingerprint<>'' AND observed_at>=?", fp, instance, tag, name, storage.Stamp(time.Now().Add(-5*time.Minute))).Scan(&total, &matched)
					if total > 0 && matched == 0 {
						rejected = append(rejected, Rejection{id, "needs_review：配置凭据与运行时认证不一致"})
						continue
					}
				}
			}
		}
		out = append(out, ResolvedClientNode{ID: id, Profile: n.Profile, Tags: n.Tags, FieldSources: n.Sources, CredentialID: credentialID})
	}
	return out, rejected, len(ids), nil
}
func CatalogPreviewFor(ctx context.Context, q catalog.Query, d CatalogDefinition) (CatalogPreview, error) {
	nodes, rejected, count, e := ResolveCatalog(ctx, q, d)
	p := CatalogPreview{Selected: count, Rejected: rejected, Nodes: []ResolvedClientNode{}}
	if e != nil {
		return p, e
	}
	render, e := Render(ctx, d.Format, nodes, d.Options)
	if e != nil {
		return p, e
	}
	p.Rejected = append(p.Rejected, render.Rejected...)
	p.Usable = count - len(p.Rejected)
	// Render the redacted copy through the identical renderer, so preview never
	// includes authentication, not even embedded in encoded share links.
	masked := make([]ResolvedClientNode, len(nodes))
	for i, n := range nodes {
		n.Profile.UUID = "11111111-1111-4111-8111-111111111111"
		n.Profile.Password = "REDACTED"
		n.CredentialID = ""
		masked[i] = n
	}
	safe, e := Render(ctx, d.Format, masked, d.Options)
	if e != nil {
		return p, e
	}
	p.Content = string(safe.Content)
	p.ContentType = safe.ContentType
	p.Nodes = masked
	return p, nil
}
func SaveCatalogSubscription(ctx context.Context, s *storage.Store, d CatalogDefinition) (string, string, error) {
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 200 || !ValidFormat(d.Format) || strings.ContainsAny(d.Name, "\r\n\x00") {
		return "", "", fmt.Errorf("订阅字段无效")
	}
	if d.ExpiresAt != nil && *d.ExpiresAt != "" {
		t, e := time.Parse(time.RFC3339, *d.ExpiresAt)
		if e != nil {
			return "", "", fmt.Errorf("到期日期需 RFC3339")
		}
		v := storage.Stamp(t)
		d.ExpiresAt = &v
	} else {
		d.ExpiresAt = nil
	}
	token, hash, e := Token()
	if e != nil {
		return "", "", e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", "", e
	}
	defer tx.Rollback()
	p, e := CatalogPreviewFor(ctx, tx, d)
	if e != nil {
		return "", "", e
	}
	if d.Enabled && p.Usable == 0 {
		return "", "", ErrNoUsableNodes
	}
	now := storage.Stamp(time.Now())
	if d.ID == "" {
		d.ID = catalog.ID()
		_, e = tx.ExecContext(ctx, "INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,enabled,expires_at,format,options_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)", d.ID, d.SubscriberID, d.Name, hash, d.Enabled, d.ExpiresAt, d.Format, jsonText(d.Options), now, now)
	} else {
		token = ""
		var res sql.Result
		res, e = tx.ExecContext(ctx, "UPDATE catalog_subscriptions SET subscriber_id=?,name=?,enabled=?,expires_at=?,format=?,options_json=?,updated_at=? WHERE id=?", d.SubscriberID, d.Name, d.Enabled, d.ExpiresAt, d.Format, jsonText(d.Options), now, d.ID)
		if e == nil {
			n, _ := res.RowsAffected()
			if n == 0 {
				return "", "", sql.ErrNoRows
			}
		}
	}
	if e != nil {
		return "", "", e
	}
	for _, table := range []string{"catalog_subscription_nodes", "catalog_subscription_groups"} {
		if _, e = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE subscription_id=?", d.ID); e != nil {
			return "", "", e
		}
	}
	for kind, ids := range map[string][]string{"nodes": d.NodeIDs, "groups": d.GroupIDs} {
		seen := map[string]bool{}
		for pos, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			table, field := "catalog_subscription_nodes", "node_id"
			if kind == "groups" {
				table, field = "catalog_subscription_groups", "group_id"
			}
			q := "INSERT INTO " + table + "(subscription_id," + field + ",sort_order) VALUES(?,?,?)"
			if _, e = tx.ExecContext(ctx, q, d.ID, id, pos); e != nil {
				return "", "", e
			}
		}
	}
	return d.ID, token, tx.Commit()
}
func CatalogSubscription(ctx context.Context, q catalog.Query, id string) (CatalogDefinition, error) {
	var d CatalogDefinition
	var options string
	d.ID = id
	e := q.QueryRowContext(ctx, "SELECT subscriber_id,name,format,enabled,expires_at,options_json FROM catalog_subscriptions WHERE id=?", id).Scan(&d.SubscriberID, &d.Name, &d.Format, &d.Enabled, &d.ExpiresAt, &options)
	if e != nil {
		return d, e
	}
	if e = json.Unmarshal([]byte(options), &d.Options); e != nil {
		return d, e
	}
	for kind, table := range map[string]string{"nodes": "catalog_subscription_nodes", "groups": "catalog_subscription_groups"} {
		field := "node_id"
		if kind == "groups" {
			field = "group_id"
		}
		rows, e := q.QueryContext(ctx, "SELECT "+field+" FROM "+table+" WHERE subscription_id=? ORDER BY sort_order,"+field, id)
		if e != nil {
			return d, e
		}
		for rows.Next() {
			var value string
			if e = rows.Scan(&value); e != nil {
				rows.Close()
				return d, e
			}
			if kind == "nodes" {
				d.NodeIDs = append(d.NodeIDs, value)
			} else {
				d.GroupIDs = append(d.GroupIDs, value)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return d, e
		}
	}
	return d, nil
}
func RotateCatalog(ctx context.Context, s *storage.Store, id string) (string, error) {
	t, h, e := Token()
	if e != nil {
		return "", e
	}
	res, e := s.DB.ExecContext(ctx, "UPDATE catalog_subscriptions SET token_hash=?,token_version=token_version+1,updated_at=? WHERE id=?", h, storage.Stamp(time.Now()), id)
	if e != nil {
		return "", e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", sql.ErrNoRows
	}
	return t, nil
}
