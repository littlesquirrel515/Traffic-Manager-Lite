package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (a *API) settings(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Interval string `json:"collect_interval"`
		Window   string `json:"active_window"`
		LogLevel string `json:"log_level"`
		Archive  bool   `json:"archive_enabled"`
	}
	if !body(w, r, &d) {
		return
	}
	for _, v := range []string{d.Interval, d.Window} {
		t, e := time.ParseDuration(v)
		if e != nil || t < time.Second || t > 24*time.Hour {
			result(w, nil, fmt.Errorf("duration must be between 1s and 24h"))
			return
		}
	}
	if d.LogLevel != "debug" && d.LogLevel != "info" && d.LogLevel != "warn" && d.LogLevel != "error" {
		result(w, nil, fmt.Errorf("invalid log level"))
		return
	}
	b, _ := json.Marshal(d)
	_, e := a.Store.DB.ExecContext(r.Context(), "INSERT INTO settings(key,value) VALUES('runtime',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(b))
	result(w, map[string]bool{"saved": e == nil, "restart_required": true}, e)
}
