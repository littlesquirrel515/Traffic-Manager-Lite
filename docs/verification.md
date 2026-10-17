> 当前 v2.0 验收见 [v2-verification.md](v2-verification.md)。以下为历史 v1 记录，不能将其旧迁移/兼容说明用于 v2；生产尚未执行 v2 清库切换。

# 验证记录

更新日期：2026-10-11（Asia/Shanghai）。各节记录对应版本的验证。本记录区别开发验证与生产验收。

## 订阅管理修正（2026-10-11）

- Windows Go 1.27.1 `go test ./...`、`go vet ./...` 通过；新测试覆盖跨实例/跨用户组合、三格式、顺序、固定节点集合、失败回滚、编辑保留 Token、旧用户隔离、禁用草稿、全部过滤和部分过滤。
- 迁移 012→013 验证旧订阅 ID、Token 哈希、归属、启用状态、节点顺序保留，外键完整性及级联删除通过；原流量、归档及重启持久化测试通过。
- HTTP 验证：管理员预览鉴权、预览不含客户端密码、正确 Token 输出、禁止缓存、空结果 422、失效 Token 404、全不可用的启用订阅保存返回 400。
- 官方 sing-box 1.14.3 `check` 与 Mihomo 1.19.32 `-t` 配置校验通过。这验证配置结构，不代表实际代理节点连通。
- Playwright：临时数据库和本地 HTTP fixture，两个实例、不同用户、两个可用节点及一个缺少连接参数的节点。跨实例多选、批量选择、筛选保留选择、返回顺序、过滤提示、三种公开输出、订阅检查、空保存拦截、Token 轮换通过。360/768/1440px 的 8 页面共 24 个尺寸检查通过，手机表单和深色模式通过，无浏览器错误。
- Windows 管理服务及 Linux amd64 无 CGO 管理服务构建通过。Linux 为 Windows 上交叉构建；本轮未宣称 Linux 运行验证。
- 本轮 Docker Desktop 停留在 `stopping`，引擎 API 返回 HTTP 500（1.55/1.54 均失败）；Linux race/容器构建/Compose 验证未能执行。之前 v1.4 的容器验证不能替代本次修改的容器验证。未强制重启用户的 Docker Desktop。
- 未访问生产 VPS 数据库；真实域名、公网端口、TLS/Reality 参数及实际客户端连接仍需部署环境验证。

设计和配置排查见 [订阅管理说明](subscription-management.md)。

## v1.4 Hysteria2 纠偏、sing-box 原生集成与配置管理

**本节替代旧版本的 Hysteria2 方向结论。旧夹具不足以证明官方方向。** 官方 2.13.0 TrafficLogger 明确 server–remote 语义，真实代理流量确认 Upload=tx、Download=rx。旧实现映射反转已在两条采集路径纠正。

