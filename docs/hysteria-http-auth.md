# Hysteria2 数据库认证与 sing-box 用户管理边界

核实日期：2026-10-16（Asia/Shanghai）。这是 v2 的增量能力，新增迁移 003，001/002 保持原样。默认没有认证后端，不自动切换任何核心配置。

## Hysteria2 的业务流程

在“订阅用户”页的“HTTP 认证后端”准备独立 Hysteria2 入站的后端。每个独立实例只允许一个后端，保存稳定 Inbound ID、实例 ID、统计端点及 Tag；目标变化时拒绝认证/新建账户，需明确重新准备。sing-box 内置 HY2 不适用。

准备时可以明确导入此入站已登记原生用户的认证载荷，保持已有客户端与流量 ID。原生类型为 userpass 时须明确勾选用户名忽略大小写，密码仍区分大小写；单一 password 则保持整个认证载荷精确匹配。不从 Traffic/Online 猜测完整用户，不修改原始凭据。切换前必须核对 TML 中的已登记用户覆盖实际静态 userpass 用户；用户列表不全时不能声称无损切换。

认证 URL 内含独立 256 位随机密钥，只在准备/明确轮换时返回一次，数据库只保存 SHA256。UI 提供核心 auth 配置片段，默认 `http://traffic-manager-lite:8080/core-auth/hysteria2/<独立密钥>`。核心通过 Docker 私网直接访问 TML。来源使用实际 TCP peer 与管理员选定私网 CIDR，不信任 X-Forwarded-For 或请求的 addr。Nginx 部署示例拒绝公网 /core-auth/ 路由；不要求暴露管理 API。

管理员完成核心一次性配置后，在后台明确确认修订和原生用户迁移。状态标记“管理员确认”，不冒充核心回读成功。最近真实认证成功时间独立记录；此前运行证据 Unknown。轮换会立即使旧 URL 失效，并撤销配置确认；停用后端会拒绝新认证，不踢出已有连接。

之后从“新增订阅用户”选择此入站，TML 生成随机认证载荷、保存加密 Desired、登记独立 Clients 资产与统一身份、绑定 Subscriber/计费来源和认证 hash。此流程无 AddUser RPC，不修改核心用户配置；持久化来自数据库，TML/核心重启后继续回调校验，无需循环补回用户。失败保留 pending 幂等任务，认证只接受已完成绑定的 managed 任务。

官方回调协议：POST JSON 含 addr/auth/tx，返回 HTTP 200 和 `{"ok":true,"id":"稳定用户 ID"}`。tx 是客户端宣告的带宽值，绝不写入流量计数器。认证资产、Traffic Stats 和 Online 保持独立；认证成功不证明在线快照，不创建统计基线。用户停用、到期、已观察到的配额耗尽、凭据/授权停用会拒绝后续认证；require_complete 配额策略在统计不完整时 fail closed，默认 report_partial 不因暂缺 Stats 删除/拒绝所有新用户。

认证接口限制 8 KiB、单载荷 4096 字节、2 秒处理超时、每直接 peer 每分钟最多 600 请求，限流表有上限。无明文认证 URL/密码、请求 addr、tx 日志。数据库备份仍包含系统原有 credential_material 客户端认证，必须私密保存，不能宣称整个数据库已加密。

## 生产切换与回退

仅部署 TML 不会修改 Hysteria2。切换生产核心 auth 配置及重启需要遵守当前 AGENTS.md 的核心配置禁写约束，取得用户对该具体变更的明确授权。修改时只替换 auth 段，保留 TLS、监听、obfs、Stats Secret 等；先一致性备份并核对完整静态用户/认证 ID。不把准备后端当作已经启用动态用户。

正式切换后，TML 是新连接认证依赖：TML 不可用时新认证失败，不能降级成全部放行。已建立 QUIC 连接及其复用流不会因为停用/到期立刻断开；本功能不自动调用 kick。回退到不支持 HTTP 后端的旧 TML 镜像会中断新认证，需要保持可用后端或经授权恢复原认证配置。新增迁移不修改历史流量、cursor、Token 或配额。

## sing-box 1.14+ 的真实边界

当前官方 latest 正式版核实为 1.14.3。StartedService 提供观测、连接关闭、出站选择等；ManagedService 有 StopService/ReloadService，但没有面向 VLESS/Trojan/Hysteria2/AnyTLS 的通用 AddUser。V2Ray 兼容 API 是统计服务，不能套用 Xray HandlerService。Clash 连接列表也不是完整用户管理接口。

确有例外：SSM REST API 自 1.12.0 提供 managed Shadowsocks 入站的用户增删改，必须显式配置 SSM service、inbound managed 与对应映射。官方 UserManager 调用 ManagedSSMServer.UpdateUsers；这是 Shadowsocks 专用能力，不适用于当前已登记的 VLESS/WS、HY2、AnyTLS。缓存可在正常退出时保存，不等价于崩溃安全的通用 Desired；本次未实现 SSM 适配器，不声明 TML 已支持。

VLESS/AnyTLS/HY2 用户目前由配置中的 users 数组定义。另一种受控方案是生成版本化配置、官方 check 后重载/重启，并由宿主受控发布器处理原子写入/回滚；这需要配置写权限，且不能承诺无连接中断，不属于当前 API-only 的动态 AddUser。源码内的 UpdateUsers 方法本身不是可远程调用的官方 RPC。Hysteria Realm 是 NAT 穿透会合服务，不是通用 HTTP 认证后端。

