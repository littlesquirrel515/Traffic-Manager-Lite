# TML v2 远程核心架构

默认部署只有 API 网络权限，不读取代理配置、不要求 SSH、不挂载 Docker Socket。服务器地址用于分组，实例的 API 地址用于访问，Endpoint 地址用于客户端连接，三者独立。

## 接入和来源

服务器 → 核心实例 → 检测 API / 诊断 → API 发现或手动登记入站 → 登记真实客户端 → 新增连接入口（原子创建绑定和默认节点）→ 分组 → Subscriber → 凭据绑定和订阅。

`remote_inbound_registry.source_type` 区分 native_api、manual、agent、optional_config。人工登记初始为 manual_unverified，不宣称核心加载了该配置；表中 present 表示登记资产保留状态，runtime_state/verification 才描述运行验证。配置完整的 Ready 不代表网络验证成功。

Xray `ListInbounds(isOnlyTags=false)` 返回官方 TypedMessage。隔离 protobuf 投影按固定官方字段解码监听、WS、gRPC、基础 XHTTP、TLS、REALITY；私钥只在内存中用于 X25519 公钥推导，不进入入站/节点 JSON。无法识别的类型和未知 XHTTP extra 明确保留限制，不虚构可用参数。解析失败保留旧资产，不能视为远程删除。完整成功枚举的真实缺项才建立缺失证据。人工连接字段不被后台发现覆盖。

V2Fly、sing-box、Hysteria2 缺少完整入站配置/Clients 原生枚举时使用人工登记。统计用户和连接列表不是完整 Clients。可选文件来源保留在独立模块；默认进程不运行文件扫描或旧配置调和后台任务，普通接入表单没有配置路径字段。

## 官方基线与能力

2026-10-15 通过官方 releases/latest 核实：Xray 26.3.27、V2Fly 5.53.0、sing-box 1.14.3、Hysteria2 2.13.0。这不是对已连接实例运行版本的推断；无 API/有效证据时显示 Unknown。

| 能力 | Xray | V2Fly | sing-box | Hysteria2 |
|---|---|---|---|---|
| 入站完整配置枚举 | ListInbounds，按实际端点验证 | 无该完整枚举 | StartedService 不枚举服务端配置 | 无 |
| 配置用户枚举 | GetInboundUsers，依协议 UserManager | Handler 无完整枚举 | 连接列表不等于用户清单 | traffic/online 不等于用户清单 |
| 用户流量 | StatsService，需开启对应计数 | 独立官方 StatsService | 独立 experimental.v2rayapi；原生仅观测连接差值 | GET /traffic，tx 上传、rx 下载 |
| 在线 | 实际支持的 Online 方法，缺项 Unknown | 不提供完整用户在线 | 原生 session；Clash 不能补造用户 | GET /online，设备数；不伪造在线 IP |
| 运行时用户写入 | AlterInbound；显式确认、回读 | AlterInbound；缺少完整回读 | 所接统计/观测 API 不提供 | Stats API 不提供；外部认证后端独立管理 |
| 核心配置持久化 | 原生用户变更无保证 | 无保证 | 不由统计 API 保证 | 不由统计 API 保证 |
| 版本 | 无可靠版本 RPC 时 Unknown | 同左 | 原生 GetVersion | 无版本接口时 Unknown |

能力矩阵状态为 supported、unsupported、unknown、unavailable、permission_denied。Disabled 映射 unavailable，网络失败不能映射 unsupported，空响应不能映射故障。诊断与采集健康独立，全部诊断只读、不清计数、不踢用户、不增删账号。

## 运行时 Desired

`remote_runtime_users` 保存加密 Desired、稳定入站和真实用户身份、批准的 endpoint/tag/protocol、修订、运行验证与 runtime_only 持久化声明。写入前需要全局 `TML_MANAGED_KEY_FILE`，独立私有 32 字节密钥；它不是代理配置文件，不影响只读接入。不要遗失此密钥。

Xray 添加先回读，已有相同账号幂等成功，不覆盖不同认证。更新是明确确认后的 remove/add，可能中断该账号；失败尝试补偿旧账号，补偿不确定必须显示 Unknown。V2Fly 不支持安全回读替换和自动恢复，直接添加/删除 RPC 成功仅为 runtime_applied_unverified。

Xray 缺失账号恢复默认关闭，管理员逐 Desired 授权后才允许读前验证、仅添加缺失用户；从不自动删除/替换冲突账号。endpoint/tag/protocol 改变则停止恢复。中断状态超过保护窗口后仅回读核查，不能凭 DB 状态推定远程成功。多个进程不能宣称跨 RPC/SQLite 原子事务。

## 访问控制

后台 API 均需管理员认证和会话 CSRF。诊断超时、响应大小、数量均有限制。API 目标仅授权已保存实例的精确主机/端口，非空 TML_ALLOWED_TARGETS 继续严格限制；实际拨号每次校验 DNS，拒绝 metadata/link-local 等地址。官方 Xray/V2Fly gRPC 通常不自带安全认证，应在 Docker 私网、VPN 或受保护的网络中访问，不要求开放公网管理端口。

官方依据：[Xray Handler schema](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/proxyman/command/command.proto)、[Xray ReceiverConfig](https://github.com/XTLS/Xray-core/blob/v26.3.27/app/proxyman/config.proto)、[V2Fly Handler schema](https://github.com/v2fly/v2ray-core/blob/v5.53.0/app/proxyman/command/command.proto)、[sing-box StartedService](https://github.com/SagerNet/sing-box/blob/v1.14.3/daemon/started_service.proto)、[sing-box V2Ray API](https://sing-box.sagernet.org/configuration/experimental/v2ray-api/)、[Hysteria Traffic Stats API](https://v2.hysteria.network/docs/advanced/Traffic-Stats-API/)。
