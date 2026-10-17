package remote

import (
	"strings"
	"traffic-manager-lite/internal/xraymonitor"
)

type Capability struct {
	Name      string `json:"capability"`
	Status    string `json:"status"`
	Evidence  string `json:"evidence"`
	CheckedAt string `json:"checked_at"`
	Version   string `json:"applicable_version"`
}

// Mutating methods are never invoked to probe capability. A registered service
// alone does not establish permission or support for every inbound/protocol.
func Capabilities(kind string, r xraymonitor.Report) []Capability {
	if r.Version == "" {
		r.Version = "Unknown"
	}
	out := []Capability{}
	for _, name := range []string{"health", "list_inbounds", "read_inbound", "list_users", "add_user", "update_user", "remove_user", "traffic", "online", "kick", "runtime_hot_update", "persistent_user_management"} {
		v := Capability{Name: name, Status: "unknown", Evidence: "未取得当前端点的有效方法证据", CheckedAt: r.CheckedAt, Version: r.Version}
		if name == "health" {
			switch r.Connected {
			case "Available":
				v.Status = "supported"
				v.Evidence = "实际只读 API 返回有效响应"
			case "Unreachable":
				v.Status = "unavailable"
				v.Evidence = "最近一次 API 连接失败或超时"
			}
		}
		if name == "kick" && kind == "hysteria2" {
			v.Evidence = "官方 POST /kick 存在；诊断不调用，写入权限未探测"
		}
		if name == "kick" && kind == "singbox" {
			v.Evidence = "原生 CloseConnection 只针对连接 ID；未执行破坏性探测，不声明完整用户踢出"
		}
		if name == "kick" && kind == "xray" {
			v.Status = "unsupported"
			v.Evidence = "所接 Handler/Stats API 无用户连接 Kick；RemoveUser 不等于终止现有连接"
		}
		if name == "persistent_user_management" {
			v.Status = "unsupported"
			v.Evidence = "所接原生 API 不保证配置持久化；运行时成功不能证明重启后仍存在"
		}
		if kind != "xray" && (name == "list_inbounds" || name == "read_inbound" || name == "list_users") {
			v.Status = "unsupported"
			v.Evidence = "已核对的官方原生服务不提供完整服务端配置/Clients 枚举；使用 manual_unverified 登记或可选配置来源"
		}
		if (kind == "singbox" || kind == "hysteria2") && (name == "add_user" || name == "update_user" || name == "remove_user" || name == "runtime_hot_update") {
			v.Status = "unsupported"
			v.Evidence = "已接原生统计/观测 API 无服务端用户写入方法；外部认证后端或可选代理需单独授权"
		}
		if kind == "v2fly" && (name == "online" || name == "kick") {
			v.Status = "unsupported"
			v.Evidence = "官方接口没有完整用户在线枚举/Kick；Observatory 是出口观测"
		}
		for _, c := range r.Checks {
			match := name == "health" && c.Status == "Available" && c.Requests > 0 || name == "traffic" && c.Group == "stats" || name == "online" && c.Group == "online" || name == "list_inbounds" && c.API == "ListInbounds" || name == "list_users" && c.API == "GetInboundUsers" || name == "read_inbound" && c.API == "ListInbounds" && strings.Contains(c.Params, "false")
			if !match {
				continue
			}
			state := map[string]string{"Available": "supported", "Unsupported": "unsupported", "Unknown": "unknown", "Disabled": "unavailable", "AuthenticationFailed": "permission_denied", "Unreachable": "unavailable", "Error": "unavailable"}[c.Status]
			if state == "" {
				state = "unknown"
			}
			// One optional failing metric must not overwrite evidence of another
			// verified method in the same broad capability.
			if v.Status != "supported" {
				v.Status = state
				v.Evidence = c.Method + "; " + c.Reason + "; " + c.Evidence
			}
		}
		out = append(out, v)
	}
	return out
}
