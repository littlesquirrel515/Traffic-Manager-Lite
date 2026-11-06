package httpapi

import (
	"net/http"
	"traffic-manager-lite/internal/catalog"
)

func (a *API) preferredSyncRoutes(admin *http.ServeMux) {
	s := a.PreferredSync
	if s == nil {
		s = &catalog.PreferredSync{Store: a.Store}
	}
	admin.HandleFunc("GET /api/v1/catalog/preferred-sync", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.List(r.Context())
		catalogResult(w, v, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/preferred-sync", func(w http.ResponseWriter, r *http.Request) {
		var d catalog.SyncConfig
		if !body(w, r, &d) {
			return
		}
		v, e := s.Create(r.Context(), d)
		catalogResult(w, v, e)
	})
	admin.HandleFunc("GET /api/v1/catalog/preferred-sync/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.History(r.Context(), r.PathValue("id"))
		catalogResult(w, v, e)
	})
	admin.HandleFunc("POST /api/v1/catalog/preferred-sync/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Sync(r.Context(), r.PathValue("id"))
		catalogResult(w, v, e)
	})
	admin.HandleFunc("PUT /api/v1/catalog/preferred-sync/{id}/sources", func(w http.ResponseWriter, r *http.Request) {
		var d catalog.SyncSourceEdit
		if !body(w, r, &d) {
			return
		}
		e := s.EditSources(r.Context(), r.PathValue("id"), d)
		catalogResult(w, map[string]bool{"updated": e == nil}, e)
	})
	admin.HandleFunc("PATCH /api/v1/catalog/preferred-sync/{id}", func(w http.ResponseWriter, r *http.Request) {
		var d struct {
			Enabled *bool `json:"enabled"`
		}
		if !body(w, r, &d) {
			return
		}
		if d.Enabled == nil {
			http.Error(w, "请选择启停状态", 400)
			return
		}
		e := s.Enable(r.Context(), r.PathValue("id"), *d.Enabled)
		catalogResult(w, map[string]bool{"updated": e == nil}, e)
	})
}
