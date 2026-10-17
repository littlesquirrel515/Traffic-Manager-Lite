// Package catalog owns credential-free node definitions, endpoints and groups.
// It never calls a core API, Docker, or a configuration writer.
package catalog

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"net"
	"strconv"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

var ErrReferenced = errors.New("对象仍被引用，请先解除关联")

func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func stamp() string       { return storage.Stamp(time.Now()) }
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
func clean(p core.NodeProfile) core.NodeProfile {
	p.UUID = ""
	p.Password = ""
	p.UserKey = ""
	return p
}

type Override struct {
	Mode  string `json:"mode"`
	Value any    `json:"value,omitempty"`
}
type Endpoint struct {
	Bindings      int            `json:"binding_count"`
	Description   string         `json:"description"`
	Kind          string         `json:"endpoint_type"`
	Subscriptions int            `json:"subscriptions"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Profile       map[string]any `json:"profile"`
	Enabled       bool           `json:"enabled"`
	Sort          int            `json:"sort_order"`
	References    int            `json:"references"`
	ConfirmShared bool           `json:"confirm_shared"`
}
type Node struct {
	BindingID   *string             `json:"binding_id"`
	Variant     string              `json:"variant_key"`
	Revision    string              `json:"revision"`
	ID          string              `json:"id"`
	InboundID   *string             `json:"inbound_id"`
	EndpointID  *string             `json:"endpoint_id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Source      string              `json:"source_type"`
	Profile     core.NodeProfile    `json:"profile"`
	Overrides   map[string]Override `json:"overrides"`
	Tags        []string            `json:"tags"`
	Enabled     bool                `json:"enabled"`
	Sort        int                 `json:"sort_order"`
	Status      string              `json:"status"`
	Sources     map[string]string   `json:"field_sources"`
	References  int                 `json:"references"`
}
type Group struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Sort        int      `json:"sort_order"`
	NodeIDs     []string `json:"node_ids"`
	References  int      `json:"references"`
}
type Query interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var fields = map[string]bool{"protocol": true, "address": true, "port": true, "sni": true, "host": true, "path": true, "service_name": true, "public_key": true, "short_id": true, "fingerprint": true, "flow": true, "method": true, "transport": true, "tls": true, "reality": true, "xhttp_mode": true, "alpn": true}
var endpointFields = map[string]bool{"address": true, "port": true, "sni": true, "host": true, "tls": true, "fingerprint": true, "alpn": true, "path": true, "service_name": true, "xhttp_mode": true, "public_key": true, "short_id": true, "flow": true}

func validName(v string) bool {
	return strings.TrimSpace(v) != "" && len(v) <= 200 && !strings.ContainsAny(v, "\r\n\x00")
}
func validate(p core.NodeProfile) error {
	if p.Port < 0 || p.Port > 65535 || len(p.Address) > 253 || len(p.Path) > 2048 || len(p.SNI) > 253 || len(p.Host) > 253 || len(p.ALPN) > 8 {
		return fmt.Errorf("连接参数超出限制")
	}
	if p.Address != "" && net.ParseIP(strings.Trim(p.Address, "[]")) == nil {
		labels := strings.Split(strings.TrimSuffix(p.Address, "."), ".")
		allNumeric := len(labels) == 4
		for _, label := range labels {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return fmt.Errorf("连接地址必须为有效域名或 IP")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return fmt.Errorf("连接地址必须为有效域名或 IP")
				}
				if c < '0' || c > '9' {
					allNumeric = false
				}
			}
		}
		if allNumeric {
			return fmt.Errorf("IPv4 地址无效")
		}
	}
	for _, v := range []string{p.SNI, p.Host, p.Path, p.ServiceName, p.Fingerprint, p.Flow, p.XHTTPMode} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("参数含无效字符")
		}
	}
	if p.SNI != "" {
		if e := validate(core.NodeProfile{Address: p.SNI}); e != nil {
			return fmt.Errorf("SNI 必须为有效域名或 IP")
		}
	}
	if p.Host != "" {
		host := p.Host
		if h, port, e := net.SplitHostPort(host); e == nil {
			number, err := strconv.Atoi(port)
			if err != nil || number < 1 || number > 65535 {
				return fmt.Errorf("Host 端口无效")
			}
			host = h
		}
		if e := validate(core.NodeProfile{Address: host}); e != nil {
			return fmt.Errorf("Host 必须为有效域名、IP 或带端口的 authority")
		}
	}
	for _, v := range p.ALPN {
		if v == "" || len(v) > 255 || strings.ContainsAny(v, ",\r\n\x00") {
			return fmt.Errorf("无效 ALPN")
		}
	}
	switch p.Protocol {
	case "", "vless", "vmess", "trojan", "shadowsocks", "hysteria2", "anytls":
	default:
		return fmt.Errorf("不支持的协议")
	}
	return nil
}

