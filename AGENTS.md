# Traffic Manager Lite

个人代理流量监控系统：Go 单体、net/http、纯 Go SQLite WAL、嵌入中文响应式 Web。

目录：cmd/traffic-manager-lite 入口；internal/config 配置；internal/core 模型；internal/adapters 核心 API；internal/collector 调度；internal/storage 事务与统计；internal/discovery 配置发现；internal/subscription 客户端输出；internal/httpapi 鉴权与 API；web 静态资源；migrations 嵌入式 SQL；deploy 外层部署模板。

构建 `go build ./cmd/traffic-manager-lite`；测试 `go test ./...`；格式化 `gofmt -w cmd internal web migrations`。

部署目录外层 /data/traffic-manager-lite 放 .env、docker-compose.yml、data、backups；内层 traffic-manager-lite 是源码。禁止挂载 Docker Socket。配置默认只读；v1.4 用户管理仅通过显式启用的受控宿主代理，经管理员二次确认才可重启/回滚。TML 仍禁止 Docker Socket 和任意命令。

迁移按编号嵌入、事务执行，不修改已执行迁移。采集 cursor、原始增量、小时/日汇总必须同事务。首次建立基线；回退重新建立基线，不能虚构重启期间流量。用户计数和 inbound 计数分别保存，查询不得相加重复统计。统计时区固定写入数据库，不能重新解释已有每日汇总。

Xray/V2Fly 使用各自官方 StatsService protobuf；sing-box 只使用实际可用的兼容 API，不将独立 Hysteria2 API 用于 sing-box；Hysteria2 v1.4 真实定向流量与官方 TrafficLogger/copyTwoWayEx 已确认 tx 是客户端上传、rx 是客户端下载（指核心向 remote 发送/从 remote 接收；纠正 v1.3 反向映射）。在线指标需区分 device/session/ip/active，不支持与过期必须明确。

配置发现保留人工覆盖和稳定节点 ID；订阅凭据通过统一用户身份映射隔离。白名单客户端参数，不能输出私钥/API Secret。Token 只存 SHA256，返回明文一次。管理认证与订阅分离。所有输入校验、参数化 SQL、限制文件路径和 API 目标地址。

状态（2026-10-09）：Phase 1–5 已实现：基础服务/安全、四核心只读采集、事务增量与持久汇总/归档、配置发现/人工覆盖/订阅、响应式后台。Phase 6：单元/API/集成、官方客户端配置校验、浏览器 21 个页面尺寸、Docker 构建/健康/只读运行/备份/重启持久化、Compose 外层 bind-mount/外部网络、Linux race/vet 检查通过。Go1.27.1、grpc1.84、protobuf1.36.12、SQLite1.60.1、yaml3.0.1。前端原生 HTML/CSS/JS，嵌入，无生产 Node。

重要发现：直接 import Xray/V2Fly 官方 StatsService Go 包会加载两套核心运行时并发生 protobuf 重复文件注册。已从固定官方版本生成 internal/proto 下的隔离文件名 schema；wire 服务/字段不变。sing-box StatsService 实际服务名为 experimental.v2rayapi.StatsService，不能调用 V2Fly 服务名。原生 daemon.StartedService 获取版本、启动时间、实例流量及真实 session 快照，兼容 API 只按实际用户计数声明能力。服务端私钥只用于 X25519 推导公钥，不进入节点 JSON。

归档先验证明细与 hourly/daily ledger/权威汇总双向一致，ledger 更新及明细删除同事务。最新 batch 单独支持当前周期。Xray/V2Fly 可用 uptime 估计核心 epoch 并持久保存；sing-box 使用 GetStartedAt。API 不提供 epoch 的不可观测重启需在文档保持限制。

实例 API Secret 不返回前端，节点客户端认证只有登录管理员能查看。管理用户名/密码由 TML 环境加载，会话密钥每启动随机生成。SQLite runtime 设置优先于环境变量，保存后重启本管理服务生效；聚合时区不可在线变更。实例 core_type 不可就地修改。

验证命令：go test ./...；go vet ./...；TML_TEST_SINGBOX=/path/sing-box TML_TEST_MIHOMO=/path/mihomo go test -v ./internal/subscription -run TestRealClientConfigurationValidation；TML_PLAYWRIGHT_MODULE=/path/playwright node scripts/e2e.cjs。浏览器测试仅使用临时数据库与 fixture API。

