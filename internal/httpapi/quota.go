package httpapi

import (
	"errors"
	"net/http"
	"time"
	"traffic-manager-lite/internal/subscription"
)

func setQuotaHeaders(w http.ResponseWriter, u subscription.Usage) {
	if info := u.UserInfo(); info != "" {
		w.Header().Set("subscription-userinfo", info)
	}
	w.Header().Set("X-TML-Usage-Status", u.Completeness)
	w.Header().Set("X-TML-Quota-Status", u.Status)
	if u.ExpiryEnabled && u.ExpiresAt != nil {
		w.Header().Set("X-TML-Expires-At", *u.ExpiresAt)
	}
}
func (a *API) quotaRoutes(admin *http.ServeMux) {
	admin.HandleFunc("POST /api/v1/catalog/nodes/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			SubscriberID string `json:"subscriber_id"`
		}
		if !body(w, r, &d) {
			return
		}
		p, e := subscription.CatalogPreviewFor(r.Context(), a.Store.DB, subscription.CatalogDefinition{SubscriberID: d.SubscriberID, Enabled: true, Format: "mihomo", NodeIDs: []string{r.PathValue("id")}})
		result(w, p, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/quota-defaults", func(w http.ResponseWriter, r *http.Request) {
		end := subscription.ThreeMonths(time.Now().UTC()).Format(time.RFC3339)
		respond(w, 200, subscription.Quota{Limit: subscription.DefaultQuota, ExpiresAt: &end, Enabled: true, ExpiryEnabled: true, Policy: "report_partial"})
	})
	admin.HandleFunc("GET /api/v1/catalog/subscriber-usage", func(w http.ResponseWriter, r *http.Request) {
		rows, e := a.Store.DB.QueryContext(r.Context(), "SELECT id FROM subscribers ORDER BY name,id LIMIT 1000")
		if e != nil {
			result(w, nil, e)
			return
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				result(w, nil, e)
				return
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			result(w, nil, e)
			return
		}
		out := map[string]subscription.Usage{}
		for _, id := range ids {
			u, er := subscription.SubscriberUsage(r.Context(), a.Store.DB, id)
			if er != nil {
				result(w, nil, er)
				return
			}
			out[id] = u
		}
		respond(w, 200, out)
	})
	for _, route := range []string{"quota", "usage"} {
		admin.HandleFunc("GET /api/v1/catalog/subscribers/{id}/"+route, func(w http.ResponseWriter, r *http.Request) {
			u, e := subscription.SubscriberUsage(r.Context(), a.Store.DB, r.PathValue("id"))
			result(w, u, e)
		})
	}
	for _, route := range []string{"PUT /api/v1/catalog/subscribers/{id}/quota", "POST /api/v1/catalog/subscribers/{id}/quota/reset"} {
		admin.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			var d subscription.QuotaUpdate
			if !body(w, r, &d) {
				return
			}
			if r.Method == "POST" {
				current, e := subscription.ReadQuota(r.Context(), a.Store.DB, r.PathValue("id"))
				if e != nil {
					result(w, nil, e)
					return
				}
				if current.Revision != d.Revision {
					respond(w, 409, map[string]string{"error": subscription.ErrQuotaConflict.Error()})
					return
				}
				d.Quota = current
				d.AddBytes = 0
				d.Reset = true
			}
			v, e := subscription.UpdateQuota(r.Context(), a.Store, r.PathValue("id"), d)
			if errors.Is(e, subscription.ErrQuotaConflict) {
				respond(w, 409, map[string]string{"error": e.Error()})
				return
			}
			result(w, v, e)
		})
	}
	admin.HandleFunc("GET /api/v1/catalog/subscribers/{id}/quota/history", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT action,before_json,after_json,created_at FROM subscriber_quota_audit WHERE subscriber_id=? ORDER BY id DESC LIMIT 200`, r.PathValue("id"))
	})
}
