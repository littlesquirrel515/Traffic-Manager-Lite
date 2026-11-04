package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/preferred"
	"traffic-manager-lite/internal/storage"
	"unicode/utf8"
)

type PreferredRequest struct {
	TemplateNodeID string `json:"template_node_id"`
	GroupID        string `json:"group_id"`
	GroupName      string `json:"group_name"`
	Prefix         string `json:"prefix"`
	Input          string `json:"input"`
	Port           int    `json:"port"`
	Limit          int    `json:"limit"`
	RequestID      string `json:"request_id"`
	PlanHash       string `json:"plan_hash"`
	Confirmed      bool   `json:"confirmed"`
}
type PreferredEntry struct {
	preferred.Address
	Name           string `json:"name"`
	Fingerprint    string `json:"fingerprint"`
	ExistingNodeID string `json:"existing_node_id,omitempty"`
}
type PreferredPlan struct {
	Hash            string           `json:"hash"`
	Template        Node             `json:"template"`
	GroupID         string           `json:"group_id"`
	GroupName       string           `json:"group_name"`
	Entries         []PreferredEntry `json:"entries"`
	Invalid         int              `json:"invalid"`
	Duplicates      int              `json:"duplicates"`
	Truncated       int              `json:"truncated"`
	ExistingMembers []string         `json:"existing_members"`
}
type PreferredResult struct {
	GroupID string   `json:"group_id"`
	NodeIDs []string `json:"node_ids"`
	Created int      `json:"created"`
	Reused  int      `json:"reused"`
}

func normalizedProfile(p core.NodeProfile) string {
	p = clean(p)
	p.Name = ""
	p.Sort = 0
	p.Enabled = true
	return encode(p)
}
func clipped(s string, max int) string {
	for len(s) > max {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}
func PlanPreferred(ctx context.Context, q Query, d PreferredRequest) (PreferredPlan, error) {
	p := PreferredPlan{Entries: []PreferredEntry{}, ExistingMembers: []string{}}
	n, e := ResolveDefinition(ctx, q, d.TemplateNodeID)
	if e != nil {
		return p, fmt.Errorf("请选择已保存的模板节点")
	}
	if !PreferredTemplate(n) {
		return p, fmt.Errorf("模板须为配置完整的 TLS VLESS/VMess/Trojan WS、XHTTP 或 gRPC 节点；不支持 REALITY/UDP 直接套用 CDN")
	}
	if len(d.Prefix) > 80 || strings.ContainsAny(d.Prefix, "\r\n\x00") {
		return p, fmt.Errorf("名称前缀无效")
	}
	parsed, e := preferred.Parse(d.Input, d.Port, d.Limit)
	if e != nil {
		return p, e
	}
	p.Template = n
	p.GroupID = d.GroupID
	p.GroupName = strings.TrimSpace(d.GroupName)
	p.Invalid = parsed.Invalid
	p.Duplicates = parsed.Duplicates
	p.Truncated = parsed.Truncated
	var groupState string
	if d.GroupID != "" {
		var enabled bool
		var sort int
		var description string
		if e = q.QueryRowContext(ctx, "SELECT name,description,enabled,sort_order FROM node_groups WHERE id=?", d.GroupID).Scan(&p.GroupName, &description, &enabled, &sort); e != nil {
			return p, fmt.Errorf("目标分组不存在")
		}
		groupState = encode([]any{p.GroupName, description, enabled, sort})
		rows, e := q.QueryContext(ctx, "SELECT node_id FROM node_group_members WHERE group_id=? ORDER BY sort_order,node_id", d.GroupID)
		if e != nil {
			return p, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return p, e
			}
			p.ExistingMembers = append(p.ExistingMembers, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return p, e
		}
	} else if !validName(p.GroupName) {
		return p, fmt.Errorf("请填写新分组名称")
	}
	prefix := strings.TrimSpace(d.Prefix)
	if prefix == "" {
		prefix = n.Name
	}
	for index, a := range parsed.Entries {
		profile := n.Profile
		profile.Address = a.Address
		profile.Port = a.Port
		sum := sha256.Sum256([]byte(normalizedProfile(profile)))
		entry := PreferredEntry{Address: a, Fingerprint: fmt.Sprintf("%x", sum)}
		suffix := a.Note
		if suffix == "" {
			suffix = a.Address
		}
		entry.Name = clipped(fmt.Sprintf("%s-%02d %s", prefix, index+1, suffix), 190)
		e = q.QueryRowContext(ctx, "SELECT node_id FROM preferred_generated_nodes WHERE template_node_id=? AND fingerprint=?", n.ID, entry.Fingerprint).Scan(&entry.ExistingNodeID)
		if e != nil && e != sql.ErrNoRows {
			return p, e
		}
		if e == nil {
			old, e := ResolveDefinition(ctx, q, entry.ExistingNodeID)
			if e != nil || old.Status != "ready" || normalizedProfile(old.Profile) != normalizedProfile(profile) {
				return p, fmt.Errorf("已有生成节点已停用或被人工修改，未覆盖；请先处理原节点")
			}
			entry.Name = old.Name
		}
		p.Entries = append(p.Entries, entry)
	}
	seen := map[string]bool{}
	for _, id := range p.ExistingMembers {
		seen[id] = true
	}
	total := len(seen)
	for _, entry := range p.Entries {
		if entry.ExistingNodeID == "" || !seen[entry.ExistingNodeID] {
			total++
			seen[entry.ExistingNodeID] = true
		}
	}
	if total > 500 {
		return p, fmt.Errorf("生成后分组将超过500个节点，请减少候选数量")
	}
	raw := encode(struct {
		Plan       PreferredPlan
		GroupState string
	}{p, groupState})
	p.Hash = fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	return p, nil
}
func GeneratePreferred(ctx context.Context, s *storage.Store, d PreferredRequest) (PreferredResult, error) {
	result := PreferredResult{NodeIDs: []string{}}
	if !d.Confirmed || len(d.RequestID) < 16 || len(d.RequestID) > 64 || d.PlanHash == "" {
		return result, fmt.Errorf("请预览并确认生成计划")
	}
	for _, c := range d.RequestID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return result, fmt.Errorf("请求标识无效")
		}
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(encode(d))))
	var prior, raw string
	e = tx.QueryRowContext(ctx, "SELECT request_hash,result_json FROM preferred_generation_requests WHERE request_id=?", d.RequestID).Scan(&prior, &raw)
	if e == nil {
		if prior != hash {
			return result, fmt.Errorf("请求标识已用于其他计划")
		}
		e = json.Unmarshal([]byte(raw), &result)
		return result, e
	}
	if e != sql.ErrNoRows {
		return result, e
	}
	p, e := PlanPreferred(ctx, tx, d)
	if e != nil {
		return result, e
	}
	if p.Hash != d.PlanHash {
		return result, fmt.Errorf("模板、候选地址或分组已变化，请重新预览")
	}
	group := p.GroupID
	if group == "" {
		group = ID()
		if _, e = tx.ExecContext(ctx, "INSERT INTO node_groups VALUES(?,?,?,1,0,?,?)", group, p.GroupName, "优选候选；未进行客户端线路测速", stamp(), stamp()); e != nil {
			return result, e
		}
	}
	result.GroupID = group
	members := map[string]bool{}
	for _, id := range p.ExistingMembers {
		members[id] = true
	}
	position := len(p.ExistingMembers)
	for _, entry := range p.Entries {
		id := entry.ExistingNodeID
		if id == "" {
			c, e := createPreferredVariant(ctx, tx, p.Template, entry.Address, entry.Name)
			if e != nil {
				return result, e
			}
			id = c.NodeID
			if _, e = tx.ExecContext(ctx, "INSERT INTO preferred_generated_nodes VALUES(?,?,?)", p.Template.ID, entry.Fingerprint, id); e != nil {
				return result, e
			}
			result.Created++
		} else {
			result.Reused++
		}
		if !members[id] {
			if _, e = tx.ExecContext(ctx, "INSERT INTO node_group_members VALUES(?,?,?)", group, id, position); e != nil {
				return result, e
			}
			position++
			members[id] = true
		}
		result.NodeIDs = append(result.NodeIDs, id)
	}
	if _, e = tx.ExecContext(ctx, "UPDATE node_groups SET updated_at=? WHERE id=?", stamp(), group); e != nil {
		return result, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO preferred_generation_requests VALUES(?,?,?,?)", d.RequestID, hash, encode(result), stamp()); e != nil {
		return result, e
	}
	return result, tx.Commit()
}

