package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"traffic-manager-lite/internal/storage"
)

type Binding struct {
	ID               string              `json:"id"`
	InboundID        string              `json:"inbound_id"`
	EndpointID       string              `json:"endpoint_id"`
	Enabled          bool                `json:"enabled"`
	Sort             int                 `json:"sort_order"`
	Overrides        map[string]Override `json:"overrides"`
	ValidationStatus string              `json:"validation_status"`
	Revision         int64               `json:"revision"`
	ConfigHash       string              `json:"config_hash"`
}
type Connection struct {
	Description string              `json:"description"`
	Kind        string              `json:"endpoint_type"`
	RequestID   string              `json:"request_id,omitempty"`
	InboundID   string              `json:"inbound_id"`
	EndpointID  string              `json:"endpoint_id"`
	Name        string              `json:"name"`
	Profile     map[string]any      `json:"profile"`
	Overrides   map[string]Override `json:"overrides"`
	Enabled     bool                `json:"enabled"`
	Sort        int                 `json:"sort_order"`
}
type ConnectionResult struct {
	EndpointID       string `json:"endpoint_id"`
	BindingID        string `json:"binding_id"`
	NodeID           string `json:"node_id"`
	ValidationStatus string `json:"validation_status"`
	NetworkStatus    string `json:"network_status"`
	BindingRevision  int64  `json:"binding_revision"`
}

func ReadBinding(ctx context.Context, q Query, id string) (Binding, error) {
	var b Binding
	var raw string
	e := q.QueryRowContext(ctx, `SELECT id,inbound_id,endpoint_id,enabled,sort_order,overrides_json,validation_status,revision,validated_config_hash FROM inbound_endpoint_bindings WHERE id=?`, id).Scan(&b.ID, &b.InboundID, &b.EndpointID, &b.Enabled, &b.Sort, &raw, &b.ValidationStatus, &b.Revision, &b.ConfigHash)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &b.Overrides)
	}
	return b, e
}

