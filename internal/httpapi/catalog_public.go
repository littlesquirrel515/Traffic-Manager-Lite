package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

type publicAttempt struct {
	start time.Time
	count int
}

func (a *API) catalogPublicHandler() http.HandlerFunc {
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		panic(e)
	}
	var mu sync.Mutex
	attempts := map[string]publicAttempt{}
	// Record only recognized client families/versions, never arbitrary UA strings
	// which can contain credentials or the subscription URL.
	uaPattern := regexp.MustCompile(`(?i)^(clash-verge|mihomo|v2rayn|sing-box|mozilla)(?:/[vV]?[0-9.]{1,20})?`)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		settings, e := subscription.LoadSettings(ctx, a.Store)
		if e != nil {
			http.Error(w, "订阅设置不可用", 503)
			return
		}
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		m := hmac.New(sha256.New, key)
		m.Write([]byte(ip))
		ipHash := hex.EncodeToString(m.Sum(nil))
		now := time.Now()
		mu.Lock()
		for k, v := range attempts {
			if now.Sub(v.start) >= time.Minute {
				delete(attempts, k)
			}
		}
		v, exists := attempts[ipHash]
		if v.start.IsZero() {
			v.start = now
		}
		v.count++
		allowed := v.count <= settings.RatePerMinute && (exists || len(attempts) < 4096)
		if exists || len(attempts) < 4096 {
			attempts[ipHash] = v
		}
		mu.Unlock()
		format := r.URL.Query().Get("format")
		status, code := 200, "ok"
		var d subscription.CatalogDefinition
		var res subscription.RenderResult
		if !allowed {
			status, code = 429, "rate_limited"
		} else {
			res, d, e = subscription.CatalogOutput(ctx, a.Store, r.PathValue("token"), format)
			if e != nil {
				status, code = 404, "unavailable"
				if errors.Is(e, subscription.ErrNoUsableNodes) {
					status, code = 422, "no_usable_nodes"
				}
				switch e.Error() {
				case "expired", "quota_exhausted", "disabled", "subscriber_disabled":
					status, code = 403, e.Error()
				case "usage_unavailable":
					status, code = 503, e.Error()
				}
			}
		}
		logFormat := format
		if logFormat == "" {
			logFormat = d.Format
		}
		if !subscription.ValidFormat(logFormat) {
			logFormat = "unknown"
		}
		ua := uaPattern.FindString(r.UserAgent())
		if ua == "" {
			ua = "unknown"
		}
		var subscriptionID any
		if d.ID != "" {
			subscriptionID = d.ID
		}
		auditCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_, _ = a.Store.DB.ExecContext(auditCtx, "INSERT INTO subscription_access_logs(subscription_id,format,accessed_at,status_code,error_code,user_agent,client_ip_hash) VALUES(?,?,?,?,?,?,?)", subscriptionID, logFormat, storage.Stamp(now), status, code, ua, ipHash)
		if d.Usage != nil {
			setQuotaHeaders(w, *d.Usage)
		}
		if status != 200 {
			if status == 429 {
				w.Header().Set("Retry-After", "60")
			}
			http.Error(w, "订阅不可用（"+code+"）", status)
			return
		}
		_, _ = a.Store.DB.ExecContext(ctx, "UPDATE catalog_subscriptions SET last_access_at=? WHERE id=?", storage.Stamp(now), d.ID)
		w.Header().Set("Content-Type", res.ContentType)
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-TML-Filtered-Nodes", strconv.Itoa(len(res.Rejected)))
		w.Header().Set("profile-update-interval", strconv.Itoa(settings.UpdateInterval))
		w.Header().Set("profile-title", "base64:"+base64.StdEncoding.EncodeToString([]byte(d.Name)))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.Name + map[string]string{"mihomo": ".yaml", "singbox": ".json"}[logFormat]}))
		_, _ = w.Write(res.Content)
	}
}
func (a *API) catalogSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		d, e := subscription.LoadSettings(r.Context(), a.Store)
		result(w, d, e)
		return
	}
	var d subscription.Settings
	if !body(w, r, &d) {
		return
	}
	e := subscription.SaveSettings(r.Context(), a.Store, d)
	result(w, map[string]bool{"updated": e == nil}, e)
}
