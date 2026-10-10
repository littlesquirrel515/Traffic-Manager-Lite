package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"traffic-manager-lite/internal/coremanage"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
)

func (a *API) telemetry(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	p, e := a.Store.Rows(r.Context(), "SELECT * FROM provider_observations WHERE instance_id=? ORDER BY provider_type,metric_scope", id)
	if e != nil {
		result(w, nil, e)
		return
	}
	traffic, e := a.Store.Rows(r.Context(), `SELECT p.*,i.inbound_tag,i.core_user_key,i.scope,i.user_id FROM traffic_provenance p JOIN identities i ON i.id=p.identity_id WHERE i.instance_id=?`, id)
	connections, err := a.Store.Rows(r.Context(), "SELECT * FROM core_connection_snapshots WHERE instance_id=?", id)
	if err != nil {
		result(w, nil, err)
		return
	}
	result(w, map[string]any{"connections": connections, "providers": p, "traffic": traffic, "direction": "客户端上传/下载", "native_user_coverage": "只累计实际观测连接差值，非完整历史用户总流量"}, e)
}
func (a *API) configHistory(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	a.rows(w, r, "SELECT * FROM config_operations WHERE instance_id=? ORDER BY id DESC LIMIT 100", id)
}
func (a *API) managedConfig(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	var kind string
	if e = a.Store.DB.QueryRowContext(r.Context(), "SELECT core_type FROM instances WHERE id=?", id).Scan(&kind); e != nil {
		result(w, nil, e)
		return
	}
	if kind != "singbox" {
		http.Error(w, "only sing-box is managed", 400)
		return
	}
	if a.Config.CoreAgentURL == "" || len(a.Config.CoreAgentToken) < 32 {
		respond(w, 503, map[string]string{"error": "未启用受控宿主配置代理；监控服务仍为只读"})
		return
	}
	endpoint, e := url.Parse(a.Config.CoreAgentURL)
	if e != nil || endpoint.Scheme != "http" && endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		http.Error(w, "invalid configured agent endpoint", 503)
		return
	}
	endpoint.Path = "/instances/" + strconv.FormatInt(id, 10) + "/config"
	var input coremanage.Request
	var body []byte
	if r.Method == "POST" {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			http.Error(w, "invalid configuration operation", 400)
			return
		}
		if input.Operation == "delete" && !input.ConfirmDelete {
			http.Error(w, "删除用户必须再次确认", 400)
			return
		}
		if (input.Operation == "apply" || input.Operation == "rollback") && !input.ConfirmRestart {
			http.Error(w, "核心重启必须再次确认并接受连接中断", 400)
			return
		}
		body, _ = json.Marshal(input)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, r.Method, endpoint.String(), bytes.NewReader(body))
	if e != nil {
		result(w, nil, e)
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.Config.CoreAgentToken)
	req.Header.Set("Content-Type", "application/json")
	client := (security.Policy{}).ForEndpoints(a.Config.CoreAgentURL).HTTP()
	client.Timeout = 80 * time.Second
	transport := client.Transport.(*http.Transport)
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
		ip := net.ParseIP(host)
		if err != nil || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
			conn.Close()
			return nil, errors.New("host agent must use a private or loopback address")
		}
		return conn, nil
	}
	defer client.CloseIdleConnections()
	resp, e := client.Do(req)
	var answer coremanage.Result
	status := 502
	if e == nil {
		defer resp.Body.Close()
		status = resp.StatusCode
		if status == 200 {
			e = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&answer)
		}
	}
	if r.Method == "POST" {
		applyStatus := answer.Status
		if e != nil || status != 200 {
			applyStatus = "OperationFailed"
		}
		_, err := a.Store.DB.ExecContext(context.WithoutCancel(r.Context()), "INSERT INTO config_operations(instance_id,operation,inbound_tag,old_revision,new_revision,apply_status,created_at,summary) VALUES(?,?,?,?,?,?,?,?)", id, input.Operation, input.Inbound, input.Revision, answer.Revision, applyStatus, storage.Stamp(time.Now()), "受控宿主代理；用户 "+input.PreviousName+" -> "+input.User.Name+"；无明文凭据；HTTP "+strconv.Itoa(status))
		if err != nil {
			http.Error(w, "operation completed but audit persistence failed; inspect host journal", 500)
			return
		}
	}
	if e != nil || status != 200 {
		if status == 409 {
			http.Error(w, "配置已被修改，请重新读取后操作", 409)
		} else {
			http.Error(w, "宿主配置操作失败；原文件/恢复状态请查宿主审计，凭据未记录", 502)
		}
		return
	}
	respond(w, 200, answer)
}
func (a *API) directionAudit(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	var kind string
	e = a.Store.DB.QueryRowContext(r.Context(), "SELECT core_type FROM instances WHERE id=?", id).Scan(&kind)
	if e != nil {
		result(w, nil, e)
		return
	}
	if kind != "hysteria2" {
		http.Error(w, "Hysteria2 audit only", 400)
		return
	}
	counts, e := a.Store.Rows(r.Context(), `SELECT (SELECT count(*) FROM traffic_samples WHERE instance_id=?) samples,(SELECT count(*) FROM traffic_hourly WHERE instance_id=?) hourly,(SELECT count(*) FROM traffic_daily WHERE instance_id=?) daily,(SELECT count(*) FROM archive_ledger l JOIN identities i ON i.id=l.identity_id WHERE i.instance_id=?) archived_hourly,(SELECT count(*) FROM identities i LEFT JOIN traffic_provenance p ON p.identity_id=i.id WHERE i.instance_id=? AND p.identity_id IS NULL) direction_unverified_identities`, id, id, id, id, id)
	if e != nil {
		result(w, nil, e)
		return
	}
	unverified, e := a.Store.Rows(r.Context(), `SELECT COUNT(*) samples_without_source_evidence FROM traffic_samples s LEFT JOIN traffic_counter_observations o ON o.identity_id=s.identity_id AND o.collected_at=s.collected_at WHERE s.instance_id=? AND o.identity_id IS NULL`, id)
	if e != nil {
		result(w, nil, e)
		return
	}
	repairs, e := a.Store.Rows(r.Context(), "SELECT source_profile,cutoff,affected_records,created_at FROM traffic_direction_repairs WHERE instance_id=? ORDER BY created_at DESC", id)
	result(w, map[string]any{"dry_run": true, "counts": counts, "unverified": unverified, "repairs": repairs, "confirmed_wrong_records": 0, "repair_performed": false, "reason": "已确认旧 TML 映射反转，新版 Upload=tx/Download=rx。历史逐条来源缺失时需审核实例历史来源后使用离线修复命令；不能盲目交换整个数据库。"}, e)
}