- Windows 最终全量 `go test ./...`、`go vet ./...` 通过；启用官方 Xray/Hysteria2/sing-box/V2Fly 实际二进制集成。Linux 隔离 Alpine 容器四核心 `go test -race ./...`、`go vet ./...` 通过；最后来源/方向证据变化另复测 coremonitor/storage race，通过。Windows/Linux 主程序、宿主代理、审计 CLI 已构建。
- Hysteria2 官方服务端+客户端：独立用户 4 MiB 下载/上传，Go 测试比较原始 tx/rx、标准化及日统计；浏览器再次校验实际官方流量、原始 API、数据库汇总、客户端视角和来源展示，全通过。`dist/ui-verification/hysteria-direction-real.json` 保存本次测量，截图 `hysteria-real-upload-download.png`。HTTP 完成与服务器计数器更新异步，测试等待稳定快照，未降低原始值/持久值一致性断言。
- sing-box 1.14.3 官方版本/API version 4、SubscribeServiceStatus、GetStartedAt、SubscribeStatus 与 reset 连接快照核查及实测通过。原生与 Clash 认证/连接分开；实验 V2Ray Stats 官方发布构建未启用，故障独立，不伪造用户累计。
- 四个实际协议 VLESS/Trojan/Hysteria2/AnyTLS：同名 `client-00` 分属四 inbound，分别通过实际代理执行 1 MiB 下载/上传，正确用户方向与归属。每次下载大约 upload=73、download=1,048,697，每次上传大约 upload=1,048,673、download=118（HTTP 开销）；不是平均分配实例流量。完整配置资产、API 断开/认证失败保留、只读诊断不改基线通过。
- 原生用户计量只累计被观察稳定连接差值；连接第一观察为基线。它有采样覆盖限制，明确未宣称完整历史用户总量、物理设备或完整在线 IP。
- 008 升级009–012不交换旧数据、不丢资产；来源未知旧 cursor/Provider切换/版本改变重新基线；基线及零增量来源记录、30 天来源归档、防重复归档通过。原始增量与小时/日 ledger 的原一致性检查保持通过。
- 历史 HY 修复：只读 Dry Run、来源证据必填、自动备份、限定实例/截止、原始+小时/日/归档同事务纠正、月查询、重复运行不二次交换、保留新方向/current cursor、拒绝过宽截止选中新方向、修复后归档通过。仅修复隔离测试库，未读取/修复生产库。
- **真实 Docker 管理验收通过**：官方校验 SHA256 的 sing-box musl 发布包构建 `tml-singbox-v14-test`；宿主 Docker CLI；VLESS/Hysteria2/AnyTLS 每种真实添加/修改/删除，官方 check、备份原子保存、确认后重启、新 epoch/STARTED/API 健康、监听/其他 inbound 参数不变。真实启动失败注入（测试证书不可用）后恢复原配置并启动成功。单文件 bind、合并配置、其他版本写入保守拒绝。
- 管理测试同时覆盖 hash 冲突、删除/重启确认、认证、幂等 apply/rollback、备份/审计不泄漏凭据。后台测试经真实宿主代理查询、增删改、待重启/已应用、三个 inbound 完整资产与审计通过。
- 最终浏览器 `scripts/core-e2e.cjs`：真实四核心 × 360/768/1440 共12组；统一检测/诊断入口、过滤、历史、真实版本、Secret/UUID 脱敏、无横向溢出、无页面错误通过。修正诊断过滤重复刷新及“完成提示早于详情刷新”造成的历史展开竞态。
- 最终 `scripts/e2e.cjs`：基础八页24组尺寸、服务器 CRUD、实例授权、采集、配置发现、订阅轮换、手机表单、深色主题通过；这是隔离 HTTP fixture 验证，不是生产流量。原 Xray `scripts/xray-e2e.cjs` 三尺寸真实版本/离线 Clients/只读诊断/脱敏通过。
- 生产镜像构建通过；最终外层 Compose、外部专用网络、非 root、只读文件系统、宿主 data/backups bind-mount、健康、备份、重启持久化通过。修正 smoke 工具 HTTP 超时/关闭测试连接，使 Docker 重启前后的请求不会无限等待。测试使用独立临时数据库、容器和网络，清理后保留结果 JSON。
- CI 已加入官方 musl 镜像构建、Docker恢复测试和宿主代理浏览器验收；**远程 CI 尚未运行**。用户 VPS、真实配置路径/API Secret/历史来源、Nginx、长期负载和现场升级均**未验证**，未改动任何生产核心。

详细官方依据、Provider 能力矩阵、文件清单、迁移、宿主代理部署和历史修复命令见 [v1.4交付说明](v1.4.md)。

## v1.3 四核心 API 诊断统一化验证（2026-10-10）

- 已核实并校验官方发布资产 SHA256：Xray 26.3.27、Hysteria2 2.13.0、sing-box 1.14.3、V2Fly 5.53.0。Linux Alpine 实测使用官方 sing-box musl 发布包；普通 glibc 包不适用于该测试镜像。
- 四核心各有独立诊断实现，统一检测/诊断入口、核心筛选、能力状态、实际版本/Unknown、证据、建议、耗时与历史；采集健康和只读诊断分别存储。保留原 Xray API 查询兼容。
- 真实 Hysteria2 三个只读 HTTP 接口空响应仍为 Available；10 个离线配置用户保留。真实认证失败、API 断开、设备在线和代理流查询通过；诊断后原连接仍能传输数据。
- 真实 sing-box 原生 gRPC、Clash 独立认证与连接快照通过；官方发布包未包含兼容统计编译能力时，不影响原生实例统计/配置用户。13 个离线资产涵盖 VLESS、Trojan、Hysteria2、AnyTLS；四协议实际连接的用户映射及诊断后连接存活通过。
- 真实 V2Fly StatsService 与可选 ReflectionService 验证 HandlerService/LoggerService 注册；完整 API 用户枚举及用户 Online 明确 Unsupported。10 个离线配置用户、真实 VMess 流量与诊断后连接存活通过；未调用用户增删或日志重启。
- Clients/Stats/Online 故障隔离、空计数不建假基线、手动诊断不更新业务健康/身份/cursor、Secret 不返回后台、外部认证完整用户导出过期保护均有测试。故障注入 HTTP fixture 与官方核心集成测试分开。
- 旧库 001–006 升级 007/008 保留已有 Xray 资产/历史；新增私有 Clash Secret 与独立端点。原统计同事务、汇总和第 30/31 天归档测试继续通过。
- Windows 官方核心集成与完整 Go 测试、Linux 四核心 `go test -race ./...`、`go vet ./...` 通过。Windows/Linux 二进制已生成；生产镜像已构建。
- 浏览器：真实四核心 12 组核心/尺寸（360/768/1440）、基础八页 24 组夹具布局、原 Xray 三组真实页面检查通过。验证统一按钮、核心筛选、手动诊断、历史、版本、脱敏和移动端无横向溢出。
- 外层 Compose、独立外部网络、非 root、只读运行、健康、备份与重启持久化复测通过。
- Windows 集成测试修正 UDP 临时端口分配和 sing-box 多监听器就绪等待；不会用 TCP 临时端口猜测 UDP 可用性，也不会将 Clash 先就绪误判为全部 API 已就绪。
- 所有进程/容器仅为隔离测试。未部署真实 VPS；宿主固定版本证据脚本只做 shell 语法检查，生产路径/挂载/加载配置仍须管理员实际验收。远端管理 API TLS 与长期生产资源监测未在本次验收。

