# Traffic Manager Lite

个人 VPS 的轻量代理流量监控与订阅管理系统。Go 单体服务，SQLite WAL，中文响应式后台；生产环境只运行一个容器，无 Node.js、Redis、Docker Socket。

已实现采集、持久统计、30 天归档、配置发现、人工覆盖、三种订阅、后台登录与管理。**尚未接入你的真实 VPS 或生产核心；本地测试通过不等于生产流量或连通性验收。** 具体记录见 [验证报告](docs/verification.md)。

## v1.3 Xray 采集纠偏与诊断

新增独立运行时 Clients 枚举、Stats/Online 独立健康状态、按服务器/实例查看的 API 诊断页面及历史。全离线用户仍通过 HandlerService 枚举；空 Stats 不删除用户；在线统计项缺失显示 Unknown。真实版本通过受控宿主证据读取，无证据显示 Unknown。

启动自动应用新增迁移 005/006，保留原有流量、cursor 和归档数据。详情、API 清单、版本检测步骤及限制见 [Xray v1.3 实施说明](docs/xray-v1.3.md)，测试结果见 [验证报告](docs/verification.md)。

## v1.2 更新与升级

- 默认由后台管理员保存的实例 API 地址授权，支持任意核心容器名称，无需逐个维护环境变量。
- `TML_ALLOWED_TARGETS` 留空或未设置为实例授权模式；非空为严格模式，旧配置继续生效。核心类型仅选择采集适配器，不代替地址授权。
- 默认授权精确到当前实例主机和端口；每次实际拨号重新检查 DNS/IP，不跟随 HTTP 重定向，不使用环境代理。仅添加地址不会保证服务可达，保存后点击“检测 API”验证真实统计 API，不写入基线或流量；点击“采集”才进入统计流程。
- 服务器支持新增、列表/详情、编辑、删除。停用服务器暂停所属实例的新采集（已进行中的采集可能完成），历史数据保留。删除有关联实例的服务器返回 409；先编辑实例迁移归属。归属迁移后，服务器维度历史统计也随实例归属变化。
- 服务器地址仅为 VPS 展示标识，不用于 API 连接或订阅节点地址。实例地址独立填写，如 `xray-multi:10085`。
- 严格模式保存实例时提前验证目标；采集时明确报告白名单或 DNS 错误，避免统一显示 gRPC Unavailable。

已有部署升级：在外层 `.env` 将 `TML_ALLOWED_TARGETS=` 留空以启用新模式，再在外层目录执行：

```bash
docker compose build traffic-manager-lite
docker compose up -d --no-deps traffic-manager-lite
```

本次无需新增数据库迁移。保留非空白名单则继续严格模式；无需重启代理核心。

## 快速部署

部署目录遵循固定布局：

```text
/data/traffic-manager-lite/
├── .env
├── docker-compose.yml
├── traffic-manager-lite/      # Git 源码仓库
├── configs/                   # 只读代理配置，或指定已有目录
├── data/traffic.db
├── backups/
└── logs/                      # 容器日志默认 stdout/stderr
```

在内层源码仓库中有 `deploy/docker-compose.yml` 和 `.env.example`；复制到外层后使用。当前尚无远程仓库地址，Go module 暂为 `traffic-manager-lite`，创建真实远程后统一调整。

```bash
mkdir -p /data/traffic-manager-lite/{data,backups,configs,logs}
cd /data/traffic-manager-lite
# 将本仓库克隆或复制到 ./traffic-manager-lite
cp traffic-manager-lite/deploy/docker-compose.yml ./docker-compose.yml
cp traffic-manager-lite/.env.example ./.env
chmod 600 .env
chown -R 10001:10001 data backups
```

编辑外层 `.env`：设置随机 `TML_ADMIN_PASSWORD`（至少 16 个字符，示例占位密码会被拒绝），`TML_ALLOWED_TARGETS` 默认留空，由后台保存的实例地址授权；需要严格模式时填写允许的容器名称/IP/CIDR，配置 `TML_CONFIG_HOST_DIR`。确认外部 `my-network` 已存在且代理核心可通过该网络访问。

```bash
docker network inspect my-network
# 若不存在，按你的现有网络规划创建；不要改动代理容器网络。
docker compose config --quiet
docker compose build --pull
docker compose up -d
docker compose ps
docker compose logs --tail=100 traffic-manager-lite
```

