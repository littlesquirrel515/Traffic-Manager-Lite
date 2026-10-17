# Traffic Manager Lite v2.0

远程优先的四核心流量监控、入站/连接入口与用户订阅管理。Go 单体、SQLite WAL、嵌入中文响应式 Web；默认无代理配置挂载、SSH、Docker Socket、Node.js 或 Redis。

**这是 breaking release。旧数据库不会自动升级或清空，旧订阅 Token 不保留。生产替换必须先完成备份、核对独占数据库及清理范围，再明确确认。本部署已于 2026-10-15 获用户确认后完成 v2 空白基线切换。** 见 [干净基线与恢复](docs/clean-baseline.md) 和 [本轮验证状态](docs/v2-verification.md)。

## 新部署

外层 `/data/traffic-manager-lite` 保存 `.env`、`docker-compose.yml`、`data`、`backups`，内层 `traffic-manager-lite` 保存源码。

1. 将 `.env.example` 和 `deploy/docker-compose.yml` 复制到外层，填写至少 16 字符的随机管理密码（例如 `openssl rand -hex 16` 的输出）。
2. 创建 data、backups 并授权容器 UID 10001 写入，连接可访问核心 API 的私有 Docker 网络。默认仅绑定宿主 127.0.0.1:18080。
3. **仅空白新部署** 执行 `docker compose build traffic-manager-lite`、`docker compose up -d traffic-manager-lite`。已有数据库先遵循 clean-baseline 文档，不能直接套用本步骤。
4. HTTPS 反代后台，登录后先添加服务器（展示归组），再添加实例 API 地址。TML_ALLOWED_TARGETS 留空时仅授权已保存实例的精确主机与端口；非空启用严格限制。
5. 检测 API 和查看诊断；Xray 可点击“从 API 发现”。其他核心在 API 缺少枚举时手动登记真实入站及已有客户端，显示 manual_unverified。
6. 为入站新增连接入口，填写公网 Address/Port、Host/SNI/Path 等。自动创建稳定绑定和默认 Proxy Node，不自动创建核心用户。
7. 在订阅用户统一创建 Xray 动态客户端或纳管真实原生客户端，再建分组并选择节点/分组生成订阅。新 Subscriber 默认 10 GiB、3 个日历月，所有订阅共用。普通连接入口仅编辑 Address、Port、Host、SNI；其余协议参数继承入站。

## 关键行为

Clients、Stats、Online 独立；空统计和全部离线不会删除配置用户。流量初次建立基线，不计入核心接入前累计。原始增量、cursor 和汇总同事务，30 天归档保留历史账本。同一活动计费来源不能分配给多个 Subscriber。

Xray / V2Fly 官方 gRPC、sing-box 原生 / V2Ray / Clash、Hysteria2 Traffic Stats 分别适配。sing-box 原生用户连接流量只为实际观测差值，partial；不伪装完整计数器。Hysteria2 tx=客户端上传、rx=客户端下载。

订阅提供 Raw、Base64、v2rayN URI、Mihomo 和 sing-box 模板；沿用已验证协议/传输白名单。Mihomo 1.19.32 支持已验证 VLESS XHTTP TLS/REALITY 四种模式；sing-box 1.14.3 不声明 XHTTP。无可用节点返回明确错误，不返回成功空白。Token 仅存 SHA256，明文创建/轮换时返回一次。

普通入站登记只改 TML 数据。运行时 Client 管理统一从订阅用户发起并明确确认，Desired 使用 TML_MANAGED_KEY_FILE 加密；Xray 创建后回读验证，授权后对精确目标验证缺失并幂等补回，不修改核心配置、不自动替换冲突账号。创建未完成或订阅用户禁用时不自动恢复。当前 sing-box/Hysteria2 原生接口不支持此动态创建流程，原生用户展示、纳管和采集仍可用；不能伪造 AddUser 成功。具体流程与限制见 [订阅用户与 Client 生命周期](docs/subscriber-client-lifecycle.md)。

## 开发验证

```sh
go build ./cmd/traffic-manager-lite ./cmd/tml-baseline
go test ./...
go vet ./...
TML_TEST_SINGBOX=/path/sing-box TML_TEST_MIHOMO=/path/mihomo go test -v ./internal/subscription -run 'TestRealClientConfigurationValidation|TestMihomoXHTTPModesTLSRealityAndOfficialValidation'
TML_PLAYWRIGHT_MODULE=/path/playwright node scripts/e2e.cjs
```

浏览器脚本只用临时库和 fixture API，不访问生产代理。Dockerfile 提供 `test` 阶段的 Linux race/vet 验证。生产镜像只包括管理程序和只读基线准备工具。

领域：[术语](docs/domain-glossary.md)、[远程适配器与真实限制](docs/remote-core-architecture.md)、[流量归属](docs/traffic-accounting.md)、[订阅](docs/subscription-architecture.md)。历史 v1 文档只用于审计，不能据其旧升级命令部署 v2。
