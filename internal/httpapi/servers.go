package httpapi

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
	"traffic-manager-lite/internal/storage"
)

func (a *API) getServer(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	rows, e := a.Store.Rows(r.Context(), "SELECT * FROM servers WHERE id=?", id)
	if e == nil && len(rows) == 0 {
		e = sql.ErrNoRows
	}
	if e != nil {
		result(w, nil, e)
		return
	}
	result(w, rows[0], nil)
}

func (a *API) updateServer(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Name    *string `json:"name"`
		Address *string `json:"address"`
		Enabled *bool   `json:"enabled"`
	}
	if !body(w, r, &d) {
		return
	}
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	if d.Name != nil {
		*d.Name = strings.TrimSpace(*d.Name)
		if len(*d.Name) < 1 || len(*d.Name) > 200 {
			respond(w, 400, map[string]string{"error": "服务器名称须为 1–200 字符"})
			return
		}
	}
	if d.Address != nil {
		*d.Address = strings.TrimSpace(*d.Address)
		if len(*d.Address) > 253 {
			respond(w, 400, map[string]string{"error": "服务器地址过长"})
			return
		}
	}
	res, e := a.Store.DB.ExecContext(r.Context(), "UPDATE servers SET name=COALESCE(?,name),address=COALESCE(?,address),enabled=COALESCE(?,enabled),updated_at=? WHERE id=?", d.Name, d.Address, d.Enabled, storage.Stamp(time.Now()), id)
	if e == nil {
		n, _ := res.RowsAffected()
		if n == 0 {
			e = sql.ErrNoRows
		}
	}
	result(w, map[string]int64{"id": id}, e)
}

func (a *API) deleteServer(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	// A conditional delete plus the foreign key prevents races with instance creation.
	res, e := a.Store.DB.ExecContext(r.Context(), "DELETE FROM servers WHERE id=? AND NOT EXISTS(SELECT 1 FROM instances WHERE server_id=?)", id, id)
	if e != nil {
		result(w, nil, e)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists int
		if e = a.Store.DB.QueryRowContext(r.Context(), "SELECT 1 FROM servers WHERE id=?", id).Scan(&exists); e != nil {
			result(w, nil, e)
			return
		}
		respond(w, 409, map[string]string{"error": "服务器仍有关联核心实例，请先编辑实例，将其迁移到其他服务器；不会删除历史统计"})
		return
	}
	result(w, map[string]bool{"deleted": true}, nil)
}
