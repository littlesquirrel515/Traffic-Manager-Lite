package httpapi

import (
	"net/http"
	"traffic-manager-lite/internal/subscriberprovision"
)

func (a *API) subscriberProvisioningRoutes(admin *http.ServeMux) {
	service := subscriberprovision.Service{Store: a.Store, Config: a.Config}
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
		a.rows(w, r, `SELECT p.request_id,p.subscriber_id,p.inbound_id,p.client_ref,p.origin,p.status,p.message,p.checked_at,r.status runtime_status,r.checked_at runtime_checked_at FROM subscriber_provisioning p LEFT JOIN remote_runtime_users r ON r.inbound_id=p.inbound_id AND r.client_ref=p.client_ref ORDER BY p.checked_at DESC`)
	})
	admin.HandleFunc("GET /api/v1/catalog/native-clients", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT c.id credential_id,k.inbound_id,c.core_client_ref,c.status,c.source,b.tag,b.protocol,b.present,b.review,b.retired,x.name instance_name,x.core_type,(SELECT json_group_array(s.name) FROM subscriber_credentials sc JOIN subscribers s ON s.id=sc.subscriber_id WHERE sc.credential_id=c.id AND sc.enabled=1) owners FROM credential_material c JOIN inbound_client_keys k ON k.credential_id=c.id JOIN catalog_inbounds b ON b.id=k.inbound_id JOIN instances x ON x.id=b.instance_id WHERE NOT EXISTS(SELECT 1 FROM subscriber_provisioning p WHERE p.inbound_id=b.id AND p.client_ref=c.core_client_ref AND p.origin='managed') ORDER BY x.id,b.tag,c.core_client_ref`)
	})
}
