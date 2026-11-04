package httpapi

import (
	"context"
	"net/http"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/preferred"
)

func (a *API) preferredRoutes(admin *http.ServeMux) {
	admin.HandleFunc("POST /api/v1/catalog/preferred/import", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			URL   string `json:"url"`
			Mode  string `json:"mode"`
			Port  int    `json:"port"`
			Limit int    `json:"limit"`
		}
		if !body(w, r, &d) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		text, e := preferred.Fetch(ctx, d.URL, d.Mode)
		if e != nil {
			catalogResult(w, nil, e)
			return
		}
		p, e := preferred.Parse(text, d.Port, d.Limit)
		catalogResult(w, p, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/preferred/random", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Count int `json:"count"`
			Port  int `json:"port"`
		}
		if !body(w, r, &d) {
			return
		}
		text, e := preferred.Random(d.Count, d.Port)
		catalogResult(w, map[string]string{"input": text}, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/preferred/preview", func(w http.ResponseWriter, r *http.Request) {
		var d catalog.PreferredRequest
		if !body(w, r, &d) {
			return
		}
		tx, e := a.Store.DB.BeginTx(r.Context(), nil)
		if e != nil {
			catalogResult(w, nil, e)
			return
		}
		defer tx.Rollback()
		p, e := catalog.PlanPreferred(r.Context(), tx, d)
		catalogResult(w, p, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/preferred/generate", func(w http.ResponseWriter, r *http.Request) {
		var d catalog.PreferredRequest
		if !body(w, r, &d) {
			return
		}
		p, e := catalog.GeneratePreferred(r.Context(), a.Store, d)
		catalogResult(w, p, e)
	})
}