## v1.3 Xray 架构纠偏验证

- 官方正式版 v26.3.27 Windows/Linux 发布二进制已核对 SHA256；实测运行时 10 个离线用户完整枚举、Stats 空结果不影响 Clients、在线统计项缺失为 Unknown、Stats/Online 服务未启用不阻止 Clients。
- 真实 VLESS TCP 流量验证在线正值、断开后的零值、真实零字节计数与未注册计数区分；同 Email 跨两个 inbound 保留 11 个资产但只建立一份统计身份/cursor，不重复累计。
- 完全断开 API 后保留已知资产；手动诊断不创建用户或流量基线、不重置计数器。配置文件移除用户或同 Email 更换 UUID 时，运行时资产保持不变，并显示来源差异；诊断不输出 UUID、密码或凭据指纹。
- 实际官方 v26.2.6 升级到 v26.3.27，重新记录实际版本证据和能力诊断；真实重启后重新建立基线，不虚构未观测流量。
- 旧 001–004 数据库升级应用 005/006，保留旧服务器数据；新增资产、独立健康、状态、诊断历史及版本证据表和私有 HMAC 指纹字段。
- 完整 Linux `go test -race ./...` 与 `go vet ./...` 通过，包含实际官方 Xray 进程集成测试；Windows 实际最新/前版测试通过。Windows/Linux 发布二进制重新生成。
- 八个页面 × 三种宽度（360/768/1440）共 24 组布局通过；另用真实官方 Xray 检查诊断手动触发/历史/实际版本/凭据脱敏及三种宽度，手机诊断表使用带字段标签的卡片。
- 生产镜像、外层 Compose 部署目录、独立外部网络、非 root bind-mount、健康、备份和重启持久化通过。
- 所有真实核心测试仅运行于临时配置与数据库；通用浏览器测试仍使用明确标注的夹具。未修改真实 VPS、既有容器或代理配置。宿主版本证据脚本仅完成 Linux shell 语法检查，真实 VPS/Docker 执行路径仍待验收；没有可信版本证据时显示 Unknown。

## v1.2 迭代验证

- 默认实例授权、非空环境白名单严格模式及旧配置兼容通过；空策略本身不会放行任意地址，只有当前实例保存的目标被授权。
- 当前实例精确主机/端口隔离、HTTP 默认端口、非法端口、链路本地/元数据/IPv4-mapped IPv6 拒绝、CIDR/IP 限制通过。
- 真实 gRPC 夹具通过 localhost 主机名接入默认和严格模式；显式 passthrough 保留原始主机名，由安全拨号器解析并检查实际 IP。
- 服务器新增/详情/列表/部分编辑/删除、404、关联实例删除拒绝（409）、停用服务器阻止新采集通过。
- “检测 API”实际只读调用统计接口，未创建身份、cursor、原始增量或日汇总；浏览器操作通过。
- 浏览器在空 TML_ALLOWED_TARGETS 下完成服务器新增/编辑/删除、API 检测和实际夹具采集；21 组页面尺寸、手机表单和深色主题检查通过。
- 最终源代码 Linux `go test -race ./...` 和 `go vet ./...` 通过；Windows/Linux 二进制重新生成。
- 生产镜像重新构建，Compose 外层目录、外部网络、非 root bind-mount、健康、备份和重启持久化复测通过。
- 本次没有新增迁移，也没有修改真实 VPS 配置。升级后非空旧白名单继续严格模式；将外层 .env 的 TML_ALLOWED_TARGETS 留空并重新创建 TML 容器，才启用实例授权模式。

## 开发环境

- Windows amd64，项目内 Go 1.27.1；官方下载 SHA256 已核对。
- Docker CLI/Engine 29.7.2，Docker Desktop 4.90.0（Linux 后端）。初始 stopped，经授权检查/启动后可用。
- Node.js 20.15.0，Playwright 1.62.1，Chromium 用于开发验证；不进入生产镜像。
- 官方 sing-box 1.14.0 和 Mihomo 1.19.32 校验程序，均核对官方发布资产 SHA256。