// EnsureBinding is also used by legacy creation and readonly configuration sync.
// Mutable names/ports/addresses never participate in business identity.
func EnsureBinding(ctx context.Context, q Query, inbound, endpoint string) (string, error) {
	var id string
	e := q.QueryRowContext(ctx, "SELECT id FROM inbound_endpoint_bindings WHERE inbound_id=? AND endpoint_id=?", inbound, endpoint).Scan(&id)
	if e == nil {
		return id, nil
	}
	if e != sql.ErrNoRows {
		return "", e
	}
	id = ID()
	_, e = q.ExecContext(ctx, `INSERT INTO inbound_endpoint_bindings(id,inbound_id,endpoint_id,validated_config_hash,created_at,updated_at) SELECT ?,id,?,config_hash,?,? FROM catalog_inbounds WHERE id=?`, id, endpoint, stamp(), stamp(), inbound)
	if e != nil {
		return "", e
	}
	if _, e = ReadBinding(ctx, q, id); e != nil {
		return "", e
	}
	return id, nil
}
func bindingNode(ctx context.Context, q Query, id, name string) (string, error) {
	var node string
	e := q.QueryRowContext(ctx, "SELECT id FROM node_profiles WHERE binding_id=? ORDER BY CASE WHEN variant_key='default' THEN 0 ELSE 1 END,created_at,id LIMIT 1", id).Scan(&node)
	if e == nil {
		return node, nil
	}
	if e != sql.ErrNoRows {
		return "", e
	}
	b, e := ReadBinding(ctx, q, id)
	if e != nil {
		return "", e
	}
	node = ID()
	_, e = q.ExecContext(ctx, `INSERT INTO node_profiles(id,inbound_id,endpoint_id,binding_id,variant_key,name,source_type,enabled,sort_order,created_at,updated_at) VALUES(?,?,?,?,'default',?,'discovered',1,?,?,?)`, node, b.InboundID, b.EndpointID, id, name, b.Sort, stamp(), stamp())
	return node, e
}
func CreateConnection(ctx context.Context, s *storage.Store, d Connection) (ConnectionResult, error) {
	var result ConnectionResult
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	requestHash := fmt.Sprintf("%x", sha256.Sum256([]byte(encode(d))))
	if d.RequestID != "" {
		if len(d.RequestID) < 16 || len(d.RequestID) > 64 {
			return result, fmt.Errorf("幂等请求标识无效")
		}
		for _, c := range d.RequestID {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return result, fmt.Errorf("幂等请求标识无效")
			}
		}
		var hash, binding, node string
		err := tx.QueryRowContext(ctx, "SELECT request_hash,binding_id FROM endpoint_connection_requests WHERE request_id=?", d.RequestID).Scan(&hash, &binding)
		if err == nil {
			if hash != requestHash {
				return result, fmt.Errorf("同一幂等请求不能用于不同入口参数")
			}
			if e = tx.QueryRowContext(ctx, "SELECT id FROM node_profiles WHERE binding_id=? ORDER BY created_at,id LIMIT 1", binding).Scan(&node); e != nil {
				return result, e
			}
			b, e := ReadBinding(ctx, tx, binding)
			if e != nil {
				return result, e
			}
			n, e := ResolveDefinition(ctx, tx, node)
			if e != nil {
				return result, e
			}
			return ConnectionResult{b.EndpointID, b.ID, node, n.Status, "unknown", b.Revision}, nil
		}
		if err != sql.ErrNoRows {
			return result, err
		}
	}
	var present, review, retired bool
	if e = tx.QueryRowContext(ctx, "SELECT present,review,retired FROM catalog_inbounds WHERE id=?", d.InboundID).Scan(&present, &review, &retired); e != nil {
		return result, fmt.Errorf("请选择真实入站")
	}
	if !present || review || retired {
		return result, fmt.Errorf("入站缺失、待确认或已退役，不能创建可发布入口")
	}
	endpoint := d.EndpointID
	if endpoint == "" {
		endpoint, e = saveEndpoint(ctx, tx, Endpoint{Name: d.Name, Profile: d.Profile, Enabled: true, Sort: d.Sort, Description: d.Description, Kind: d.Kind})
		if e != nil {
			return result, e
		}
	} else {
		if _, e = ReadEndpoint(ctx, tx, endpoint); e != nil {
			return result, e
		}
	}
	var existing string
	e = tx.QueryRowContext(ctx, "SELECT id FROM inbound_endpoint_bindings WHERE inbound_id=? AND endpoint_id=?", d.InboundID, endpoint).Scan(&existing)
	if e != nil && e != sql.ErrNoRows {
		return result, e
	}
	id, e := EnsureBinding(ctx, tx, d.InboundID, endpoint)
	if e != nil {
		return result, e
	}
	if existing == "" {
		if e = validateBindingOverrides(d.Overrides); e != nil {
			return result, e
		}
		if d.Overrides == nil {
			d.Overrides = map[string]Override{}
		}
		_, e = tx.ExecContext(ctx, "UPDATE inbound_endpoint_bindings SET enabled=?,sort_order=?,overrides_json=? WHERE id=?", d.Enabled, d.Sort, encode(d.Overrides), id)
		if e != nil {
			return result, e
		}
	}
	ep, e := ReadEndpoint(ctx, tx, endpoint)
	if e != nil {
		return result, e
	}
	name := d.Name
	if !validName(name) {
		name = ep.Name
	}
	node, e := bindingNode(ctx, tx, id, name)
	if e != nil {
		return result, e
	}
	definition, e := ResolveDefinition(ctx, tx, node)
	if e != nil {
		return result, e
	}
	if existing == "" && d.Enabled && definition.Status != "ready" {
		return result, fmt.Errorf("启用入口的客户端配置不完整或不兼容：%s；%s", definition.Status, strings.Join(definition.Profile.Limitations, "；"))
	}
	b, e := ReadBinding(ctx, tx, id)
	if e != nil {
		return result, e
	}
	result = ConnectionResult{endpoint, id, node, definition.Status, "unknown", b.Revision}
	if d.RequestID != "" {
		if _, e = tx.ExecContext(ctx, "INSERT INTO endpoint_connection_requests VALUES(?,?,?)", d.RequestID, requestHash, id); e != nil {
			return result, e
		}
	}
	if existing == "" {
		_, e = tx.ExecContext(ctx, "INSERT INTO endpoint_binding_audit(binding_id,action,summary_json,created_at) VALUES(?,'create',?,?)", id, encode(result), stamp())
		if e != nil {
			return result, e
		}
	}
	return result, tx.Commit()
}
func validateBindingOverrides(v map[string]Override) error {
	if len(v) > len(endpointFields) {
		return fmt.Errorf("入口覆盖字段过多")
	}
	for key, ov := range v {
		if !endpointFields[key] || ov.Mode != "inherit" && ov.Mode != "clear" && ov.Mode != "set" {
			return fmt.Errorf("入口覆盖字段或模式无效")
		}
		if key == "xhttp_mode" && ov.Mode == "set" && ov.Value != "" && ov.Value != "auto" && ov.Value != "stream-one" && ov.Value != "stream-up" && ov.Value != "packet-up" {
			return fmt.Errorf("XHTTP 模式无效")
		}
	}
	return nil
}

