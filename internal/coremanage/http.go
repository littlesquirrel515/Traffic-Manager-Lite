package coremanage

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

func (m *Manager) Handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/instances/{id}/config", func(w http.ResponseWriter, r *http.Request) {
		if len(token) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if e != nil {
			http.Error(w, "invalid instance", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
		defer cancel()
		var result Result
		if r.Method == "GET" {
			result, e = m.Read(ctx, id)
		} else if r.Method == "POST" {
			var req Request
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&req) != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			if req.Operation == "apply" || req.Operation == "rollback" {
				result, e = m.Apply(ctx, id, req)
			} else {
				result, e = m.Update(ctx, id, req)
			}
		} else {
			http.Error(w, "method not allowed", 405)
			return
		}
		if e != nil {
			status := 400
			if errors.Is(e, ErrConflict) {
				status = 409
			}
			http.Error(w, e.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	return mux
}
