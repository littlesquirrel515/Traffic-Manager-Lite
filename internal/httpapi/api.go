package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"traffic-manager-lite/internal/collector"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/discovery"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
	"traffic-manager-lite/web"
)

type API struct {
	Store     *storage.Store
	Scheduler *collector.Scheduler
	Config    config.Config
	Auth      *security.Auth
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	admin := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if e := a.Store.DB.PingContext(ctx); e != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/auth/login", a.Auth.Login)
	mux.HandleFunc("GET /api/v1/auth/session", a.Auth.Session)
	admin.HandleFunc("POST /api/v1/auth/logout", a.Auth.Logout)
	admin.HandleFunc("GET /api/v1/dashboard", a.dashboard)
	admin.HandleFunc("GET /api/v1/servers", func(w http.ResponseWriter, r *http.Request) { a.rows(w, r, "SELECT * FROM servers ORDER BY id") })
	admin.HandleFunc("POST /api/v1/servers", a.saveServer)
	admin.HandleFunc("GET /api/v1/servers/{id}", a.getServer)
	admin.HandleFunc("PATCH /api/v1/servers/{id}", a.updateServer)
	admin.HandleFunc("DELETE /api/v1/servers/{id}", a.deleteServer)
	admin.HandleFunc("GET /api/v1/xray/diagnostics", a.diagnosticSummary)
	admin.HandleFunc("GET /api/v1/cores/diagnostics", a.diagnosticSummary)
	admin.HandleFunc("GET /api/v1/instances/{id}/diagnostics", a.diagnosticDetails)
	admin.HandleFunc("POST /api/v1/instances/{id}/diagnostics", a.diagnose)
	admin.HandleFunc("GET /api/v1/instances/{id}/diagnostics/history", a.diagnosticHistory)
	admin.HandleFunc("GET /api/v1/instances/{id}/health", a.collectorHealth)
	admin.HandleFunc("GET /api/v1/instances/{id}/clients", a.clients)
	admin.HandleFunc("GET /api/v1/instances", func(w http.ResponseWriter, r *http.Request) { v, e := a.Store.Instances(r.Context()); result(w, v, e) })
	admin.HandleFunc("POST /api/v1/instances", a.saveInstance)
	admin.HandleFunc("PATCH /api/v1/instances/{id}", a.saveInstance)
	admin.HandleFunc("POST /api/v1/instances/{id}/test", a.testInstance)
	admin.HandleFunc("POST /api/v1/instances/{id}/collect", func(w http.ResponseWriter, r *http.Request) {
		id, e := pathID(r)
		if e == nil {
			e = a.Scheduler.Collect(r.Context(), id)
		}
		result(w, map[string]bool{"collected": e == nil}, e)
	})
	admin.HandleFunc("GET /api/v1/traffic/summary", a.traffic)
	admin.HandleFunc("GET /api/v1/traffic/history", a.traffic)
	admin.HandleFunc("GET /api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, "SELECT u.*, (SELECT MAX(last_active_at) FROM identities WHERE user_id=u.id) last_active_at,(SELECT json_group_array(json_object('instance_id',i.instance_id,'state',st.stats_state,'checked_at',st.checked_at)) FROM identities i JOIN (SELECT instance_id,email,stats_state,checked_at FROM xray_user_states UNION ALL SELECT instance_id,email,stats_state,checked_at FROM core_user_states) st ON st.instance_id=i.instance_id AND st.email=i.core_user_key WHERE i.user_id=u.id AND i.scope='user') stats_states FROM users u WHERE display_name LIKE ? ORDER BY display_name", "%"+r.URL.Query().Get("search")+"%")
	})
	admin.HandleFunc("GET /api/v1/identities", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, "SELECT i.*,x.name instance_name FROM identities i JOIN instances x ON x.id=i.instance_id WHERE i.scope='user' ORDER BY i.id")
	})
	admin.HandleFunc("PATCH /api/v1/users/{id}", a.patchUser)
	admin.HandleFunc("POST /api/v1/users/{id}/identities", a.mergeIdentity)
	admin.HandleFunc("GET /api/v1/online", func(w http.ResponseWriter, r *http.Request) { v, e := a.online(r.Context()); result(w, v, e) })
	admin.HandleFunc("GET /api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes, e := discovery.Nodes(r.Context(), a.Store)
		out := []map[string]any{}
		for _, n := range nodes {
			compat := map[string]string{}
			for _, f := range []string{"v2ray", "mihomo", "singbox"} {
				compat[f] = subscription.Compatible(f, n.Profile)
			}
			out = append(out, map[string]any{"node": n, "compatibility": compat})
		}
		result(w, out, e)
	})
	admin.HandleFunc("POST /api/v1/discovery/scan", a.scan)
	admin.HandleFunc("PATCH /api/v1/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]any
		if !body(w, r, &patch) {
			return
		}
		e := discovery.Patch(r.Context(), a.Store, r.PathValue("id"), patch)
		result(w, map[string]bool{"updated": e == nil}, e)
	})
	admin.HandleFunc("GET /api/v1/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		a.rows(w, r, "SELECT id,name,format,user_id,enabled,created_at,updated_at,(SELECT json_group_array(node_id) FROM subscription_nodes WHERE subscription_id=s.id ORDER BY position) node_ids FROM subscriptions s ORDER BY id")
	})
	admin.HandleFunc("POST /api/v1/subscriptions", a.saveSubscription)
	admin.HandleFunc("PATCH /api/v1/subscriptions/{id}", a.saveSubscription)
	admin.HandleFunc("POST /api/v1/subscriptions/{id}/rotate", func(w http.ResponseWriter, r *http.Request) {
		id, e := pathID(r)
		t := ""
		if e == nil {
			t, e = subscription.Rotate(r.Context(), a.Store, id)
		}
		result(w, map[string]string{"token": t}, e)
	})
	admin.HandleFunc("POST /api/v1/maintenance/archive", func(w http.ResponseWriter, r *http.Request) {
		n, e := a.Store.Archive(r.Context(), time.Now())
		result(w, map[string]int64{"deleted": n}, e)
	})
	admin.HandleFunc("POST /api/v1/maintenance/backup", func(w http.ResponseWriter, r *http.Request) {
		p, e := a.Store.Backup(r.Context(), a.Config.BackupDir)
		result(w, map[string]string{"backup": p}, e)
	})
	admin.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		mode := "instance"
		if len(a.Config.AllowedTargets) > 0 {
			mode = "restricted"
		}
		respond(w, 200, map[string]any{"collect_interval": a.Config.Interval.String(), "active_window": a.Config.ActiveWindow.String(), "timezone": a.Config.Timezone, "raw_retention_days": 30, "archive_enabled": a.Config.Archive, "log_level": a.Config.LogLevel, "target_policy": mode, "allowed_targets": a.Config.AllowedTargets, "configuration": "默认授权后台保存的实例地址；非空 TML_ALLOWED_TARGETS 启用严格限制。环境变量修改后重启管理服务生效；聚合时区固定"})
	})
	admin.HandleFunc("PATCH /api/v1/settings", a.settings)
	mux.Handle("/api/v1/", a.Auth.Protect(admin))
	mux.HandleFunc("GET /sub/{format}/{token}", a.output)
	files := http.FileServer(http.FS(web.Files))
	mux.Handle("GET /login.html", files)
	mux.Handle("GET /style.css", files)
	mux.Handle("GET /app.js", files)
	mux.Handle("GET /login.js", files)
	mux.Handle("/", a.Auth.Protect(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		mux.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func result(w http.ResponseWriter, v any, e error) {
	if e != nil {
		status := 400
		if errors.Is(e, sql.ErrNoRows) {
			status = 404
		}
		if errors.Is(e, collector.ErrBusy) {
			status = 409
		}
		respond(w, status, map[string]string{"error": e.Error()})
		return
	}
	respond(w, 200, v)
}
func body(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		respond(w, 400, map[string]string{"error": "请求字段或格式无效"})
		return false
	}
	return true
}
func pathID(r *http.Request) (int64, error) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}
func (a *API) rows(w http.ResponseWriter, r *http.Request, q string, args ...any) {
	v, e := a.Store.Rows(r.Context(), q, args...)
	result(w, v, e)
}
func (a *API) saveServer(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Enabled bool   `json:"enabled"`
	}
	if !body(w, r, &d) {
		return
	}
	d.Name = strings.TrimSpace(d.Name)
	d.Address = strings.TrimSpace(d.Address)
	if len(d.Name) < 1 || len(d.Name) > 200 || len(d.Address) > 253 {
		result(w, nil, fmt.Errorf("invalid server"))
		return
	}
	now := storage.Stamp(time.Now())
	res, e := a.Store.DB.ExecContext(r.Context(), "INSERT INTO servers(name,address,enabled,created_at,updated_at) VALUES(?,?,?,?,?)", d.Name, d.Address, d.Enabled, now, now)
	var id int64
	if e == nil {
		id, e = res.LastInsertId()
	}
	result(w, map[string]int64{"id": id}, e)
}
func (a *API) saveInstance(w http.ResponseWriter, r *http.Request) {
	var d struct {
		ServerID        int64   `json:"server_id"`
		Name            string  `json:"name"`
		CoreType        string  `json:"core_type"`
		APIEndpoint     string  `json:"api_endpoint"`
		ControlEndpoint string  `json:"control_endpoint"`
		ClashEndpoint   string  `json:"clash_endpoint"`
		ClashSecret     *string `json:"clash_secret"`
		APISecret       *string `json:"api_secret"`
		ConfigPath      string  `json:"config_path"`
		Version         string  `json:"version"`
		Enabled         bool    `json:"enabled"`
	}
	if !body(w, r, &d) {
		return
	}
	if d.ServerID < 1 || d.Name == "" || len(d.Name) > 200 || (d.CoreType != "xray" && d.CoreType != "v2fly" && d.CoreType != "singbox" && d.CoreType != "hysteria2") {
		result(w, nil, fmt.Errorf("invalid instance"))
		return
	}
	if e := security.ValidateEndpoint(d.APIEndpoint, d.CoreType == "hysteria2"); e != nil && !(d.CoreType == "singbox" && d.APIEndpoint == "" && (d.ControlEndpoint != "" || d.ClashEndpoint != "")) {
		result(w, nil, e)
		return
	}
	if d.ConfigPath != "" {
		if _, e := discovery.SafePath(a.Config.ConfigRoot, d.ConfigPath); e != nil {
			result(w, nil, e)
			return
		}
	}
	if d.ControlEndpoint != "" {
		if d.CoreType != "singbox" {
			result(w, nil, fmt.Errorf("control endpoint only applies to sing-box"))
			return
		}
		if e := security.ValidateEndpoint(d.ControlEndpoint, false); e != nil {
			result(w, nil, e)
			return
		}
	}
	if d.ClashEndpoint != "" {
		if d.CoreType != "singbox" {
			result(w, nil, fmt.Errorf("Clash endpoint only applies to sing-box"))
			return
		}
		if e := security.ValidateEndpoint(d.ClashEndpoint, true); e != nil {
			result(w, nil, e)
			return
		}
	}
	if len(a.Config.AllowedTargets) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		policy := security.Policy{Allowed: a.Config.AllowedTargets}
		for _, endpoint := range []string{d.APIEndpoint, d.ControlEndpoint, d.ClashEndpoint} {
			if endpoint != "" {
				if e := policy.Check(ctx, security.TargetAddress(endpoint)); e != nil {
					result(w, nil, e)
					return
				}
			}
		}
	}
	now := storage.Stamp(time.Now())
	secret := ""
	if d.APISecret != nil {
		secret = *d.APISecret
	}
	clashSecret := ""
	if d.ClashSecret != nil {
		clashSecret = *d.ClashSecret
	}
	var id int64
	var e error
	if r.Method == "PATCH" {
		id, e = pathID(r)
		if e == nil {
			var res sql.Result
			res, e = a.Store.DB.ExecContext(r.Context(), "UPDATE instances SET server_id=?,name=?,api_endpoint=?,control_endpoint=?,clash_endpoint=?,api_secret=CASE WHEN ? THEN ? ELSE api_secret END,clash_secret=CASE WHEN ? THEN ? ELSE clash_secret END,config_path=?,version=?,enabled=?,updated_at=? WHERE id=? AND core_type=?", d.ServerID, d.Name, d.APIEndpoint, d.ControlEndpoint, d.ClashEndpoint, d.APISecret != nil, secret, d.ClashSecret != nil, clashSecret, d.ConfigPath, d.Version, d.Enabled, now, id, d.CoreType)
			if e == nil {
				n, _ := res.RowsAffected()
				if n == 0 {
					e = fmt.Errorf("instance unavailable or core type cannot change")
				}
			}
		}
	} else {
		var res sql.Result
		res, e = a.Store.DB.ExecContext(r.Context(), "INSERT INTO instances(server_id,name,core_type,api_endpoint,api_secret,config_path,version,enabled,created_at,updated_at,control_endpoint,clash_endpoint,clash_secret) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", d.ServerID, d.Name, d.CoreType, d.APIEndpoint, secret, d.ConfigPath, d.Version, d.Enabled, now, now, d.ControlEndpoint, d.ClashEndpoint, clashSecret)
		if e == nil {
			id, e = res.LastInsertId()
		}
	}
	if e == nil {
		a.Scheduler.QueueDiagnosis(id, "instance_saved")
	}
	result(w, map[string]int64{"id": id}, e)
}
func (a *API) saveSubscription(w http.ResponseWriter, r *http.Request) {
	var d subscription.Definition
	if !body(w, r, &d) {
		return
	}
	var id int64
	var e error
	if r.Method == "PATCH" {
		id, e = pathID(r)
	}
	token := ""
	if e == nil {
		id, token, e = subscription.Save(r.Context(), a.Store, id, d)
	}
	result(w, map[string]any{"id": id, "token": token}, e)
}
func (a *API) scan(w http.ResponseWriter, r *http.Request) {
	var d struct {
		InstanceID int64 `json:"instance_id"`
	}
	if !body(w, r, &d) {
		return
	}
	list, e := a.Store.Instances(r.Context())
	if e != nil {
		result(w, nil, e)
		return
	}
	out := []map[string]any{}
	for _, i := range list {
		if (d.InstanceID == 0 || d.InstanceID == i.ID) && i.ConfigPath != "" {
			n, e := discovery.Scan(r.Context(), a.Store, i, a.Config.ConfigRoot)
			row := map[string]any{"instance_id": i.ID, "nodes": n}
			if e != nil {
				row["error"] = e.Error()
			}
			out = append(out, row)
		}
	}
	respond(w, 200, out)
}
func (a *API) output(w http.ResponseWriter, r *http.Request) {
	b, ct, rejected, e := subscription.Output(r.Context(), a.Store, r.PathValue("format"), r.PathValue("token"))
	if e != nil {
		http.Error(w, "订阅不可用", 404)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-TML-Filtered-Nodes", strconv.Itoa(len(rejected)))
	w.Write(b)
}
func (a *API) patchUser(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	var d struct {
		DisplayName string `json:"display_name"`
	}
	if !body(w, r, &d) {
		return
	}
	if e == nil && (d.DisplayName == "" || len(d.DisplayName) > 200) {
		e = fmt.Errorf("invalid display name")
	}
	if e == nil {
		_, e = a.Store.DB.ExecContext(r.Context(), "UPDATE users SET display_name=?,updated_at=? WHERE id=?", d.DisplayName, storage.Stamp(time.Now()), id)
	}
	result(w, map[string]bool{"updated": e == nil}, e)
}
func (a *API) mergeIdentity(w http.ResponseWriter, r *http.Request) {
	user, e := pathID(r)
	var d struct {
		IdentityID int64 `json:"identity_id"`
	}
	if !body(w, r, &d) {
		return
	}
	if e == nil {
		e = a.reassign(r.Context(), user, d.IdentityID)
	}
	result(w, map[string]bool{"updated": e == nil}, e)
}
func (a *API) reassign(ctx context.Context, user, id int64) error {
	tx, e := a.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var inst int64
	var key string
	if e = tx.QueryRowContext(ctx, "SELECT instance_id,core_user_key FROM identities WHERE id=? AND scope='user'", id).Scan(&inst, &key); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE identities SET user_id=? WHERE id=?", user, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE nodes SET user_id=? WHERE instance_id=? AND json_extract(effective_json,'$.user_key')=?", user, inst, key); e != nil {
		return e
	}
	return tx.Commit()
}
