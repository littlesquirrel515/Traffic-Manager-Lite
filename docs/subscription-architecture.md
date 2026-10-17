# TML v2 订阅架构

节点定义不包含 UUID/密码。定义由稳定 Inbound + Endpoint Binding + Node 显式覆盖组成，认证由 Subscriber 授权的真实入站客户端提供。

服务器和 API 实例接入后，使用官方完整枚举或管理员 manual_unverified 登记入站/已有客户端。新增连接入口原子创建 Endpoint、唯一入站绑定和默认节点；request_id 可幂等。多入口不复制核心客户端。

Subscriber 默认 10 GiB、3 个 UTC 日历月。绑定真实客户端后选择显式节点或有序分组创建订阅；动态分组的成员变化来自管理员操作，不从未知新资产自动扩权。同一计费来源只能有一个活动 Subscriber 归属，多个订阅共用账户。

授权完成后 Resolver 产生 ResolvedClientNode，再交给纯 Renderer。Renderer 不读数据库、不读核心。预览掩码，完整凭据导出须明确确认，公开拉取只接受哈希 Token 授权并限流。管理员认证与公开订阅分离，日志不保存 Token、UUID、密码或 API Secret。

Raw/Base64/v2rayN、Mihomo、sing-box 格式共享授权与配额校验；完整模板和节点集合模板分开。Mihomo 完整模板含选择组和路由，避免客户端导入后没有可选代理。已验证的 VLESS WS/XHTTP/REALITY、Trojan、HY2、AnyTLS 等参数按各 Renderer 实际能力过滤。sing-box 1.14.3 不输出不支持的 XHTTP，未知 extra 不伪造为兼容。

过期/耗尽/停用返回 403；require_complete 来源不完整返回 503；授权节点全部不可输出返回 422。可用统计写入 subscription-userinfo，缺证据时使用质量头且精确剩余为 null，不返回伪造的 0/0。拉取不会延长有效期或重置流量。

Clients/Stats/Online 缺失互不删除。完整运行资产明确证明用户移除时可过滤该认证；统计空列表或在线缺项不作为删除证据。源入站协议/监听/传输冲突保留稳定资源和已有历史，暂停受影响发布，重新核对映射后恢复。

v2 没有旧 user/bundle 订阅表或旧 URL 兼容；旧 Token 随明确确认后的干净重建退出，不自动迁移授权。
