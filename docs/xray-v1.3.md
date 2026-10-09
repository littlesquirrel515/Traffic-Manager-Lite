# Xray 用户资产、流量、在线及 API 诊断（v1.3）

核实日期：2026-10-09。GitHub `/releases/latest` 返回最新正式版 **v26.3.27**（非 prerelease）。本次直接修改现有管理服务；不修改代理配置或重启已有代理核心。

## 原有偏差及修正

旧实现没有独立的运行时 Clients Collector，用户主要来自 Stats、Online 或节点文件扫描。Stats 失败会提前终止整轮采集；GetAllOnlineUsers 空列表可能让没有在线统计项的用户被判离线；核心版本只是管理员声明。

新路径 `internal/xraymonitor` 独立调用 Clients、Stats、Online，各自拥有请求期限、健康记录和成功时间。某一类失败不会阻止另外两类；只在指定 inbound 的枚举成功且响应有效时更新该 inbound 的资产存在状态。失败保留上次资产，不删除统一用户或历史统计。

## 正式版 API 核实

| 分类 | 方法 | 正式版行为 / 配置 |
|---|---|---|
| Clients | ListInbounds | HandlerService；isOnlyTags=true，仅获取实际运行 inbound 标签，不请求完整配置 |
| Clients | GetInboundUsers | tag 指定 inbound，email 留空枚举当前 UserManager 用户；不要求在线或已有流量 |
| Clients | GetInboundUsersCount | tag 指定 inbound；枚举数量交叉检查，不一致时保留上次资产 |
| Stats | GetUsersStats | v26.3.27 schema 不存在此 RPC；有对应版本证据时 Unsupported/未调用，其他构建 Unknown |
| Stats | QueryStats | StatsService + stats:{}；reset=false，按实际返回计数器采集 |
| Stats | GetStats | reset=false；NotFound 是统计项未注册，不等于零流量 |
| Stats | GetSysStats | 获取 uptime 等运行指标；不包含版本/构建信息 |
| Online | GetAllOnlineUsers | 仅返回正计数在线 map 的 key；不是配置用户列表，空列表不能证明在线统计已启用 |
| Online | GetStatsOnline | 逐用户 map 计数；NotFound -> Unknown，成功零值才可按该接口口径判 Offline |
| Online | GetStatsOnlineIpList | IP 与最后观测时间；诊断只保存数量，不保存 IP 或用户凭据 |

官方固定版本来源：[HandlerService proto](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/proxyman/command/command.proto)、[HandlerService 实现](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/proxyman/command/command.go)、[StatsService proto](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/stats/command/command.proto)、[StatsService 实现](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/stats/command/command.go)、[OnlineMap](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/stats/online_map.go)、[Dispatcher](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/dispatcher/default.go)。

最新正式版 OnlineMap 使用 IP 引用计数，核心上下文结束时释放；不是 TCP/UDP 连接数或设备数，回环源地址可能不计入。旧版可能使用活动窗口，不能把正计数描述为确定的当前连接。页面按实际版本证据说明口径，未获得逐用户 map 时 Unknown。

同一 Email 出现在多个 inbound 时，Xray 用户计数器仍是 `user>>>Email>>>traffic>>>uplink/downlink`，没有 inbound 维度。资产按 inbound 分开存储，但统计身份为实例+Email，只累计一次；不能将这些用户流量精确拆回每个 inbound。需要 inbound 总量时独立启用系统 inbound 统计，不与用户层相加。

## 数据模型与差异展示

新增迁移 `005_xray_observability.sql` 和 `006_xray_client_fingerprints.sql`，原有迁移、cursor/增量/小时日汇总/归档逻辑保留：

- `xray_clients`：inbound 用户资产、来源 runtime_api/config_file、存在状态、观测时间。匿名用户只有不可逆资产 key，不关联 Email 流量。
- `xray_user_states`：按实例+Email 保存 zero/present/not_generated/unknown/reset_baseline 及 online/offline/unknown。原始累计仍由既有 cursor 管理。
- `collector_health`：三类采集器健康、数量、最近检查/成功、失败次数和脱敏摘要。
- `xray_diagnostics`：诊断能力/耗时/参数摘要/错误代码历史；每实例保留最近 200 条，历史接口返回最近 50 条。
- `xray_runtime`：实际版本证据及检测方式；无有效证据时 Unknown。