func createPreferredVariant(ctx context.Context, tx *sql.Tx, template Node, address preferred.Address, name string) (ConnectionResult, error) {
	// Preserve effective template overrides; external fields alone vary by address.
	binding, e := ReadBinding(ctx, tx, *template.BindingID)
	if e != nil {
		return ConnectionResult{}, e
	}
	overrides := map[string]Override{}
	external := map[string]bool{"address": true, "port": true, "host": true, "sni": true}
	for k, v := range binding.Overrides {
		if !external[k] && v.Mode != "inherit" {
			overrides[k] = v
		}
	}
	for k, v := range template.Overrides {
		if !external[k] && v.Mode != "inherit" {
			overrides[k] = v
		}
	}
	baseEndpoint, e := ReadEndpoint(ctx, tx, *template.EndpointID)
	if e != nil {
		return ConnectionResult{}, e
	}
	profile := map[string]any{}
	for k, v := range baseEndpoint.Profile {
		profile[k] = v
	}
	profile["address"] = address.Address
	profile["port"] = address.Port
	profile["host"] = template.Profile.Host
	profile["sni"] = template.Profile.SNI
	c, e := createConnection(ctx, tx, Connection{InboundID: *template.InboundID, Name: name, Description: "优选候选（网络未验证）：" + address.Note, Kind: "preferred_ip", Enabled: true, Profile: profile})
	if e != nil {
		return ConnectionResult{}, e
	}
	id := c.NodeID
	if _, e = tx.ExecContext(ctx, "UPDATE node_profiles SET overrides_json=?,tags_json=? WHERE id=?", encode(overrides), encode(template.Tags), id); e != nil {
		return ConnectionResult{}, e
	}
	resolved, e := ResolveDefinition(ctx, tx, id)
	if e != nil || resolved.Status != "ready" {
		return ConnectionResult{}, fmt.Errorf("生成节点参数校验失败，未保存任何生成对象")
	}
	expected := template.Profile
	expected.Address = address.Address
	expected.Port = address.Port
	if normalizedProfile(resolved.Profile) != normalizedProfile(expected) {
		return ConnectionResult{}, fmt.Errorf("模板参数无法完整继承，未保存任何生成对象")
	}
	return c, nil
}

func PreferredTemplate(n Node) bool {
	return n.Status == "ready" && n.InboundID != nil && n.BindingID != nil && n.EndpointID != nil && n.Profile.TLS && n.Profile.SNI != "" && !n.Profile.Reality && (n.Profile.Transport == "ws" || n.Profile.Transport == "xhttp" || n.Profile.Transport == "grpc") && (n.Profile.Protocol == "vless" || n.Profile.Protocol == "vmess" || n.Profile.Protocol == "trojan")
}
