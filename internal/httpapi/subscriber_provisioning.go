package httpapi

import (
	"net/http"
	"traffic-manager-lite/internal/subscriberprovision"
	"traffic-manager-lite/internal/subscription"
)

func (a *API) subscriberProvisioningRoutes(admin *http.ServeMux) {
	service := subscriberprovision.Service{Store: a.Store, Config: a.Config}
	admin.HandleFunc("GET /api/v1/catalog/subscriber-node-assignments", func(w http.ResponseWriter, r *http.Request) {
		v, e := subscription.NodeAssignments(r.Context(), a.Store.DB, r.URL.Query().Get("subscriber_id"))
		result(w, v, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/subscriber-node-requests/preview", func(w http.ResponseWriter, r *http.Request) {
		var d subscriberprovision.NodeRequest
		if !body(w, r, &d) {
			return
		}
		v, e := service.PlanNodes(r.Context(), d)
		result(w, v, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/subscriber-node-requests", func(w http.ResponseWriter, r *http.Request) {
		var d subscriberprovision.NodeRequest
		if !body(w, r, &d) {
			return
		}
		v, e := service.CreateNodes(r.Context(), d)
		if e != nil {
			result(w, nil, e)
			return
		}
		code := 200
		if v.Status != "ready" {
			code = 202
		}
		respond(w, code, v)
	})
	admin.HandleFunc("POST /api/v1/catalog/subscriber-node-requests/{request}/retry", func(w http.ResponseWriter, r *http.Request) {
		v, e := service.RetryNodes(r.Context(), r.PathValue("request"))
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/subscriber-node-requests", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT p.request_id,p.subscriber_id,s.name subscriber_name,p.status,p.message,p.checked_at,json_array_length(p.plan_json,'$.nodes') node_count,json_array_length(p.plan_json,'$.targets') target_count FROM subscriber_node_requests p JOIN subscribers s ON s.id=p.subscriber_id ORDER BY p.checked_at DESC`)
	})
	admin.HandleFunc("GET /api/v1/catalog/subscriber-node-grants", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT g.subscriber_id,s.name subscriber_name,g.node_id,n.name node_name,g.created_at FROM subscriber_node_grants g JOIN node_profiles n ON n.id=g.node_id JOIN subscribers s ON s.id=g.subscriber_id ORDER BY g.created_at,g.node_id`)
	})
	admin.HandleFunc("GET /api/v1/catalog/subscriber-node-requests/{request}/tasks", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT t.status,t.message,t.checked_at,i.name instance_name,b.protocol FROM subscriber_node_tasks t JOIN catalog_inbounds b ON b.id=t.inbound_id JOIN instances i ON i.id=b.instance_id WHERE t.request_id=? ORDER BY i.name,b.protocol,t.inbound_id`, r.PathValue("request"))
	})
	admin.HandleFunc("POST /api/v1/catalog/subscriber-provisioning", func(w http.ResponseWriter, r *http.Request) {
		var d subscriberprovision.Request
		if !body(w, r, &d) {
			return
		}
		v, e := service.Create(r.Context(), d)
		if e != nil {
			result(w, nil, e)
			return
		}
		status := 200
		if v.Status != "ready" {
			status = 202
		}
		respond(w, status, v)
	})
	admin.HandleFunc("POST /api/v1/catalog/subscriber-provisioning/{request}/retry", func(w http.ResponseWriter, r *http.Request) {
		v, e := service.Retry(r.Context(), r.PathValue("request"))
		result(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/subscriber-provisioning", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT p.request_id,p.subscriber_id,p.inbound_id,p.client_ref,p.origin,p.status,p.message,p.checked_at,r.status runtime_status,r.checked_at runtime_checked_at FROM subscriber_provisioning p LEFT JOIN remote_runtime_users r ON r.inbound_id=p.inbound_id AND r.client_ref=p.client_ref WHERE NOT EXISTS(SELECT 1 FROM subscriber_node_tasks t WHERE t.child_request_id=p.request_id) ORDER BY p.checked_at DESC`)
	})
	admin.HandleFunc("GET /api/v1/catalog/native-clients", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT c.id credential_id,k.inbound_id,c.core_client_ref,c.status,c.source,b.tag,b.protocol,b.present,b.review,b.retired,x.name instance_name,x.core_type,(SELECT json_group_array(s.name) FROM subscriber_credentials sc JOIN subscribers s ON s.id=sc.subscriber_id WHERE sc.credential_id=c.id AND sc.enabled=1) owners FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id JOIN catalog_inbounds b ON b.id=k.inbound_id JOIN instances x ON x.id=b.instance_id WHERE NOT EXISTS(SELECT 1 FROM subscriber_provisioning p WHERE p.inbound_id=b.id AND p.client_ref=c.core_client_ref AND p.origin='managed') ORDER BY x.id,b.tag,c.core_client_ref`)
	})
}
