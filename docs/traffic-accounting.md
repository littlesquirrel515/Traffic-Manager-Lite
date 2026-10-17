# TML v2 流量归属与配额

Clients 是资产，Stats 是计数器事实，Online 是实时观测。三者独立保存；统计或在线缺失不会删除 Clients。

流量源身份由核心实际粒度决定：Xray/V2Fly/Hysteria2 为 instance + 用户统计身份；sing-box 原生用户来源额外使用稳定 inbound + name。节点、入口和订阅不产生新的流量源。统计用户 users/identities 不是 Subscriber。

同一个活动计费来源只能属于一个 Subscriber。`subscriber_usage_sources` 的部分唯一索引约束 instance/inbound/client_ref，绑定、基线和归属检查在同一事务；冲突回滚全部绑定变更。多个节点、多个入口、多个订阅不会复制或相加同一来源。代理链由管理员明确选择 billable 入口；非计费的中间跳不加入配额。

每条真实计数观察的 cursor、增量、原计数/来源证据和小时/日汇总在同一事务。首次观察只建基线，不计入核心先前累计值；计数回退或可观测 epoch 改变重新基线。API 停机期间流量和不可观测重启不能凭空恢复。Hysteria2 tx 是客户端上传、rx 是客户端下载。

计费仅查询权威 traffic_daily 用户账本，不叠加 raw、hourly、实例或 inbound 计数。归档保留权威汇总并核对双向账本，超过 30 天明细清理不减少已计流量。

新 Subscriber 默认 10 GiB、激活后 3 个日历月（UTC、月底截断），多个订阅共用额度。到期时刻持久保存，拉取订阅不续期。绑定捕获 TML 账本基线；空白 v2 从未观测且总量为零时基线已知，但来源在首次真实观察前仍为 unavailable。首次核心观察跳过历史累计，之后才计算变化。解绑冻结已计用量，不能通过删节点/解绑清零。周期重置只调整计费基线，不重置远程计数器。

complete、partial、stale、unavailable 独立展示。sing-box 原生用户连接差值是 partial，不宣称完整历史流量。未有真实证据时精确剩余额为 null；已知下界仍可触发耗尽。report_partial 可输出带质量标记订阅，require_complete 在数据不完整时 503；耗尽/过期 403。禁止通过自动踢人、删除客户端或核心 clear 实现配额。