## 已通过

- Windows amd64 编译；Linux amd64 `CGO_ENABLED=0` 交叉编译。
- `go test ./...`、`go vet ./...`、JS 语法检查。
- 首次基线、重复/过时采集、负计数器、缺失方向配对、计数器回退、SQLite 重开恢复 cursor。
- 可获取启动标识时核心重启的 epoch 变更；重启后即使计数更大，也重新建基线。
- 增量写入失败不推进 cursor，重试不丢已观察增量。
- 第 30 天边界保留、第 31 天清理、重复归档、清理事务失败/取消回滚。
- 小时/日汇总损坏及缺失行阻止归档，归档后累计不变。
- 自然月、跨年、UTC/Asia Shanghai 日边界、跨原始与已归档日期范围、固定聚合时区。
- 一致性备份重开，恢复统计正确。
- 用户/inbound 计数分别查询，最新 batch 当前周期查询不重复累计。
- 仅在线 API 出现的用户同步为身份，不产生虚构流量样本或 cursor。
- 官方 protobuf gRPC 夹具：Xray、V2Fly、sing-box 的独立服务名、reset=false、在线“不支持”降级。
- sing-box 原生只读 API 夹具：真实版本字段、GetStartedAt、实例总量、完整初始连接快照、Bearer Secret。
- Hysteria2 HTTP 夹具：Authorization、/traffic 和 /online、上传下载方向、不清零、错误体不写日志。
- 同实例互斥、失败/超时实例不阻塞另一实例，超时被持久记录。
- 配置解析白名单、不输出服务端私钥、稳定节点 ID、保留人工覆盖、配置移除停用。
- obfs/ECH 等尚不支持的客户端参数明确标记限制，避免生成缺少必要参数的订阅。
- 同用户节点选择、跨用户事务拒绝、Token 哈希保存/轮换/禁用、公开订阅格式校验。
- 官方 sing-box `check` 与 Mihomo `-t`：VLESS TCP、WS、Reality、VMess、Trojan/gRPC、Hysteria2、AnyTLS、Shadowsocks。
- 管理鉴权、CSRF、健康接口脱敏、实例 Secret 不返回、在线过期区别离线、SSRF allowlist 和元数据地址拒绝。
- 浏览器集成：登录、服务器/实例表单、HTTP 夹具实际采集、配置扫描、覆盖编辑、创建订阅、下载/轮换 Token。
- 七个页面 × 360/768/1440px 共 21 组布局，无整体横向溢出；手机表单、深色主题，无 JS/控制台错误。
- Docker Compose 模板解析通过（本地模板验证使用 `--no-env-resolution`；生产 .env 在外层部署目录）。
- Docker 生产镜像构建通过；Docker Hub auth 连接超时后通过 mirror.gcr.io 获取相同版本的官方缓存镜像。生产默认仍使用 Docker Hub，可通过 GO_IMAGE/RUNTIME_IMAGE/GOPROXY build args 指定受信任镜像源。
- 隔离 Linux Docker test 构建阶段 `CGO_ENABLED=1 go test -race ./...` 与 `go vet ./...` 通过。
- 生产容器以 UID 10001、根文件系统只读、cap_drop ALL、no-new-privileges、256 MiB/0.5 CPU 上限运行通过；健康检查、API 鉴权、SQLite 一致性备份、卷数据重启持久化通过。
- 单次空闲容器观测：CPU 0.00%，内存 5.336 MiB / 256 MiB；这不是带真实核心的长期负载基准。
- 最终镜像复测：原 Compose 模板外层部署目录、外部网络、非 root 对宿主 data/backups 的 bind-mount、健康及重启数据持久化通过。测试使用随机本机端口，重启后重新读取 Docker 分配端口；测试容器、临时卷和专用网络均已清理。

## 环境受限 / 生产待验证

- Windows 本机缺少 C 编译器，因此 race 改在隔离 Linux 容器执行并通过；远程 CI 尚未运行。
- 未提供真实 VPS、实际核心 API/Secret、服务器配置和 Nginx；没有变更任何生产服务器或代理配置。
- 原生 sing-box API 与兼容统计已按 v1.14.0 官方 schema/实现核对，并做 gRPC 夹具验证；你的实际构建是否启用 API、是否为各用户/inbound 注册计数器需在真实实例探测。
- 客户端 `check/-t` 只证明配置可以解析，不证明网络、证书、认证和路由实际可用；需要真实节点连通测试。
- AnyTLS v2rayN 分享 URI、XHTTP、obfs、端口跳跃、特殊 Shadowsocks 插件/ECH 等复杂组合未完成端到端支持；已保守过滤或标记限制。
- Hysteria2 HTTP/command 动态认证不能从静态文件推导客户端凭据。V1 不调用认证后端猜测用户。
- 核心 API 不报告启动标识且重启后计数仍大于旧值时，无法可靠识别重启；文档明确此可观测性限制。

