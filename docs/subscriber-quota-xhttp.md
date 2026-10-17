# Subscriber 配额与 Mihomo XHTTP

## 审计与数据边界

原 Subscriber 与核心 Client 已分离，通过 subscriber_credentials 显式绑定。原 Subscriber 没有流量配额或到期字段；catalog_subscriptions 的 expires_at 是独立交付截止时间。旧 subscriptions 支持多用户节点集合，不可以据此自动构造 Subscriber 身份。

流量事实仍由原采集器提交到 SQLite cursor、原始增量、小时/日汇总，归档账本用于验证而非再次相加。配额只读取用户 scope 的权威 traffic_daily。Xray/V2Fly/Hysteria2 按实例+用户标识去重，不能复制到每个 Node 或 Inbound；sing-box 原生按稳定 Inbound+用户名，通过身份别名查找实际计数。无法归属到此粒度的兼容 API 不自动冒充完整统计。

原 XHTTP 过滤发生在 generator.Compatible：仅允许 URI，Mihomo 一律拒绝；Mihomo 渲染器没有 xhttp-opts。另有 Provider 将所有扩展写入全局 limitations，导致单个格式限制污染节点状态。本轮改为独立 XHTTP 参数及格式判断。

## 配额

新增迁移016，001–015不变。Subscriber 持有统一配额和有效期，同一 Subscriber 的多个 Token 共享使用量；Subscription 继续负责节点授权与交付，其已有独立截止时间仍生效。

新建 Subscriber 从创建激活时间起默认10 GiB（10737418240字节）、三个月。月份按UTC日历运算，目标月份没有同一天时截断至月底，不采用90天。到期值只在创建/明确修改时持久化，拉取不会顺延。UI 使用本机时区；未修改日期时保留原UTC秒/纳秒。

内部全部使用int64字节。后台GiB输入用整数分数转换，拒绝不能对应整数字节的值。管理额度上限1 PiB，避免前端JSON整数精度损失。剩余为max(limit-upload-download,0)，仅完整统计时返回精确remaining；百分比只在完整统计时展示。

subscriber_usage_sources仅保存计费来源、基线和停止计费的截止值，不建立第二套流量采集表。新绑定建立已有用量基线；解绑/取消计费保留已计入消耗并停止增加，重新关联跳过暂停期间流量。重置周期确认后更新基线，历史统计不删除。增加额外额度修改总额度，使用量不变。代理链须由管理员在凭据绑定中明确指定一个计费入口，其他跳转取消billable；不同实例的链路关系不能由系统猜测。

统计状态：complete表示已有可归属、当前有效的累计计数来源和已建立的周期基线；partial表示连接差值、未知初始基线等部分覆盖；unavailable表示无可归属统计；stale表示最近证据超过5分钟。complete仍以TML实际持久化观察为边界，不声称采集前或不可观测核心重启间隔的完整历史。sing-box原生连接差值明确partial。旧用户已知历史消耗不清零，不能推断的完整性不升级为complete。

旧Subscriber迁移默认quota_initialized=false、限制禁用、migration_review。不自动给旧用户套10GiB并拒绝服务，不覆盖原Subscription到期值。管理员在配额页预览当前已知用量、填写额度/日期并确认。若明确重置，审计记录与原流量事实分离。

## HTTP与交付

已知统计且启用额度时，所有格式和所有User-Agent返回数据库驱动的subscription-userinfo：upload、download、total、expire（限制启用时）。附加X-TML-Usage-Status和X-TML-Quota-Status；有效期另有X-TML-Expires-At。

部分统计在report_partial策略下可交付，响应头数值是已知消耗下界，不能将Clash据此计算的剩余视为完整消耗后的精确余额。require_complete策略在不完整时503。完全无统计不发送标准userinfo，避免Clash将缺少字段默认为0而伪造0/0配额；此时只能在TML准确显示未知，Clash标准配额/到期展示需等待可用统计。此策略不因User-Agent改变。