剩余验收：真实 VPS API/Secret/配置/Nginx、多协议实际连通、资源长期监测。复杂 XHTTP/AnyTLS分享 URI/obfs/ECH/插件等未验证组合保守过滤，不能输出虚构可用配置。配置中的 HY HTTP/command 认证需管理员实际身份/凭据才能扩展发现。准确状态见 docs/verification.md。禁止把夹具流量宣称为生产真实数据。

版本 v1.2：TML_ALLOWED_TARGETS 空/未设置时，只授权当前已保存实例的 API/原生 API 主机和端口；非空保留严格白名单。禁止退化成任意目标放行。DNS 每次实际拨号验证，禁止 link-local/metadata/unspecified/multicast。服务器 CRUD 删除有实例时返回409；停用暂停新采集，历史保留。实例迁移归属会改变服务器维度历史归属。

版本 v1.3：Xray 通过 internal/xraymonitor 独立采集运行时 Clients/Stats/Online；资产按 inbound+source 保存，流量按 instance+Email，不得复制到每个 inbound。逐用户 online map 缺项为 Unknown，不从 GetAllOnlineUsers 空列表判 Offline。新增迁移005/006，凭据差异以持久随机密钥HMAC比对且指纹不返回API；诊断只读且不创建身份/统计基线，全部reset=false。版本无证据Unknown；宿主固定命令证据绑定endpoint/config hash、24h有效，无Docker Socket。API诊断/历史和健康分开。每实例资产上限10000、inbound256，在线诊断按方法/结果聚合，历史200条。最新正式版已核实v26.3.27，GetUsersStats不存在；v26.2.6->v26.3.27隔离升级实测。见docs/xray-v1.3.md。

版本 v1.3 多核心统一诊断（2026-10-10）：internal/coremonitor 复用 Xray 路径，其余核心独立 Clients/Stats/Online。新迁移007/008与旧迁移并存；统一接口 cores/diagnostics，原 Xray 路由兼容。非 Xray 完整资产只从配置/明确完整的认证后端元数据导出获取；不能从流量或连接列表补造完整 Clients。Hysteria2 userpass 名称大小写按官方归一化，password 单身份 user，HTTP/command 导出24h有效，不自动调用认证端点。sing-box native/V2Ray/Clash 三套服务分开，Clash Secret 独立且不返回API；原生/Clash 实例总量只持久一份，Clash 不能映射用户。V2Fly Handler 无用户枚举，Online Unsupported；可选反射只读确认 Handler/Logger 注册，Observatory 是出口状态。正式版核实 HY2.13.0 / sing-box1.14.3 / V2Fly5.53.0 / Xray26.3.27；固定宿主版本证据无任意命令与Socket。见docs/core-diagnostics-v1.3.md。

版本 v1.4：真实 Hysteria2 4MiB 上传/下载与官方 TrafficLogger/copyTwoWayEx 确认 Upload=tx、Download=rx（核心面向 remote，不是客户端网卡收发）；旧 v1.3 反向映射已修正，固定方向 epoch 避免跨旧 cursor 错算。009 来源/粒度/原计数/Provider cursor、010 显式离线修复审计、011 独立连接快照；001–008 不变。sing-box 用户身份与 Online 按 inbound+name；原生连接差值同统计事务，不声明完整历史用户总量，实例流量与 Clash 不相加。配置用户 CRUD 只经可选 cmd/tml-core-agent，固定 host/container/config 映射、官方1.14.3 check、哈希并发检查、备份原子保存、确认重启/epoch健康、失败恢复；单文件 bind/merged config/其他版本写入保守拒绝。cmd/tml-traffic-audit 默认只读 Dry Run，仅有来源确认的旧 HY 实例可修复，未知/导入混合历史不自动交换。见 docs/v1.4.md。

v1.4 来源证据补充：迁移012保留每次计数器观察（含基线/零增量）的来源、版本、配置hash、原值、方向和增量，30天后按日归档元数据证据；来源证据不能再次加入流量总计。旧provider未知游标升级后重新基线。历史修复拒绝选定范围中已纠正证据，原生用户raw累计是observed_connection_deltas虚拟账本，不冒充核心完整用户计数器。
