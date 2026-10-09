package xraymonitor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"strings"
	"time"
	"traffic-manager-lite/internal/adapters/stats"
	"traffic-manager-lite/internal/core"
	sp "traffic-manager-lite/internal/proto/xray"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
	"traffic-manager-lite/internal/storage"
)

func (o *observation) check(group, api, params string, start time.Time, count int, e error) Check {
	service := "StatsService"
	required := "StatsService、stats:{}，用户流量需要对应 level 的上下行统计开关"
	if group == "clients" {
		service = "HandlerService"
		required = "HandlerService；Inbound 必须实现 UserManager"
	}
	if group == "online" {
		required = "StatsService、stats:{}、用户 level 的 statsUserOnline:true；仅已注册在线项能可靠判定"
	}
	c := Check{Group: group, API: api, Method: "/xray.app." + map[bool]string{true: "proxyman", false: "stats"}[group == "clients"] + ".command." + service + "/" + api, Params: params, Status: "Available", Count: count, DurationMS: time.Since(start).Milliseconds(), Code: status.Code(e).String(), Required: required, CheckedAt: storage.Stamp(time.Now())}
	if e != nil {
		c.Status = "Error"
		c.Reason = "请求失败（" + c.Code + "），未保存服务端错误体"
		c.Advice = "检查 API 地址、网络、权限和运行配置"
		switch status.Code(e) {
		case codes.Unimplemented:
			c.Status = "Unknown"
			c.Reason = "服务未启用或版本不支持，单凭 Unimplemented 无法区分"
			c.Advice = "核实运行版本及 api.services；不要依据镜像 latest 标签判断"
			if strings.Contains(status.Convert(e).Message(), "unknown method") {
				c.Status = "Unsupported"
				c.Reason = "运行实例未提供此方法"
			}
			if o.report.Version == "26.3.27" && strings.HasPrefix(status.Convert(e).Message(), "unknown service ") {
				c.Status = "Disabled"
				c.Reason = "版本有可靠宿主证据，但当前端点未注册所需 API 服务"
				c.Advice = "检查实际加载的 api.services，启用对应只读服务"
			}
		case codes.NotFound:
			c.Status = "Available"
			c.Reason = "接口正常，但指定统计项尚未注册；不是零值或离线"
		}
	}
	c.Requests = 1
	// Aggregate per-user online calls by method/result to keep history bounded.
	if group == "online" && api != "GetAllOnlineUsers" {
		for j := range o.report.Checks {
			old := &o.report.Checks[j]
			if old.API == c.API && old.Status == c.Status && old.Code == c.Code {
				old.Requests++
				old.Count += c.Count
				old.DurationMS += c.DurationMS
				old.CheckedAt = c.CheckedAt
				return c
			}
		}
	}
	o.report.Checks = append(o.report.Checks, c)
	return c
}
func healthFrom(group string, checks []Check, count int) Health {
	h := Health{Collector: group, Status: "Healthy", Count: count}
	for _, c := range checks {
		if c.Group == group && c.Status != "Available" {
			h.Status = c.Status
			h.Code = c.Code
			h.Summary = c.Reason
			return h
		}
	}
	return h
}
func (o *observation) clientsRPC(ctx context.Context, conn *grpc.ClientConn) {
	c := hp.NewHandlerServiceClient(conn)
	start := time.Now()
	r, e := c.ListInbounds(ctx, &hp.ListInboundsRequest{IsOnlyTags: true})
	n := 0
	if r != nil {
		n = len(r.Inbounds)
	}
	o.check("clients", "ListInbounds", "isOnlyTags=true（不请求敏感配置）", start, n, e)
	if e != nil {
		o.report.Health = append(o.report.Health, healthFrom("clients", o.report.Checks, 0))
		return
	}
	if n > 256 {
		o.report.Health = append(o.report.Health, Health{Collector: "clients", Status: "Error", Summary: "超过 256 个 inbound 采集上限"})
		return
	}
	o.listOK = true
	for _, in := range r.Inbounds {
		tag := in.Tag
		o.listedTags[tag] = true
		start = time.Now()
		users, e := c.GetInboundUsers(ctx, &hp.GetInboundUserRequest{Tag: tag})
		n = 0
		if users != nil {
			n = len(users.Users)
		}
		check := o.check("clients", "GetInboundUsers", "tag="+tag+", email=空（枚举全部用户）", start, n, e)
		if e != nil {
			if strings.Contains(status.Convert(e).Message(), "proxy is not a UserManager") {
				check.Status = "Unsupported"
				check.Reason = "此 inbound 的协议未实现 UserManager"
				o.report.Checks[len(o.report.Checks)-1] = check
			}
			continue
		}
		if n > 10000 || len(o.clients)+n > 10000 {
			o.report.Checks[len(o.report.Checks)-1].Status = "Error"
			o.report.Checks[len(o.report.Checks)-1].Reason = "超过每实例 10000 个用户资产的保护上限，保留旧数据"
			continue
		}
		start = time.Now()
		count, ce := c.GetInboundUsersCount(ctx, &hp.GetInboundUserRequest{Tag: tag})
		cn := 0
		if count != nil {
			cn = int(count.Count)
		}
		o.check("clients", "GetInboundUsersCount", "tag="+tag, start, cn, ce)
		if ce == nil && cn != n {
			o.report.Checks[len(o.report.Checks)-1].Status = "Error"
			o.report.Checks[len(o.report.Checks)-1].Reason = "枚举数量与 Count 不一致（可能有并发变更），保留已有用户"
			continue
		}
		valid := true
		batch := []Client{}
		for _, u := range users.Users {
			if u == nil || len(u.Email) > 512 {
				valid = false
				break
			}
			key := "email:" + u.Email
			proto := "unknown"
			fp := ""
			if u.Account != nil {
				proto = strings.TrimSuffix(strings.TrimPrefix(u.Account.Type, "xray.proxy."), ".Account")
				fields := accountStrings(u.Account.Value)
				flow := ""
				if proto == "vless" {
					flow = fields[2]
				}
				fp = fingerprint(o.fingerprintKey, proto, fields[1], flow)
				if u.Email == "" {
					key = fmt.Sprintf("anonymous:%x", sha256.Sum256(u.Account.Value))
				}
			}
			batch = append(batch, Client{Inbound: tag, Key: key, Email: u.Email, Protocol: proto, Level: u.Level, Source: "runtime_api", Fingerprint: fp})
			// Drop account bytes, including generated protobuf unknown fields, immediately.
			u.Account = nil
		}
		if valid {
			o.completeTags[tag] = true
			o.clients = append(o.clients, batch...)
		}
	}
	o.report.Health = append(o.report.Health, healthFrom("clients", o.report.Checks, len(o.clients)))
}
func (o *observation) statsRPC(ctx context.Context, conn *grpc.ClientConn, inst core.Instance) {
	c := sp.NewStatsServiceClient(conn)
	start := time.Now()
	r, e := c.QueryStats(ctx, &sp.QueryStatsRequest{Reset_: false})
	n := 0
	if r != nil {
		n = len(r.Stat)
	}
	o.check("stats", "QueryStats", "pattern=空, reset=false", start, n, e)
	if e != nil {
		o.report.Health = append(o.report.Health, healthFrom("stats", o.report.Checks, 0))
		return
	}
	counters := []stats.Counter{}
	for _, s := range r.Stat {
		if s == nil {
			continue
		}
		counters = append(counters, stats.Counter{Name: s.Name, Value: s.Value})
	}
	rows, pe := stats.Records(inst, counters)
	directions := map[string]int{}
	for _, counter := range counters {
		parts := strings.Split(counter.Name, ">>>")
		if len(parts) == 4 && parts[2] == "traffic" && (parts[0] == "user" || parts[0] == "inbound") {
			if parts[3] == "uplink" {
				directions[parts[0]+">>>"+parts[1]] |= 1
			}
			if parts[3] == "downlink" {
				directions[parts[0]+">>>"+parts[1]] |= 2
			}
		}
	}
	for _, direction := range directions {
		if direction != 3 {
			pe = fmt.Errorf("incomplete counter pair")
		}
	}
	if pe != nil {
		o.report.Checks[len(o.report.Checks)-1].Status = "Error"
		o.report.Checks[len(o.report.Checks)-1].Reason = "响应异常：统计项重复、负数或上下行不完整"
		o.report.Health = append(o.report.Health, Health{Collector: "stats", Status: "Error", Summary: "统计项重复、负数或上下行不完整"})
		return
	}
	start = time.Now()
	sys, se := c.GetSysStats(ctx, &sp.SysStatsRequest{})
	o.check("stats", "GetSysStats", "空请求；只能获取 uptime，不能获取版本", start, 0, se)
	if se == nil {
		boot := time.Now().Add(-time.Duration(sys.Uptime) * time.Second)
		for j := range rows {
			rows[j].BootEstimate = &boot
		}
	}
	o.records = rows
	count := 0
	for _, row := range rows {
		if row.Scope == "user" {
			count++
			state := "present"
			if row.UploadBytes == 0 && row.DownloadBytes == 0 {
				state = "zero"
			}
			o.states[row.UserKey] = &State{Email: row.UserKey, Stats: state, Online: "unknown"}
		}
	}
	// GetStats is probed once without resetting counters. QueryStats remains authoritative.
	key := "user>>>__tml_missing_probe__>>>traffic>>>uplink"
	if len(r.Stat) > 0 {
		key = r.Stat[0].Name
	}
	start = time.Now()
	one, ge := c.GetStats(ctx, &sp.GetStatsRequest{Name: key, Reset_: false})
	on := 0
	if one != nil && one.Stat != nil {
		on = 1
	}
	o.check("stats", "GetStats", "选定一项, reset=false", start, on, ge)
	o.report.Health = append(o.report.Health, Health{Collector: "stats", Status: "Healthy", Count: count, Summary: "按实例+Email 统计；不按 inbound 重复累加"})
}
func (o *observation) onlineRPC(ctx context.Context, conn *grpc.ClientConn) {
	c := sp.NewStatsServiceClient(conn)
	start := time.Now()
	r, e := c.GetAllOnlineUsers(ctx, &sp.GetAllOnlineUsersRequest{})
	n := 0
	if r != nil {
		n = len(r.Users)
	}
	o.check("online", "GetAllOnlineUsers", "空请求；返回正计数用户，不是 Clients 列表", start, n, e)
	if e != nil {
		o.report.Health = append(o.report.Health, healthFrom("online", o.report.Checks, 0))
		return
	}
	emails := map[string]bool{}
	for email := range o.states {
		if email != "" {
			emails[email] = true
		}
	}
	for _, key := range r.Users {
		emails[strings.TrimSuffix(strings.TrimPrefix(key, "user>>>"), ">>>online")] = true
	}
	sorted := []string{}
	for email := range emails {
		sorted = append(sorted, email)
	}
	sort.Strings(sorted)
	count := 0
	missing := 0
	failed := 0
	if len(sorted) > 10000 {
		o.report.Health = append(o.report.Health, Health{Collector: "online", Status: "Error", Summary: "超过 10000 个在线身份的保护上限"})
		return
	}
	for _, email := range sorted {
		if o.states[email] == nil {
			o.states[email] = &State{Email: email, Stats: "unknown"}
		}
		state := o.states[email]
		state.Online = "unknown"
		state.Count = nil
		state.Basis = "未获得可靠在线统计项；缺项不判定离线"
		key := "user>>>" + email + ">>>online"
		start = time.Now()
		s, se := c.GetStatsOnline(ctx, &sp.GetStatsRequest{Name: key, Reset_: false})
		returned := 0
		if s != nil && s.Stat != nil {
			returned = 1
		}
		o.check("online", "GetStatsOnline", "user 在线 key, reset=false", start, returned, se)
		if se != nil {
			if status.Code(se) == codes.NotFound {
				missing++
			} else {
				failed++
			}
			continue
		}
		if s.Stat == nil || s.Stat.Value < 0 {
			failed++
			continue
		}
		value := s.Stat.Value
		state.Count = &value
		state.Online = "offline"
		if value > 0 {
			state.Online = "online"
			count++
		}
		state.Basis = "成功读取在线统计项的 IP 计数；零仅表示该统计项无已追踪 IP，非设备数或连接数。版本未确认或旧版可能使用活动时间窗口；不保证当前连接仍存在，回环 IP 可能不计入"
		if o.report.Version == "26.3.27" {
			state.Basis = "v26.3.27 在线 map 的 IP 引用计数；零仅表示无已追踪 IP，非设备数/连接数。回环 IP 可能不计入；状态遵循核心上下文结束时的清理语义"
		}
		start = time.Now()
		ips, ie := c.GetStatsOnlineIpList(ctx, &sp.GetStatsRequest{Name: key, Reset_: false})
		ic := 0
		if ips != nil {
			ic = len(ips.Ips)
		}
		o.check("online", "GetStatsOnlineIpList", "user 在线 key, reset=false；不保存 IP 到诊断", start, ic, ie)
		o.online = append(o.online, core.OnlineRecord{UserKey: email, Count: value, Kind: "ip", CollectedAt: time.Now(), Source: "xray"})
	}
	h := Health{Collector: "online", Status: "Healthy", Count: count, Summary: "仅逐用户在线统计项成功时判定 Online/Offline"}
	if missing > 0 {
		h.Status = "Unknown"
		h.Summary = "部分在线项未注册：可能尚无连接或未启用 statsUserOnline；不推断离线"
	}
	if failed > 0 {
		h.Status = "Error"
		h.Summary = "部分在线请求失败；相关用户状态 Unknown"
	}
	// NotFound is an available RPC with an absent online map, never proof of disabled policy.
	if len(sorted) == 0 {
		h.Status = "Unknown"
		h.Summary = "没有可核实的在线统计项；空列表不证明功能已启用"
	}
	o.report.Health = append(o.report.Health, h)
}