配置文件按只读根目录白名单读取，和运行时列表分别保存。同 inbound/Email 的协议和 level 分别比对；VLESS/VMess 的标准 UUID、Trojan/Shadowsocks 的密码，以及 VLESS Flow 通过持久本机随机密钥 HMAC 比对，页面只显示一致/不一致，不返回凭据或指纹。自定义 ID 映射及未覆盖协议显示无法可靠比对；matching_verified_fields 不证明 transport、TLS、SS cipher 或全部运行配置一致。不支持枚举的协议可显示文件资产，但文件不保证是实际加载版本；运行资产绝不悄悄合并。无 Email 的用户不能关联到 Email 计数器。

Stats 成功但没有该用户统计项 -> not_generated；真实返回双向零值 -> zero；失败 -> unknown；计数回退/可观测启动时间变更 -> reset_baseline。失败/缺项不清零旧 cursor，不删除 Clients。历史累计与当前原始统计项状态分别展示。

## 版本检测

当前 gRPC 服务没有可靠版本字段，GetSysStats 的 uptime 不能当版本。本管理容器继续禁止 Docker Socket，也不接受后台填写的任意 shell 命令。

可在 **Docker 宿主机** 按需运行固定只读脚本：

```bash
sh traffic-manager-lite/scripts/xray-version-evidence.sh \
  xray-multi xray-multi:10085 /实际宿主配置目录/config.json
```

脚本仅执行 `docker exec <container> <固定候选 xray 路径> version` 和 docker inspect；校验 endpoint 网络身份、配置 bind-mount 及明确的 -config/-c 参数，无法核实则拒绝写证据。旁边生成 `config.json.version.json`，需与 config.json 一起挂载到 TML 的只读配置根目录，且实例的配置路径指向该 config.json。

证据必须匹配 API endpoint、文件 SHA256，时间不得超过 24 小时；过期、格式错误、路径不匹配显示 Unknown。它依赖受信任宿主维护者，不是远程硬件证明。容器以 shell 脚本启动、confdir/多配置合并等无法核实的部署，不猜测实际版本。更新镜像/配置后重新执行脚本。测试直接执行官方二进制的证据标为 controlled_xray_version，不冒充 Docker Exec。

## 诊断与鉴权接口

- `GET /api/v1/xray/diagnostics?server_id=&instance_id=`：服务器/实例摘要。
- `GET /api/v1/instances/{id}/diagnostics`：最新 API 详情。
- `POST /api/v1/instances/{id}/diagnostics`：只读立即诊断；所有 reset=false，不创建身份或流量基线。
- `GET /api/v1/instances/{id}/diagnostics/history`：历史。
- `GET /api/v1/instances/{id}/health`：独立采集健康。
- `GET /api/v1/instances/{id}/clients`：带来源和差异的资产。

全部复用管理鉴权/CSRF；订阅 Token 不能访问。API 错误只保存 gRPC code 和固定脱敏说明，不保存服务端错误体、UUID、密码、完整配置或 account payload。只读 HandlerService projection 不生成任何增删用户 RPC。

Available 表示调用可用（NotFound 也可证明方法存在，但数据缺项）；Unsupported 表示明确的 unknown method 或已证实 v26.3.27 且正式版 schema 不存在；Disabled 仅在已证实 v26.3.27 且所需服务未注册时采用。没有版本证据的 Unimplemented -> Unknown，网络/超时 -> Error。不把配置文件推测当运行事实。

新增/修改 Xray 实例自动排队诊断；与同实例采集互斥。若排队时正在采集，下一轮通过 updated_at 识别更新。观测到版本变化立即记录新诊断，连续至少三次异常自动记录并以 5 分钟间隔限频；至少每日刷新一次。单轮按三类独立期限执行，最坏耗时可能约三倍 TML_COLLECT_TIMEOUT；手动 HTTP 请求取消/管理服务退出会终止后续请求。

## 测试

```bash
TML_TEST_XRAY=/已核验路径/xray \
TML_TEST_XRAY_PREVIOUS=/已核验旧版路径/xray \
go test -v ./internal/xraymonitor -run TestRealXrayCollectorsAndDiagnostics
TML_TEST_XRAY=/已核验路径/xray TML_PLAYWRIGHT_MODULE=/path/playwright node scripts/xray-e2e.cjs
```

这些测试启动独立的官方 Xray 进程及临时配置/数据库，通过实际 VLESS TCP 传输产生计数。API 单元夹具另行标注，不冒充真实集成。未设置二进制路径时真实集成测试显式 Skip。CI 已配置下载固定正式版并校验 SHA256；远程 CI 是否执行见验证报告。

升级前创建一致性备份，启动新镜像自动应用迁移。回退旧二进制不会消除新增表；不要删除/重写迁移或生产数据库。真实 VPS 的配置挂载、证据脚本和 API 联调仍需按实际部署核实。