默认映射 `127.0.0.1:18080 → 8080`，通过现有 Nginx HTTPS 反向代理使用。`TML_SECURE_COOKIE=true` 要求浏览器 HTTPS；仅本地 HTTP 开发时设为 `false`。如 Nginx 在同一 Docker 网络，可转发 `http://traffic-manager-lite:8080`。反向代理需保留 Host；不要记录 `/sub/` 完整 URL（含 Token）。参见 [Nginx 示例](deploy/nginx.conf.example)。

容器以 UID/GID 10001 运行、根文件系统只读、去掉所有 capability、禁止新增特权；内存 256 MiB、CPU 0.5。只读配置目录须允许此 UID 读取。不同核心配置在后台填写容器内 `/app/configs/...` 的完整路径。

## 首次接入

1. 登录后在“核心实例”添加服务器和实例。
2. 填写 API 地址，Secret 只用于独立 Hysteria2 或 sing-box 原生 API。管理列表不会返回 API Secret。
3. 首次采集建立基线，之后增量才累计；不会把核心启动以来的已有流量冒充本系统观察到的流量。
4. 为实例设置只读配置路径，在“代理节点”扫描。
5. 补充客户端地址、真实连接端口、SNI/Host 等；服务端监听端口不会自动作为客户端端口。
6. 如同一逻辑用户有多核心身份，在“用户流量 → 关联身份”合并归属。改显示名称不会改采集标识或 cursor。
7. 创建订阅、选择属于该用户的节点，保存首次显示的订阅地址。

项目不会修改代理配置、添加代理用户、重启任何代理核心。

## 核心 API 与指标

| 核心 | 接入 | 流量 | 在线 |
|---|---|---|---|
| Xray | 官方 StatsService gRPC，`host:port` | 用户/inbound 独立计数器 | 探测扩展 API；IP 指标，不等同 TCP 连接 |
| V2Fly | 独立官方 StatsService gRPC，`host:port` | 用户/inbound | 明确“不支持”；最近活跃来自增量 |
| 独立 Hysteria2 | 官方 HTTP `/traffic`、`/online`，`http(s)://host:port` | 用户；客户端上传=rx、下载=tx | device，客户端实例数 |
| sing-box 1.14 | 自己的兼容 gRPC 服务 `experimental.v2rayapi.StatsService` | 只统计实际返回的用户/inbound 计数器 | 兼容 API 不提供在线；原生 API 支持时读取 session 快照 |
| sing-box 1.14 原生 API | `daemon.StartedService`，可单独填写 control endpoint | 实例累计总量独立存储，不分摊给用户 | 当前连接初始完整快照；GetVersion 探测真实版本 |

sing-box 可将统计 API 地址填为兼容接口，control endpoint 填为原生 API；原生 API Secret 通过 `authorization: Bearer ...` 发送。只有原生 API 时，也可将 API 地址填为原生地址：兼容统计探测会返回不可用，而实例流量与真实连接仍可独立获取。原生 API 缺失时，实际版本显示未知，管理员填写的版本只是声明。

sing-box 用户级能力“supported”仅说明已观察到相应计数器，不代表所有 Hysteria2、AnyTLS 等 inbound 均支持用户级统计。没有计数器时标记 unknown；不会伪造用户数据。sing-box 内部 Hysteria2 不使用独立 Hysteria2 的 HTTP API。

采集默认 10 秒、超时 8 秒、活跃窗口 60 秒。同实例调度和手动采集互斥，失败实例不阻塞其他实例。在线失败保留快照，超过 `2 × interval + timeout` 后显示“数据过期”。设备、IP、连接、最近活跃不可互换。当前采集周期只查询最新成功提交的 batch。

