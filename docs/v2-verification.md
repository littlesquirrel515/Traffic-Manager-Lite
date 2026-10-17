# v2.0 实施与验收记录（2026-10-15）

## 已实施

1. 清理：旧 nodes/subscriptions/subscription_nodes/legacy_node_redirects 不进入新库；移除旧节点/订阅 HTTP 授权、独立手动节点/手动节点凭据模型、旧 URL 兼容、旧菜单/表单、配置调和计划/动作/策略表、旧调和模块与恢复命令、配置生命周期前端和对应 HTTP 分支。纯渲染、四核心采集、存储/归档、管理鉴权复用。
2. 新基线：单一 001_v2_initial.sql；新库空业务数据，application_id/user_version/baseline 所有权标记和初始 SQL 指纹（LF 规范化）；旧库/外部库在任何写入前拒绝。旧迁移只是历史审计资料。
3. 重建范围：所有 TML 数据重新建立，包括基础资源、客户端/凭据、节点/组、订阅用户/Token、流量/游标/归档、配额、观测和历史；不向远程核心发送任何清理请求。
4. 备份：tml-baseline 检查归属、生成一致性数据库备份、完整性验证、表计数、空白候选库和校验和。工具没有执行 reset 的能力。
5. **生产重建：已执行。** 用户最终明确确认后，于 2026-10-15 11:16 UTC 完成停服备份和 v2 切换；资产、订阅授权及流量从空白基线开始。代理核心没有被本任务写入或重启。
6. 能力矩阵：四核心提供实际诊断和独立矩阵页面，含证据/时间/版本及 supported/unsupported/unknown/unavailable/permission_denied，不把临时错误/空列表当不支持。
7. 适配器：Xray Handler 完整序列化配置投影，Xray/V2Fly 独立官方 wire 服务，sing-box 原生/V2Ray/Clash 分开，Hysteria2 官方只读统计。
8. 入站登记：API-only 实例表单无需文件路径；native_api/manual/agent/optional_config 来源模型。人工登记初始 manual_unverified，Xray API 解析失败保留资产。
9. 连接绑定：沿用显式选入站、外部端口与监听分离、幂等原子生成；配置字段变化暂停相关发布，保留 Endpoint/Node 身份。
10. 节点：无认证定义，默认绑定节点稳定，多入口复用真实客户端。普通创建通过连接入口，不开放旧独立节点 POST。
11. 用户：已有真实客户端登记独立于 Subscriber；不因统计为空/全部离线而删除。运行时变更需明确确认和加密 Desired；Xray 回读、V2Fly 未回读状态区分。
12. 恢复：Xray 逐 Desired 授权后仅补回已验证缺失的账号，默认关闭，不替换冲突；目标地址/Tag/协议改变停止恢复。运行时成功不冒充核心配置持久化。
13. 计费：活动来源唯一归属数据库约束与事务检查，绑定基线/解绑冻结，空白账本首次观测不追收核心历史，默认 10 GiB/3 日历月，多订阅共享。
14. 输出：保留 Raw/Base64/v2rayN、Mihomo/sing-box 完整/集合模板及实际已验证协议范围；XHTTP 按 Renderer 限制过滤。
15. UI：入站/入口/分组/Subscriber/订阅/配额/诊断/矩阵/日志；普通实例没有配置路径字段，未接入旧全局节点库/旧调和页面。54 个页面/尺寸组合和新增入口实时预览流程通过。
16. 测试：Windows `go test ./...`、`go vet ./...`；Mihomo 1.19.32 和 sing-box 1.14.3 官方配置校验，XHTTP TLS/REALITY 四种 mode；临时数据库浏览器端到端流程通过。Docker/Linux 验收见下方状态，不以夹具流量冒充生产数据。
17. 文档/部署：README、AGENTS、领域/远程架构/计费/订阅/重建说明更新，默认 Compose 删除配置挂载，镜像加入只读准备工具且不再打包旧历史修复命令。
18. 新用户步骤：Server → API 实例 → 检测/诊断 → native/manual 入站及真实客户端 → Endpoint/Node → Group → Subscriber/Quota → 凭据绑定 → Subscription → 首次基线后增量。

## 生产备份准备