## 产物

`dist/traffic-manager-lite.exe`（Windows）、`dist/traffic-manager-lite`（Linux amd64）、`dist/ui-verification/`（浏览器截图）。dist 和 .tools 被 Git 忽略。包含夹具数据的截图名称含 `fixture`，不代表生产使用量。

版本依赖由 go.mod/go.sum 固定。所有 generated protobuf 已入源码，生产构建无需 protoc 或完整代理核心 Go 依赖。

## 真实 VPS 订阅修复（2026-10-11）

此前各节的生产未验证说明为阶段记录；本节记录本次现场修复。

- 修复前已有三个订阅，但节点及订阅节点集合均为空。增加 Xray/Hysteria2/sing-box 配置目录只读挂载，保存实例路径并扫描出 11 个节点；未挂载 Docker Socket。
- 已备份数据库、外层 Compose 和环境文件并保留旧镜像。只重建管理服务，未修改或重启生产代理核心，也未修改 Nginx。API Secret 和客户端凭据不写入本报告。
- 纠正新版 Xray method 优先级及传输别名，支持简单 VLESS/TLS/XHTTP 分享链接；扩展参数未验证时明确拒绝。后台增加 XHTTP 模式和 ALPN 字段。无需新 SQL 迁移。
- go test ./...、go vet ./...、Windows 构建、JavaScript 语法检查通过。浏览器夹具验收通过：跨实例多选、顺序、过滤、三种输出、阻止空订阅、Token 旋转及 24 个页面/尺寸检查，无浏览器错误。
- 官方 sing-box/Mihomo 配置验证通过；官方 Xray 四种基础 XHTTP 模式配置验证通过。包含额外 XHTTP 参数的发现配置仍拒绝生成，避免丢弃扩展配置。
- 使用 VPS 实际 Xray v26.9.9、真实客户端参数及隔离临时 SOCKS 客户端，经 Cloudflare 的 443 和路径源站规则，以 packet-up 成功代理请求 HTTPS 健康接口并返回状态 ok；临时容器及配置已清理。其他模式只验证配置可解析，未宣称全协议实际连接已通过。
- 公网订阅响应及原 Token 保留情况以现场复测为准。长期资源监测、所有协议连接、sing-box 的公网防火墙连通性仍需后续验收；夹具数据不代表生产流量。
- 最终现场复测：三个原订阅均有一个可输出节点，原 Token 哈希与升级前备份一致。三个通用订阅选择全部 11 个节点，v2ray 返回 9 个、Mihomo/sing-box 各返回 7 个，公网均 HTTP 200，Cache-Control 为 private, no-store。实际 sing-box 输出通过 v1.14.3 官方 check。差额由格式兼容性过滤，未输出虚构节点。

## Node Catalog & Subscription v1.0（2026-10-12）

- 新增迁移 014；历史库迁移测试确认旧 Token/节点 ID/人工覆盖保留，UUID/密码移至独立认证来源，外键一致。统计迁移 001–013 未修改。
- go test ./...、go vet ./...、Windows 构建及 JavaScript 语法检查通过。新增回归覆盖不同 Subscriber 共用节点而认证隔离、来源删除、完整运行资产移除、Stats/Online 缺失、协议覆盖拒绝、相同入站新增不能冒充改名、三态覆盖、共享入口引用保护、Token 旋转/到期/禁用、动态组去重、新旧公开路由限流及脱敏审计。
- sing-box 1.14.3 和 Mihomo 1.19.32 的官方配置检查通过：集合模板、完整模板分别验证。隔离 Mihomo 核心的 controller 实际返回节点选择组及成员；不修改用户当前 Clash 配置和系统代理。
- 浏览器使用临时数据库和 HTTP 夹具：42 个页面/尺寸检查、新节点复制/共享入口、Subscriber 绑定、多选与过滤、分组排序、脱敏预览、确认完整导出、旧订阅回归、移动端及暗色主题通过，无浏览器错误。
- 本轮未重新执行 Linux race 或全协议生产连通测试；历史阶段的结果不作为本轮的新验证。配置检查不证明公网连通，复杂 XHTTP/AnyTLS 分享 URI/插件等组合继续按兼容规则过滤。
- 真实 VPS 已部署新管理服务并完成迁移：健康检查正常，SQLite integrity_check/foreign_key_check 通过；只读根文件系统保留，未挂载 Docker Socket。Xray/Hysteria2/sing-box、另一个 Reality 实例及 Nginx 的启动时间与备份完全一致。
- 线上发现 10 个 Inbound、11 个全局 Node、11 份独立认证来源。6 个旧订阅的 Token 哈希与升级前一致，旧格式预览仍分别输出 1/1/1/9/7/7 个节点。没有自动创建 Subscriber 或替管理员改变授权。
- 用户电脑已有的 Mihomo 公网订阅地址返回 HTTP 200：7 个 proxies、1 个 proxy-group、6 条 rules，Cache-Control 为 private,no-store；实际响应通过官方 Mihomo 1.19.32 配置检查。这里只验证订阅和客户端配置，未宣称每个代理节点均已实际连通。
- 升级备份位于 VPS `/data/traffic-manager-lite/backups/catalog-deployment-20261011-170159`，数据库备份为 `/data/traffic-manager-lite/backups/traffic-20261011-170159.108606469.db`。路径时间来自服务器 UTC 命名；本次验收日期按 Asia/Shanghai 为 2026-10-12。回滚必须匹配数据库、Compose 和备份镜像。


