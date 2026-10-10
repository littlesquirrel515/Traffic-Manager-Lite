# 验证记录

日期：2026-10-10（Asia/Shanghai）。本记录区别开发验证与生产验收。

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