已验证现有容器 `/app/data` 实际绑定 `/data/traffic-manager-lite/data`，数据库为 TML 独占结构，未发现其他应用表。

备份目录：`/data/traffic-manager-lite/backups/v2-preflight-20261015-061300`。

- `database/before.db`：SQLite 一致性备份，integrity_check=ok。
- `database-final-20261015-063242/v2-empty.db`：最终空白候选库，含严格入站/凭据绑定关系和 schema 指纹。早期 `database/v2-empty.db` 只作准备过程审计，不作为最终切换文件。
- `database/manifest.json`：全部表记录数量、文件 SHA256、reset_executed=false。
- `.env`、`docker-compose.yml`、源码归档、独立 Desired 密钥（配置存在时）、部署清单和旧镜像标签均保留；目录及敏感文件权限收紧。
- 旧镜像保留为 `traffic-manager-lite:pre-v2-20261015-061300`。
- 备份 SHA256：`aec6b0af2313b69fa8f7bfb65499ce8194a66d4dbf356a01ad30e3cbf11405cc`。
- 空白候选库 SHA256：`0fc7cdb4d8a8a6ddcafcf07205d715a4ba895b4edf8ce77afbd2cd73ea95bc28`。

快照关键计数：3 台 Server、3 个实例、13 个 Inbound、14 个 Endpoint、14 个入口绑定、16 个 Node、14 份凭据、1 个分组、1 个 Subscriber、2 个现用订阅、6 个旧订阅、47,007 条流量明细、172,692 次计数来源观察、190 条小时汇总、22 条日汇总。完整计数见私有 manifest。生产采集继续运行，最终确认后停服备份必须重新统计。

准备前后 Xray-multi、Xray-reality、Hysteria2、sing-box、Nginx 的 StartedAt 完全相同。

## 限制与人工事项

- 原生 API 未提供的能力不伪造；HY、sing-box 完整服务端配置/Clients 和 V2Fly 完整用户枚举需手动登记或独立授权来源。
- 核心无可信版本接口时 Unknown，不由 Docker latest 推断；人工声明不等于运行证据。
- 原生用户写入不保证配置持久化；V2Fly 无完整回读，禁止自动替换/恢复。统计诊断不通过写请求验证 Kick/用户写入权限。
- sing-box 原生用户流量为观测差值，partial；复杂未知 XHTTP/插件/ECH 等组合继续保守过滤。
- 原生 API 无不可变入站 ID；单纯外部 Tag 变化缺少确定证据时不能猜测同一身份，需管理员用稳定 ID 明确登记更新/核对映射。
- 新入口不配置 Cloudflare/Nginx/Docker 转发，也不测试真实客户端连接；这些需要管理员维护。配置字段完整不代表公网连通。
- 准备备份不等于生产 reset。最终切换前必须按用户 v2 §1.1.7 再明确确认；切换使现有全部订阅 URL/Token 失效，需要重新建立。

## Docker / Linux 状态

最终候选镜像：`traffic-manager-lite:v2-candidate-20261015-063939`。隔离运行目录：`/tmp/tml-v2-isolated-20261015-063939`。

实际通过：运行包 Docker build、全新空库初始化、最终候选 DB 副本启动与 schema 指纹匹配、UID 10001 非 root、只读根文件系统、无 Socket、network=none、重启持久化、旧 v1 DB 副本拒绝且 SHA256 不变。最终候选 DB 原文件未被运行容器修改。生产 TML 及五个代理/反代容器的 StartedAt 与准备前一致。

本次在 VPS 使用本地交叉编译的 Linux 程序和已固定的现有 Alpine 运行基础构建隔离镜像，以避免共享生产磁盘上下载完整 SDK/缓存。**未在本轮重新执行完整多阶段 SDK 构建或 Linux race**；没有把既往 v1 race 结果算作本轮结果。Dockerfile 的独立 test 阶段保留可重现命令。

官方独立临时进程实测：Xray 26.3.27 完整序列化入站配置、用户添加/替换/删除回读，V2Fly 5.53.0 添加/删除 ACK 与 runtime_only 区分，两者原配置文件字节不变；Hysteria2 2.13.0 三个 HTTP API、认证/网络、空列表、4 MiB 真上传/下载方向；sing-box 1.14.3 原生/Clash、四种协议真实局部流量方向及归属、兼容接口失败隔离。都是本地隔离测试，不是生产流量或生产账号操作。