## 配置调和与稳定身份 v1.1（2026-10-13，本轮）

- 新增迁移015，001–014保持原样；升级测试保留兼容Node/凭据/订阅Token及原人工覆盖。稳定ID支持唯一改名、结构变更review、歧义pending、整组Tag交换人工确认、不同实例隔离、来源消失保留、退役重复身份释放Tag槽位。
- 托管/adopted认证AES-256-GCM加密，显式确认才保存Desired；修订/代次冲突拒绝应用；文件回滚不删除Desired；中断应用可根据精确宿主证据验证或经再次确认恢复待重启步骤。
- Base/Effective分离回归：实例配置发现读取Effective，策略独立读取Base，托管凭据进入配置观察供订阅绑定；应用后不会因Base仍无托管用户而反复重启。完整凭据观察库仍按原私有SQLite机制保存，不宣称所有历史认证已经加密。
- go test ./...、go vet ./...、Windows构建通过。新增验收覆盖事务批量入口、共享影响、不同Endpoint保留、节点去重预览修订、引用迁移、入站合并/复现、默认管理员鉴权与策略保护、加密key/错误key、代次恢复。浏览器54个页面/尺寸及入口复制/批量、托管adopt/只读计划流程通过，无浏览器错误；使用临时DB和HTTP夹具。
- sing-box1.14.3与Mihomo1.19.32官方配置校验（集合及完整模板）和Mihomo真实选择组controller检查通过。
- 在VPS隔离测试容器验证：sing-box1.14.3三种入站用户CRUD、官方check、真实重启epoch、实际启动失败及恢复（37.71s）；Xray26.9.9持久Overlay、官方test、Handler账号核对、加密备份及真实回滚（3.18s）；原生Hysteria2.13.0仅userpass补丁、非认证字段不变、真实重启epoch、只读Traffic API健康及回滚（3.22s）。测试容器自动清理，未用生产代理进行写入测试。
- Docker现场测试纠正旧sing-box二进制路径写死问题；现在通过固定程序名调用，不接受用户命令。首次测试私有fixture文件与容器UID不一致导致只读拒绝，改为匹配文件所有者运行隔离测试后通过，未放宽文件权限。
- 新功能不声称Xray已支持热AddUser；默认文件持久化并确认重启。Hysteria2官方无离线check，非认证参数补丁拒绝；HTTP/command/password模式不做多用户托管。管理健康/配置校验不等于真实客户端代理连通；本轮未重新跑全部协议公网连通。

- 本轮升级前备份：VPS `/data/traffic-manager-lite/backups/reconciliation-deployment-20261012-171604`，数据库 `/data/traffic-manager-lite/backups/traffic-20261012-171604.882521489.db`；保存6个旧订阅Token哈希、全局入站/节点/凭据ID及5个生产服务启动时间。目录名使用服务器UTC，本轮用户日期为2026-10-13。

- 最后补充回归通过：应用后立即更新订阅认证观察，回滚立即过滤已经移除的认证；adopted用户连续两次认证轮换保留原Base所有权证据。单独Base/Effective生成文件不会循环重启。

- 最终源码在VPS Go1.27.1 Alpine测试镜像中执行 `CGO_ENABLED=1 go test -race ./...`、`go vet ./...` 和三个工具的Linux编译通过。首次Docker test阶段也通过；因VPS磁盘较小，最终检查复用已有测试镜像并只打包项目Dockerfile的运行阶段，临时测试镜像/构建缓存在验收后清理，未删除旧部署回滚镜像。
- 已部署生产TML并执行迁移015一次：健康、SQLite integrity_check/foreign_key_check通过，根文件系统只读、无Docker Socket；现有13个Inbound、14个Node、14份配置认证来源及旧Token/ID均保留。代理核心及Nginx五个服务的StartedAt与升级前完全一致，历史用户/身份/小时/日记录没有减少。新Desired数量为0，宿主代理未启用，默认Observe。
- 6个旧订阅预览可用数为1/1/1/7/4/4。在隔离旧镜像及升级前数据库中复测得到相同结果，确认没有因本轮升级减少。用户现有Mihomo公网订阅HTTP200，4个proxies、1个选择组、6条rules，private,no-store；响应通过Mihomo1.19.32官方配置校验。此项不证明所有节点实际代理连通。

