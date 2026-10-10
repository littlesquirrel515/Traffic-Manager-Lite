# v1.3 多核心 API 检测与诊断统一化

核实日期：2026-10-10。官方 GitHub `releases/latest` 的非预发布正式版：Xray v26.3.27、Hysteria app/v2.13.0、sing-box v1.14.3、V2Fly v5.53.0。已核对对应 tag 的 schema/实现及 Windows/Linux 发布二进制 SHA256；不依据镜像 latest 推断实际版本。

## 原有问题

- 前端仅对 `core_type=xray` 渲染“API 诊断”；后台实例诊断服务也只接受 Xray，摘要查询限制 Xray。
- Hysteria2、sing-box、V2Fly 的旧调度先调用 Stats，失败即提前返回，Online 无法独立完成。
- 非 Xray 没有独立 Clients 资产采集、分组健康和持久诊断历史；未产生流量的用户需额外配置发现才能显示。
- 快速 API 检测只执行流量采集方法，不能给出分项认证、故障原因与实际限制。旧在线展示可能在 API 失败后短暂继续使用上次快照作离线判断。

本次在现有 Go 服务内增量增加 `internal/coremonitor`，保留 Xray 运行时枚举和已验证的统计/归档事务。未重写后台，没有挂载 Docker Socket，也没有重启或修改既有代理核心。

## 四核心能力对比

| 核心 | 完整 Clients | Stats | Online / 连接 | 实际版本 |
|---|---|---|---|---|
| Xray 26.3.27 | HandlerService 的 ListInbounds、GetInboundUsers、GetInboundUsersCount；运行时与文件分别保存 | QueryStats/GetStats reset=false，GetSysStats uptime；用户计数器无 inbound 维度 | 官方在线 map / IP 引用计数；缺项 Unknown，空列表不能判离线；不是设备数 | 官方 API 无版本字段；受控宿主版本证据，否则 Unknown |
| Hysteria2 2.13.0 | password 模式一个 `user` 身份；userpass 配置全部用户名；HTTP/command 必须提供认证后端完整导出 | GET /traffic，rx=客户端上传、tx=客户端下载；不清零 | GET /online 是客户端实例/设备数；GET /dump/streams 是 TCP 代理 QUIC 流；在线 IP Unsupported | Traffic Stats API 无版本方法；受控证据，否则 Unknown |
| sing-box 1.14.3 | 配置 inbounds.users（VLESS、Trojan、Hysteria2、AnyTLS 等）；三套 API 均不能枚举全部服务端用户 | V2Ray 兼容 StatsService 的实际用户/inbound 计数；原生 SubscribeStatus 的实例总量；Clash /connections 总量为备用，实例总量只保存一份 | 原生完整 reset 连接快照，按实际 user 映射 session；Clash 只能观测连接，元数据没有服务端用户身份，不推导用户在线/设备数 | 原生 GetVersion；Clash /version；安全证据备用。两个 API 版本不一致则 Unknown 并说明 |
| V2Fly 5.53.0 | HandlerService 只有增删改，没有 Xray 用户/Inbound 枚举接口；使用配置 clients/accounts | 官方独立 StatsService QueryStats reset=false、GetSysStats uptime | 逐用户在线/IP Unsupported；Observatory GetOutboundStatus 是出口健康，不是用户在线 | 官方管理 API 无版本字段；受控证据，否则 Unknown |

sing-box 发布构建是否包含 `with_v2ray_api`、是否配置 `experimental.v2ray_api.stats.enabled/users/inbounds` 必须分开判断。v1.14.3 官方发布构建本次实测原生与 Clash 可用，但不包含 V2Ray 兼容 API；不能由此判整个核心无法采集。用户统计的协议支持只按实际返回计数器确认，空结果不代表全部协议均支持或不支持。

V2Fly LoggerService 官方还有不稳定的 FollowLog 流；本工具不采集日志正文，禁止 RestartLogger。HandlerService / LoggerService 的注册状态可通过只读 ReflectionService ListServices 验证；未启用反射且没有可验证依据时 Unknown，不尝试执行用户变更作为探测。Observatory 可以只读查询，但不存在时不影响 Clients/Stats。

## 状态、独立采集与安全

诊断能力使用 Available、Unsupported、Disabled、AuthenticationFailed、Unreachable、Error、Unknown。只有实际成功的请求才说明 Available，空响应也可用；官方当前 schema 缺少的能力才声明 Unsupported。未配置 API 是 Unknown。已取得对应正式版版本证据且端点没有注册正式服务时 Disabled；单凭 Unimplemented 且版本未知时 Unknown。

每项记录方法、参数摘要、官方/实际检测依据、脱敏响应数量、代码、原因、耗时、时间、适用版本与修复建议。原始错误体、API Secret、Token、UUID、密码、IP/目标列表不进入诊断历史。认证失败（HTTP 401/403、gRPC Unauthenticated/PermissionDenied）与网络错误明确区分。

Clients、流量 cursor/增量/汇总、Online 快照及状态分别保存，再按实例+用户身份关联。文件失败或 API 断开保留上次资产；健康和状态显示失败或 Unknown，不把旧快照当作新离线结论。配置文件与运行时可能不同，只有 Xray 可通过运行时枚举比对。匿名/无用户名称资产不能可靠关联流量。