Windows 最终 `go test ./...`、`go vet ./...`、Windows/Linux 编译、Mihomo/sing-box 官方配置校验、54 页面尺寸浏览器流程通过；手机截图另行检查并改为完整字段卡片布局。最新源码内的旧 Xray 诊断别名条件也已移除。


## 主要新增/修改文件

| 范围 | 文件 |
|---|---|
| 初始数据库/拒绝旧库 | migrations/001_v2_initial.sql；internal/storage/storage.go、baseline.go、baseline_test.go；.gitattributes |
| 离线备份准备 | cmd/tml-baseline/main.go；internal/baseline/prepare.go、prepare_test.go |
| 远程注册/发现/能力/运行时 | internal/remote/registry.go、xray.go、capabilities.go、runtime.go、reconnect.go；remote_test.go、official_test.go、attribution_test.go |
| 官方 wire 投影 | internal/proto/xrayhandler/handler.proto 及生成的两个 Go 文件；internal/proto/README.md |
| 接口与流程 | internal/httpapi/api.go、remote.go、catalog.go、catalog_public.go、quota.go、inbound_endpoints.go、diagnostics.go；对应 API 测试 |
| 独立采集/配置可选化 | cmd/traffic-manager-lite/main.go；internal/collector/scheduler.go；internal/coremonitor/clients.go；internal/discovery/reconcile.go、sync.go；internal/inboundidentity/identity.go、merge.go |
| 计费与资源 | internal/subscription/catalog_store.go、quota.go；internal/storage/query.go；internal/catalog/catalog.go、bindings.go、merge.go |
| 界面与浏览器 | web/app.js、index.html、catalog.js、inbounds.js、quota.js、remote.js、style.css；scripts/e2e.cjs |
| 发布/规范 | Dockerfile；deploy/docker-compose.yml；.env.example；README.md；AGENTS.md；领域、远程、计费、订阅、重建与本报告 |

移除：旧可执行迁移链；internal/reconciliation；cmd/tml-recovery；旧配置调和/协议迁移 HTTP 页面实现；web/reconciliation.js；旧节点/订阅业务授权与迁移兼容测试。历史 SQL/旧文档只放 docs/history，不参与初始化。成熟增量、归档、隔离诊断、Renderer 和安全测试继续运行。

## 最终确认对应的具体动作

仅切换 `/data/traffic-manager-lite/data/traffic.db` 和 TML 管理镜像/源码/外层 Compose。确认后先停止 TML 并补最后一致性备份，保存旧 DB/sidecar 与匹配镜像，再安装上面已验证的空白候选数据库和候选镜像、设置 UID 权限、启动验证。旧订阅共 8 份及全部旧 URL/Token 将退出，新用户须重新接入和生成。

已核对五个代理/反代容器的 Mounts：没有任何挂载位于 `/data/traffic-manager-lite/traffic-manager-lite` 源码目录中。切换使用干净源码目录并保留旧目录，不能覆盖解压留下旧迁移文件。最终源码归档、镜像标识、候选数据库指纹及“不执行生产 reset”状态已持久保存到备份目录 `ready-to-review.json`。最终候选的 62 张业务表全部为零行；设置和新基线记录单独保留。

以上准备完成后，用户已明确批准切换，实际结果见下一节。可选运行时写入需独立私有密钥及逐次确认；反代、Cloudflare、Docker 公网端口映射和真实客户端凭据仍需管理员正确登记。不同物理核心应创建独立实例，不应用地址改名假装延续同一不可观测计数器。

## 已批准的生产切换结果