// ResolveDefinition applies explicit tri-state overrides. Authentication has no
// writable field here: it is supplied later by the subscriber resolver.
func ResolveDefinition(ctx context.Context, q Query, id string) (Node, error) {
	var n Node
	var base, over, tags string
	var present, review, endpointEnabled bool
	e := q.QueryRowContext(ctx, `SELECT id,inbound_id,endpoint_id,name,description,source_type,profile_json,overrides_json,tags_json,enabled,sort_order,binding_id,variant_key FROM node_profiles WHERE id=?`, id).Scan(&n.ID, &n.InboundID, &n.EndpointID, &n.Name, &n.Description, &n.Source, &base, &over, &tags, &n.Enabled, &n.Sort, &n.BindingID, &n.Variant)
	if e != nil {
		return n, e
	}
	n.Sources = map[string]string{}
	m := map[string]any{}
	sourceProtocol := ""
	if n.InboundID != nil {
		e = q.QueryRowContext(ctx, "SELECT profile_json,present,review FROM catalog_inbounds WHERE id=?", *n.InboundID).Scan(&base, &present, &review)
		if e != nil {
			return n, e
		}
	}
	if e = json.Unmarshal([]byte(base), &m); e != nil {
		return n, e
	}
	if n.InboundID != nil {
		sourceProtocol, _ = m["protocol"].(string)
	}
	for k := range m {
		n.Sources[k] = n.Source
	}
	if n.EndpointID != nil {
		var raw string
		e = q.QueryRowContext(ctx, "SELECT profile_json,enabled FROM node_endpoints WHERE id=?", *n.EndpointID).Scan(&raw, &endpointEnabled)
		if e != nil {
			return n, e
		}
		v := map[string]any{}
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			return n, e
		}
		for k, x := range v {
			m[k] = x
			n.Sources[k] = "endpoint"
		}
	}
	bindingEnabled := true
	bindingStatus := "ready"
	if n.BindingID != nil {
		b, err := ReadBinding(ctx, q, *n.BindingID)
		if err != nil {
			return n, err
		}
		if n.InboundID == nil || n.EndpointID == nil || b.InboundID != *n.InboundID || b.EndpointID != *n.EndpointID {
			return n, fmt.Errorf("节点绑定归属不一致")
		}
		bindingEnabled, bindingStatus = b.Enabled, b.ValidationStatus
		for k, ov := range b.Overrides {
			switch ov.Mode {
			case "inherit":
			case "set":
				m[k] = ov.Value
				n.Sources[k] = "binding"
			case "clear":
				delete(m, k)
				n.Sources[k] = "binding_clear"
			default:
				return n, fmt.Errorf("绑定覆盖无效")
			}
		}
	}
	if e = json.Unmarshal([]byte(over), &n.Overrides); e != nil {
		return n, e
	}
	_ = json.Unmarshal([]byte(tags), &n.Tags)
	for k, v := range n.Overrides {
		if !fields[k] {
			return n, fmt.Errorf("禁止覆盖凭据或未知字段")
		}
		switch v.Mode {
		case "inherit":
		case "set":
			m[k] = v.Value
			n.Sources[k] = "node"
		case "clear":
			delete(m, k)
			n.Sources[k] = "node_clear"
		default:
			return n, fmt.Errorf("无效覆盖模式")
		}
	}
	if e = json.Unmarshal([]byte(encode(m)), &n.Profile); e != nil {
		return n, fmt.Errorf("字段类型无效")
	}
	n.Profile = clean(n.Profile)
	n.Profile.Name = n.Name
	n.Profile.Enabled = n.Enabled
	n.Profile.Sort = n.Sort
	if e = validate(n.Profile); e != nil {
		return n, e
	}
	n.Status = "ready"
	if len(n.Profile.Limitations) > 0 {
		n.Status = "incompatible"
	}
	if n.Profile.Address == "" || n.Profile.Port == 0 || n.Profile.Protocol == "" || n.Profile.TLS && n.Profile.SNI == "" || n.Profile.Reality && n.Profile.PublicKey == "" {
		n.Status = "incomplete"
	}
	if review {
		n.Status = "needs_review"
	}
	if bindingStatus == "needs_validation" {
		n.Status = "needs_validation"
	}
	if n.Source == "discovered" && n.BindingID == nil {
		n.Status = "inbound_unresolved"
	}
	if sourceProtocol != "" && sourceProtocol != n.Profile.Protocol {
		n.Status = "incompatible"
		n.Profile.Limitations = append(n.Profile.Limitations, "节点协议与来源入站协议不一致")
	}
	if n.InboundID != nil && !present {
		n.Status = "source_missing"
	}
	var retired bool
	if n.InboundID != nil {
		_ = q.QueryRowContext(ctx, "SELECT retired FROM catalog_inbounds WHERE id=?", *n.InboundID).Scan(&retired)
	}
	if retired {
		n.Status = "retired"
	}
	if !n.Enabled || !bindingEnabled || n.EndpointID != nil && !endpointEnabled {
		n.Status = "disabled"
	}
	_ = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM catalog_subscription_nodes WHERE node_id=?)+(SELECT count(*) FROM node_group_members WHERE node_id=?)`, id, id).Scan(&n.References)
	n.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(encode(n))))
	return n, nil
}
func Nodes(ctx context.Context, q Query) ([]Node, error) {
	rows, e := q.QueryContext(ctx, "SELECT id FROM node_profiles ORDER BY sort_order,name,id")
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Node{}
	for _, id := range ids {
		n, e := ResolveDefinition(ctx, q, id)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, nil
}
func SaveNode(ctx context.Context, s *storage.Store, n Node) (string, error) {
	if n.InboundID == nil {
		return "", fmt.Errorf("节点必须关联已登记入站；通过连接入口创建节点")
	}
	if !validName(n.Name) || len(n.Description) > 2000 || len(n.Tags) > 32 || len(n.Overrides) > 32 {
		return "", fmt.Errorf("节点字段无效")
	}
	for _, tag := range n.Tags {
		if !validName(tag) {
			return "", fmt.Errorf("标签无效")
		}
	}
	n.Profile = clean(n.Profile)
	n.Source = "discovered"
	if e := validate(n.Profile); e != nil {
		return "", e
	}
	if n.ID == "" {
		n.ID = ID()
		n.Variant = "variant:" + n.ID
	}
	if n.Overrides == nil {
		n.Overrides = map[string]Override{}
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var oldInbound *string
	err := tx.QueryRowContext(ctx, "SELECT inbound_id FROM node_profiles WHERE id=?", n.ID).Scan(&oldInbound)
	if err == nil && ((oldInbound == nil) != (n.InboundID == nil) || oldInbound != nil && *oldInbound != *n.InboundID) {
		return "", fmt.Errorf("普通节点编辑不能修改底层入站；请使用明确的迁移流程")
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if n.InboundID != nil {
		if n.EndpointID == nil {
			endpoint, e := saveEndpoint(ctx, tx, Endpoint{Name: n.Name, Profile: map[string]any{}, Enabled: true})
			if e != nil {
				return "", e
			}
			n.EndpointID = &endpoint
		}
		binding, e := EnsureBinding(ctx, tx, *n.InboundID, *n.EndpointID)
		if e != nil {
			return "", e
		}
		n.BindingID = &binding
	} else {
		n.BindingID = nil
	}
	if n.Variant == "" {
		n.Variant = "variant:" + n.ID
	}
	now := stamp()
	_, e = tx.ExecContext(ctx, `INSERT INTO node_profiles(id,inbound_id,endpoint_id,name,description,source_type,profile_json,overrides_json,tags_json,enabled,sort_order,created_at,updated_at,binding_id,variant_key) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET inbound_id=excluded.inbound_id,endpoint_id=excluded.endpoint_id,binding_id=excluded.binding_id,name=excluded.name,description=excluded.description,source_type=excluded.source_type,profile_json=excluded.profile_json,overrides_json=excluded.overrides_json,tags_json=excluded.tags_json,enabled=excluded.enabled,sort_order=excluded.sort_order,updated_at=excluded.updated_at`, n.ID, n.InboundID, n.EndpointID, n.Name, n.Description, n.Source, encode(n.Profile), encode(n.Overrides), encode(n.Tags), n.Enabled, n.Sort, now, now, n.BindingID, n.Variant)
	if e != nil {
		return "", e
	}
	if _, e = ResolveDefinition(ctx, tx, n.ID); e != nil {
		return "", e
	}
	return n.ID, tx.Commit()
}
func SaveEndpoint(ctx context.Context, s *storage.Store, n Endpoint) (string, error) {
	return saveEndpoint(ctx, s.DB, n)
}
func saveEndpoint(ctx context.Context, q Query, n Endpoint) (string, error) {
	profileCopy := make(map[string]any, len(n.Profile))
	for k, v := range n.Profile {
		profileCopy[k] = v
	}
	n.Profile = profileCopy
	if sni, ok := n.Profile["sni"].(string); ok && sni != "" {
		if _, explicit := n.Profile["tls"]; !explicit {
			n.Profile["tls"] = true
		}
	}
	if n.ID != "" && n.Kind == "" {
		var description string
		if e := q.QueryRowContext(ctx, "SELECT endpoint_type,description FROM node_endpoints WHERE id=?", n.ID).Scan(&n.Kind, &description); e != nil {
			return "", e
		}
		if n.Description == "" {
			n.Description = description
		}
	}
	if n.ID != "" {
		var refs int
		if e := q.QueryRowContext(ctx, "SELECT count(*) FROM node_profiles WHERE endpoint_id=?", n.ID).Scan(&refs); e != nil {
			return "", e
		}
		if refs > 0 && !n.ConfirmShared {
			return "", fmt.Errorf("共享入口影响 %d 个节点，请确认影响范围", refs)
		}
	}
	if n.Kind == "" {
		n.Kind = "custom"
	}
	if n.Kind != "custom" && n.Kind != "direct" && n.Kind != "cloudflare" && n.Kind != "preferred_ip" {
		return "", fmt.Errorf("入口类型无效")
	}
	if !validName(n.Name) || len(n.Description) > 2000 || len(n.Profile) > len(endpointFields) {
		return "", fmt.Errorf("入口无效")
	}
	for k := range n.Profile {
		if !endpointFields[k] {
			return "", fmt.Errorf("入口字段不允许")
		}
	}
	if mode, ok := n.Profile["xhttp_mode"]; ok && mode != "" && mode != "auto" && mode != "stream-one" && mode != "stream-up" && mode != "packet-up" {
		return "", fmt.Errorf("XHTTP 模式无效")
	}
	var p core.NodeProfile
	if e := json.Unmarshal([]byte(encode(n.Profile)), &p); e != nil {
		return "", e
	}
	if e := validate(p); e != nil {
		return "", e
	}
	if n.ID == "" {
		n.ID = ID()
	}
	now := stamp()
	_, e := q.ExecContext(ctx, `INSERT INTO node_endpoints(id,name,profile_json,enabled,sort_order,created_at,updated_at,description,endpoint_type) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,profile_json=excluded.profile_json,enabled=excluded.enabled,sort_order=excluded.sort_order,updated_at=excluded.updated_at,description=excluded.description,endpoint_type=excluded.endpoint_type`, n.ID, n.Name, encode(n.Profile), n.Enabled, n.Sort, now, now, n.Description, n.Kind)
	return n.ID, e
}
func SaveGroup(ctx context.Context, s *storage.Store, n Group) (string, error) {
	if !validName(n.Name) || len(n.Description) > 2000 || len(n.NodeIDs) > 500 {
		return "", fmt.Errorf("节点组无效")
	}
	if n.ID == "" {
		n.ID = ID()
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	now := stamp()
	_, e = tx.ExecContext(ctx, `INSERT INTO node_groups VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,enabled=excluded.enabled,sort_order=excluded.sort_order,updated_at=excluded.updated_at`, n.ID, n.Name, n.Description, n.Enabled, n.Sort, now, now)
	if e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM node_group_members WHERE group_id=?", n.ID); e != nil {
		return "", e
	}
	seen := map[string]bool{}
	for pos, id := range n.NodeIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, e = tx.ExecContext(ctx, "INSERT INTO node_group_members VALUES(?,?,?)", n.ID, id, pos); e != nil {
			return "", e
		}
	}
	return n.ID, tx.Commit()
}
func Delete(ctx context.Context, s *storage.Store, kind, id string) error {
	tables := map[string]string{"nodes": "node_profiles", "endpoints": "node_endpoints", "groups": "node_groups", "subscribers": "subscribers"}
	table, ok := tables[kind]
	if !ok {
		return fmt.Errorf("无效对象")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id=?", id)
	if e != nil && strings.Contains(e.Error(), "FOREIGN KEY") {
		return ErrReferenced
	}
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Sync runs in the same transaction as legacy discovery. Config presence is
// evidence of configuration only; runtime_state is deliberately unknown.
func Sync(ctx context.Context, tx *sql.Tx, instance int64, source, hash string, b []byte) error {
	rows, e := tx.QueryContext(ctx, "SELECT id,tag FROM catalog_inbounds WHERE instance_id=? AND present=1", instance)
	if e != nil {
		return e
	}
	type entry struct{ id, tag string }
	entries := []entry{}
	for rows.Next() {
		var v entry
		if e = rows.Scan(&v.id, &v.tag); e != nil {
			rows.Close()
			return e
		}
		entries = append(entries, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range entries {
		var count int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM node_profiles WHERE inbound_id=?", v.id).Scan(&count); e != nil {
			return e
		}
		if count == 0 {
			ep, err := saveEndpoint(ctx, tx, Endpoint{Name: v.tag, Profile: map[string]any{}, Enabled: true})
			if err != nil {
				return err
			}
			binding, err := EnsureBinding(ctx, tx, v.id, ep)
			if err != nil {
				return err
			}
			if _, err = bindingNode(ctx, tx, binding, v.tag); err != nil {
				return err
			}
		}
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO catalog_sync_runs(instance_id,config_hash,status,summary_json,created_at) VALUES(?,?,'success',?,?)", instance, hash, encode(map[string]any{"inbounds": len(entries), "runtime_state": "unknown"}), stamp())
	return e
}
