package subscriberprovision

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

type NodeEditRequest struct {
	RequestID    string   `json:"request_id"`
	SubscriberID string   `json:"subscriber_id"`
	NodeIDs      []string `json:"node_ids"`
	GroupIDs     []string `json:"group_ids"`
	PlanHash     string   `json:"plan_hash"`
	Confirmed    bool     `json:"confirmed"`
}
type NodeEditPlan struct {
	Hash      string                        `json:"hash"`
	Added     []PlannedNode                 `json:"added"`
	Repaired  []PlannedNode                 `json:"repaired"`
	Removed   []subscription.NodeAssignment `json:"removed"`
	Retained  int                           `json:"retained"`
	Provision *NodePlan                     `json:"provision,omitempty"`
	Ready     bool                          `json:"ready"`
}

func (s Service) PreviewNodeEdit(ctx context.Context, d NodeEditRequest) (NodeEditPlan, error) {
	tx, e := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return NodeEditPlan{}, e
	}
	defer tx.Rollback()
	return s.previewNodeEdit(ctx, tx, d)
}
func (s Service) previewNodeEdit(ctx context.Context, q catalog.Query, d NodeEditRequest) (NodeEditPlan, error) {
	p := NodeEditPlan{Added: []PlannedNode{}, Removed: []subscription.NodeAssignment{}, Ready: true}
	var user string
	if e := q.QueryRowContext(ctx, "SELECT id FROM subscribers WHERE id=?", d.SubscriberID).Scan(&user); e != nil {
		return p, fmt.Errorf("订阅用户不存在")
	}
	for _, g := range d.GroupIDs {
		var enabled bool
		if e := q.QueryRowContext(ctx, "SELECT enabled FROM node_groups WHERE id=?", g).Scan(&enabled); e != nil || !enabled {
			return p, fmt.Errorf("所选节点组不存在或已停用，请重新加载")
		}
	}
	ids, e := subscription.ExpandNodeSelection(ctx, q, d.NodeIDs, d.GroupIDs)
	if e != nil {
		return p, e
	}
	old, e := subscription.NodeAssignments(ctx, q, d.SubscriberID)
	if e != nil {
		return p, e
	}
	assigned := map[string]bool{}
	wanted := map[string]bool{}
	added := []string{}
	for _, n := range old {
		assigned[n.NodeID] = true
	}
	for _, id := range ids {
		wanted[id] = true
		if assigned[id] {
			p.Retained++
			repair, err := needsNativeBinding(ctx, q, d.SubscriberID, id)
			if err != nil {
				return p, err
			}
			if repair {
				added = append(added, id)
			}
		} else {
			added = append(added, id)
		}
	}
	for _, n := range old {
		if !wanted[n.NodeID] {
			p.Removed = append(p.Removed, n)
		}
	}
	if len(added) > 0 {
		plan, err := s.planNodes(ctx, q, NodeRequest{Subscriber: subscription.Subscriber{ID: d.SubscriberID}, NodeIDs: added})
		if err != nil {
			return p, err
		}
		p.Provision = &plan
		for _, n := range plan.Nodes {
			if assigned[n.ID] {
				p.Repaired = append(p.Repaired, n)
			} else {
				p.Added = append(p.Added, n)
			}
		}
		p.Ready = plan.Ready
	}
	// Include the previous assignment snapshot to reject concurrent edits.
	raw, _ := json.Marshal(struct {
		User    string
		Before  []subscription.NodeAssignment
		Desired []string
		Groups  []string
		Plan    NodeEditPlan
	}{d.SubscriberID, old, ids, d.GroupIDs, p})
	p.Hash = fmt.Sprintf("%x", sha256.Sum256(raw))
	return p, nil
}
func (s Service) ApplyNodeEdit(ctx context.Context, d NodeEditRequest) (Result, error) {
	r := Result{RequestID: d.RequestID, SubscriberID: d.SubscriberID}
	if !d.Confirmed || d.PlanHash == "" || len(d.RequestID) < 16 || len(d.RequestID) > 100 || strings.ContainsAny(d.RequestID, "\r\n\x00") {
		return r, fmt.Errorf("请预览并确认节点权限变更")
	}
	raw, _ := json.Marshal(d)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return r, e
	}
	defer tx.Rollback()
	var previous string
	e = tx.QueryRowContext(ctx, "SELECT request_hash FROM subscriber_node_edits WHERE request_id=?", d.RequestID).Scan(&previous)
	if e == nil {
		tx.Rollback()
		if previous != hash {
			return r, fmt.Errorf("请求标识已用于其他节点变更")
		}
		return s.RetryNodeEdit(ctx, d.RequestID)
	}
	if e != sql.ErrNoRows {
		return r, e
	}
	plan, e := s.previewNodeEdit(ctx, tx, d)
	if e != nil {
		return r, e
	}
	if plan.Hash != d.PlanHash {
		return r, fmt.Errorf("节点权限或配置已变化，请重新预览")
	}
	if !plan.Ready {
		return r, fmt.Errorf("新增节点尚不能配置，未更改任何权限")
	}
	now := storage.Stamp(time.Now())
	inbounds := map[string]bool{}
	for _, n := range plan.Removed {
		var inbound string
		if e = tx.QueryRowContext(ctx, "SELECT inbound_id FROM node_profiles WHERE id=?", n.NodeID).Scan(&inbound); e != nil {
			return r, e
		}
		inbounds[inbound] = true
		if _, e = tx.ExecContext(ctx, "DELETE FROM subscriber_node_grants WHERE subscriber_id=? AND node_id=?", d.SubscriberID, n.NodeID); e != nil {
			return r, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO subscriber_node_revocations VALUES(?,?,?) ON CONFLICT(subscriber_id,node_id) DO NOTHING", d.SubscriberID, n.NodeID, now); e != nil {
			return r, e
		}
	}
	// Group membership is an explicit confirmed entitlement; changing group
	// selection never creates a proxy-core account for future members.
	if _, e = tx.ExecContext(ctx, "DELETE FROM subscriber_group_grants WHERE subscriber_id=?", d.SubscriberID); e != nil {
		return r, e
	}
	for _, group := range d.GroupIDs {
		if _, e = tx.ExecContext(ctx, "INSERT INTO subscriber_group_grants(subscriber_id,group_id,created_at) VALUES(?,?,?)", d.SubscriberID, group, now); e != nil {
			return r, e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM subscriber_node_revocations WHERE subscriber_id=? AND node_id IN (SELECT node_id FROM node_group_members WHERE group_id=?)`, d.SubscriberID, group); e != nil {
			return r, e
		}
		// The UI treats grouped nodes as group entitlements, not independent picks.
		if _, e = tx.ExecContext(ctx, `DELETE FROM subscriber_node_grants WHERE subscriber_id=? AND node_id IN (SELECT node_id FROM node_group_members WHERE group_id=?)`, d.SubscriberID, group); e != nil {
			return r, e
		}
	}
	remaining, e := subscription.NodeAssignments(ctx, tx, d.SubscriberID)
	if e != nil {
		return r, e
	}
	for _, n := range remaining {
		var inbound string
		if e = tx.QueryRowContext(ctx, "SELECT inbound_id FROM node_profiles WHERE id=?", n.NodeID).Scan(&inbound); e != nil {
			return r, e
		}
		delete(inbounds, inbound)
	}
	for inbound := range inbounds {
		// Preserve the actual core account and metering. Only stop automatic restore
		// of TML-owned accounts that no longer serve any assigned node.
		if _, e = tx.ExecContext(ctx, `UPDATE remote_runtime_users SET restore_authorized=0 WHERE inbound_id=? AND EXISTS(SELECT 1 FROM subscriber_provisioning p WHERE p.subscriber_id=? AND p.inbound_id=remote_runtime_users.inbound_id AND p.client_ref=remote_runtime_users.client_ref AND p.origin='managed')`, inbound, d.SubscriberID); e != nil {
			return r, e
		}
	}
	addition := ""
	status := "ready"
	if plan.Provision != nil && len(plan.Provision.Nodes) > 0 {
		ids := []string{}
		for _, n := range plan.Provision.Nodes {
			ids = append(ids, n.ID)
		}
		child := fmt.Sprintf("edit-%x", sha256.Sum256([]byte(d.RequestID)))
		request := NodeRequest{RequestID: child, Subscriber: subscription.Subscriber{ID: d.SubscriberID}, NodeIDs: ids, PlanHash: plan.Provision.Hash, Confirmed: true}
		data, _ := json.Marshal(request)
		addition = string(data)
		status = "pending"
	}
	message := fmt.Sprintf("保留 %d 个、取消分配 %d 个、新增 %d 个节点、补齐 %d 个原生认证；取消分配不删除核心账号或中断已有连接", plan.Retained, len(plan.Removed), len(plan.Added), len(plan.Repaired))
	if _, e = tx.ExecContext(ctx, "UPDATE subscriber_node_edits SET status='superseded',message='已被后续节点权限变更替代',checked_at=? WHERE subscriber_id=? AND status='pending'", now, d.SubscriberID); e != nil {
		return r, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO subscriber_node_edits VALUES(?,?,?,?,?,?,?)", d.RequestID, hash, d.SubscriberID, addition, status, message, now); e != nil {
		return r, e
	}
	audit, _ := json.Marshal(struct {
		Added    []PlannedNode
		Removed  []subscription.NodeAssignment
		Repaired []PlannedNode
	}{plan.Added, plan.Removed, plan.Repaired})
	if _, e = tx.ExecContext(ctx, "INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at) VALUES(?,'node_permissions','{}',?,?)", d.SubscriberID, string(audit), now); e != nil {
		return r, e
	}
	if e = tx.Commit(); e != nil {
		return r, e
	}
	return s.RetryNodeEdit(ctx, d.RequestID)
}
func (s Service) RetryNodeEdit(ctx context.Context, id string) (Result, error) {
	r := Result{RequestID: id}
	var raw string
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT subscriber_id,addition_json,status,message FROM subscriber_node_edits WHERE request_id=?", id).Scan(&r.SubscriberID, &raw, &r.Status, &r.Message); e != nil {
		return r, e
	}
	if r.Status == "ready" || r.Status == "superseded" || raw == "" {
		return r, nil
	}
	var request NodeRequest
	if e := json.Unmarshal([]byte(raw), &request); e != nil {
		return r, e
	}
	v, e := s.CreateNodes(ctx, request)
	summary := strings.SplitN(r.Message, "；配置结果：", 2)[0]
	if e != nil {
		r.Status = "pending"
		r.Message = summary + "；配置结果：新增配置尚未完成，请重试；旧权限不会自动恢复"
	} else {
		r.Status = v.Status
		r.Message = summary + "；配置结果：" + v.Message
	}
	c, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer done()
	if r.Status == "ready" {
		if _, e := s.Store.DB.ExecContext(c, `DELETE FROM subscriber_node_grants WHERE subscriber_id=? AND EXISTS (SELECT 1 FROM subscriber_group_grants g JOIN node_group_members m ON m.group_id=g.group_id WHERE g.subscriber_id=subscriber_node_grants.subscriber_id AND m.node_id=subscriber_node_grants.node_id)`, r.SubscriberID); e != nil {
			return r, e
		}
	}
	_, err := s.Store.DB.ExecContext(c, "UPDATE subscriber_node_edits SET status=?,message=?,checked_at=? WHERE request_id=? AND status!='superseded'", r.Status, r.Message, storage.Stamp(time.Now()), id)
	return r, err
}
