# 订阅管理修正

日期：2026-10-11。增量修改，保留原有节点发现、三种生成器和流量存储。

## 为什么会出现空白

原界面按单一 `user_id` 筛选，后端也拒绝其他用户的节点。同一管理员的多个实例不一定映射到同一个统计用户，因此无法通过订阅组合所有实例的节点。

原保存逻辑允许启用没有节点的订阅。生成器会过滤地址、端口、认证或安全参数不完整以及当前格式不兼容的节点；全部过滤后仍返回 HTTP 200。Base64 结果为空字符串，其他格式只有空列表，客户端可能显示为空白。初次修正已确认的代码路径后，本次进一步检查并修复真实 VPS 部署，详见下文。

只接入核心统计 API 不等于获得可连接的客户端节点。API 通常不提供客户端域名、公网端口和完整认证配置。系统仍需扫描实例的只读配置，并在代理节点页面补充客户端连接参数，不能用 API 端口或容器内部监听地址替代。

## 使用流程

1. 在核心实例设置可读取的配置路径；配置必须在管理服务挂载的 `TML_CONFIG_ROOT` 内。
2. 在代理节点页面扫描配置，填写实际客户端连接的域名/IP、端口、TLS SNI 等缺失字段，检查目标客户端兼容性。
3. 创建订阅，默认“通用订阅 · 跨实例多选”。按实例或关键词筛选，逐个选择或全选当前可用节点；过滤条件改变不会清空已选节点。
4. 查看已选、可输出、过滤数量。切换格式保留选择并重新判断兼容性；返回顺序可以调整，选中但失效的节点也可移除。上限 500 个节点。
5. 保存订阅地址到客户端。地址仅在创建或重新生成时显示，重新生成会撤销旧地址。列表“检查订阅”可查看当前可用数量和各节点过滤原因。

通用订阅的访问者可以使用所有选中节点的客户端凭据；这由登录管理员明确授权。新实例、新节点不会自动加入，避免扩展现有地址权限。统计用户和订阅范围相互独立，不要求合并流量身份才能创建跨实例订阅。

单用户模式继续提供原有凭据归属限制。迁移 013 将全部旧订阅保留为单用户模式，保留 ID、Token 哈希、启用状态、节点集合及顺序；不会自动扩大旧地址权限。已有订阅可编辑为通用订阅，再明确增加所需节点。编辑不旋转 Token；需要撤销已分享的旧地址时使用“重新生成地址”。

## 返回内容和错误

| 格式 | 返回内容 | 客户端用法 |
|---|---|---|
| v2ray | Base64 编码的逐行分享链接 | 支持相应协议的订阅客户端导入 |
| Mihomo | YAML `proxies` 节点集合 | 使用 HTTP proxy provider 或合并至客户端配置 |
| sing-box | JSON `outbounds` 出站集合 | 支持此集合格式的客户端导入，或合并至客户端配置 |

Mihomo 和 sing-box 输出节点集合，不包含自动接管流量的本地监听、DNS、路由规则。sing-box 核心本身不是订阅导入管理器，需要客户端导入或自行合并。参见官方 [Mihomo proxy-providers](https://wiki.metacubex.one/config/proxy-providers/) 和 [sing-box outbounds](https://sing-box.sagernet.org/configuration/outbound/)。未验证的协议组合继续保守过滤，不输出虚构可用配置。

启用订阅保存前由后端再次检查，至少一个节点可输出才保存。禁用草稿可以暂时没有节点。已保存的订阅因配置删除、禁用或修改而全部不可用时，公开接口返回 HTTP 422 与明确的无可用节点提示，后台显示各节点具体原因。部分节点不可用时正常返回其他节点，响应头 `X-TML-Filtered-Nodes` 给出过滤数量。错误 Token、错误格式或禁用订阅仍返回 404。配置响应禁止缓存。

后台 `POST /api/v1/subscriptions/preview` 需要管理员鉴权及原有会话 CSRF 保护，只返回选中数、可用数和节点过滤原因，不返回密码、UUID、私钥或 API Secret。公开地址只能输出保存的节点 ID，不能通过请求参数自行扩大范围。

## 修改文件

- `migrations/013_subscription_bundles.sql`：增量迁移，增加选择范围及可为空的通用订阅所属用户。
- `internal/subscription/store.go`：显式节点集合解析、后端预览、保存校验、空结果错误、旧用户隔离及顺序保持。
- `internal/httpapi/api.go`：管理员预览接口、列表范围和顺序、公开错误及缓存控制。
- `web/app.js`、`web/style.css`：跨实例选择、搜索筛选、批量选择、顺序、移除、兼容性和订阅检查。
- `internal/subscription/bundle_test.go`、`internal/httpapi/subscriptions_test.go`、`internal/storage/subscription_migration_test.go`、`scripts/e2e.cjs`：功能与回归验证。
- `internal/storage/traffic_test.go`：重启测试只读取 SQLite main 数据库路径，避免迁移创建的 temp 数据库空路径误报。

升级只需要构建并重启管理服务，启动自动执行迁移 013。使用现有部署的数据库备份流程保留升级前备份，无需重启代理核心。测试记录见 [verification.md](verification.md)。

## 真实部署修正（2026-10-11）

部署初始有三个订阅，但没有配置路径、发现节点或节点选择。管理容器增加三种核心配置目录的只读挂载，再保存实例配置路径并扫描，发现 11 个节点。配置中的监听端口只作参考；订阅填写客户端实际连接端口。通过 Cloudflare 源站规则按路径转发时，客户端端口可统一为 443，不能直接填写各个源站端口。

新版 Xray 的 `streamSettings.method` 优先于旧 `network`；解析器现已识别 `raw`、`websocket`、`splithttp` 别名并保留传输路径。依据 [Xray v26.9.9 官方源码](https://github.com/XTLS/Xray-core/blob/v26.9.9/infra/conf/transport_internet.go)，不将这些字段套用至 V2Fly。

简单 VLESS/TLS/XHTTP 可生成 v2ray 分享链接，保留连接地址、443 端口、SNI、HTTP Host、path、mode、fingerprint 和 ALPN。四种基础模式通过官方 Xray 配置检查；packet-up 使用实际域名及 Cloudflare 路径转发完成代理请求。链接字段参照 [v2rayN 官方解析器](https://github.com/2dust/v2rayN/blob/master/v2rayN/ServiceLib/Handler/Fmt/BaseFmt.cs)。Mihomo/sing-box 输出仍过滤 XHTTP；包含 `extra` 等扩展参数的配置也继续过滤，避免生成缺失关键参数的节点。

本次修改 `internal/core/models.go`、`internal/discovery/parser.go`、`reconcile.go`、`internal/subscription/generator.go` 和 `web/app.js`，增加 XHTTP 模式及 ALPN 编辑、验证与生成。参数仍保存在节点 JSON 中，无需新 SQL 迁移。新增及更新解析、生成和官方客户端检查测试。原有流量采集及归档逻辑未修改。