sing-box 只有实际观察过原生 user 映射的用户，才能把后续完整快照中缺失的连接判为离线；未验证协议映射时 Unknown。session、device、ip 分别说明，不将最近活跃冒充在线。

快速“检测 API”只读取基础能力，不推进业务状态，也不替换完整诊断历史。详细“API 诊断”可持久化报告，但不创建用户、Clients 资产、统计基线或更新采集健康。业务采集持久化健康，与手动诊断观测分开显示。

所有请求只读；gRPC reset=false；Hysteria2 永远不附加 clear，禁止 kick；不调用增删用户、连接关闭、日志重启或 URLTest。仍复用实例地址授权/严格白名单及每次拨号 DNS 验证。每个请求独立截止时间，新核心最大 3 秒/请求，Xray 每个采集组最大 8 秒，可通过较小的 TML 请求超时进一步缩短。

## 数据库迁移

- 007：新增 core_clients、core_user_states、core_diagnostics、core_runtime，以及实例可选 clash_endpoint。
- 008：新增独立 clash_secret（API 不返回）。未设置时使用已有 API Secret；编辑留空保留。
- 005/006 与旧迁移未修改；Xray 旧资产、健康、诊断历史通过统一后台查询保留。
- 旧 001–004 和 v1.3 001–006 升级均有验证；不会重解释已有每日汇总，不更改原 30 天归档规则。
- 每实例资产最多 10000、inbound 最多 256；每份响应最多 4 MiB；诊断历史每实例最多 200 条，查询最近 50 条。

## 主要新增与修改文件

- `internal/coremonitor/`：统一接口、四核心独立诊断、配置 Clients、版本证据、独立状态持久化及真实核心/故障隔离测试。
- `internal/collector/{scheduler,diagnostics}.go`：四核心统一接入，保持诊断和业务采集独立。
- `internal/httpapi/{api,diagnostics,instance_test_connection,stats}.go`、`internal/storage/query.go`、`internal/core/models.go`：统一管理 API、独立端点/Secret、状态查询；新增 API/升级回归测试。
- `internal/xraymonitor/{types,rpc,service,version,persist}.go`：复用原 Xray 实现，补齐统一报告字段、错误分类及版本证据入口。
- `internal/proto/v2flyobserve/`、`internal/proto/README.md`、`scripts/generate-proto.ps1`：隔离的官方 ObservatoryService wire 定义与出处。
- `migrations/007_core_diagnostics.sql`、`008_clash_secret.sql`：新增迁移；历史迁移不变。
- `web/{app.js,index.html,style.css}`：统一按钮、核心筛选、详情、实际版本、独立 Clash 参数及响应式页面。
- `scripts/{core-e2e.cjs,core-version-evidence.sh,download-test-cores.py}`：真实四核心浏览器验证、受控宿主证据、校验官方测试二进制；更新原浏览器测试与 CI。
- `README.md`、`AGENTS.md`、本说明和 `docs/verification.md`：升级、限制、人工配置与验收记录。

## 后台与 API

所有实例统一拥有“采集 / 检测 API / API 诊断 / 编辑”。左侧改为“核心 API 诊断”，可按核心、服务器、实例筛选；桌面表格、手机字段卡片、平板布局沿用现有页面。

新增统一 GET `/api/v1/cores/diagnostics?core_type=&server_id=&instance_id=`。保留 `/api/v1/xray/diagnostics` 兼容原 Xray 查询。

四核心复用现有鉴权/CSRF：

- POST `/api/v1/instances/{id}/test`：基础只读检测。
- GET/POST `/api/v1/instances/{id}/diagnostics`：最近详情/手动诊断。
- GET `/api/v1/instances/{id}/diagnostics/history`：历史。
- GET `/api/v1/instances/{id}/health`：业务采集健康。
- GET `/api/v1/instances/{id}/clients`：脱敏用户资产与统计/在线状态。

新增/编辑实例自动诊断；首次采集、版本改变、配置更新、连续失败与周期刷新重新检测能力。诊断与采集共享实例互斥，防止重复操作。

## 需要管理员配置

1. 让 TML 与核心加入同一 Docker 内部网络，填写真实 API 主机与端口；无须将管理 API 暴露公网。
2. Xray/V2Fly 必须按自身配置启用 StatsService、stats 与 policy；Xray 运行时枚举还需要 HandlerService。V2Fly 可选 ReflectionService、ObservatoryService，不得套用 Xray Online 方法。
3. Hysteria2 配置 trafficStats.listen/secret。sing-box 原生 services.type=api 的地址放“原生 gRPC 地址”，兼容统计地址单独填写；仅原生/Clash 时兼容统计地址可以留空。Clash 使用独立 HTTP 地址和可选独立 Secret；不混用三套协议定义。
4. 配置目录只读挂载至 TML_CONFIG_ROOT，后台填写 TML 容器内绝对路径。没有配置时 Hysteria2/sing-box/V2Fly 的完整 Clients 是 Unknown；不将 Stats 用户填进完整资产。
5. Hysteria2 HTTP/command 认证后端没有标准完整枚举协议。由管理员导出 `<配置路径>.clients.json`：

