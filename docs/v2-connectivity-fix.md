# v2.0 接入与手动客户端采集修正

Hysteria2 的 Traffic Stats API 使用 HTTP。实例保存现在允许 `hysteria2:9999` 并持久化为 `http://hysteria2:9999`；显式 HTTPS 保留，用户信息、查询串、片段、非法端口和元数据地址仍拒绝。表单提示各核心的地址格式。

sing-box 1.14.3 原生 `daemon.StartedService` 提供版本、实例状态和连接快照，不提供完整服务端入站/用户枚举。只读 API 发现入口只列出支持完整枚举的 Xray；其他核心使用明确登记的真实资产。实例总流量独立于资产清单，用户连接差值必须匹配真实 inbound+name，不能从活跃连接猜测完整 Clients。

修复手动登记只写凭据图、未写采集资产/身份的问题：非 Xray 登记事务同时保存 `core_clients(source=manual_unverified)` 和用户身份；重复登记不创建额外身份。无配置文件时可关联这些明确资产，匹配按入站去重，退役/失效的登记不用于新归属；同名用户跨入站时不归属无 Tag 的兼容计数器。API 不可用或全部离线不会删除资产，不声称手动登记证明完整运行时配置。

sing-box 原生地址和可选 V2Ray 兼容地址分开。未配置兼容地址显示 Disabled，不向原生端口尝试调用不相容方法。原生流量与用户连接差值仍独立采集，后者只覆盖实际观察到的连接差值，不追收接入前或漏采期间流量。

验证包括 HTTP 地址正反例、无配置/无 Stats 时客户端资产保留、重复登记、同名跨入站歧义和退役过滤，以及 `go test ./...`、`go vet ./...` 和前端脚本语法检查。上线不清库、不修改 schema，只备份并更新 TML 镜像/源码，远程核心配置与 StartedAt 另行比对。

官方依据：[Hysteria2 Traffic Stats API](https://v2.hysteria.network/docs/advanced/Traffic-Stats-API/)、[sing-box 原生 API](https://sing-box.sagernet.org/configuration/service/api/)。

## 线上验证（2026-10-15）

已部署 `traffic-manager-lite:v2-connectivity-20261015-114722`，恢复备份位于 `/data/traffic-manager-lite/backups/v2-connectivity-20261015-114722`。没有清库，更新前已有资源计数通过保持校验。

Hysteria2 地址已保存为 `http://hysteria2:9999`，密钥仅从现有宿主配置在内存读取并通过管理员 API 保存，不写诊断日志；Stats/Online 均 Healthy。sing-box 原生地址登记为 `sing-box:9091`，未启用的 V2Ray 兼容地址留空；从已有只读配置一次性登记 VLESS/WS、Hysteria2、AnyTLS 三个入站及三个客户端，来源保持 manual_unverified，不宣称由官方 API 完整枚举。

sing-box Clients 检查为 Unknown、已登记数量 3（完整性无法证明），Stats/Online 为 Healthy；验证时两类核心均没有活动在线记录，未据此删除资产或声称真实用户流量已压测。用户连接计量仍遵循首次观察基线和 partial 覆盖，不补造历史。

两份代理配置文件 SHA256、五个核心/反代容器 StartedAt 前后相同。TML healthy、RestartCount=0，公网 health HTTP 200。完整 Go 测试与 vet 先通过，最后的 Clients 展示补充又通过 coremonitor/remote/httpapi 测试与 vet，前端脚本语法通过；Linux 运行包隔离健康检查通过。

后续补齐独立 Hysteria2 入站（12:01 UTC）：前次只完成其 API 接入，未登记 catalog 入站。根据现有只读配置一次性登记独立实例 3 的 Hysteria2 监听 443 及两个 userpass 客户端；客户端认证使用官方 username:password，API Secret 不用作代理认证。来源仍为 manual_unverified，不能称为统计 API 自动发现。后台 inbound-management 已读回确认客户端数 2；Clients 完整性 Unknown、已登记数 2，Stats/Online Healthy。配置 SHA256 与核心 StartedAt 不变，无核心或 TML 重启；操作前在线一致性备份为 `/data/traffic-manager-lite/backups/traffic-20261015-120157.865962881.db`。
