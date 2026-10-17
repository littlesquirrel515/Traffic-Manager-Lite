# 订阅用户统一管理核心客户端

订阅用户是运行时 Client 创建和纳管的管理入口。入站保存稳定身份和协议配置，入站页只读展示 Client；普通连接入口只编辑 Address、外部 Port、Host、SNI，协议/Path/XHTTP Mode/Flow 等继承入站。填写 SNI 表示客户端入口使用 TLS，服务端内部监听与客户端入口安全分别处理。旧高级覆盖不批量清空，未知 XHTTP 参数仍保守标记并展示具体原因。

## TML 动态用户

管理员在订阅用户选择目标入站并确认创建。TML 生成认证信息，新增迁移 `002_subscriber_provisioning.sql` 持久化创建任务和目标 endpoint/tag/protocol；独立密钥 AES-GCM 保存 Desired。用户与任务同事务登记，远程调用不放在 SQLite 事务里。创建中的新用户禁用，只有核心回读确认并绑定凭据后启用；网络错误或未知结果保持 pending，可重试同一请求，不显示成已生效。相同请求不得换参数，目标变更时拒绝向新目标重试。

Xray VLESS/VMess/Trojan 复用官方 HandlerService AddUser 和回读；按已授权 Desired 验证缺失后补回，不删除/替换冲突用户。TML 和核心都重启后仍读取数据库与独立密钥恢复。订阅用户禁用或创建未完成时不自动恢复；原生客户端不会因为出现在列表里而被接管为 Desired。一个订阅用户可以通过“添加核心客户端”继续绑定其他入站。

`POST catalog/subscribers` 原独立建档入口关闭；新建统一使用 `POST catalog/subscriber-provisioning`。旧入站 runtime-users 写接口返回 409，引导统一流程。已有订阅用户资料仍可 PATCH。关联创建任务的用户不能直接删除而丢失核心客户端归属，可停用并保留审计；删除/撤销客户端的完整业务尚未作为本次功能开放。

## 原生客户端

订阅用户页面读取已持久化的真实客户端资产，展示身份、来源、所属入站、已归属订阅用户，不返回 UUID/密码。纳管已有客户端只绑定既有凭据及计量来源，不向核心添加重复 Client。已有活跃归属不能重复纳管；绑定计量保持已有唯一 meter 所有权规则及首次基线，不追收历史。

此列表的完整性受资产来源能力限制。Xray 支持的运行时枚举、明确的既有配置登记可以产生资产；sing-box/Hysteria2 的连接/Stats 不能补造完整原生用户清单。当前后台展示的是已保存的资产，不等于所有核心都具备完整自动枚举。

## 必须如实显示的核心限制

当前动态创建/回读/恢复流程仅开放给 Xray 支持的用户协议。sing-box 1.14.3 已接原生 StartedService/V2Ray/Clash API 没有经过验证的服务端用户增删入口；Hysteria2 的 Traffic Stats API 也没有 AddUser。两者新建动态用户在创建 Subscriber 前拒绝，不伪造成功；原生用户展示、纳管、现有统计不受影响。

Hysteria2 可使用官方 HTTP 认证后端实现数据库驱动身份，但这需要先配置核心的认证后端，不能将现有静态 userpass 伪装成已支持动态添加。本次没有替换生产认证方式、改写核心配置或重启代理。sing-box 需要协议/版本实际支持的受控用户管理方案，不能套用 Xray Protobuf 或独立 Hysteria API。

官方依据：[Hysteria2 HTTP 认证](https://v2.hysteria.network/docs/advanced/Full-Server-Config/#http-authentication)、[Traffic Stats API](https://v2.hysteria.network/docs/advanced/Traffic-Stats-API/)、[sing-box API](https://sing-box.sagernet.org/configuration/service/api/)。

## 验证范围

新增 RPC 夹具验证创建/回读/凭据绑定、请求幂等、失败后重试同一用户、目标变化拒绝、禁用用户不恢复、SQLite 关闭重开及模拟核心丢失后的恢复；夹具不是生产真实用户。迁移测试由已存在的 v2 单基线数据库升级，验证资源与初始 schema 指纹保留。原有四核心采集、汇总/配额、订阅输出测试保留。

中断后的显式重试仅在租约过期且目标证据一致时回收本地 applying 状态，随后仍须先回读再决定是否添加；Mutate 内部再次核对目标，防止校验与实际写入之间被切换到其他核心。测试还覆盖同一 Subscriber 追加第二个入站而不重建用户、不覆盖配额。

## 线上记录（2026-10-16，Asia/Shanghai）

已部署 `traffic-manager-lite:v2-subscribers-20261015-173617`，最终更新备份 `/data/traffic-manager-lite/backups/v2-subscribers-20261015-173617`，含旧部署、SQLite 一致性快照、旧源码和独立 Desired 密钥的私有备份。未清库，原 3 台服务器、3 个实例、10 个入站及 11 份客户端凭据保持。后台原生客户端接口读回 11 条资产记录，创建任务为空；未在生产新增测试 Client，也未自动把原生客户端激活成配额用户。

v2 生产数据库副本先在禁用实例采集、network=none 的隔离容器中验证迁移，再切换生产。TML healthy，公网 health 200，代理核心及 Nginx StartedAt 不变。全量 Go 测试/vet、新增 HTTP 鉴权与旧写入口关闭测试、54 个页面/尺寸及新原生用户纳管/四字段表单流程通过，隔离官方 Xray 运行时操作测试通过；最后中断恢复补充通过 provisioning/remote/httpapi 测试和 vet。没有把夹具或隔离测试当作生产真实用户流量。


2026-10-16 增量：独立 Hysteria2 已增加官方 HTTP 认证后端（迁移 003），不伪造 AddUser。须先准备后端、迁移核对已有凭据、经授权配置核心并在 TML 明确确认，之后订阅用户由数据库认证。sing-box 1.14.3 的 SSM 仅管理 managed Shadowsocks，原生 gRPC 不提供当前 VLESS/HY2/AnyTLS 的通用 AddUser。实现及上线边界见 [hysteria-http-auth.md](hysteria-http-auth.md)。