官方参考：[Xray Stats](https://github.com/XTLS/Xray-core/tree/main/app/stats/command)、[V2Fly Stats](https://github.com/v2fly/v2ray-core/tree/master/app/stats/command)、[Hysteria2 Traffic API](https://v2.hysteria.network/docs/advanced/Traffic-Stats-API/)、[sing-box V2Ray API](https://sing-box.sagernet.org/configuration/experimental/v2ray-api/)、[sing-box 1.14 原生 API](https://sing-box.sagernet.org/configuration/service/api/)。

## 数据正确性与归档

UTC 高精度时间戳；默认聚合时区 Asia/Shanghai。时区写入数据库后固定，改变 `TML_TIMEZONE` 会拒绝启动，防止把旧日汇总重新解释。重新分区历史数据需单独迁移；V1 不提供在线重建时区功能。

同一事务提交 cursor、采集批次、原始增量、小时汇总和日汇总。首次基线为零累计；数值回退重新建基线；负值、重复 counter、缺失上传/下载配对被拒绝/忽略；过时采集不推进 cursor。Xray/V2Fly 的 GetSysStats 可用时，用启动时间估计标记核心 epoch；sing-box 原生 API 使用 GetStartedAt。API 没有启动标识时，只能检测计数器回退；无法观察的重启间流量不估算。

用户/inbound/实例三种 counter scope 分别查询，不能相加。默认 Dashboard 为用户计数器的总和，不是整机网卡流量；缺少用户计数的核心，可在“用户流量 → 维度汇总”选择 inbound 或实例 scope 查询。节点只有唯一映射时才归属；不明确时为“未归属”。所有日期范围使用权威日汇总，原始明细与汇总不会重复累加。

每次启动及之后每 24 小时执行安全归档，也可手动触发：

- cutoff 为当前 UTC 时间减 30 天，等于边界的明细保留。
- 按小时、日期验证“剩余明细 + 已归档 ledger = 权威汇总”，同时检测缺失汇总行。
- 在同一事务累计归档 ledger、删除已验证明细并记录归档结果；失败整体回滚。
- 重复归档不产生新贡献，累计值保持不变。小时、日汇总与 cursor 长期保留。
- 采集错误记录保留 30 天；不会自动执行完整 VACUUM。

## 配置发现与订阅

解析 Xray/V2Fly JSON、sing-box JSON、Hysteria2 YAML。仅白名单客户端字段进入 NodeProfile；不保存原始私钥、API Secret 到节点 JSON。Reality 公钥可通过 X25519 服务端私钥推导，私钥不会输出。文件必须位于 `TML_CONFIG_ROOT`，符号链接解析后也不能越界。

节点 ID 来自实例、inbound、核心用户标识；人工覆盖单独保存。重新扫描更新自动发现字段，保留覆盖；已从配置移除的节点标为缺失并停止输出。`PATCH` 覆盖字段为 `null` 可恢复自动发现值。

| 协议组合 | v2rayN Base64 | Mihomo YAML | sing-box JSON |
|---|---|---|---|
| VLESS / VMess / Trojan + TCP、WS、gRPC | 支持分享链接 | 支持 | 支持 |
| VLESS + TCP + Reality | 支持 | 支持 | 支持 |
| Shadowsocks（无未配置插件/TLS） | 支持 | 支持 | 支持 |
| Hysteria2 | 支持 | 支持 | 支持 |
| AnyTLS | 保守过滤，URI 范围未验证 | 支持 | 支持 |
| XHTTP / 其他未验证组合 | 明确过滤 | 明确过滤 | 明确过滤 |

Mihomo 输出 `proxies`、sing-box 输出 `outbounds`，用于导入客户端或合并到用户自己的路由配置，不覆盖 DNS/路由规则。实际生成配置已通过官方 sing-box 1.14.0 `check` 和 Mihomo 1.19.32 `-t`；仍需实际节点连接验收。过滤原因可在节点后台查看，订阅响应头 `X-TML-Filtered-Nodes` 给出数量。

独立 Hysteria2 的 password/userpass 静态认证可发现。HTTP/command 认证无法从静态配置推导客户端凭据，扫描明确返回限制。端口跳跃、obfs、ECH、插件、复杂 XHTTP、特殊 Shadowsocks 多用户加密等组合不在已验证输出范围；不要假设普通配置能连接这些服务。

Token 随机 256 位、数据库仅存 SHA256；创建/轮换返回一次明文。禁用可撤销；格式与 Token 对应。选择节点及每次生成时均检查用户归属，身份转移后旧用户订阅不能输出新用户节点。

## 环境变量与设置

见 [.env.example](.env.example)。核心参数均使用 `TML_`；`TZ` 仅为系统展示习惯。

后台可保存采集间隔、活跃窗口、归档开关和日志级别到 SQLite；重启**本管理服务**后生效，保存设置不会重启代理核心。数据库保存值优先于环境变量。聚合时区和 30 天保留策略固定，不能在后台误改历史边界。

管理 API 使用会话 Cookie + CSRF，自动化运维可使用 HTTP Basic（通过 HTTPS 或本机连接）。订阅 Token 不能用于管理认证。API 目标默认限定为当前实例由管理员保存的主机和端口；非空 `TML_ALLOWED_TARGETS` 则额外使用部署白名单；HTTP 不跟随重定向、不使用环境代理，DNS 地址在每次实际拨号时检查。禁止未指定、组播、链路本地及已知云元数据地址。登录限速、Cookie HttpOnly/SameSite、CSP、请求体大小限制。SQLite/备份文件默认权限 0600；数据库含客户端凭据和 API Secret，备份也需保密。

## 更新

```bash
cd /data/traffic-manager-lite/traffic-manager-lite
git pull --ff-only
cd ..
docker compose build --pull traffic-manager-lite
docker compose up -d traffic-manager-lite
```

更新前创建数据库备份；迁移按编号在事务中执行。生产 `.env`、data、backups 在 Git 仓库之外。上述操作仅更新本管理服务。

## 备份与恢复

后台“系统设置 → 创建一致性备份”使用 SQLite `VACUUM INTO` 导出独立数据库；不会在运行时直接复制 WAL 数据库。也可以通过认证 API `POST /api/v1/maintenance/backup`。备份保存在外层 backups，路径由 `TML_BACKUP_DIR` 控制；建议另行加密备份到异机。

恢复是管理员显式操作，不能在运行中覆盖数据库：

```bash
cd /data/traffic-manager-lite
docker compose stop traffic-manager-lite
mv data data-before-restore
mkdir data
cp backups/traffic-YYYYMMDD-HHMMSS.NNNNNNNNN.db data/traffic.db
chown -R 10001:10001 data
chmod 700 data
chmod 600 data/traffic.db
docker compose up -d traffic-manager-lite
```

保留旧目录以便回滚；不要把旧数据库的 `-wal`/`-shm` 文件与恢复备份混用。确认 health、采集基线和历史总量正常后再自行处理旧数据。

## 本地开发与测试

Go 1.27.1（go.mod 最低 1.27.0），不用 Node 构建前端。下载依赖后：

```bash
go mod download
go test ./...
go vet ./...
go build -o dist/traffic-manager-lite ./cmd/traffic-manager-lite
TML_ADMIN_PASSWORD='replace-with-long-random-password' TML_SECURE_COOKIE=false TML_LISTEN=127.0.0.1:18080 ./dist/traffic-manager-lite
```

官方客户端校验与浏览器验证：

```bash
TML_TEST_SINGBOX=/path/to/sing-box TML_TEST_MIHOMO=/path/to/mihomo go test -v ./internal/subscription -run TestRealClientConfigurationValidation
# Install Playwright for development tests only; production does not need Node.
TML_PLAYWRIGHT_MODULE=/path/to/playwright node scripts/e2e.cjs
```

浏览器测试自动启动 127.0.0.1:18081 的管理服务、临时 SQLite、测试夹具 HTTP API；不会连接生产核心。截图在 `dist/ui-verification`，带有夹具流量的截图文件名包含 `fixture`。CI 包括 Go 测试、race、vet、Docker build、健康检查和浏览器测试。

## 管理 API

全部管理路径使用 `/api/v1` 且必须认证；health 仅返回 ok/unavailable。核心接口：

- `GET dashboard / servers / instances / users / identities / online / nodes / subscriptions / settings`
- `POST servers / instances / instances/{id}/collect / discovery/scan / subscriptions`
- `PATCH instances/{id} / nodes/{id} / users/{id} / subscriptions/{id} / settings`
- `POST users/{id}/identities / subscriptions/{id}/rotate / maintenance/archive / maintenance/backup`
- `GET traffic/summary / traffic/history`
- 公开订阅 `GET /sub/{v2ray|mihomo|singbox}/{token}`

流量参数：`range=today|yesterday|7d|30d|month|last_month|all|custom|cycle`；`from/to=YYYY-MM-DD`（含结束日期）；`dimension=user|server|instance|inbound|protocol|node|core`；`scope=user|inbound|instance`；可按 `user_id/instance_id/server_id/inbound/protocol/node_id` 过滤。默认 scope=user。

项目架构和后续会话约定见 [AGENTS.md](AGENTS.md)。第三方 schema 版本及许可见 [internal/proto/README.md](internal/proto/README.md)。
