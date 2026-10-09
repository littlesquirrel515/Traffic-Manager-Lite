package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

func (a *API) filter(r *http.Request) (storage.Filter, error) {
	q := r.URL.Query()
	f := storage.Filter{From: q.Get("from"), To: q.Get("to"), Dimension: q.Get("dimension"), Scope: q.Get("scope"), Inbound: q.Get("inbound"), Protocol: q.Get("protocol"), NodeID: q.Get("node_id")}
	f.Cycle = q.Get("range") == "cycle"
	for k, p := range map[string]*int64{"user_id": &f.UserID, "instance_id": &f.InstanceID, "server_id": &f.ServerID} {
		if q.Get(k) != "" {
			n, e := strconv.ParseInt(q.Get(k), 10, 64)
			if e != nil || n < 1 {
				return f, fmt.Errorf("invalid filter")
			}
			*p = n
		}
	}
	now := time.Now().In(a.Store.Location)
	today := now.Format("2006-01-02")
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, a.Store.Location)
	switch q.Get("range") {
	case "today":
		f.From = today
		f.To = today
	case "yesterday":
		f.From = now.AddDate(0, 0, -1).Format("2006-01-02")
		f.To = f.From
	case "7d":
		f.From = now.AddDate(0, 0, -6).Format("2006-01-02")
		f.To = today
	case "30d":
		f.From = now.AddDate(0, 0, -29).Format("2006-01-02")
		f.To = today
	case "month":
		f.From = month.Format("2006-01-02")
		f.To = today
	case "last_month":
		f.From = month.AddDate(0, -1, 0).Format("2006-01-02")
		f.To = month.AddDate(0, 0, -1).Format("2006-01-02")
	case "", "all", "custom", "cycle":
	default:
		return f, fmt.Errorf("invalid range")
	}
	for _, v := range []string{f.From, f.To} {
		if v != "" {
			if _, e := time.Parse("2006-01-02", v); e != nil {
				return f, fmt.Errorf("dates must be YYYY-MM-DD")
			}
		}
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		return f, fmt.Errorf("invalid date range")
	}
	return f, nil
}
func (a *API) traffic(w http.ResponseWriter, r *http.Request) {
	f, e := a.filter(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	v, e := a.Store.Traffic(r.Context(), f, strings.HasSuffix(r.URL.Path, "history"))
	result(w, v, e)
}
func (a *API) dashboard(w http.ResponseWriter, r *http.Request) {
	now := time.Now().In(a.Store.Location)
	today := now.Format("2006-01-02")
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, a.Store.Location).Format("2006-01-02")
	out := map[string]any{"timezone": a.Config.Timezone, "updated_at": storage.Stamp(time.Now())}
	for _, p := range []struct{ k, f, t string }{{"today", today, today}, {"month", month, today}, {"all", "", ""}} {
		v, e := a.Store.Traffic(r.Context(), storage.Filter{From: p.f, To: p.t}, false)
		if e != nil {
			result(w, nil, e)
			return
		}
		tot := map[string]any{"upload": int64(0), "download": int64(0)}
		if len(v) > 0 {
			tot = v[0]
		}
		out[p.k] = tot
	}
	hist, e := a.Store.Traffic(r.Context(), storage.Filter{From: now.AddDate(0, 0, -29).Format("2006-01-02"), To: today}, true)
	if e != nil {
		result(w, nil, e)
		return
	}
	out["history"] = hist
	cores, e := a.Store.Traffic(r.Context(), storage.Filter{Dimension: "core"}, false)
	if e != nil {
		result(w, nil, e)
		return
	}
	out["cores"] = cores
	instances, e := a.Store.Instances(r.Context())
	if e != nil {
		result(w, nil, e)
		return
	}
	out["instances"] = instances
	online, e := a.online(r.Context())
	if e != nil {
		result(w, nil, e)
		return
	}
	out["online"] = online
	respond(w, 200, out)
}
func (a *API) online(ctx context.Context) ([]map[string]any, error) {
	insts, e := a.Store.Instances(ctx)
	if e != nil {
		return nil, e
	}
	ident, e := a.Store.Rows(ctx, "SELECT i.id,i.instance_id,i.user_id,i.core_user_key,i.inbound_tag,i.last_active_at,u.display_name FROM identities i JOIN users u ON u.id=i.user_id WHERE i.scope='user'")
	if e != nil {
		return nil, e
	}
	snapshots, e := a.Store.Rows(ctx, "SELECT * FROM online_snapshots")
	if e != nil {
		return nil, e
	}
	snaps := map[int64]map[string]any{}
	for _, row := range snapshots {
		snaps[row["instance_id"].(int64)] = row
	}
	instMap := map[int64]core.Instance{}
	for _, i := range insts {
		instMap[i.ID] = i
	}
	out := []map[string]any{}
	now := time.Now()
	for _, i := range ident {
		id := i["instance_id"].(int64)
		inst := instMap[id]
		i["instance_name"] = inst.Name
		i["core_type"] = inst.CoreType
		i["status"] = "unknown"
		i["active"] = false
		i["count"] = nil
		i["kind"] = nil
		i["updated_at"] = nil
		if s, ok := i["last_active_at"].(string); ok {
			t, er := time.Parse(time.RFC3339Nano, s)
			i["active"] = er == nil && now.Sub(t) <= a.Config.ActiveWindow
		}
		supported := false
		explicitUnsupported := false
		for _, c := range inst.Capabilities {
			if c.Metric == "online" && c.Status == "unsupported" {
				i["status"] = "unsupported"
			}
			if (c.Metric == "online_ip" || c.Metric == "online_devices" || c.Metric == "online_sessions") && c.Status == "supported" {
				supported = true
			}
			if c.Metric == "online_ip" && c.Status == "unsupported" && inst.CoreType == "xray" {
				i["status"] = "unsupported"
				explicitUnsupported = true
			}
			if c.Metric == "online_sessions" && c.Status == "unsupported" {
				explicitUnsupported = true
				i["status"] = "unsupported"
			}
		}
		if _, ok := snaps[id]; ok && !explicitUnsupported && (inst.CoreType == "xray" || inst.CoreType == "singbox") {
			supported = true
		}
		if inst.CoreType == "v2fly" || (inst.CoreType == "singbox" && !supported) {
			i["status"] = "unsupported"
		}
		if inst.CoreType == "hysteria2" {
			supported = true
		}
		if snap, ok := snaps[id]; ok && supported {
			updated := snap["updated_at"].(string)
			t, er := time.Parse(time.RFC3339Nano, updated)
			i["updated_at"] = updated
			if er != nil || now.Sub(t) > 2*a.Config.Interval+a.Config.Timeout {
				i["status"] = "stale"
			} else {
				i["status"] = "offline"
				i["count"] = int64(0)
				var records []core.OnlineRecord
				if e = json.Unmarshal([]byte(snap["records_json"].(string)), &records); e != nil {
					return nil, e
				}
				for _, o := range records {
					if o.UserKey == i["core_user_key"] {
						i["count"] = o.Count
						i["kind"] = o.Kind
						i["ips"] = o.IPs
						if o.Count > 0 {
							i["status"] = "online"
						}
						break
					}
				}
			}
		}
		out = append(out, i)
	}
	return out, nil
}