到期、已知用量达到额度、Subscriber禁用返回403及可诊断原因，不输出节点。限制失效不代表核心认证撤销，已缓存的客户端配置和已连接代理不会被自动断开。本轮不调用kick/AddUser/RemoveUser、重置核心计数器或自动启用写入代理。HTTP错误时Clash更新失败，原缓存是否继续使用是客户端行为，不能称为已停用连接。

保留旧/sub/{format}/{token}。旧订阅可在“配额用户”中明确关联Subscriber，但其全部凭据必须已属于该Subscriber；不会自动创建用户或扩张授权。未关联的旧地址标记legacy_unassigned并保留原行为。新/sub/{token}?format=mihomo统一使用Subscriber配额。响应private,no-store，不开放CORS；无需额外跨域白名单。

## 管理API与UI

- GET /api/v1/catalog/subscribers/{id}/quota、usage：配置和已知使用来源/完整性。
- PUT .../quota：quota_revision乐观锁，额度、到期、开关、策略、add_bytes和可选reset_usage。
- POST .../quota/reset：confirmed=true，更新基线。
- GET .../quota/history：最近200条脱敏审计。
- PUT /api/v1/subscriptions/{id}/subscriber：旧地址显式归属确认。
- POST /api/v1/catalog/nodes/{id}/preview：按选定Subscriber凭据生成脱敏Mihomo预览。

沿用管理员鉴权/CSRF。创建、修改、绑定计费、解除绑定、周期重置和旧订阅关联记录审计。订阅用户页显示配置额度、已知使用、可信剩余、到期/天数、状态，详情显示来源、最近订阅更新及历史。所有重要配额更新拒绝过期修订。

## Mihomo XHTTP

目标验证版本Mihomo1.19.32；可在RenderOptions.client_version显式指定，其他尚未验证的目标对XHTTP给出格式限制，不改变全局Node。默认兼容目标为已验证版本。sing-box1.14.3生成器仍不声明XHTTP，复杂分享URI仍按原范围过滤。

VLESS+XHTTP支持TLS及REALITY，四种mode保留原值。xhttp-opts显式转换path/host/mode、headers、noGRPCHeader→no-grpc-header、xPaddingBytes→x-padding-bytes。Xray extra只展开已确认的基础字段，未知reuse/download及其他参数明确拒绝，不能原样序列化。敏感Authorization/Cookie等header拒绝输出，数值范围/CRLF校验。REALITY仅公钥，HTTP/3+REALITY拒绝；不默认skip-cert-verify。连接IP/公网端口/SNI/Host/Path保持独立，遵循Inbound→Endpoint→Node覆盖顺序。

官方依据：[Mihomo传输文档](https://wiki.metacubex.one/en/config/proxies/transport/)、[1.19.32 VLESS参数及XHTTP实现](https://github.com/MetaCubeX/mihomo/blob/v1.19.32/adapter/outbound/vless.go)、[Clash Verge Rev2.5.8响应头读取及HTTP错误处理](https://github.com/clash-verge-rev/clash-verge-rev/blob/v2.5.8/src-tauri/src/config/prfitem.rs)。

## 升级与恢复

备份一致SQLite、旧镜像和部署文件后，仅更新TML并执行迁移016；代理Compose/配置不改，核心不重启。检查旧Token、Node/Subscriber ID和统计事实保留。回退016需恢复匹配迁移015数据库及镜像，不能只删迁移记录。新功能需要管理员确认旧用户配额及代理链计费范围，配置观察或流量样本不是授权依据。

文件：migrations/016_subscriber_quota.sql；internal/subscription/quota.go及测试、catalog_store/catalog_http、generator/renderer、xhttp_validation/mihomo_xhttp_test；core/models、inboundconfig/parser；httpapi/quota及测试/catalog_public/catalog/api；web/quota.js/catalog/app/index；scripts/e2e.cjs。实际验收记录见docs/verification.md。

已参与原生用户计费的稳定入站不允许直接合并去重，避免无证据改写计费身份。旧交付订阅若已有更早的明确到期日，响应使用两者较早者，数据库策略各自保留。未完成迁移确认的旧用户不发送标准配额头。
