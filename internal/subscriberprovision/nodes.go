package subscriberprovision

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/remote"
	"traffic-manager-lite/internal/secretstore"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

// NodeRequest is the business-facing workflow. Core targets are always resolved
// from saved nodes, never supplied by the caller.
type NodeRequest struct {
	RequestID  string                  `json:"request_id"`
	Subscriber subscription.Subscriber `json:"subscriber"`
	NodeIDs    []string                `json:"node_ids"`
	GroupIDs   []string                `json:"group_ids"`
	PlanHash   string                  `json:"plan_hash"`
	Confirmed  bool                    `json:"confirmed"`
}
type PlannedNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	InboundID string `json:"inbound_id"`
}
type NodeTarget struct {
	InboundID    string `json:"inbound_id"`
	InstanceName string `json:"instance_name"`
	CoreType     string `json:"core_type"`
	Endpoint     string `json:"endpoint"`
	Tag          string `json:"tag"`
	Protocol     string `json:"protocol"`
	CredentialID string `json:"credential_id,omitempty"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
}
type NodePlan struct {
	Hash    string        `json:"hash"`
	Nodes   []PlannedNode `json:"nodes"`
	Targets []NodeTarget  `json:"targets"`
	Ready   bool          `json:"ready"`
}
type NodeResult struct {
	Result
	NodeCount   int `json:"node_count"`
	TargetCount int `json:"target_count"`
	ReadyCount  int `json:"ready_count"`
}

// PlanNodes is read-only; selecting a group snapshots its current members.
func (s Service) PlanNodes(ctx context.Context, d NodeRequest) (NodePlan, error) {
	tx, e := s.Store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return NodePlan{}, e
	}
	defer tx.Rollback()
	return s.planNodes(ctx, tx, d)
}
func (s Service) planNodes(ctx context.Context, q catalog.Query, d NodeRequest) (NodePlan, error) {
	p := NodePlan{Nodes: []PlannedNode{}, Targets: []NodeTarget{}, Ready: true}
	if len(d.NodeIDs)+len(d.GroupIDs) > 500 {
		return p, fmt.Errorf("最多选择 500 个节点或分组")
	}
	ids := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range d.NodeIDs {
		add(id)
	}
	for _, group := range d.GroupIDs {
		var enabled bool
		if e := q.QueryRowContext(ctx, "SELECT enabled FROM node_groups WHERE id=?", group).Scan(&enabled); e != nil || !enabled {
			return p, fmt.Errorf("节点组不存在或已停用")
		}
		rows, e := q.QueryContext(ctx, "SELECT node_id FROM node_group_members WHERE group_id=? ORDER BY sort_order,node_id", group)
		if e != nil {
			return p, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return p, e
			}
			add(id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return p, e
		}
	}
	if len(ids) == 0 || len(ids) > 500 {
		return p, fmt.Errorf("请选择 1–500 个代理节点（分组按当前成员展开）")
	}
	targets := map[string]bool{}
	assigned := map[string]bool{}
	if d.Subscriber.ID != "" {
		old, e := subscription.NodeAssignments(ctx, q, d.Subscriber.ID)
		if e != nil {
			return p, e
		}
		for _, n := range old {
			assigned[n.NodeID] = true
		}
	}
	// Hash resolved definitions as well as core identity: confirmation must match
	// exactly the nodes and targets shown by the preview, including group changes.
	definitions := []catalog.Node{}
	for _, id := range ids {
		if assigned[id] {
			continue
		}
		n, e := catalog.ResolveDefinition(ctx, q, id)
		if e != nil {
			return p, fmt.Errorf("节点不存在或无法解析")
		}
		if n.Status != "ready" || n.InboundID == nil {
			return p, fmt.Errorf("节点 %s 暂不可分配：%s", n.Name, n.Status)
		}
		definitions = append(definitions, n)
		p.Nodes = append(p.Nodes, PlannedNode{ID: id, Name: n.Name, InboundID: *n.InboundID})
		if targets[*n.InboundID] {
			continue
		}
		targets[*n.InboundID] = true
		t := NodeTarget{InboundID: *n.InboundID, Status: "create"}
		var valid bool
		e = q.QueryRowContext(ctx, `SELECT x.name,x.core_type,x.api_endpoint,b.tag,b.protocol,(b.present=1 AND b.review=0 AND b.retired=0 AND x.enabled=1 AND v.enabled=1) FROM catalog_inbounds b JOIN instances x ON x.id=b.instance_id JOIN servers v ON v.id=x.server_id WHERE b.id=?`, t.InboundID).Scan(&t.InstanceName, &t.CoreType, &t.Endpoint, &t.Tag, &t.Protocol, &valid)
		if e != nil {
			return p, e
		}
		if !valid {
			t.Status = "blocked"
			t.Reason = "节点来源或核心实例已停用、退役或需要复核"
		}
		if d.Subscriber.ID != "" && valid {
			e = q.QueryRowContext(ctx, `SELECT c.id FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id JOIN inbound_client_keys k ON k.credential_id=c.id AND k.inbound_id=b.inbound_id WHERE b.subscriber_id=? AND b.inbound_id=? AND b.enabled=1 AND c.status='available'`, d.Subscriber.ID, t.InboundID).Scan(&t.CredentialID)
			if e != nil && e != sql.ErrNoRows {
				return p, e
			}
			if e == nil {
				t.Status = "reuse"
				t.Reason = "复用此用户已有认证，不重复创建 Client"
			}
			if e == sql.ErrNoRows {
				var count int
				if e = q.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_provisioning WHERE subscriber_id=? AND inbound_id=?", d.Subscriber.ID, t.InboundID).Scan(&count); e != nil {
					return p, e
				}
				if count > 0 {
					t.Status = "blocked"
					t.Reason = "已有配置任务或已停用的绑定，请先在原任务处理"
				}
				if e = q.QueryRowContext(ctx, `SELECT count(*) FROM subscriber_node_tasks t JOIN subscriber_node_requests p ON p.request_id=t.request_id WHERE p.subscriber_id=? AND t.inbound_id=? AND p.status!='ready'`, d.Subscriber.ID, t.InboundID).Scan(&count); e != nil {
					return p, e
				}
				if count > 0 {
					t.Status = "blocked"
					t.Reason = "此节点来源已有待完成的节点配置任务，请先重试原任务"
				}
			}
		}
		if t.Status == "create" {
			switch {
			case t.CoreType == "xray" && (t.Protocol == "vless" || t.Protocol == "vmess" || t.Protocol == "trojan"):
				t.Reason = "创建独立认证并回读确认；同一来源的多个节点仅配置一次"
			case t.CoreType == "hysteria2" && t.Protocol == "hysteria2" && hysteriaReady(ctx, q, t.InboundID):
				t.Reason = "通过已确认的 HTTP 认证后端登记账户"
			default:
				t.Status = "blocked"
				t.Reason = "此节点尚无可用的动态用户配置能力；请先纳管已有原生用户或配置认证后端"
			}
		}
		if t.Status == "blocked" {
			p.Ready = false
		}
		p.Targets = append(p.Targets, t)
	}
	if len(p.Targets) > 50 {
		return p, fmt.Errorf("一次最多涉及 50 个不同的核心认证目标")
	}
	if len(p.Nodes) == 0 {
		return p, fmt.Errorf("所选节点均已分配，无需重复提交；如配置未完成，请重试原任务")
	}
	raw, _ := json.Marshal(struct {
		Subscriber string
		Nodes      []catalog.Node
		Targets    []NodeTarget
	}{d.Subscriber.ID, definitions, p.Targets})
	p.Hash = fmt.Sprintf("%x", sha256.Sum256(raw))
	return p, nil
}

// Keep the readiness check in the same read snapshot as the plan.
func hysteriaReady(ctx context.Context, q catalog.Query, inbound string) bool {
	var n int
	e := q.QueryRowContext(ctx, `SELECT count(*) FROM hysteria_auth_backends h JOIN catalog_inbounds b ON b.id=h.inbound_id JOIN instances i ON i.id=b.instance_id WHERE h.inbound_id=? AND h.enabled=1 AND h.confirmed=1 AND h.target_endpoint=i.api_endpoint AND h.target_tag=b.tag`, inbound).Scan(&n)
	return e == nil && n == 1
}

func (s Service) CreateNodes(ctx context.Context, d NodeRequest) (NodeResult, error) {
	r := NodeResult{Result: Result{RequestID: d.RequestID}}
	if !d.Confirmed || len(d.RequestID) < 16 || len(d.RequestID) > 100 || strings.ContainsAny(d.RequestID, "\r\n\x00") || d.PlanHash == "" {
		return r, fmt.Errorf("请先预览节点配置计划并确认")
	}
	raw, _ := json.Marshal(d)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	var old string
	e := s.Store.DB.QueryRowContext(ctx, "SELECT request_hash FROM subscriber_node_requests WHERE request_id=?", d.RequestID).Scan(&old)
	if e == nil {
		if old != hash {
			return r, fmt.Errorf("请求标识已用于其他参数")
		}
		return s.RetryNodes(ctx, d.RequestID)
	}
	if e != sql.ErrNoRows {
		return r, e
	}
	if strings.TrimSpace(d.Subscriber.Name) == "" && d.Subscriber.ID == "" {
		return r, fmt.Errorf("请填写订阅用户名称")
	}
	// A new user and all node authorizations/target tasks commit together. No core
	// operation occurs until this durable plan has committed.
	save := func(tx *sql.Tx, id string) error {
		plan, err := s.planNodes(ctx, tx, d)
		if err != nil {
			return err
		}
		if plan.Hash != d.PlanHash {
			return fmt.Errorf("节点、分组成员或目标状态已变化，请重新预览并确认")
		}
		if !plan.Ready {
			return fmt.Errorf("所选节点存在无法配置的认证目标，请先处理或取消选择")
		}
		var revision int64
		if err = tx.QueryRowContext(ctx, "SELECT quota_revision FROM subscribers WHERE id=?", id).Scan(&revision); err != nil {
			return err
		}
		data, _ := json.Marshal(plan)
		if _, err = tx.ExecContext(ctx, `INSERT INTO subscriber_node_requests(request_id,request_hash,subscriber_id,plan_json,requested_enabled,activation_revision,checked_at) VALUES(?,?,?,?,?,?,?)`, d.RequestID, hash, id, string(data), d.Subscriber.Enabled, revision, storage.Stamp(time.Now())); err != nil {
			return err
		}
		for _, n := range plan.Nodes {
			if _, err = tx.ExecContext(ctx, `INSERT INTO subscriber_node_grants(subscriber_id,node_id,inbound_id,created_at) VALUES(?,?,?,?) ON CONFLICT(subscriber_id,node_id) DO NOTHING`, id, n.ID, n.InboundID, storage.Stamp(time.Now())); err != nil {
				return err
			}
		}
		for _, t := range plan.Targets {
			sum := sha256.Sum256([]byte(d.RequestID + "\x00" + t.InboundID))
			child := fmt.Sprintf("node-%x", sum)
			if _, err = tx.ExecContext(ctx, "INSERT INTO subscriber_node_tasks(request_id,child_request_id,inbound_id) VALUES(?,?,?)", d.RequestID, child, t.InboundID); err != nil {
				return err
			}
		}
		if d.Subscriber.ID == "" {
			_, err = tx.ExecContext(ctx, "UPDATE subscribers SET node_authorization_required=1 WHERE id=?", id)
		}
		return err
	}
	if d.Subscriber.ID == "" {
		profile := d.Subscriber
		profile.Enabled = false
		_, e = subscription.SaveSubscriberWith(ctx, s.Store, profile, save)
	} else {
		// Adding node access does not edit the user's profile, quota or activation.
		tx, err := s.Store.DB.BeginTx(ctx, nil)
		if err != nil {
			return r, err
		}
		defer tx.Rollback()
		var enabled bool
		if e = tx.QueryRowContext(ctx, "SELECT enabled FROM subscribers WHERE id=?", d.Subscriber.ID).Scan(&enabled); e != nil {
			return r, e
		}
		d.Subscriber.Enabled = false // existing users are never automatically enabled
		if e = save(tx, d.Subscriber.ID); e == nil {
			e = tx.Commit()
		}
	}
	if e != nil {
		return r, e
	}
	return s.RetryNodes(ctx, d.RequestID)
}

func (s Service) RetryNodes(ctx context.Context, request string) (NodeResult, error) {
	r := NodeResult{Result: Result{RequestID: request, Status: "pending"}}
	var raw, state string
	var enabled bool
	var revision int64
	e := s.Store.DB.QueryRowContext(ctx, `SELECT subscriber_id,plan_json,requested_enabled,activation_revision,status FROM subscriber_node_requests WHERE request_id=?`, request).Scan(&r.SubscriberID, &raw, &enabled, &revision, &state)
	if e != nil {
		return r, e
	}
	var p NodePlan
	if e = json.Unmarshal([]byte(raw), &p); e != nil {
		return r, e
	}
	r.NodeCount = len(p.Nodes)
	r.TargetCount = len(p.Targets)
	if state == "ready" {
		r.Status = "ready"
		r.ReadyCount = r.TargetCount
		r.Message = "节点授权与认证配置已完成"
		return r, nil
	}
	// Bounded synchronous work; unfinished targets stay durable and retryable.
	work, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	// Least-recently attempted targets go first so an unavailable core cannot
	// starve later targets across bounded retries.
	attempts := map[string]string{}
	rows, e := s.Store.DB.QueryContext(work, "SELECT inbound_id,checked_at FROM subscriber_node_tasks WHERE request_id=?", request)
	if e != nil {
		return r, e
	}
	for rows.Next() {
		var id, at string
		if e = rows.Scan(&id, &at); e != nil {
			rows.Close()
			return r, e
		}
		attempts[id] = at
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, e
	}
	sort.SliceStable(p.Targets, func(i, j int) bool { return attempts[p.Targets[i].InboundID] < attempts[p.Targets[j].InboundID] })
	record := func(target, state, message string) {
		c, done := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer done()
		_, _ = s.Store.DB.ExecContext(c, "UPDATE subscriber_node_tasks SET status=?,message=?,checked_at=? WHERE request_id=? AND inbound_id=?", state, message, storage.Stamp(time.Now()), request, target)
	}
	for _, t := range p.Targets {
		if work.Err() != nil {
			break
		}
		var valid int
		e = s.Store.DB.QueryRowContext(work, `SELECT count(*) FROM catalog_inbounds b JOIN instances x ON x.id=b.instance_id JOIN servers v ON v.id=x.server_id WHERE b.id=? AND b.tag=? AND b.protocol=? AND x.api_endpoint=? AND b.present=1 AND b.review=0 AND b.retired=0 AND x.enabled=1 AND v.enabled=1`, t.InboundID, t.Tag, t.Protocol, t.Endpoint).Scan(&valid)
		if e != nil || valid != 1 {
			record(t.InboundID, "pending", "来源、实例状态或目标证据已变化，未向新目标写入")
			continue
		}
		if t.Status == "reuse" {
			var n int
			e = s.Store.DB.QueryRowContext(work, `SELECT count(*) FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id WHERE b.subscriber_id=? AND b.inbound_id=? AND b.credential_id=? AND b.enabled=1 AND c.status='available'`, r.SubscriberID, t.InboundID, t.CredentialID).Scan(&n)
			if e == nil && n == 1 {
				r.ReadyCount++
				record(t.InboundID, "ready", "已复用此用户的现有认证")
			} else {
				record(t.InboundID, "pending", "已有认证已停用、移除或改变，请先复核绑定")
			}
			continue
		}
		var child string
		if e = s.Store.DB.QueryRowContext(work, "SELECT child_request_id FROM subscriber_node_tasks WHERE request_id=? AND inbound_id=?", request, t.InboundID).Scan(&child); e != nil {
			return r, e
		}
		childCtx, done := context.WithTimeout(work, 4*time.Second)
		var prior string
		_ = s.Store.DB.QueryRowContext(childCtx, "SELECT status FROM subscriber_provisioning WHERE request_id=?", child).Scan(&prior)
		v, err := s.Create(childCtx, Request{RequestID: child, Subscriber: subscription.Subscriber{ID: r.SubscriberID}, InboundID: t.InboundID, Mode: "managed", Confirmed: true})
		if err == nil && v.Status == "ready" && prior == "ready" {
			// An incomplete parent may outlive a core restart. Read the actual
			// account again using the original encrypted Desired and precise target.
			// Do not bind again: that would overwrite an administrator's billing choice.
			err = s.recheckNodeClient(childCtx, child, t)
		}
		done()
		if err == nil && v.Status == "ready" {
			var n int
			e = s.Store.DB.QueryRowContext(work, `SELECT count(*) FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id JOIN subscriber_provisioning p ON p.credential_id=c.id AND p.request_id=? WHERE b.subscriber_id=? AND b.inbound_id=? AND b.enabled=1 AND c.status='available'`, child, r.SubscriberID, t.InboundID).Scan(&n)
			if e == nil && n == 1 {
				r.ReadyCount++
				record(t.InboundID, "ready", v.Message)
			} else {
				record(t.InboundID, "pending", "认证绑定已变化，请复核后重试")
			}
		} else if err == nil {
			record(t.InboundID, "pending", v.Message)
		} else {
			record(t.InboundID, "pending", "认证配置尚未完成；检查能力、目标与密钥后重试原任务")
		}
	}
	r.Message = fmt.Sprintf("已授权 %d 个节点；认证目标已完成 %d/%d。未完成项保留原任务重试，不重复创建账号", r.NodeCount, r.ReadyCount, r.TargetCount)
	// The administrator may disable/edit a pending user. Never undo that decision
	// on a later retry: activation is guarded by the saved profile/quota revision.
	finalCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer done()
	tx, e := s.Store.DB.BeginTx(finalCtx, nil)
	if e != nil {
		return r, e
	}
	defer tx.Rollback()
	if r.ReadyCount == r.TargetCount {
		r.Status = "ready"
		if enabled {
			_, e = tx.ExecContext(finalCtx, "UPDATE subscribers SET enabled=1,updated_at=? WHERE id=? AND quota_revision=?", storage.Stamp(time.Now()), r.SubscriberID, revision)
			if e != nil {
				return r, e
			}
		}
	}
	_, e = tx.ExecContext(finalCtx, "UPDATE subscriber_node_requests SET status=?,message=?,checked_at=? WHERE request_id=? AND status!='ready'", r.Status, r.Message, storage.Stamp(time.Now()), request)
	if e != nil {
		return r, e
	}
	return r, tx.Commit()
}

func (s Service) recheckNodeClient(ctx context.Context, child string, t NodeTarget) error {
	if t.CoreType == "hysteria2" {
		if !hysteriaReady(ctx, s.Store.DB, t.InboundID) {
			return fmt.Errorf("HTTP 认证后端当前不可用")
		}
		return nil
	}
	if t.CoreType != "xray" {
		return fmt.Errorf("当前核心不支持运行时回读")
	}
	var sealed string
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT desired_sealed FROM subscriber_provisioning WHERE request_id=? AND origin='managed'", child).Scan(&sealed); e != nil {
		return e
	}
	key, e := secretstore.Key(s.Config.ManagedKeyFile)
	if e != nil {
		return e
	}
	raw, e := secretstore.Open(key, sealed)
	if e != nil {
		return e
	}
	var client remote.Client
	if e = json.Unmarshal(raw, &client); e != nil {
		return e
	}
	var revision int64
	if e = s.Store.DB.QueryRowContext(ctx, "SELECT revision FROM remote_runtime_users WHERE inbound_id=? AND client_ref=?", t.InboundID, client.Reference).Scan(&revision); e != nil {
		return e
	}
	v, e := remote.Mutate(ctx, s.Store, s.Config.ManagedKeyFile, s.Config.AllowedTargets, remote.Mutation{Client: client, Action: "add", Revision: revision, RestoreAuthorized: true, ExpectedEndpoint: t.Endpoint, ExpectedTag: t.Tag, ExpectedProtocol: t.Protocol})
	if e != nil {
		return e
	}
	if v.Status != "runtime_verified" {
		return fmt.Errorf("运行时认证未确认")
	}
	return nil
}