- 最终恢复目录：`/data/traffic-manager-lite/backups/v2-cutover-20261015-111644`。停 TML 后保存数据库及存在的 sidecar，完整性检查通过；旧库各业务表合计 234,746 行，逐表计数、SHA256 与旧镜像 ID 在私有 `cutover-manifest.json`。旧源码整体保留于 `source-before`，`.env` 与旧 Compose 独立备份，原 data 目录与其中独立密钥保留。
- 生产镜像固定为 `traffic-manager-lite:v2-candidate-20261015-063939`，使用已验证的空白候选库，干净源码替换，未残留旧可执行迁移。外层 Compose 不再挂载核心配置，保留已有网络、localhost 18080 端口和安全限制。
- 验证：Docker healthy、UID 10001、只读根目录、无 Docker Socket、仅 data/backups bind mount；基线和初始 SQL 指纹匹配；62 张业务/运行表中资产、订阅、流量全部零行。启动归档仅新增 `deleted=0` 的运行记录，不属于旧业务数据。
- 已鉴权检查 servers、instances、users、online、inbounds、nodes、endpoints、groups、subscribers、subscriptions 共 10 个接口为空。公网健康及登录页 HTTP 200。未在生产插入测试资产，旧 Token 未迁入新库。
- xray-multi、hysteria2、sing-box、xray-reality、nginx 的 StartedAt 和 Mounts 与切换前一致；未向代理核心发出清理或账号修改请求。
- 中途三次校验触发自动恢复旧镜像/数据库/源码：tmpfs 不应列入 Docker Mounts、启动归档审计不应算旧业务残留、Python 公网探测收到 403。修正部署校验后最终通过；curl 公网健康及登录均返回 200。前述失败备份保留，不代替本节最终恢复点。
- 本次仅修改部署记录与执行切换；沿用前述已通过的代码/隔离测试。没有重新宣称完整 SDK 构建或 Linux race 通过。


## 全部待清理表的快照数量

以下为 2026-10-15 06:13 UTC 一致性备份计数，不是生产已清理结果；最终停服备份须再统计。旧设置和迁移记录退出，新库只保存新的默认设置与基线记录。

| TML 表 | 旧快照记录数 |
|---|---:|
| archive_daily_ledger | 0 |
| archive_ledger | 0 |
| archive_runs | 17 |
| archived_counter_evidence | 0 |
| catalog_inbounds | 13 |
| catalog_subscription_groups | 2 |
| catalog_subscription_nodes | 17 |
| catalog_subscriptions | 2 |
| catalog_sync_runs | 200 |
| collection_batches | 3 |
| collector_errors | 1107 |
| collector_health | 9 |
| config_operations | 0 |
| core_asset_states | 6 |
| core_clients | 8 |
| core_connection_snapshots | 1 |
| core_diagnostics | 220 |
| core_runtime | 2 |
| core_user_states | 8 |
| credential_material | 14 |
| direction_audits | 0 |
| endpoint_binding_audit | 6 |
| endpoint_connection_requests | 0 |
| identities | 18 |
| inbound_client_keys | 14 |
| inbound_endpoint_bindings | 14 |
| inbound_identity_aliases | 13 |
| inbound_identity_history | 6 |
| inbound_identity_pending | 0 |
| inbound_merge_audit | 0 |
| inbound_retirements | 0 |
| instance_epochs | 1 |
| instances | 3 |
| legacy_node_redirects | 0 |
| managed_user_state | 0 |
| native_connection_cursors | 1812 |
| native_user_totals | 2 |
| node_endpoints | 14 |
| node_group_members | 14 |
| node_groups | 1 |
| node_profiles | 16 |
| nodes | 14 |
| online_snapshots | 3 |
| provider_observations | 4 |
| reconciliation_actions | 0 |
| reconciliation_instance_policy | 0 |
| reconciliation_plans | 3 |
| schema_migrations | 17 |
| servers | 3 |
| settings | 3 |
| subscriber_credentials | 4 |
| subscriber_quota_audit | 0 |
| subscriber_usage_sources | 0 |
| subscribers | 1 |
| subscription_access_logs | 29 |
| subscription_nodes | 36 |
| subscriptions | 6 |
| traffic_counter_observations | 172692 |
| traffic_cursors | 7 |
| traffic_daily | 22 |
| traffic_direction_repairs | 0 |
| traffic_hourly | 190 |
| traffic_provenance | 7 |
| traffic_samples | 47007 |
| users | 17 |
| xray_clients | 12 |
| xray_diagnostics | 200 |
| xray_runtime | 1 |
| xray_user_states | 6 |