```json
{"complete":true,"observed_at":"2026-10-10T00:00:00Z","users":[{"name":"alice"},{"name":"bob"}]}
```

名称应等于认证后端实际返回的统计 ID；禁止包含密码/Token。文件必须在允许目录内，24h 有效；过期或无完整导出时 Unknown，保留旧资产。工具不会自动访问或执行认证后端。

6. API 无版本接口时，在 Docker **宿主机**执行固定版本证据脚本，不能在 TML 挂 Docker Socket：

```sh
sh scripts/core-version-evidence.sh hysteria2 hy-instance http://hy-instance:9999 /实际宿主路径/hysteria.yaml
sh scripts/core-version-evidence.sh v2fly v2ray-instance v2ray-instance:10085 /实际宿主路径/config.json
# Xray 委托现有专用脚本；sing-box 原生/Clash 通常直接返回版本
sh scripts/core-version-evidence.sh xray xray-multi xray-multi:10085 /实际宿主路径/config.json
```

只调用固定已知二进制的 version 子命令，绑定容器网络身份、明确的加载配置参数/挂载以及配置 SHA256，原子写入版本旁文件。需要宿主 Python3；不支持不能验证实际配置参数的 shell 启动器、confdir/合并配置。没有可信证据显示 Unknown。原始版本输出只放受控旁文件，不返回后台或保存到诊断报告。

## 测试与限制

真实官方二进制测试使用临时配置/数据库/本地回环连接；不会触碰生产代理。Go 测试包含全部离线资产、真实流量、认证/断连、无修改诊断、来源导出、旧库升级、不同核心协议隔离；故障注入 HTTP fixture 明确单独标注，不能冒充官方核心。

```sh
python3 scripts/download-test-cores.py # Alpine 测试选 --musl
TML_TEST_HYSTERIA2=/path/hysteria TML_TEST_SINGBOX_DIAGNOSTICS=/path/sing-box TML_TEST_V2FLY=/path/v2ray TML_TEST_XRAY=/path/xray go test -race ./...
go vet ./...
TML_PLAYWRIGHT_MODULE=/path/playwright TML_TEST_HYSTERIA2=/path/hysteria TML_TEST_SINGBOX_DIAGNOSTICS=/path/sing-box TML_TEST_V2FLY=/path/v2ray TML_TEST_XRAY=/path/xray node scripts/core-e2e.cjs
```

浏览器四核心实测 12 组核心/尺寸（360/768/1440），基础八页 24 组，以及 Xray 原有真实页面测试。准确结果见 [验证记录](verification.md)。

没有真实 VPS/Nginx 与实际生产配置，宿主脚本的生产容器路径仍需实际验收。API 没有启动标识且重启后计数仍高于旧值时，无法可靠识别重启；原可观测性限制保留。远端 gRPC 目前沿用内部网络明文端点，TLS 管理端点需要单独受控接入配置，不能把 HTTP URL 填成 gRPC 地址。复杂协议分享/订阅能力没有因此扩大。

## 官方依据

- [Xray v26.3.27](https://github.com/XTLS/Xray-core/releases/tag/v26.3.27)、[HandlerService](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/proxyman/command/command.proto)、[StatsService](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/stats/command/command.proto)。
- [Hysteria v2.13.0](https://github.com/apernet/hysteria/releases/tag/app%2Fv2.13.0)、[Traffic Stats 实现](https://github.com/apernet/hysteria/blob/app/v2.13.0/extras/trafficlogger/http.go)、[password 认证身份](https://github.com/apernet/hysteria/blob/app/v2.13.0/extras/auth/password.go)、[userpass 身份](https://github.com/apernet/hysteria/blob/app/v2.13.0/extras/auth/userpass.go)。
- [sing-box v1.14.3](https://github.com/SagerNet/sing-box/releases/tag/v1.14.3)、[原生服务](https://github.com/SagerNet/sing-box/blob/v1.14.3/daemon/started_service.proto)、[原生 API 配置](https://sing-box.sagernet.org/configuration/service/api/)、[兼容统计实现](https://github.com/SagerNet/sing-box/blob/v1.14.3/experimental/v2rayapi/stats.go)、[Clash 连接响应](https://github.com/SagerNet/sing-box/blob/v1.14.3/experimental/clashapi/connections.go)。
- [V2Fly v5.53.0](https://github.com/v2fly/v2ray-core/releases/tag/v5.53.0)、[HandlerService](https://github.com/v2fly/v2ray-core/blob/v5.53.0/app/proxyman/command/command.proto)、[StatsService](https://github.com/v2fly/v2ray-core/blob/v5.53.0/app/stats/command/command.proto)、[LoggerService](https://github.com/v2fly/v2ray-core/blob/v5.53.0/app/log/command/config.proto)、[ObservatoryService](https://github.com/v2fly/v2ray-core/blob/v5.53.0/app/observatory/command/command.proto)、[ReflectionService](https://github.com/v2fly/v2ray-core/blob/v5.53.0/app/commander/service.go)。
