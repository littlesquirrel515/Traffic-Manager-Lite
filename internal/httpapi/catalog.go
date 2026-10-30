package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

func catalogResult(w http.ResponseWriter, v any, e error) {
	if errors.Is(e, catalog.ErrReferenced) {
		respond(w, 409, map[string]string{"error": e.Error()})
		return
	}
	result(w, v, e)
}
func (a *API) catalogRoutes(admin *http.ServeMux) {
	a.quotaRoutes(admin)
	a.inboundEndpointRoutes(admin)
	admin.HandleFunc("POST /api/v1/catalog/nodes/batch", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			NodeIDs []string `json:"node_ids"`
			Enabled bool     `json:"enabled"`
		}
		if !body(w, r, &d) {
			return
		}
		if len(d.NodeIDs) > 500 {
			result(w, nil, fmt.Errorf("批量上限 500"))
			return
		}
		tx, e := a.Store.DB.BeginTx(r.Context(), nil)
		if e != nil {
			result(w, nil, e)
			return
		}
		defer tx.Rollback()
		for _, id := range d.NodeIDs {
			if _, e = tx.ExecContext(r.Context(), "UPDATE node_profiles SET enabled=?,updated_at=? WHERE id=?", d.Enabled, storage.Stamp(time.Now()), id); e != nil {
				break
			}
		}
		if e == nil {
			e = tx.Commit()
		}
		result(w, map[string]bool{"updated": e == nil}, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/settings", a.catalogSettings)
	admin.HandleFunc("PATCH /api/v1/catalog/settings", a.catalogSettings)
	admin.HandleFunc("GET /api/v1/catalog/inbounds", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT i.*,x.name instance_name,x.core_type FROM catalog_inbounds i JOIN instances x ON x.id=i.instance_id ORDER BY x.id,i.tag`)
	})
	admin.HandleFunc("GET /api/v1/catalog/sync-history", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, "SELECT * FROM catalog_sync_runs ORDER BY id DESC LIMIT 200")
	})
	admin.HandleFunc("GET /api/v1/catalog/nodes", func(w http.ResponseWriter, r *http.Request) {
		v, e := catalog.Nodes(r.Context(), a.Store.DB)
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := catalog.ResolveDefinition(r.Context(), a.Store.DB, r.PathValue("id"))
		result(w, v, e)
	})
	for _, method := range []string{"PATCH /api/v1/catalog/nodes/{id}"} {
		admin.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			var d catalog.Node
			if !body(w, r, &d) {
				return
			}
			d.ID = r.PathValue("id")
			id, e := catalog.SaveNode(r.Context(), a.Store, d)
			catalogResult(w, map[string]string{"id": id}, e)
		})
	}
	admin.HandleFunc("POST /api/v1/catalog/nodes/{id}/copy", func(w http.ResponseWriter, r *http.Request) {
		v, e := catalog.ResolveDefinition(r.Context(), a.Store.DB, r.PathValue("id"))
		if e == nil {
			v.ID = ""
			v.Name += " · 副本"
			var id string
			id, e = catalog.SaveNode(r.Context(), a.Store, v)
			catalogResult(w, map[string]string{"id": id}, e)
			return
		}
		result(w, nil, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/endpoints", func(w http.ResponseWriter, r *http.Request) {
		rows, e := a.Store.Rows(r.Context(), `SELECT e.*,(SELECT count(*) FROM node_profiles WHERE endpoint_id=e.id) AS references_count FROM node_endpoints e ORDER BY sort_order,name,id`)
		out := []catalog.Endpoint{}
		for _, v := range rows {
			n := catalog.Endpoint{ID: v["id"].(string), Name: v["name"].(string), Enabled: v["enabled"].(int64) == 1, Sort: int(v["sort_order"].(int64)), References: int(v["references_count"].(int64))}
			_ = a.Store.DB.QueryRowContext(r.Context(), `SELECT count(DISTINCT subscription_id) FROM (SELECT sn.subscription_id FROM catalog_subscription_nodes sn JOIN node_profiles n ON n.id=sn.node_id WHERE n.endpoint_id=? UNION SELECT sg.subscription_id FROM catalog_subscription_groups sg JOIN node_group_members gm ON gm.group_id=sg.group_id JOIN node_profiles n ON n.id=gm.node_id WHERE n.endpoint_id=?)`, n.ID, n.ID).Scan(&n.Subscriptions)
			_ = json.Unmarshal([]byte(v["profile_json"].(string)), &n.Profile)
			n.Description = v["description"].(string)
			n.Kind = v["endpoint_type"].(string)
			_ = a.Store.DB.QueryRowContext(r.Context(), "SELECT count(*) FROM inbound_endpoint_bindings WHERE endpoint_id=?", n.ID).Scan(&n.Bindings)
			out = append(out, n)
		}
		result(w, out, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/endpoints/{id}/copy", func(w http.ResponseWriter, r *http.Request) {
		n, e := catalog.ReadEndpoint(r.Context(), a.Store.DB, r.PathValue("id"))
		if e != nil {
			result(w, nil, e)
			return
		}
		n.ID = ""
		n.Name += " · 副本"
		id, e := catalog.SaveEndpoint(r.Context(), a.Store, n)
		catalogResult(w, map[string]string{"id": id}, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/endpoints/batch", func(w http.ResponseWriter, r *http.Request) {
		var d catalog.EndpointBatch
		if !body(w, r, &d) {
			return
		}
		e := catalog.BatchEndpoints(r.Context(), a.Store, d)
		catalogResult(w, map[string]bool{"updated": e == nil}, e)
	})
	for _, method := range []string{"POST /api/v1/catalog/endpoints", "PATCH /api/v1/catalog/endpoints/{id}"} {
		admin.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			var d catalog.Endpoint
			if !body(w, r, &d) {
				return
			}
			d.ID = r.PathValue("id")
			id, e := catalog.SaveEndpoint(r.Context(), a.Store, d)
			catalogResult(w, map[string]string{"id": id}, e)
		})
	}
	admin.HandleFunc("GET /api/v1/catalog/groups", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT g.*,(SELECT json_group_array(node_id) FROM (SELECT node_id FROM node_group_members WHERE group_id=g.id ORDER BY sort_order,node_id)) node_ids,(SELECT count(*) FROM catalog_subscription_groups WHERE group_id=g.id) references_count FROM node_groups g ORDER BY sort_order,name,id`)
	})
	for _, method := range []string{"POST /api/v1/catalog/groups", "PATCH /api/v1/catalog/groups/{id}"} {
		admin.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			var d catalog.Group
			if !body(w, r, &d) {
				return
			}
			d.ID = r.PathValue("id")
			id, e := catalog.SaveGroup(r.Context(), a.Store, d)
			catalogResult(w, map[string]string{"id": id}, e)
		})
	}
	for _, kind := range []string{"nodes", "endpoints", "groups", "subscribers"} {
		admin.HandleFunc("DELETE /api/v1/catalog/"+kind+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			if kind == "subscribers" {
				var n int
				if err := a.Store.DB.QueryRowContext(r.Context(), "SELECT count(*) FROM subscriber_provisioning WHERE subscriber_id=?", r.PathValue("id")).Scan(&n); err != nil {
					result(w, nil, err)
					return
				}
				if n > 0 {
					respond(w, 409, map[string]string{"error": "此用户关联客户端创建/纳管任务，不能直接删除；请先停用用户保留凭据与审计，避免失去运行时客户端的归属"})
					return
				}
			}
			e := catalog.Delete(r.Context(), a.Store, kind, r.PathValue("id"))
			catalogResult(w, map[string]bool{"deleted": e == nil}, e)
		})
	}
	admin.HandleFunc("GET /api/v1/catalog/credentials", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT c.id,c.instance_id,c.inbound_tag,c.core_client_ref,c.user_id,c.status,c.source,c.verified_at,k.inbound_id FROM credential_material c LEFT JOIN inbound_client_keys k ON k.credential_id=c.id ORDER BY c.instance_id,c.inbound_tag,c.core_client_ref`)
	})
	// A protected manual credential source is separate from every node definition.

	a.catalogSubscriptionRoutes(admin)
}

func (a *API) catalogSubscriptionRoutes(admin *http.ServeMux) {
	admin.HandleFunc("POST /api/v1/catalog/subscriptions/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Confirmed bool `json:"confirmed"`
		}
		if !body(w, r, &d) {
			return
		}
		if !d.Confirmed {
			result(w, nil, fmt.Errorf("导出包含客户端认证，需要明确确认"))
			return
		}
		definition, e := subscription.CatalogSubscription(r.Context(), a.Store.DB, r.PathValue("id"))
		if e != nil {
			result(w, nil, e)
			return
		}
		nodes, _, _, e := subscription.ResolveCatalog(r.Context(), a.Store.DB, definition)
		if e != nil {
			result(w, nil, e)
			return
		}
		render, e := subscription.Render(r.Context(), definition.Format, nodes, definition.Options)
		if e != nil {
			result(w, nil, e)
			return
		}
		if len(nodes) == len(render.Rejected) {
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "没有可用节点，拒绝导出空配置"})
			return
		}
		w.Header().Set("Content-Type", render.ContentType)
		w.Header().Set("Cache-Control", "private, no-store")
		w.Write(render.Content)
	})
	admin.HandleFunc("GET /api/v1/catalog/capabilities", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, subscription.CapabilityMatrix()) })
	admin.HandleFunc("GET /api/v1/catalog/subscribers", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT s.*,(SELECT count(*) FROM catalog_subscriptions WHERE subscriber_id=s.id) subscription_count,(SELECT count(*) FROM subscriber_credentials WHERE subscriber_id=s.id) credential_count FROM subscribers s ORDER BY name,id`)
	})
	admin.HandleFunc("POST /api/v1/catalog/subscribers", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 409, map[string]string{"error": "创建订阅用户必须通过节点授权流程选择代理节点或节点组，原生客户端可使用高级纳管流程"})
	})
	for _, method := range []string{"PATCH /api/v1/catalog/subscribers/{id}"} {
		admin.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			d := subscription.Subscriber{Enabled: true}
			if !body(w, r, &d) {
				return
			}
			d.ID = r.PathValue("id")
			id, e := subscription.SaveSubscriber(r.Context(), a.Store, d)
			result(w, map[string]string{"id": id}, e)
		})
	}
	admin.HandleFunc("GET /api/v1/catalog/bindings", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT b.*,c.core_client_ref,c.status,c.source,c.verified_at FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id ORDER BY b.subscriber_id,b.id`)
	})
	admin.HandleFunc("POST /api/v1/catalog/bindings", func(w http.ResponseWriter, r *http.Request) {
		var d subscription.Binding
		if !body(w, r, &d) {
			return
		}
		id, e := subscription.Bind(r.Context(), a.Store, d)
		result(w, map[string]string{"id": id}, e)
	})
	admin.HandleFunc("DELETE /api/v1/catalog/bindings/{id}", func(w http.ResponseWriter, r *http.Request) {
		e := subscription.Unbind(r.Context(), a.Store, r.PathValue("id"))
		result(w, map[string]bool{"deleted": e == nil}, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT s.id,s.subscriber_id,s.name,s.token_version,s.enabled,s.expires_at,s.format,s.options_json,s.last_access_at,s.created_at,s.updated_at,(SELECT json_group_array(node_id) FROM (SELECT node_id FROM catalog_subscription_nodes WHERE subscription_id=s.id ORDER BY sort_order,node_id)) node_ids,(SELECT json_group_array(group_id) FROM (SELECT group_id FROM catalog_subscription_groups WHERE subscription_id=s.id ORDER BY sort_order,group_id)) group_ids FROM catalog_subscriptions s ORDER BY name,id`)
	})
	admin.HandleFunc("GET /api/v1/catalog/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		d, e := subscription.CatalogSubscription(r.Context(), a.Store.DB, r.PathValue("id"))
		result(w, d, e)
	})
	for _, method := range []string{"POST /api/v1/catalog/subscriptions", "PATCH /api/v1/catalog/subscriptions/{id}"} {
		admin.HandleFunc(method, func(w http.ResponseWriter, r *http.Request) {
			var d subscription.CatalogDefinition
			if !body(w, r, &d) {
				return
			}
			d.ID = r.PathValue("id")
			id, t, e := subscription.SaveCatalogSubscription(r.Context(), a.Store, d)
			result(w, map[string]string{"id": id, "token": t}, e)
		})
	}
	admin.HandleFunc("POST /api/v1/catalog/subscriptions/preview", func(w http.ResponseWriter, r *http.Request) {
		var d subscription.CatalogDefinition
		if !body(w, r, &d) {
			return
		}
		p, e := subscription.CatalogPreviewFor(r.Context(), a.Store.DB, d)
		result(w, p, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/subscriptions/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		d, e := subscription.CatalogSubscription(r.Context(), a.Store.DB, r.PathValue("id"))
		if e != nil {
			result(w, nil, e)
			return
		}
		p, e := subscription.CatalogPreviewFor(r.Context(), a.Store.DB, d)
		result(w, p, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/subscriptions/{id}/rotate-token", func(w http.ResponseWriter, r *http.Request) {
		t, e := subscription.RotateCatalog(r.Context(), a.Store, r.PathValue("id"))
		result(w, map[string]string{"token": t}, e)
	})
	admin.HandleFunc("DELETE /api/v1/catalog/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, e := a.Store.DB.ExecContext(r.Context(), "DELETE FROM catalog_subscriptions WHERE id=?", r.PathValue("id"))
		result(w, map[string]bool{"deleted": e == nil}, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/access-logs", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, "SELECT * FROM subscription_access_logs ORDER BY id DESC LIMIT 200")
	})
}
