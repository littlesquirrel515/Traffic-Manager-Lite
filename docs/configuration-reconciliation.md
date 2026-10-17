# 配置调和 v1.1

## 修改前审计

2026-10-12，基于当前源码和已部署迁移014：

1. catalog_inbounds 为随机内部ID，迁移的旧ID保留；唯一定位仍为 instance/tag/protocol。
2. 相同 tag 保留ID；只有结构完全一致、候选唯一的 tag 改名会保留ID。端口和Path同时变化没有历史确认机制。
3. catalog 自动建立默认节点；旧 nodes 的ID仍由 instance/tag/user 哈希生成，因此改名会重复创建兼容节点和认证来源。
4. Xray 当前仅只读 Handler 枚举，没有热添加入口；不能声称已支持持久化 Xray AddUser。
5. sing-box 的受控宿主代理把 CRUD 写到实际配置，但没有独立 Desired State。外部替换文件可丢失这些用户。原生HY没有文件CRUD。
6. 代理核实际加载文件需通过宿主白名单、Docker启动参数及目录bind验证；监控服务只读挂载不证明文件已运行。
7. Config/API分别保存观察；配置用户认证不一定与运行资产一致。Stats或Online缺项不能证明删除。
8. 新订阅引用稳定catalogID，旧订阅引用Tag派生nodeID。历史统计同时存在instance/email和instance/inbound/name粒度。
9. 宿主备份/原子保存/重启健康/恢复只保护文件及宿主journal，不与SQLite组成分布式事务。
10. 迁移001–014及统计事实必须保留；任何身份合并只增加历史映射，不重写流量。

## 权限边界

Base由管理员维护，Managed为显式托管期望状态，Observed不覆盖Managed，Effective由核心专用适配器合并。默认Observe Only；文件应用需宿主目标白名单、匹配配置修订、管理员确认和重启二次确认。不能自动修改生产Compose或挂载Docker Socket。

## 数据所有权与稳定身份

Base 是管理员的网络配置；Managed 是明确授权的用户期望状态；Observed 是配置/API 的独立观察；Effective 是 Base 加 Managed Overlay 后的实际配置。自动观察不会覆盖 Managed，不将 Stats/Online 作为用户删除依据。外部用户默认为 external，只有显式 adopted 才接管认证；新建 managed 不允许暗中覆盖同名外部用户。

稳定 ID 为独立随机内部 ID，并绑定实例。Tag、来源路径、监听端口、协议和辅助结构指纹均为属性。相同 Tag 默认保留 ID；唯一且结构相同的改名保留 ID；Tag/端口交换、协议变化、多个相似候选、Tag/Path/端口同时变化先写入 pending，管理员检查差异后映射旧身份或确认新入站。结构指纹不是业务主键。历史别名记录以前的 Tag 和来源，旧的客户端 Node/凭据 ID 通过 inbound_client_keys 保留。

不同实例不会合并。协议变化不将旧认证绑定迁到新协议。来源暂时消失只标记 source_missing，保留对象、期望用户、绑定、订阅与流量。再出现并可靠匹配时恢复对象。现存对象结构变化标记 review，确认前过滤客户端输出。

## 托管用户、恢复与调和

托管认证采用 AES-256-GCM 加密，密钥来自独立 `TML_MANAGED_KEY_FILE`，不得放在数据库或代码中。密钥文件为 32 字节随机二进制或 64 个十六进制字符，Linux 权限必须为0600；密钥缺失时拒绝托管保存/应用/加密修复。数据库只返回用户身份、所有权、期望代次、应用代次及状态，不返回加密载荷或认证。

配置中已有用户需选择 adopted；新增用户选择 managed；禁用表示期望移除，保存并不立即改核心。同名、认证不同而无授权的外部用户是冲突。Overlay 只修改用户认证列表，保留路由、监听、TLS等管理员设置。已托管用户重新部署后缺失时生成恢复计划，不自动删除 Desired。回滚恢复文件，但保留管理员最新 Desired，标记 pending_apply，防止撤销管理员意图。

