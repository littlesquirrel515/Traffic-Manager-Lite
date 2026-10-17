> 历史 v1 文档；不适用于 v2 安装或升级。

# TML 领域术语

本规范适用于管理后台、Go API、迁移及后续开发任务。中文名称与实际存储表的对应如下。

| 中文 / 英文 | 职责 | 现有实体 |
|---|---|---|
| 服务器 / Server | VPS 或实际宿主机；展示归属，不是统计 API 地址 | servers |
| 核心实例 / Core Instance | 一个核心运行实例，拥有独立 API、配置来源与采集健康 | instances |
| 入站 / Inbound | 服务端监听和协议配置，具有独立稳定 ID | catalog_inbounds |
| 入站客户端 / Inbound Client | 核心配置或可信运行资产中的真实认证身份 | credential_material、inbound_client_keys；运行资产独立存储 |
| 连接入口 / Endpoint、Host | 客户端连接的公网地址、端口、SNI、Host、ALPN 等；可保存为共享定义 | node_endpoints |
| 入站连接绑定 / Inbound Endpoint Binding | 一个明确入站与一个入口定义的稳定关联；含局部覆盖、启用、排序、验证状态 | inbound_endpoint_bindings |
| 订阅节点 / Proxy Node | 稳定的订阅业务资源，持有名称、备注、启用、排序、标签和客户端专属覆盖 | node_profiles |
| 节点分组 / Node Group | Proxy Node 的有序多对多集合；不对入口定义直接分组 | node_groups、node_group_members |
| 订阅用户 / Subscriber | TML 的业务用户，拥有共享配额与有效期 | subscribers |
| 用户凭据绑定 / Credential Binding | Subscriber 对真实入站客户端或显式手动认证来源的授权 | subscriber_credentials |
| 用户订阅 / Subscription | 为 Subscriber 保存的节点/分组分配、格式和 Token | catalog_subscriptions；subscriptions 保留旧地址兼容 |

Inbound 是服务端资源，Endpoint 是外部连接方式，Proxy Node 是订阅资源。三者均不等于 Docker 容器，也不代表实际在线。Inbound Listen Port 与 Endpoint Connection Port 独立；443 公网入口可以转发到 10443/11443 等监听，也可以转发到 Unix socket。

Inbound Client 与 Subscriber 必须分开。多个入口引用同一入站时，一个 Subscriber 只需一份入站凭据绑定；不得新增多份 UUID、密码，不能按节点数量重复计费。Clients、Stats、Online 继续独立采集和保存。

旧“全局节点库”保留为“订阅节点（原节点库）”。旧 `catalog/nodes`、旧订阅及配置生命周期入口继续可用；用户凭据绑定 API `catalog/bindings` 与新入口绑定 API `catalog/endpoint-bindings` 不得混淆。
