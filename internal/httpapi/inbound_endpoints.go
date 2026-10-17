package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"traffic-manager-lite/internal/catalog"
)

const inboundListQuery = `SELECT i.*,r.source_type,r.verification,r.revision registry_revision,x.name instance_name,x.core_type,x.server_id,s.name server_name,
 (SELECT count(*) FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id WHERE k.inbound_id=i.id AND c.status='available') client_count,
 (SELECT count(*) FROM inbound_endpoint_bindings b WHERE b.inbound_id=i.id) endpoint_count,
 (SELECT count(*) FROM node_profiles n WHERE n.inbound_id=i.id) node_count,
 (SELECT json_group_array(tag) FROM inbound_identity_aliases a WHERE a.inbound_id=i.id) historical_tags
 FROM catalog_inbounds i LEFT JOIN remote_inbound_registry r ON r.inbound_id=i.id JOIN instances x ON x.id=i.instance_id JOIN servers s ON s.id=x.server_id`

func (a *API) inboundEndpointRoutes(admin *http.ServeMux) {
	admin.HandleFunc("GET /api/v1/catalog/endpoint-bindings", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT b.*,i.tag,i.protocol,i.present,i.review,i.retired,x.name instance_name,e.name endpoint_name FROM inbound_endpoint_bindings b JOIN catalog_inbounds i ON i.id=b.inbound_id JOIN instances x ON x.id=i.instance_id JOIN node_endpoints e ON e.id=b.endpoint_id ORDER BY x.id,i.tag,e.name`)
	})
	admin.HandleFunc("POST /api/v1/catalog/nodes/{id}/move", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Binding   string `json:"binding_id"`
			Revision  string `json:"revision"`
			Confirmed bool   `json:"confirmed"`
		}
		if !body(w, r, &d) {
			return
		}
		v, e := catalog.MoveNode(r.Context(), a.Store, r.PathValue("id"), d.Binding, d.Revision, d.Confirmed)
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/inbounds/{id}", func(w http.ResponseWriter, r *http.Request) {
		rows, e := a.Store.Rows(r.Context(), inboundListQuery+" WHERE i.id=?", r.PathValue("id"))
		if e != nil || len(rows) == 0 {
			result(w, nil, fmt.Errorf("入站不存在"))
			return
		}
		respond(w, 200, rows[0])
	})
	admin.HandleFunc("GET /api/v1/catalog/inbound-management", func(w http.ResponseWriter, r *http.Request) { a.rows(w, r, inboundListQuery+" ORDER BY x.id,i.tag") })
	admin.HandleFunc("PATCH /api/v1/catalog/inbounds/{id}", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Name      string `json:"display_name"`
			Retired   bool   `json:"retired"`
			Confirmed bool   `json:"confirmed"`
		}
		if !body(w, r, &d) {
			return
		}
		if len(d.Name) > 200 || strings.ContainsAny(d.Name, "\r\n\x00") || !d.Confirmed {
			result(w, nil, fmt.Errorf("显示名称或退役操作需要确认"))
			return
		}
		res, e := a.Store.DB.ExecContext(r.Context(), "UPDATE catalog_inbounds SET display_name=?,retired=? WHERE id=?", d.Name, d.Retired, r.PathValue("id"))
		if e == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				e = fmt.Errorf("入站不存在")
			}
		}
		result(w, map[string]bool{"updated": e == nil}, e)
	})
	admin.HandleFunc("DELETE /api/v1/catalog/inbounds/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 409, map[string]string{"error": "入站是历史身份资源，请使用确认退役；绑定、节点、用户和历史记录保留"})
	})
	admin.HandleFunc("GET /api/v1/catalog/inbounds/{id}/clients", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT c.id,c.core_client_ref,c.status,c.source,c.verified_at FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id WHERE k.inbound_id=? ORDER BY core_client_ref,id`, r.PathValue("id"))
	})
	admin.HandleFunc("GET /api/v1/catalog/inbounds/{id}/endpoints", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT b.*,e.name,e.profile_json,e.description,e.endpoint_type,e.enabled endpoint_enabled,(SELECT count(*) FROM node_profiles n WHERE n.binding_id=b.id) node_count FROM inbound_endpoint_bindings b JOIN node_endpoints e ON e.id=b.endpoint_id WHERE b.inbound_id=? ORDER BY b.sort_order,e.name,b.id`, r.PathValue("id"))
	})
	for _, method := range []string{"POST /api/v1/catalog/connections", "POST /api/v1/catalog/inbounds/{id}/endpoints"} {
		admin.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			d := struct {
				catalog.Connection
				InboundCamel  string  `json:"inboundId"`
				EndpointCamel string  `json:"endpointId"`
				Address       *string `json:"address"`
				Port          *int    `json:"connectionPort"`
				Host          *string `json:"host"`
				SNI           *string `json:"sni"`
			}{Connection: catalog.Connection{Enabled: true}}
			if !body(w, r, &d) {
				return
			}
			if d.InboundCamel != "" {
				if d.InboundID != "" && d.InboundID != d.InboundCamel {
					result(w, nil, fmt.Errorf("入站参数冲突"))
					return
				}
				d.InboundID = d.InboundCamel
			}
			if d.EndpointCamel != "" {
				if d.EndpointID != "" && d.EndpointID != d.EndpointCamel {
					result(w, nil, fmt.Errorf("入口参数冲突"))
					return
				}
				d.EndpointID = d.EndpointCamel
			}
			if d.Profile == nil {
				d.Profile = map[string]any{}
			}
			flat := map[string]any{}
			if d.Address != nil {
				flat["address"] = *d.Address
			}
			if d.Port != nil {
				flat["port"] = *d.Port
			}
			if d.Host != nil {
				flat["host"] = *d.Host
			}
			if d.SNI != nil {
				flat["sni"] = *d.SNI
			}
			for key, value := range flat {
				if old, ok := d.Profile[key]; ok {
					a, _ := json.Marshal(old)
					b, _ := json.Marshal(value)
					if string(a) != string(b) {
						result(w, nil, fmt.Errorf("入口参数冲突"))
						return
					}
				}
				d.Profile[key] = value
			}
			if id := r.PathValue("id"); id != "" {
				if d.InboundID != "" && d.InboundID != id {
					result(w, nil, fmt.Errorf("表单入站与请求归属不一致"))
					return
				}
				d.InboundID = id
			}
			v, e := catalog.CreateConnection(r.Context(), a.Store, d.Connection)
			catalogResult(w, v, e)
		})
	}
	admin.HandleFunc("GET /api/v1/catalog/endpoint-bindings/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := catalog.ReadBinding(r.Context(), a.Store.DB, r.PathValue("id"))
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/endpoint-bindings/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, "SELECT action,summary_json,created_at FROM endpoint_binding_audit WHERE binding_id=? ORDER BY id DESC LIMIT 200", r.PathValue("id"))
	})
	admin.HandleFunc("POST /api/v1/catalog/inbounds/{id}/ack-source", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Revision  string `json:"config_hash"`
			Confirmed bool   `json:"confirmed"`
		}
		if !body(w, r, &d) {
			return
		}
		if !d.Confirmed {
			result(w, nil, fmt.Errorf("请确认已核对入站配置差异"))
			return
		}
		res, e := a.Store.DB.ExecContext(r.Context(), "UPDATE catalog_inbounds SET review=0 WHERE id=? AND config_hash=? AND present=1 AND retired=0", r.PathValue("id"), d.Revision)
		if e == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				e = fmt.Errorf("来源修订已改变或入站不可用")
			}
		}
		result(w, map[string]bool{"accepted": e == nil}, e)
	})
	admin.HandleFunc("PUT /api/v1/catalog/endpoint-bindings/{id}", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			catalog.Binding
			Confirmed bool `json:"confirmed_mapping"`
		}
		if !body(w, r, &d) {
			return
		}
		d.ID = r.PathValue("id")
		v, e := catalog.UpdateBinding(r.Context(), a.Store, d.Binding, d.Confirmed)
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/endpoints/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := catalog.ReadEndpoint(r.Context(), a.Store.DB, r.PathValue("id"))
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/endpoints/{id}/references", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT b.*,i.tag,i.protocol,x.name instance_name,(SELECT json_group_array(id) FROM node_profiles WHERE binding_id=b.id) node_ids FROM inbound_endpoint_bindings b JOIN catalog_inbounds i ON i.id=b.inbound_id JOIN instances x ON x.id=i.instance_id WHERE endpoint_id=? ORDER BY x.id,i.tag`, r.PathValue("id"))
	})
	admin.HandleFunc("POST /api/v1/catalog/endpoint-bindings/{id}/copy", func(w http.ResponseWriter, r *http.Request) {
		b, e := catalog.ReadBinding(r.Context(), a.Store.DB, r.PathValue("id"))
		if e != nil {
			result(w, nil, e)
			return
		}
		ep, e := catalog.ReadEndpoint(r.Context(), a.Store.DB, b.EndpointID)
		if e != nil {
			result(w, nil, e)
			return
		}
		d := catalog.Connection{InboundID: b.InboundID, Name: ep.Name + " · 副本", Profile: ep.Profile, Overrides: b.Overrides, Enabled: b.Enabled, Sort: b.Sort + 1, Description: ep.Description, Kind: ep.Kind}
		v, e := catalog.CreateConnection(r.Context(), a.Store, d)
		catalogResult(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/migration-preview", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT n.id node_id,n.inbound_id,n.endpoint_id,n.binding_id,n.variant_key,n.name,CASE WHEN n.source_type='manual' THEN 'manual_preserved' WHEN n.binding_id IS NULL THEN 'inbound_unresolved' WHEN i.present=0 THEN 'inbound_missing' WHEN i.review=1 THEN 'inbound_unresolved' ELSE 'linked' END migration_status FROM node_profiles n LEFT JOIN catalog_inbounds i ON i.id=n.inbound_id ORDER BY n.id`)
	})
}
