# TML v2 领域术语

Server → Core Instance → Inbound → Endpoint Binding → Proxy Node → Subscription。

Subscriber → Credential Binding → 真实 Inbound Client → Inbound。

Traffic Source / Meter Identity → Subscriber Traffic Account → Quota。

| 实体 | 职责 | 存储 |
|---|---|---|
| Server | 宿主/归组，地址仅展示 | servers |
| Core Instance | 独立 API 地址、核心类型、版本与健康 | instances |
| Inbound | 稳定服务端资源；Tag/监听/显示名是可变属性 | catalog_inbounds、remote_inbound_registry |
| Inbound Client | 核心真实用户或管理员明确登记；不从 Stats 推断完整清单 | credential_material、inbound_client_keys；运行资产独立 |
| Endpoint | 外部 Address/Port/SNI/Host/Path 等，可共享 | node_endpoints |
| Endpoint Binding | 明确入站与入口的关联、覆盖、排序及验证 | inbound_endpoint_bindings |
| Proxy Node | 订阅资源，绑定创建时原子生成；不包含认证 | node_profiles |
| Node Group | 有序节点集合 | node_groups、node_group_members |
| Subscriber | 订阅用户、统一流量账户与配额 | subscribers |
| Credential Binding | Subscriber 到真实入站客户端的明确授权 | subscriber_credentials |
| Subscription | Subscriber 的节点/分组、格式、哈希 Token | catalog_subscriptions 及关联表 |
| Meter Identity | 核心实际可观测的计费身份，粒度依协议/API | identities、traffic_provenance |
| Subscriber Traffic Account | 去重来源、绑定基线、解绑冻结 | subscriber_usage_sources |

Inbound 监听端口与 Endpoint 公网端口独立。新建入口不创建 UUID/密码，不修改核心/反代/端口映射。多个入口共享同一真实客户端和流量源，不复制计费。

manual_unverified 是登记尚未验证，不是核心已存在的证明。Ready 仅表示客户端配置字段完整，网络状态仍 Unknown。API 能力、采集健康、版本证据分别保存。

旧 nodes/subscriptions/subscription_nodes 及旧兼容路由已经退出 v2。catalog 是现用 API 命名空间，不代表旧全局节点库页面；用户凭据 bindings 与入口 endpoint-bindings 仍须区分。
