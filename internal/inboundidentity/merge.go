package inboundidentity

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"time"
	"traffic-manager-lite/internal/storage"
)

type MergePreview struct {
	Primary, Secondary                           string
	InstanceID                                   int64
	Nodes, Bindings, ManagedUsers, Subscriptions int
	Conflicts                                    []string
	HistoryPolicy                                string
}

type mergeQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func PreviewMerge(ctx context.Context, s *storage.Store, primary, secondary string) (MergePreview, error) {
	return previewMerge(ctx, s.DB, primary, secondary)
}
func previewMerge(ctx context.Context, q mergeQuery, primary, secondary string) (MergePreview, error) {
	p := MergePreview{Primary: primary, Secondary: secondary, Conflicts: []string{}, HistoryPolicy: "原始流量不修改；历史Tag通过身份别名关联"}
	if primary == secondary {
		return p, errors.New("cannot merge identity into itself")
	}
	var pi, si int64
	var pp, sp string
	var a, b bool
	e := q.QueryRowContext(ctx, "SELECT instance_id,protocol,present FROM catalog_inbounds WHERE id=?", primary).Scan(&pi, &pp, &a)
	if e != nil {
		return p, e
	}
	e = q.QueryRowContext(ctx, "SELECT instance_id,protocol,present FROM catalog_inbounds WHERE id=?", secondary).Scan(&si, &sp, &b)
	if e != nil {
		return p, e
	}
	p.InstanceID = pi
	if pi != si || pp != sp {
		p.Conflicts = append(p.Conflicts, "不同实例或协议不能合并")
	}
	if !a || b {
		p.Conflicts = append(p.Conflicts, "主身份必须是现存对象，待合并身份必须已缺失")
	}
	q.QueryRowContext(ctx, "SELECT count(*) FROM node_profiles WHERE inbound_id=?", secondary).Scan(&p.Nodes)
	q.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_credentials WHERE inbound_id=?", secondary).Scan(&p.Bindings)
	q.QueryRowContext(ctx, "SELECT count(*) FROM remote_runtime_users WHERE inbound_id=?", secondary).Scan(&p.ManagedUsers)
	if p.ManagedUsers > 0 {
		p.Conflicts = append(p.Conflicts, "已有运行时 Desired 的身份不能隐式合并")
	}
	q.QueryRowContext(ctx, "SELECT count(DISTINCT subscription_id) FROM catalog_subscription_nodes sn JOIN node_profiles n ON n.id=sn.node_id WHERE n.inbound_id=?", secondary).Scan(&p.Subscriptions)
	var quotaSources int
	if e := q.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_usage_sources WHERE inbound_id IN (?,?)", primary, secondary).Scan(&quotaSources); e == nil && quotaSources > 0 {
		p.Conflicts = append(p.Conflicts, "已有关联的配额用量来源，需先单独核对归属；此修复不会自动改写计费身份")
	}
	var collisions int
	q.QueryRowContext(ctx, `SELECT count(*) FROM inbound_endpoint_bindings a JOIN inbound_endpoint_bindings b ON a.endpoint_id=b.endpoint_id WHERE a.inbound_id=? AND b.inbound_id=?`, primary, secondary).Scan(&collisions)
	if collisions > 0 {
		p.Conflicts = append(p.Conflicts, "共享入口在两个入站都有独立绑定，需先人工迁移，不能隐式合并绑定身份")
	}
	q.QueryRowContext(ctx, `SELECT count(*) FROM subscriber_credentials a JOIN subscriber_credentials b ON a.subscriber_id=b.subscriber_id WHERE a.inbound_id=? AND b.inbound_id=?`, primary, secondary).Scan(&collisions)
	if collisions > 0 {
		p.Conflicts = append(p.Conflicts, "同Subscriber存在两份绑定，需先人工选择认证")
	}
	q.QueryRowContext(ctx, `SELECT count(*) FROM remote_runtime_users a JOIN remote_runtime_users b ON a.client_ref=b.client_ref WHERE a.inbound_id=? AND b.inbound_id=?`, primary, secondary).Scan(&collisions)
	if collisions > 0 {
		p.Conflicts = append(p.Conflicts, "重复托管用户需先核对所有权和认证")
	}
	q.QueryRowContext(ctx, `SELECT count(*) FROM inbound_client_keys a JOIN inbound_client_keys b ON a.client_ref=b.client_ref JOIN credential_material ca ON ca.id=a.credential_id JOIN credential_material cb ON cb.id=b.credential_id WHERE a.inbound_id=? AND b.inbound_id=? AND (ca.auth_json<>cb.auth_json OR ca.user_id<>cb.user_id)`, primary, secondary).Scan(&collisions)
	if collisions > 0 {
		p.Conflicts = append(p.Conflicts, "同名用户的凭据或授权身份不同，禁止合并")
	}
	return p, nil
}
func Merge(ctx context.Context, s *storage.Store, primary, secondary, primaryHash, secondaryHash, backup string) error {
	if backup == "" {
		return errors.New("database backup required")
	}
	p, e := PreviewMerge(ctx, s, primary, secondary)
	if e != nil {
		return e
	}
	if len(p.Conflicts) > 0 {
		return errors.New("合并存在冲突")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	p, e = previewMerge(ctx, tx, primary, secondary)
	if e != nil {
		return e
	}
	if len(p.Conflicts) > 0 {
		return errors.New("合并关联已变化或存在冲突")
	}
	var a, b string
	if e = tx.QueryRowContext(ctx, "SELECT config_hash FROM catalog_inbounds WHERE id=?", primary).Scan(&a); e != nil {
		return e
	}
	if e = tx.QueryRowContext(ctx, "SELECT config_hash FROM catalog_inbounds WHERE id=?", secondary).Scan(&b); e != nil {
		return e
	}
	if a != primaryHash || b != secondaryHash {
		return errors.New("身份配置修订已变化")
	}
	for _, q := range []string{"UPDATE inbound_endpoint_bindings SET inbound_id=? WHERE inbound_id=?", "UPDATE node_profiles SET inbound_id=? WHERE inbound_id=?", "UPDATE subscriber_credentials SET inbound_id=? WHERE inbound_id=?", "UPDATE inbound_identity_aliases SET inbound_id=? WHERE inbound_id=?", "UPDATE inbound_identity_history SET inbound_id=? WHERE inbound_id=?", "UPDATE inbound_identity_pending SET resolved_inbound_id=? WHERE resolved_inbound_id=?"} {
		if _, e = tx.ExecContext(ctx, q, primary, secondary); e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, `UPDATE inbound_client_keys SET inbound_id=? WHERE inbound_id=? AND client_ref NOT IN(SELECT client_ref FROM inbound_client_keys WHERE inbound_id=?)`, primary, secondary, primary)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO inbound_retirements VALUES(?,?,?)", secondary, primary, storage.Stamp(time.Now()))
	if e != nil {
		return e
	}
	// Free the live tag slot; the original tag remains in aliases/history on the
	// canonical identity. A retired row must not block a later confirmed rename.
	_, e = tx.ExecContext(ctx, "UPDATE catalog_inbounds SET present=0,review=1,tag=? WHERE id=?", "__tml_retired_"+secondary, secondary)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO inbound_merge_audit VALUES(?,?,?,?,?,?,?)", uuid.NewString(), p.InstanceID, primary, secondary, backup, encode(p), storage.Stamp(time.Now()))
	if e != nil {
		return e
	}
	return tx.Commit()
}
