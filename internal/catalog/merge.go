package catalog

import (
	"context"
	"errors"
	"traffic-manager-lite/internal/storage"
)

// MergeNodes is explicit, not part of discovery. Different Endpoint identities
// are deliberately preserved even when they currently resolve to the same IP.
func MergeNodes(ctx context.Context, s *storage.Store, primary, secondary, backup string, expected ...string) error {
	if backup == "" || primary == secondary {
		return errors.New("明确选择两个节点并先备份")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	a, e := ResolveDefinition(ctx, tx, primary)
	if e != nil {
		return e
	}
	b, e := ResolveDefinition(ctx, tx, secondary)
	if e != nil {
		return e
	}
	if len(expected) != 2 || expected[0] != a.Revision || expected[1] != b.Revision {
		return errors.New("节点预览修订已变化，请重新预览")
	}
	if a.InboundID == nil || b.InboundID == nil || *a.InboundID != *b.InboundID {
		return errors.New("节点不属于同一稳定入站")
	}
	if (a.EndpointID == nil) != (b.EndpointID == nil) || a.EndpointID != nil && *a.EndpointID != *b.EndpointID {
		return errors.New("不同Endpoint是合法多入口，不能去重")
	}
	x, y := a.Profile, b.Profile
	x.Name, y.Name = "", ""
	x.Sort, y.Sort = 0, 0
	if encode(x) != encode(y) || encode(a.Overrides) != encode(b.Overrides) {
		return errors.New("最终参数或覆盖规则不同，不是重复节点")
	}
	for _, q := range []string{`INSERT OR IGNORE INTO catalog_subscription_nodes SELECT subscription_id,?,sort_order,enabled FROM catalog_subscription_nodes WHERE node_id=?`, `INSERT OR IGNORE INTO node_group_members SELECT group_id,?,sort_order FROM node_group_members WHERE node_id=?`} {
		if _, e = tx.ExecContext(ctx, q, primary, secondary); e != nil {
			return e
		}
	}
	for _, q := range []string{"DELETE FROM catalog_subscription_nodes WHERE node_id=?", "DELETE FROM node_group_members WHERE node_id=?", "DELETE FROM node_profiles WHERE id=?"} {
		if _, e = tx.ExecContext(ctx, q, secondary); e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO inbound_merge_audit(id,instance_id,primary_id,secondary_id,backup_path,summary_json,created_at) SELECT ?,instance_id,?,?,?,'{"kind":"explicit_node_merge","historical_traffic":"unchanged"}',? FROM catalog_inbounds WHERE id=?`, ID(), primary, secondary, backup, stamp(), *a.InboundID)
	if e != nil {
		return e
	}
	return tx.Commit()
}