默认 Observe Only 每30秒检查有托管状态的启用实例，保存元数据计划并标记漂移。Assisted Apply 必须逐计划确认及再次确认重启。Managed 自动恢复必须逐实例授权托管字段和自动重启，且代理/独立密钥已配置。计划绑定 Base hash 和 Managed generation/hash；应用前重新检查，过期计划拒绝。

流程：Plan / Dry Run → 加密数据库备份 → durable prepare → 宿主保存与原子替换 → 保存宿主修订 → 明确重启 → 核心健康/运行验证 → Applied。失败恢复旧文件并验证；恢复失败不能显示成功。SQLite与外部重启不是同一事务。中断留下 applying/unknown；Verify 必须证明此计划的精确 Effective hash及运行健康后才能结案，认证或期望代次变化则拒绝确认旧计划。保存后尚未重启的中断，可在核对宿主精确修订后再次明确确认应用，以恢复待重启步骤；不会偷偷重启。不能凭数据库的‘保存成功’判断核心已加载。

## 核心应用策略与真实限制

| 核心 | 持久化策略 | 校验/运行证据 | 限制 |
|---|---|---|---|
| Xray | 保存 Overlay 后确认重启 | 固定授权版本的 `xray run -test -c`；Stats GetSysStats；Handler GetInboundUsers核对 Email和账号 | 当前没有原有热添加实现可复用；本版不调用 AddUser/RemoveUser，不宣称零中断。需启用相关只读API，否则拒绝确认 |
| sing-box | 合并配置用户后确认重启 | 固定1.14.3官方check及既有native健康/用户验证 | 单文件配置、目录bind；合并目录/其他版本/不支持协议保守拒绝 |
| 原生 Hysteria2 | 只管理静态userpass，认证字段补丁后确认重启 | 固定授权版本；YAML和userpass检查；非认证字段语义不变；官方只读/traffic健康 | 官方2.13.0无离线check命令，不能冒充完整官方校验。password单身份、HTTP/command认证不托管；Traffic API不能证明全部认证用户或实际客户端连接 |
| V2Fly | 只读身份/观察保持原有支持 | 独立官方API | 本次没有写入适配器，不能套用Xray写入能力 |