// UpdateBinding never accepts a different inbound or endpoint identity.
func UpdateBinding(ctx context.Context, s *storage.Store, d Binding, confirm bool) (Binding, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Binding{}, e
	}
	defer tx.Rollback()
	old, e := ReadBinding(ctx, tx, d.ID)
	if e != nil {
		return old, e
	}
	if old.Revision != d.Revision {
		return old, fmt.Errorf("绑定修订已改变，请刷新")
	}
	if d.InboundID != old.InboundID || d.EndpointID != old.EndpointID {
		return old, fmt.Errorf("不能在普通编辑中改变入站归属")
	}
	if e = validateBindingOverrides(d.Overrides); e != nil {
		return old, e
	}
	if d.Overrides == nil {
		d.Overrides = map[string]Override{}
	}
	status := old.ValidationStatus
	hash := old.ConfigHash
	if confirm {
		var present, review, retired bool
		if e = tx.QueryRowContext(ctx, "SELECT config_hash,present,review,retired FROM catalog_inbounds WHERE id=?", old.InboundID).Scan(&hash, &present, &review, &retired); e != nil {
			return old, e
		}
		if !present || review || retired {
			return old, fmt.Errorf("请先确认有效入站身份及协议凭据")
		}
		status = "ready"
	}
	_, e = tx.ExecContext(ctx, `UPDATE inbound_endpoint_bindings SET enabled=?,sort_order=?,overrides_json=?,validation_status=?,validated_config_hash=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, d.Enabled, d.Sort, encode(d.Overrides), status, hash, stamp(), d.ID, d.Revision)
	if e != nil {
		return old, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id FROM node_profiles WHERE binding_id=?", d.ID)
	if e != nil {
		return old, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return old, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return old, e
	}
	for _, id := range ids {
		if _, e = ResolveDefinition(ctx, tx, id); e != nil {
			return old, e
		}
	}
	after, e := ReadBinding(ctx, tx, d.ID)
	if e != nil {
		return old, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO endpoint_binding_audit(binding_id,action,summary_json,created_at) VALUES(?,'update',?,?)", d.ID, encode(map[string]any{"before": old, "after": after, "mapping_confirmed": confirm}), stamp())
	if e != nil {
		return old, e
	}
	return after, tx.Commit()
}

// MoveNode is an explicit business-resource migration. Keep subscriptions/groups,
// pause publication, and never migrate core credentials or historical counters.
func MoveNode(ctx context.Context, s *storage.Store, node, binding, revision string, confirmed bool) (Node, error) {
	if !confirmed {
		return Node{}, fmt.Errorf("迁移须确认暂停发布及重新核对凭据授权")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Node{}, e
	}
	defer tx.Rollback()
	n, e := ResolveDefinition(ctx, tx, node)
	if e != nil {
		return n, e
	}
	if n.Revision != revision {
		return n, fmt.Errorf("节点修订已改变，请重新预览")
	}
	b, e := ReadBinding(ctx, tx, binding)
	if e != nil {
		return n, e
	}
	var present, review, retired bool
	if e = tx.QueryRowContext(ctx, "SELECT present,review,retired FROM catalog_inbounds WHERE id=?", b.InboundID).Scan(&present, &review, &retired); e != nil {
		return n, e
	}
	if !present || review || retired {
		return n, fmt.Errorf("目标入站身份不可用")
	}
	_, e = tx.ExecContext(ctx, "UPDATE node_profiles SET inbound_id=?,endpoint_id=?,binding_id=?,variant_key=?,source_type='discovered',profile_json='{}',enabled=0,updated_at=? WHERE id=?", b.InboundID, b.EndpointID, b.ID, "moved:"+node, stamp(), node)
	if e != nil {
		return n, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO endpoint_binding_audit(binding_id,action,summary_json,created_at) VALUES(?,'node_move',?,?)", b.ID, encode(map[string]any{"node_id": node, "previous_binding": n.BindingID, "publication": "paused", "credential_grants": "unchanged"}), stamp())
	if e != nil {
		return n, e
	}
	after, e := ResolveDefinition(ctx, tx, node)
	if e != nil {
		return n, e
	}
	return after, tx.Commit()
}