## 官方依据与验证

- [Hysteria HTTP 认证协议](https://v2.hysteria.network/docs/advanced/Full-Server-Config/#http-authentication)
- [sing-box 1.14.3 release](https://github.com/SagerNet/sing-box/releases/tag/v1.14.3)
- [StartedService 方法](https://github.com/SagerNet/sing-box/blob/v1.14.3/daemon/started_service.proto)
- [ManagedService 方法](https://github.com/SagerNet/sing-box/blob/v1.14.3/daemon/managed_service.proto)
- [SSM 官方配置](https://sing-box.sagernet.org/configuration/service/ssm-api/)
- [SSM 用户管理源码](https://github.com/SagerNet/sing-box/blob/v1.14.3/service/ssmapi/user.go)
- [HY2 配置用户与 Realm](https://github.com/SagerNet/sing-box/blob/v1.14.3/docs/configuration/inbound/hysteria2.md)

单元/API 验证覆盖未配置拒绝且无幽灵用户、单独密钥 hash、私网来源/伪造转发头、格式/长度限制、原生迁移、订阅用户创建和幂等、停用/到期/耗尽拒绝、采集暂停不影响认证、数据库重开、认证 URL 轮换、目标变化保护，以及认证不写统计基线/原始流量。官方 HY2 临时服务端已启动后再从 TML 创建用户，官方客户端成功传输 64 KiB，官方 /traffic 使用相同稳定 ID。测试不操作生产核心或连接。

## 部署准备阶段记录（2026-10-16；正式启用见下一节）

已部署 `traffic-manager-lite:v2-hyauth-20261016-123401`，TML healthy。备份及待审阅候选目录 `/data/traffic-manager-lite/backups/v2-hyauth-20261016-123401`。新增 003 已在生产应用；10 个稳定入站、现有订阅 Token/配额和每日账本保持。此次更新没有清库，没有重启代理核心或 Nginx。

生产 Hysteria2 仍为静态 userpass。已只读核对核心全部 2 个原生用户与 TML 登记认证一致，并准备独立认证后端（confirmed=0，configured_at=NULL）、精确 Docker 来源 CIDR 172.18.0.0/16、2 个原生认证账户。准备阶段不影响原有静态客户端或订阅发布，也不能新建假装已生效的 HY 动态用户。

`hysteria-config.before.yaml` 为受保护的原配置副本；`hysteria-config.http-auth.yaml` 为候选，YAML 语义差异仅 auth；`hysteria-switch-plan.json` 绑定当前配置 hash 与后端修订。它们包含敏感信息，目录仅 root 可读，不作为公开下载。核心实际文件 `/data/hysteria2/configs/config.yaml` 尚未改写。正式切换仍待用户明确授权修改 auth 段及重启 hysteria2；之后才可在 TML 确认后端已配置，并采集实际回调证据。

本轮全量 Go 测试、vet、Windows/Linux 构建通过；后续 userpass 大小写兼容专项、HTTP API 与存储/订阅回归也通过；官方 Hysteria2 隔离真实认证与流量 ID 实测通过。浏览器新增后端准备、未确认创建拒绝、既有订阅不受准备影响与 54 项页面尺寸检查通过。浏览器专项原超时来自抢先检查旧表单，已修正为等待正确弹窗，HY 关键等待设 5 秒错误上限。

## 用户明确授权后的正式启用（2026-10-16 20:47，Asia/Shanghai）

用户已明确授权“切换并重启 Hysteria2”。生产 `/data/hysteria2/configs/config.yaml` 已仅替换 auth 段为 HTTP 后端；监听、TLS/ACME、Stats Secret、其它配置均保持。该配置为单文件 bind mount，操作期间停止 Hysteria2 后在原 inode 写入，并验证容器内实际读取内容与候选一致。仅 Hysteria2 重启，TML/Xray/sing-box/xray-reality/Nginx 启动时间不变。

正式切换前暂停本实例采集并等待在途请求结束；对现有 2 个 cursor 标记已知受控重启边界，随后由正常采集事务重新建立基线，不修改累计账本、不清理或重置核心 API 计数器。完成后恢复原采集开关，统计 /traffic、/online、/dump/streams 正常。

两个现有原生账号均使用真实官方客户端对生产核心完成认证，HTTP 后端记录了各自的真实成功时间。其中一个账号使用大写用户名验证原 userpass 的大小写兼容；没有创建测试 Subscriber 或更改任何原生密码，没有转发模拟流量或把测试值写成流量事实。临时客户端容器均已移除。认证后端 confirmed=1，TML healthy，数据库完整性正常；已有入站 ID、订阅 Token、配额与历史账本保留。

最初验收工具使用 `hysteria2:443` 作为 client server 地址，被官方客户端误当作分享 URI，地址解析为空，已触发自动回滚恢复静态认证。只读检查定位后，将验收地址改为 Docker 私网 IP，先验证原静态原生账号可连接，再在同一授权下重新切换并通过全部认证。此问题来自验收地址，不是 HTTP 认证后端故障。

最终一致性备份与验收记录位于 `/data/traffic-manager-lite/backups/v2-hyauth-20261016-123401/activation-20261016-124730`；摘要另存父目录 `hysteria-activation.json`。原始静态配置备份、当前 HTTP 候选及独立 Desired 密钥仍受保护保存。后续新增 HY 订阅用户由 TML 数据库认证，无需重启核心或调用 AddUser。