## 全局节点编辑修正（2026-10-13）

- 修正继承字段可以输入但提交被忽略的问题：用户修改连接字段自动切换为set，显式inherit/clear保持原语义，不修改共享Endpoint或核心配置。
- `node --check web/catalog.js`、`node --check scripts/e2e.cjs`、Windows/Linux构建通过；浏览器54页面/尺寸检查通过。新增真实表单回归覆盖地址/端口/SNI/Host直接编辑、PATCH后读取、配置同步后保留、重新打开值保留，以及显式改回inherit恢复Endpoint值。
- 已部署TML；健康、DB完整性、原Token/节点ID/统计和代理StartedAt核验通过；公网Mihomo订阅及官方配置校验通过。无需新迁移。旧镜像保留为 `traffic-manager-lite:before-node-editor-20261012-181003`，仅重建管理服务。

## Subscriber 配额与 VLESS XHTTP（2026-10-13）

- 审计发现：Subscriber/核心Client已经分离，但原Subscriber没有配额字段，订阅未返回subscription-userinfo；旧Mihomo兼容检查无条件拒绝XHTTP且Renderer缺少xhttp-opts。迁移016在现有Subscriber扩展策略，001–015不变，不另造流量账本。
- 全量 `go test ./...`、`go vet ./...`、Windows/Linux编译及JS语法检查通过。覆盖10GiB/三日历月（月末/闰年）、1GiB上传+2GiB下载、增加额度/延长时间不改Token或消耗、反复拉取不延长、修订冲突、周期重置不删事实、重复Email跨入站去重、解绑冻结/重绑跳过暂停流量、部分/未知/过期/耗尽、旧订阅凭据归属与撤销、旧用户015→016迁移及重复启动保留旧Token/独立明确到期时间。
- Linux隔离Alpine Go1.27.1容器的 `CGO_ENABLED=1 go test -race ./internal/subscription ./internal/httpapi ./internal/storage` 通过（27.76s/40.01s/76.83s）。最后补充的敏感header清理与旧订阅关联后解绑拒绝通过本地全量测试和vet，未宣称重新执行Linux全仓race。临时容器及SDK镜像标签清理，回滚镜像/数据库保留。
- 浏览器临时DB/HTTP夹具54个页面/尺寸（360/768/1440）检查通过，无浏览器错误；配额默认值、修改、周期确认、审计、节点编辑继承覆盖、原管理流程保持。没有将夹具流量当作生产流量。
- sing-box1.14.3/Mihomo1.19.32集合和完整模板的官方配置检查通过；Mihomo真实controller选择组检查通过。TLS与REALITY分别覆盖auto/stream-one/stream-up/packet-up，共8种XHTTP组合，解析YAML核验结构和具体字段并实际执行官方 `-t`。未知extra/敏感headers只限制对应格式，不伪造可用。
- 已备份并部署TML，迁移016只执行一次；health/integrity_check/foreign_key_check通过。13个Inbound、14个Node、14份凭据、1个旧Subscriber及6个旧Token保留；升级时统计事实行数未减少。Xray、Hysteria2、sing-box、Reality及Nginx五服务StartedAt不变，管理容器根只读、无Docker Socket，默认Observe且宿主代理未启用。
- 备份目录 `/data/traffic-manager-lite/backups/quota-deployment-20261013-084652`；一致数据库 `/data/traffic-manager-lite/backups/traffic-20261013-084652.249493197.db`；回滚镜像 `traffic-manager-lite:before-quota-20261013-084652`。源码、外层.env/Compose和脱敏验证清单保存在该目录，回退需匹配015数据库与旧镜像。
- 用户保存的公网Mihomo订阅HTTP200，7个proxies、1个组、6条rules，通过Mihomo1.19.32官方配置检查；其中3个XHTTP恢复输出。隔离本机Mihomo使用该真实订阅packet-up节点请求 `https://www.gstatic.com/generate_204`，curl退出0、HTTP204，未改用户Clash配置/系统代理或生产核心。
- 公开响应头的上传/下载/total/Unix expire、刷新即时更新与所有UA行为通过API测试；Clash Verge Rev2.5.8官方源码核对其元数据解析及非2xx错误处理。未实际操作Clash GUI导入配额测试订阅，未实测其错误提示/缓存展示；不能将源码和HTTP测试等同于GUI验收。真实连接只验证上述packet-up，不宣称所有协议/模式均公网连通。
- 现有testuser001有4个凭据绑定，为migration_review且统计partial；2个Catalog订阅、6个旧订阅均没有被擅自赋予新限额。旧公网地址仍legacy_unassigned、不虚构标准配额头；管理员需确认旧用户策略并通过“关联统一配额用户”关联已授权凭据，原Token保留。部分统计的已知下界不是精确剩余；需要严格交付可选require_complete。额度限制只拒绝后续订阅更新，不自动断开已有连接。

