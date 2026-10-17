package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
	"traffic-manager-lite/internal/hysteriaauth"
)

func (a *API) hysteriaAuthRoutes(mux, admin *http.ServeMux) {
	admin.HandleFunc("GET /api/v1/catalog/hysteria-auth", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, `SELECT h.inbound_id,h.allowed_cidrs,h.enabled,h.confirmed,h.revision,h.last_callback_at,h.last_success_at,(SELECT count(*) FROM hysteria_auth_accounts x WHERE x.inbound_id=h.inbound_id) account_count,CASE WHEN h.target_endpoint=i.api_endpoint AND h.target_tag=b.tag AND b.present=1 AND b.review=0 AND b.retired=0 THEN 1 ELSE 0 END target_matches FROM hysteria_auth_backends h JOIN catalog_inbounds b ON b.id=h.inbound_id JOIN instances i ON i.id=b.instance_id ORDER BY h.inbound_id`)
	})
	admin.HandleFunc("POST /api/v1/catalog/hysteria-auth/{id}/prepare", func(w http.ResponseWriter, r *http.Request) {
		var d hysteriaauth.Setup
		if !body(w, r, &d) {
			return
		}
		v, e := hysteriaauth.Prepare(r.Context(), a.Store, r.PathValue("id"), d)
		result(w, v, e)
	})
	admin.HandleFunc("PUT /api/v1/catalog/hysteria-auth/{id}", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Revision     int64 `json:"revision"`
			Enabled      bool  `json:"enabled"`
			Confirmed    bool  `json:"confirmed"`
			Acknowledged bool  `json:"acknowledged"`
		}
		if !body(w, r, &d) {
			return
		}
		if !d.Acknowledged {
			respond(w, 400, map[string]string{"error": "请明确确认认证后端的核心配置或停用影响"})
			return
		}
		e := hysteriaauth.SetState(r.Context(), a.Store, r.PathValue("id"), d.Revision, d.Enabled, d.Confirmed)
		result(w, map[string]bool{"saved": e == nil}, e)
	})
	// The official core cannot send custom auth headers. A per-inbound random
	// capability URL and immediate-peer CIDRs isolate this from admin/sub tokens.
	var mu sync.Mutex
	attempts := map[string]publicAttempt{}
	mux.HandleFunc("POST /core-auth/hysteria2/{token}", func(w http.ResponseWriter, r *http.Request) {
		deny := func(status int) { respond(w, status, map[string]any{"ok": false, "id": ""}) }
		peer, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil {
			deny(200)
			return
		}
		now := time.Now()
		mu.Lock()
		for k, v := range attempts {
			if now.Sub(v.start) >= time.Minute {
				delete(attempts, k)
			}
		}
		v, found := attempts[peer]
		if v.start.IsZero() {
			v.start = now
		}
		v.count++
		allowed := v.count <= 600 && (found || len(attempts) < 1024)
		if allowed {
			attempts[peer] = v
		}
		mu.Unlock()
		if !allowed {
			deny(429)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		defer r.Body.Close()
		var d struct {
			Addr string `json:"addr"`
			Auth string `json:"auth"`
			Tx   uint64 `json:"tx"`
		}
		decoder := json.NewDecoder(r.Body)
		if decoder.Decode(&d) != nil || len(d.Addr) > 256 || len(d.Auth) > 4096 {
			deny(400)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			deny(400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		id, e := hysteriaauth.Authenticate(ctx, a.Store, r.PathValue("token"), peer, d.Auth)
		if e != nil {
			deny(503)
			return
		}
		respond(w, 200, map[string]any{"ok": id != "", "id": id})
	})
}