完整代理连接、多协议公网链路需真实客户端另行验证。测试夹具、配置校验和管理健康均不能证明代理公网可连接。资料：[Xray官方命令](https://xtls.github.io/en/document/command)、[Hysteria2官方server源码](https://github.com/apernet/hysteria/blob/app/v2.13.0/app/cmd/server.go)。

## External Deploy 与 Managed Deploy

External Deploy 兼容原有单配置文件：管理员部署后，只读发现变化；确认应用会通过宿主受控代理修改实际文件。外部部署仍可能覆盖 Effective，Desired 保留并提示恢复。不会要求更改现有代理Compose。

Managed Deploy 使用两份不同文件：只读 Base（管理员维护）和生成的 Effective（代理实际加载）。在TML实例的只读配置路径填写实际Effective，在调和策略单独填写只读Base完整路径（两者均需在配置根目录内）；宿主目标映射中 `host_base_config` 指向Base、`host_config` 指向Effective、`container_config` 指向容器实际加载Effective。宿主会验证启动参数、目录bind、文件路径、授权版本及实际运行。Base和Effective不能是同一个文件。后台只选择模式不会修改Compose或切换核心加载路径；应先由管理员审阅部署示例并自行改好加载路径。

`core-agent-targets.example.json` 只定义可选宿主代理的写入授权：已有实例ID到固定容器、配置文件和健康API的映射。它不是发现实例的第二份清单，也不取代后台添加服务器/实例URL。只读采集不需要这个文件。控制权限需比URL更具体，不能让后台任意URL变成宿主任意文件/容器写权限。

## Node / Endpoint / Subscription

Node 引用稳定 Inbound，并可引用独立 Endpoint。Endpoint的优选地址、客户端端口、SNI、Host、ALPN与TLS 不由核心配置自动覆盖；不同Endpoint代表合法多入口，不能因落到同IP就合并。Node可显式 inherit/set/clear 字段，覆盖值优先。

Endpoint支持CRUD、复制、排序、批量启停和连接字段修改，修改前展示关联节点/订阅数量并确认共享影响。节点行可直接编辑其共享入口。批量更新事务执行，任意无效字段/缺失ID导致整批回滚。订阅下次请求按最新入口解析；旧Token/授权保持原样，不授权任何新节点或用户。

历史入站合并先Dry Run：同实例同协议，主对象必须现存、待合并对象缺失；身份/认证/所有权/绑定冲突拒绝。合并先加密备份，在事务中再次核对修订和引用；迁移Node、Subscriber绑定、Managed、历史别名，旧兼容Node引用通过重定向保留。不同实例/协议、两个现存对象、认证冲突拒绝。

Node去重必须同一稳定入站、同一Endpoint、最终连接参数与覆盖规则一致；明确确认后迁移订阅/分组引用，不删除核心用户。预览修订过期拒绝合并。原始流量、小时/日汇总、归档账本不改写；历史Tag通过身份别名解释，不把历史计数迁移/重复加入总量。

## REST API 与后台

新增管理员鉴权及CSRF保护的API：

- `GET /api/v1/inbound-identities`、`/pending`、`/{id}/history`；`POST /resolve`。
- `GET/POST /api/v1/managed-users`：只返回认证无关元数据。
- `POST /api/v1/reconciliation/plan`；`GET /plans`、`/plans/{id}`、`/plans/{id}/history`。
- `POST /plans/{id}/apply|rollback|verify`；`GET /status|policies`、`PATCH /policies/{instance_id}`。
- `GET /api/v1/duplicates/candidates|nodes`；`POST /preview|merge|nodes/merge`。
- `POST /api/v1/catalog/endpoints/{id}/copy`、`/endpoints/batch`；原CRUD保留。

后台新增‘入站身份’‘托管用户’‘配置调和’‘重复数据修复’，支持电脑、平板及手机。敏感值输入为密码框、更新留空保留；预览/审计不输出UUID、密码、Token、Secret。

## 升级、安全及回滚

迁移015增量执行，001–014保持原样。首次升级不自动接管任何已有外部用户，不开启写入/自动重启。现有订阅Token、节点/凭据ID和统计数据保留。

现有生产部署只升级TML镜像即可运行观察/身份/入口功能，无需改代理Compose。操作顺序：

1. 创建SQLite一致性备份，保存匹配的旧镜像、外层`.env`/Compose及节点、Token哈希、统计/核心启动时间基线。
2. 在源码目录测试、构建镜像，外层 `/data/traffic-manager-lite` 执行 `docker compose up -d --no-deps traffic-manager-lite`。
3. 检查健康、迁移015、数据库integrity/foreign_key、原订阅输出和代理未被重启。
4. 需要托管时，再创建独立随机密钥文件，以单文件只读挂载给TML，配置 `TML_MANAGED_KEY_FILE`。宿主代理独立运行，0700状态目录、0600token/目标文件/备份key，受控私网API，保留禁用自动写默认值。
5. 先adopt/managed 保存Desired，检查Dry Run，再设置Assisted并确认重启。仅在验证恢复流程后可显式启用Managed。

新增Desired认证和应用/合并数据库备份加密。配置核心文件本身必须含认证，不能用加密文件替代核心所需配置。credential_material配置观察库（含后续配置观察）/旧数据库/历史备份仍按原版本保存配置认证，应限制访问并离线加密长期备份；不声称升级会自动加密全部旧数据。配置Managed密钥后，后台maintenance/backup也生成加密备份；缺key不回退明文。

宿主备份独立AES-GCM密钥位于StateDir/backup.key，旧明文备份仅在匹配宿主journal路径和原始hash时原地加密转换，未知文件拒绝。TML恢复备份密钥与宿主备份密钥用途不同，务必分别异地保存。单次内置数据库加密恢复备份限制16MiB，较大库应离线工具备份；宿主配置4MiB。

回滚015需停止TML，恢复匹配的迁移014数据库及旧镜像后启动，不能只删迁移记录；不得覆盖仍运行的WAL数据库。加密数据库用 `tml-recovery -help` 的固定文件输入/输出离线解密到新文件（0600），先检查一致性再替换；密钥丢失无法解密，不记录明文或尝试执行用户命令。文件回滚不等于回滚SQLite全部业务状态；宿主与TML的持久journal用于解释中断，人工核对后确认恢复。

## 验证

单元/集成测试覆盖稳定身份、改名、结构歧义、旧Node/凭据ID保留、加密Desired、重复应用、修订冲突、明确确认、回滚后Desired保留、共享Endpoint和真实官方客户端输出校验。浏览器仅使用临时数据库和fixtureAPI，不接触生产代理。实际执行结果记录于 docs/verification.md。

原v1.4 managed-config配置操作API保留兼容，属于配置编辑，不能推断调用者已授权持久托管。后台旧入口现已转到托管用户/配置调和流程；旧API或外部工具维护的已有用户需明确adopted，不能根据旧配置观察自动接管。

未结案的applying/unknown会阻止该实例后台自动生成并应用其他计划，避免未知状态不断重试。当前Desired已经变更时，Verify只能将已证明的旧宿主修订记录为superseded，不更新当前Desired应用代次；最新状态仍需新计划确认。临时明文数据库备份置于0700专用目录，加密后立即清理。


## 本轮代码文件

- `migrations/015_configuration_reconciliation.sql`：新增身份、Desired、计划/策略、历史修复与重定向表。
- `internal/inboundidentity/identity.go`、`merge.go`：稳定身份、待确认映射、事务合并。
- `internal/reconciliation/service.go`、`agent.go`：计划/应用/验证/回滚、定时观察、受控宿主适配器。
- `internal/coremanage/overlay.go`、`backup.go`、`docker_multicore.go`、`xray_verify.go`，以及原manager/docker/http：核心专用补丁、加密备份、版本/健康验证、静态目标权限。
- `internal/secretstore/seal.go`、`cmd/tml-recovery/main.go`：独立加密及离线恢复工具。
- `internal/catalog/endpoint_batch.go`、`merge.go`、原catalog：共享入口事务批量、复制影响数量、节点去重修订。
- `internal/discovery/reconcile.go`、`internal/subscription/store.go`：旧客户端ID保留及旧订阅共享入口联动。
- `internal/httpapi/reconciliation.go`、catalog/api：新管理API、权限/确认、加密备份接口。
- `internal/config/config.go`、`cmd/traffic-manager-lite/main.go`：密钥配置、调和worker生命周期。
- `web/reconciliation.js`、catalog/app/index、`scripts/e2e.cjs`：四个新后台页面、入口批量、旧用户操作入口统一。
- `internal/catalog/*identity*test.go`、`*merge*test.go`、`reconciliation_test.go`，`internal/reconciliation/service_test.go`、`internal/coremanage/*overlay*test.go`、`*real_test.go`、`internal/httpapi/reconciliation_test.go`、`internal/storage/catalog_migration_test.go`、`internal/secretstore/seal_test.go`：升级、身份、恢复、共享/历史和真实核心容器回归。
- `Dockerfile`、`.env.example`、`deploy/docker-compose.reconciliation.example.yml`、`deploy/core-agent-targets.example.json`、README/AGENTS及验证文档。

应用和回滚确认成功后立即读取匹配修订的Effective并同步凭据观察，避免订阅继续使用旧认证；观察失败单独标记applied_observation_pending，不否认宿主已应用的证据。adopted的最初Base认证证据随Desired加密保留，多次轮换认证仍可安全合并未被管理员改动的原Base；未知外部认证仍是冲突。