## 入站中心节点与连接入口 v1.2（2026-10-14）

- 新增迁移017，001–016不改；稳定Binding、Node变体、入口元数据与验证/审计复用原业务实体。普通Node编辑拒绝变更入站，明确迁移保留ID/分组/订阅并暂停发布，不迁移核心凭据或流量。
- 全量 `go test ./...`、`go vet ./...` 和Windows/Linux构建通过；回归覆盖创建原子性、重复请求/模板绑定幂等、IP/域名/端口/覆盖校验、三入口共享一个认证及计费来源、共享模板跨WS/XHTTP、Tag改名、监听变更保留公网端口并暂停、来源缺失/退役、显式节点迁移及授权保护、跨协议确认后旧授权暂停/新认证重新绑定。旧未初始化配额来源在替换/暂停前保存历史基线，冻结消耗，不激活新策略。
- 016→017升级及重复启动测试保留合法多变体、Node ID、节点备注/人工覆盖/排序、分组顺序、Subscription Token、Subscriber配额、凭据及历史流量。SQL外键一致。现有源码包的采集、归档、用户CRUD及配置调和单元/API测试持续通过。
- 浏览器使用临时数据库和HTTP夹具：60个页面/尺寸（360/768/1440）及原全流程通过。新流程实际点击入站详情、预选来源、动态参数/实时预览、入口页搜索/服务器/协议筛选、手机保存、编辑共享定义、独立复制、跨入站复用、三个入口一份凭据授权，无浏览器错误。不是生产真实流量。
- sing-box1.14.3/Mihomo1.19.32集合及完整模板官方配置检查通过；Mihomo真实controller选择组检查和TLS/REALITY四种XHTTP模式共8组合官方-t通过。此项验证配置，不宣称全模式公网连通。
- VPS隔离Alpine Go1.27.1运行 `CGO_ENABLED=1 go test -race ./internal/catalog ./internal/discovery ./internal/inboundconfig ./internal/subscription ./internal/httpapi ./internal/storage` 通过，分别41.27s/12.81s/1.02s/38.80s/67.49s/99.48s；最后增加旧订阅引用删除保护和跨协议暂停旧交付的SQL保护后，本地全量测试/vet继续通过，未重复声明最终代码完整Linux全仓race。
- 部署前一致备份 `/data/traffic-manager-lite/backups/inbound-deployment-20261014-151701`；SQLite `/data/traffic-manager-lite/backups/traffic-20261014-151701.265540208.db`；旧镜像 `traffic-manager-lite:before-inbound-20261014-151701`。保存源码、外层.env/Compose、原Token/节点/分组/配额/凭据及5个代理/Nginx服务StartedAt。
- 在备份的独立临时副本执行离线017迁移：16个原Node保留，生成14个Binding，unresolved=0；17张受保护的订阅/用户/凭据/分组/游标/小时/日及归档表的逐行摘要前后完全相同。无生产DB写入，无代理API调用。后续现场部署核验另行记录。

- 已部署最终TML运行镜像并应用017一次：健康与SQLite完整性/外键通过；13个Inbound、16个原Node、14份真实凭据、1个旧Subscriber及14个Binding保留。旧Token、Node连接字段、分组/分配、凭据授权及配额修订逐项与备份一致，统计明细/汇总没有减少。根文件系统只读、无Docker Socket；默认Observe/宿主代理未启用；5个生产代理与Nginx StartedAt完全不变。
- 现场使用本地交叉编译的三个Linux工具，按项目Dockerfile相同的Alpine运行阶段构建；未冒充重新执行完整Docker builder阶段。隔离race容器已自动清理，SDK镜像标签清理，升级前数据库/源码和旧镜像保留。
- 原6个订阅预览可用数1/1/1/7/7/4；用户电脑已有公网Mihomo地址HTTP200、7个proxies、1个选择组、6条rules，含3个XHTTP，通过Mihomo1.19.32官方配置检查。隔离本机Mihomo再次使用该真实packet-up节点访问gstatic/generate_204：curl退出0/HTTP204；未改用户Clash配置或系统代理，未声称全模式全协议连通。
- 管理员需处理Source Missing/Unresolved、实际Docker/Nginx转发确认及有托管Desired的跨协议变更。本次现场离线报告unresolved=0，没有自动合并变体、接管用户、初始化旧配额或改变旧订阅授权。旧订阅仍使用兼容显式分配；新入口可在新的Subscriber订阅中选择，不自动加入任何原Token。
