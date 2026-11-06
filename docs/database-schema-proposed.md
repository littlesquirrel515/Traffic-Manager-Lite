# Traffic Manager Lite 数据库结构与用途

生成日期：2026-11-07。以仓库 migrations/001–012 在空 SQLite 数据库重建；不读取业务数据。

本文件反映源码预期结构，实际服务器以其 schema_migrations 和 SQLite PRAGMA 为准。迁移 012 属于本次改造；其他服务器版本可能不同。 字段类型是 SQLite 声明类型；NOT NULL、默认值和复合主键以表格和建表 SQL 为准。

本文件仅列模式，不包含密码、令牌、UUID、真实地址、数据行或密钥。

当前共 81 张表（含 schema_migrations）。

## 表索引

| 表 | 用途 |
|---|---|
| `archive_daily_ledger` | 归档前日级核对账本 |
| `archive_ledger` | 归档前小时级核对账本 |
| `archive_runs` | 归档任务记录 |
| `archived_counter_evidence` | 归档后的计数器来源证据 |
| `catalog_inbounds` | 持久化的入站身份、协议和来源状态 |
| `catalog_subscription_groups` | 高级订阅直接引用的分组 |
| `catalog_subscription_nodes` | 高级订阅直接包含的节点 |
| `catalog_subscriptions` | 订阅资源、Token 哈希、格式和状态 |
| `catalog_sync_runs` | 节点目录同步运行记录 |
| `collection_batches` | 实例采集批次游标 |
| `collector_errors` | 采集错误历史 |
| `collector_health` | Clients、Stats、Online 等采集健康状态 |
| `config_operations` | 配置操作及执行状态 |
| `core_asset_states` | 核心资产清单完整性及采集状态 |
| `core_clients` | 非 Xray 客户端资产观察 |
| `core_connection_snapshots` | 核心连接观测快照 |
| `core_diagnostics` | 多核心 API 诊断历史 |
| `core_runtime` | 多核心版本与运行信息 |
| `core_user_states` | 非 Xray 用户统计及在线状态 |
| `credential_material` | 客户端认证材料及状态 |
| `direction_audits` | 流量方向审计 |
| `endpoint_binding_audit` | 连接绑定配置变化审计 |
| `endpoint_connection_requests` | 连接入口创建请求的幂等记录 |
| `hysteria_auth_accounts` | Hysteria2 HTTP 认证账号映射 |
| `hysteria_auth_backends` | Hysteria2 HTTP 认证后端绑定与确认 |
| `identities` | 核心用户、入站或实例到用户资产的映射 |
| `inbound_client_keys` | 稳定入站与客户端身份映射 |
| `inbound_endpoint_bindings` | 入站与连接入口的绑定及覆盖 |
| `inbound_identity_aliases` | 入站稳定身份别名 |
| `inbound_identity_history` | 入站身份及来源变化历史 |
| `inbound_identity_pending` | 等待确认的入站身份变更 |
| `inbound_merge_audit` | 入站归并审计 |
| `inbound_retirements` | 入站退役记录 |
| `instance_epochs` | 核心启动代次估计 |
| `instances` | 核心实例接入地址、类型、启停与能力 |
| `native_connection_cursors` | 原生连接差值采集游标 |
| `native_user_totals` | 原生连接观测累计账本 |
| `node_endpoints` | 客户端公网连接入口定义 |
| `node_group_members` | 分组与节点的成员关系及顺序 |
| `node_groups` | 代理节点分组定义 |
| `node_profiles` | 可交付代理节点与来源映射 |
| `online_snapshots` | 实例实时在线数据快照 |
| `preferred_generated_nodes` | 优选批次生成地址到节点的去重映射 |
| `preferred_generation_requests` | 优选节点批次预览及生成幂等记录 |
| `preferred_sync_jobs` | 定时优选多源任务、配置及缓存状态 |
| `preferred_sync_runs` | 优选同步历史：触发方式、时间、来源脱敏统计、发布数量和状态 |
| `preferred_sync_slots` | 同步任务的稳定入口槽位和发布状态 |
| `provider_observations` | 流量来源观测及游标信息 |
| `remote_inbound_registry` | 远程入站登记与来源能力信息 |
| `remote_inventory_runs` | 远程资产清单采集运行记录 |
| `remote_runtime_users` | 远程核心运行时客户端的受控期望状态 |
| `schema_migrations` | 已执行迁移及时间，防止重复执行 |
| `servers` | 服务器资源及展示信息 |
| `settings` | 系统与业务设置 |
| `subscriber_credentials` | 订阅用户在入站上的认证绑定及计费属性 |
| `subscriber_group_grants` | 订阅用户对节点组的动态授权（010 新增） |
| `subscriber_node_edits` | 节点权限变更与重试任务 |
| `subscriber_node_grants` | 订阅用户逐节点明确授权 |
| `subscriber_node_requests` | 节点授权及认证配置的父任务 |
| `subscriber_node_revocations` | 订阅用户逐节点撤销记录 |
| `subscriber_node_tasks` | 按入站拆分的认证配置子任务 |
| `subscriber_provisioning` | 用户认证配置任务及加密期望状态 |
| `subscriber_quota_audit` | 订阅额度与权限变更审计 |
| `subscriber_unified_links` | 订阅用户与统一订阅的映射 |
| `subscriber_usage_sources` | 订阅用户计费来源、去重和冻结状态 |
| `subscribers` | 订阅用户、额度、到期与启用状态 |
| `subscription_access_logs` | 订阅请求的脱敏访问日志及来源 IP |
| `subscription_admin_audit` | 订阅管理操作审计 |
| `subscription_link_secrets` | 管理员可查看订阅令牌的加密密文及哈希 |
| `traffic_counter_observations` | 计数器原值、方向和增量观察 |
| `traffic_cursors` | 采集计数器游标及重置基线 |
| `traffic_daily` | 按日汇总的流量账本 |
| `traffic_direction_repairs` | 流量方向修复审计 |
| `traffic_hourly` | 按小时汇总的流量账本 |
| `traffic_provenance` | 流量来源、方向和粒度证据 |
| `traffic_samples` | 流量增量明细 |
| `users` | 采集侧统一用户资产 |
| `xray_clients` | Xray 客户端资产观察 |
| `xray_diagnostics` | Xray API 诊断历史 |
| `xray_runtime` | Xray 版本及运行证据 |
| `xray_user_states` | Xray 用户流量与在线能力状态 |

## archive_daily_ledger

归档前日级核对账本

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `date` | `TEXT` | 是 | — | 1 |
| `identity_id` | `INTEGER` | 是 | — | 2 |
| `upload_bytes` | `INTEGER` | 是 | — | — |
| `download_bytes` | `INTEGER` | 是 | — | — |

外键：无声明外键。

索引：`sqlite_autoindex_archive_daily_ledger_1`（唯一，`date`, `identity_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE archive_daily_ledger(date TEXT NOT NULL, identity_id INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, PRIMARY KEY(date,identity_id));
```

</details>

## archive_ledger

归档前小时级核对账本

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `hour_start` | `TEXT` | 是 | — | 1 |
| `identity_id` | `INTEGER` | 是 | — | 2 |
| `upload_bytes` | `INTEGER` | 是 | — | — |
| `download_bytes` | `INTEGER` | 是 | — | — |

外键：无声明外键。

索引：`sqlite_autoindex_archive_ledger_1`（唯一，`hour_start`, `identity_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE archive_ledger(hour_start TEXT NOT NULL, identity_id INTEGER NOT NULL, upload_bytes INTEGER NOT NULL, download_bytes INTEGER NOT NULL, PRIMARY KEY(hour_start,identity_id));
```

</details>

## archive_runs

归档任务记录

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `cutoff` | `TEXT` | 是 | — | — |
| `deleted` | `INTEGER` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：无声明外键。

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE archive_runs(id INTEGER PRIMARY KEY, cutoff TEXT NOT NULL, deleted INTEGER NOT NULL, created_at TEXT NOT NULL);
```

</details>

## archived_counter_evidence

归档后的计数器来源证据

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `identity_id` | `INTEGER` | 是 | — | 1 |
| `date` | `TEXT` | 是 | — | 2 |
| `provider_type` | `TEXT` | 是 | — | 3 |
| `instance_version` | `TEXT` | 是 | — | 4 |
| `config_revision` | `TEXT` | 是 | — | 5 |
| `traffic_direction` | `TEXT` | 是 | — | 6 |
| `source_upload_name` | `TEXT` | 是 | — | 7 |
| `source_download_name` | `TEXT` | 是 | — | 8 |
| `metric_scope` | `TEXT` | 是 | — | 9 |
| `capability_status` | `TEXT` | 是 | — | 10 |
| `mapped` | `INTEGER` | 是 | — | 11 |
| `coverage` | `TEXT` | 是 | — | 12 |
| `first_observed_at` | `TEXT` | 是 | — | — |
| `last_observed_at` | `TEXT` | 是 | — | — |
| `observations` | `INTEGER` | 是 | — | — |
| `upload_delta` | `INTEGER` | 是 | — | — |
| `download_delta` | `INTEGER` | 是 | — | — |

外键：`identity_id` → `identities.id`（删除 NO ACTION）

索引：`sqlite_autoindex_archived_counter_evidence_1`（唯一，`identity_id`, `date`, `provider_type`, `instance_version`, `config_revision`, `traffic_direction`, `source_upload_name`, `source_download_name`, `metric_scope`, `capability_status`, `mapped`, `coverage`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE archived_counter_evidence(
 identity_id INTEGER NOT NULL REFERENCES identities(id), date TEXT NOT NULL,
 provider_type TEXT NOT NULL, instance_version TEXT NOT NULL, config_revision TEXT NOT NULL,
 traffic_direction TEXT NOT NULL, source_upload_name TEXT NOT NULL, source_download_name TEXT NOT NULL,
 metric_scope TEXT NOT NULL, capability_status TEXT NOT NULL, mapped INTEGER NOT NULL, coverage TEXT NOT NULL,
 first_observed_at TEXT NOT NULL, last_observed_at TEXT NOT NULL, observations INTEGER NOT NULL,
 upload_delta INTEGER NOT NULL, download_delta INTEGER NOT NULL,
 PRIMARY KEY(identity_id,date,provider_type,instance_version,config_revision,traffic_direction,source_upload_name,source_download_name,metric_scope,capability_status,mapped,coverage));
```

</details>

## catalog_inbounds

持久化的入站身份、协议和来源状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `source_ref` | `TEXT` | 是 | — | — |
| `tag` | `TEXT` | 是 | — | — |
| `protocol` | `TEXT` | 是 | — | — |
| `listen_address` | `TEXT` | 是 | '' | — |
| `listen_port` | `INTEGER` | 是 | — | — |
| `profile_json` | `TEXT` | 是 | — | — |
| `structure_hash` | `TEXT` | 是 | — | — |
| `config_hash` | `TEXT` | 是 | — | — |
| `present` | `INTEGER` | 是 | 1 | — |
| `review` | `INTEGER` | 是 | 0 | — |
| `runtime_state` | `TEXT` | 是 | 'unknown' | — |
| `discovered_at` | `TEXT` | 是 | — | — |
| `authentication_type` | `TEXT` | 是 | 'unknown' | — |
| `source_capabilities_json` | `TEXT` | 是 | '{"configured_clients":"source_only","runtime_loaded":"unknown","dynamic_management":"unknown"}' | — |
| `display_name` | `TEXT` | 是 | '' | — |
| `listen_ports_json` | `TEXT` | 是 | '[]' | — |
| `retired` | `INTEGER` | 是 | 0 | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`inbound_tag_one_live`（唯一，`instance_id`, `tag`）；`sqlite_autoindex_catalog_inbounds_2`（唯一，`instance_id`, `tag`, `protocol`）；`sqlite_autoindex_catalog_inbounds_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE catalog_inbounds(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),source_ref TEXT NOT NULL,tag TEXT NOT NULL,protocol TEXT NOT NULL,listen_address TEXT NOT NULL DEFAULT '',listen_port INTEGER NOT NULL,profile_json TEXT NOT NULL,structure_hash TEXT NOT NULL,config_hash TEXT NOT NULL,present INTEGER NOT NULL DEFAULT 1,review INTEGER NOT NULL DEFAULT 0,runtime_state TEXT NOT NULL DEFAULT 'unknown',discovered_at TEXT NOT NULL, authentication_type TEXT NOT NULL DEFAULT 'unknown', source_capabilities_json TEXT NOT NULL DEFAULT '{"configured_clients":"source_only","runtime_loaded":"unknown","dynamic_management":"unknown"}', display_name TEXT NOT NULL DEFAULT '', listen_ports_json TEXT NOT NULL DEFAULT '[]', retired INTEGER NOT NULL DEFAULT 0,UNIQUE(instance_id,tag,protocol));
```

</details>

## catalog_subscription_groups

高级订阅直接引用的分组

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscription_id` | `TEXT` | 是 | — | 1 |
| `group_id` | `TEXT` | 是 | — | 2 |
| `sort_order` | `INTEGER` | 是 | — | — |

外键：`group_id` → `node_groups.id`（删除 NO ACTION）；`subscription_id` → `catalog_subscriptions.id`（删除 CASCADE）

索引：`sqlite_autoindex_catalog_subscription_groups_1`（唯一，`subscription_id`, `group_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE catalog_subscription_groups(subscription_id TEXT NOT NULL REFERENCES catalog_subscriptions(id) ON DELETE CASCADE,group_id TEXT NOT NULL REFERENCES node_groups(id),sort_order INTEGER NOT NULL,PRIMARY KEY(subscription_id,group_id));
```

</details>

## catalog_subscription_nodes

高级订阅直接包含的节点

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscription_id` | `TEXT` | 是 | — | 1 |
| `node_id` | `TEXT` | 是 | — | 2 |
| `sort_order` | `INTEGER` | 是 | — | — |
| `enabled` | `INTEGER` | 是 | 1 | — |

外键：`node_id` → `node_profiles.id`（删除 NO ACTION）；`subscription_id` → `catalog_subscriptions.id`（删除 CASCADE）

索引：`sqlite_autoindex_catalog_subscription_nodes_1`（唯一，`subscription_id`, `node_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE catalog_subscription_nodes(subscription_id TEXT NOT NULL REFERENCES catalog_subscriptions(id) ON DELETE CASCADE,node_id TEXT NOT NULL REFERENCES node_profiles(id),sort_order INTEGER NOT NULL,enabled INTEGER NOT NULL DEFAULT 1,PRIMARY KEY(subscription_id,node_id));
```

</details>

## catalog_subscriptions

订阅资源、Token 哈希、格式和状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `subscriber_id` | `TEXT` | 是 | — | — |
| `name` | `TEXT` | 是 | — | — |
| `token_hash` | `TEXT` | 是 | — | — |
| `token_version` | `INTEGER` | 是 | 1 | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `expires_at` | `TEXT` | 否 | — | — |
| `format` | `TEXT` | 是 | — | — |
| `options_json` | `TEXT` | 是 | '{}' | — |
| `last_access_at` | `TEXT` | 否 | — | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |

外键：`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_catalog_subscriptions_2`（唯一，`token_hash`）；`sqlite_autoindex_catalog_subscriptions_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE catalog_subscriptions(id TEXT PRIMARY KEY,subscriber_id TEXT NOT NULL REFERENCES subscribers(id),name TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,token_version INTEGER NOT NULL DEFAULT 1,enabled INTEGER NOT NULL DEFAULT 1,expires_at TEXT,format TEXT NOT NULL,options_json TEXT NOT NULL DEFAULT '{}',last_access_at TEXT,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
```

</details>

## catalog_sync_runs

节点目录同步运行记录

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 否 | — | — |
| `config_hash` | `TEXT` | 是 | '' | — |
| `status` | `TEXT` | 是 | — | — |
| `summary_json` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE catalog_sync_runs(id INTEGER PRIMARY KEY,instance_id INTEGER REFERENCES instances(id),config_hash TEXT NOT NULL DEFAULT '',status TEXT NOT NULL,summary_json TEXT NOT NULL,created_at TEXT NOT NULL);
```

</details>

## collection_batches

实例采集批次游标

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 否 | — | 1 |
| `batch_id` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE collection_batches(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), batch_id TEXT NOT NULL);
```

</details>

## collector_errors

采集错误历史

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `message` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE collector_errors(id INTEGER PRIMARY KEY, instance_id INTEGER NOT NULL REFERENCES instances(id), message TEXT NOT NULL, created_at TEXT NOT NULL);
```

</details>

## collector_health

Clients、Stats、Online 等采集健康状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `collector` | `TEXT` | 是 | — | 2 |
| `status` | `TEXT` | 是 | — | — |
| `result_count` | `INTEGER` | 是 | 0 | — |
| `checked_at` | `TEXT` | 是 | — | — |
| `success_at` | `TEXT` | 否 | — | — |
| `error_code` | `TEXT` | 是 | '' | — |
| `summary` | `TEXT` | 是 | '' | — |
| `failures` | `INTEGER` | 是 | 0 | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_collector_health_1`（唯一，`instance_id`, `collector`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE collector_health(instance_id INTEGER NOT NULL REFERENCES instances(id), collector TEXT NOT NULL, status TEXT NOT NULL, result_count INTEGER NOT NULL DEFAULT 0, checked_at TEXT NOT NULL, success_at TEXT, error_code TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', failures INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(instance_id,collector));
```

</details>

## config_operations

配置操作及执行状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `operation` | `TEXT` | 是 | — | — |
| `inbound_tag` | `TEXT` | 是 | — | — |
| `old_revision` | `TEXT` | 是 | — | — |
| `new_revision` | `TEXT` | 是 | — | — |
| `apply_status` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |
| `summary` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE config_operations(id INTEGER PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),operation TEXT NOT NULL,inbound_tag TEXT NOT NULL,old_revision TEXT NOT NULL,new_revision TEXT NOT NULL,apply_status TEXT NOT NULL,created_at TEXT NOT NULL,summary TEXT NOT NULL);
```

</details>

## core_asset_states

核心资产清单完整性及采集状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `inbound_tag` | `TEXT` | 是 | — | 2 |
| `email` | `TEXT` | 是 | — | 3 |
| `online_state` | `TEXT` | 是 | — | — |
| `online_count` | `INTEGER` | 否 | — | — |
| `online_kind` | `TEXT` | 是 | — | — |
| `online_basis` | `TEXT` | 是 | — | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_core_asset_states_1`（唯一，`instance_id`, `inbound_tag`, `email`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE core_asset_states(instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,email TEXT NOT NULL,online_state TEXT NOT NULL,online_count INTEGER,online_kind TEXT NOT NULL,online_basis TEXT NOT NULL,checked_at TEXT NOT NULL,PRIMARY KEY(instance_id,inbound_tag,email));
```

</details>

## core_clients

非 Xray 客户端资产观察

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `inbound_tag` | `TEXT` | 是 | — | 2 |
| `asset_key` | `TEXT` | 是 | — | 3 |
| `email` | `TEXT` | 是 | — | — |
| `protocol` | `TEXT` | 是 | '' | — |
| `level` | `INTEGER` | 是 | 0 | — |
| `source` | `TEXT` | 是 | — | 4 |
| `present` | `INTEGER` | 是 | 1 | — |
| `observed_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_core_clients_1`（唯一，`instance_id`, `inbound_tag`, `asset_key`, `source`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE core_clients(instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,asset_key TEXT NOT NULL,email TEXT NOT NULL,protocol TEXT NOT NULL DEFAULT '',level INTEGER NOT NULL DEFAULT 0,source TEXT NOT NULL,present INTEGER NOT NULL DEFAULT 1,observed_at TEXT NOT NULL,PRIMARY KEY(instance_id,inbound_tag,asset_key,source));
```

</details>

## core_connection_snapshots

核心连接观测快照

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `provider_type` | `TEXT` | 是 | — | 2 |
| `collected_at` | `TEXT` | 是 | — | — |
| `capability_status` | `TEXT` | 是 | — | — |
| `metric_scope` | `TEXT` | 是 | — | — |
| `connections_json` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_core_connection_snapshots_1`（唯一，`instance_id`, `provider_type`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE core_connection_snapshots(instance_id INTEGER NOT NULL REFERENCES instances(id),provider_type TEXT NOT NULL,collected_at TEXT NOT NULL,capability_status TEXT NOT NULL,metric_scope TEXT NOT NULL,connections_json TEXT NOT NULL,PRIMARY KEY(instance_id,provider_type));
```

</details>

## core_diagnostics

多核心 API 诊断历史

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `checked_at` | `TEXT` | 是 | — | — |
| `trigger` | `TEXT` | 是 | — | — |
| `report_json` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`core_diagnostics_instance_time`（非唯一，`instance_id`, `id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE core_diagnostics(id INTEGER PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),checked_at TEXT NOT NULL,trigger TEXT NOT NULL,report_json TEXT NOT NULL);
```

</details>

## core_runtime

多核心版本与运行信息

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 否 | — | 1 |
| `version` | `TEXT` | 是 | 'Unknown' | — |
| `build_info` | `TEXT` | 是 | 'Unknown' | — |
| `version_source` | `TEXT` | 是 | 'unavailable' | — |
| `verified_at` | `TEXT` | 否 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE core_runtime(instance_id INTEGER PRIMARY KEY REFERENCES instances(id),version TEXT NOT NULL DEFAULT 'Unknown',build_info TEXT NOT NULL DEFAULT 'Unknown',version_source TEXT NOT NULL DEFAULT 'unavailable',verified_at TEXT);
```

</details>

## core_user_states

非 Xray 用户统计及在线状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `email` | `TEXT` | 是 | — | 2 |
| `stats_state` | `TEXT` | 是 | 'unknown' | — |
| `online_state` | `TEXT` | 是 | 'unknown' | — |
| `online_count` | `INTEGER` | 否 | — | — |
| `online_basis` | `TEXT` | 是 | '' | — |
| `checked_at` | `TEXT` | 是 | — | — |
| `online_kind` | `TEXT` | 是 | '' | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_core_user_states_1`（唯一，`instance_id`, `email`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE core_user_states(instance_id INTEGER NOT NULL REFERENCES instances(id),email TEXT NOT NULL,stats_state TEXT NOT NULL DEFAULT 'unknown',online_state TEXT NOT NULL DEFAULT 'unknown',online_count INTEGER,online_basis TEXT NOT NULL DEFAULT '',checked_at TEXT NOT NULL,online_kind TEXT NOT NULL DEFAULT '',PRIMARY KEY(instance_id,email));
```

</details>

## credential_material

客户端认证材料及状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `inbound_tag` | `TEXT` | 是 | — | — |
| `core_client_ref` | `TEXT` | 是 | — | — |
| `user_id` | `INTEGER` | 否 | — | — |
| `auth_json` | `TEXT` | 是 | — | — |
| `status` | `TEXT` | 是 | 'available' | — |
| `source` | `TEXT` | 是 | 'manual_unverified' | — |
| `verified_at` | `TEXT` | 是 | — | — |

外键：`user_id` → `users.id`（删除 NO ACTION）；`instance_id` → `instances.id`（删除 NO ACTION）

索引：`credential_inbound`（非唯一，`instance_id`, `inbound_tag`, `core_client_ref`）；`sqlite_autoindex_credential_material_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE credential_material(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,core_client_ref TEXT NOT NULL,user_id INTEGER REFERENCES users(id),auth_json TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'available',source TEXT NOT NULL DEFAULT 'manual_unverified',verified_at TEXT NOT NULL);
```

</details>

## direction_audits

流量方向审计

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `created_at` | `TEXT` | 是 | — | — |
| `instance_id` | `INTEGER` | 是 | — | — |
| `affected_records` | `INTEGER` | 是 | — | — |
| `status` | `TEXT` | 是 | — | — |
| `backup_path` | `TEXT` | 是 | '' | — |

外键：无声明外键。

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE direction_audits(id INTEGER PRIMARY KEY,created_at TEXT NOT NULL,instance_id INTEGER NOT NULL,affected_records INTEGER NOT NULL,status TEXT NOT NULL,backup_path TEXT NOT NULL DEFAULT '');
```

</details>

## endpoint_binding_audit

连接绑定配置变化审计

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `binding_id` | `TEXT` | 否 | — | — |
| `action` | `TEXT` | 是 | — | — |
| `summary_json` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`binding_id` → `inbound_endpoint_bindings.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE endpoint_binding_audit(id INTEGER PRIMARY KEY,binding_id TEXT REFERENCES inbound_endpoint_bindings(id),action TEXT NOT NULL,summary_json TEXT NOT NULL,created_at TEXT NOT NULL);
```

</details>

## endpoint_connection_requests

连接入口创建请求的幂等记录

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `request_id` | `TEXT` | 否 | — | 1 |
| `request_hash` | `TEXT` | 是 | — | — |
| `binding_id` | `TEXT` | 是 | — | — |

外键：`binding_id` → `inbound_endpoint_bindings.id`（删除 NO ACTION）

索引：`sqlite_autoindex_endpoint_connection_requests_1`（唯一，`request_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE endpoint_connection_requests(request_id TEXT PRIMARY KEY,request_hash TEXT NOT NULL,binding_id TEXT NOT NULL REFERENCES inbound_endpoint_bindings(id));
```

</details>

## hysteria_auth_accounts

Hysteria2 HTTP 认证账号映射

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `inbound_id` | `TEXT` | 是 | — | 1 |
| `credential_id` | `TEXT` | 是 | — | — |
| `auth_hash` | `TEXT` | 是 | — | 2 |
| `origin` | `TEXT` | 是 | — | — |
| `match_mode` | `TEXT` | 是 | 'exact' | — |
| `last_success_at` | `TEXT` | 否 | — | — |

外键：`credential_id` → `credential_material.id`（删除 NO ACTION）；`inbound_id` → `hysteria_auth_backends.inbound_id`（删除 NO ACTION）

索引：`sqlite_autoindex_hysteria_auth_accounts_2`（唯一，`inbound_id`, `auth_hash`）；`sqlite_autoindex_hysteria_auth_accounts_1`（唯一，`credential_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE hysteria_auth_accounts (
 inbound_id TEXT NOT NULL REFERENCES hysteria_auth_backends(inbound_id),
 credential_id TEXT NOT NULL UNIQUE REFERENCES credential_material(id),
 auth_hash TEXT NOT NULL,
 origin TEXT NOT NULL CHECK(origin IN ('managed','native')),
 match_mode TEXT NOT NULL DEFAULT 'exact' CHECK(match_mode IN ('exact','userpass')),
 last_success_at TEXT,
 PRIMARY KEY(inbound_id,auth_hash)
);
```

</details>

## hysteria_auth_backends

Hysteria2 HTTP 认证后端绑定与确认

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `inbound_id` | `TEXT` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `token_hash` | `TEXT` | 是 | — | — |
| `allowed_cidrs` | `TEXT` | 是 | — | — |
| `target_endpoint` | `TEXT` | 是 | — | — |
| `target_tag` | `TEXT` | 是 | — | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `confirmed` | `INTEGER` | 是 | 0 | — |
| `revision` | `INTEGER` | 是 | 1 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `configured_at` | `TEXT` | 否 | — | — |
| `last_callback_at` | `TEXT` | 否 | — | — |
| `last_success_at` | `TEXT` | 否 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：`sqlite_autoindex_hysteria_auth_backends_3`（唯一，`token_hash`）；`sqlite_autoindex_hysteria_auth_backends_2`（唯一，`instance_id`）；`sqlite_autoindex_hysteria_auth_backends_1`（唯一，`inbound_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE hysteria_auth_backends (
 inbound_id TEXT PRIMARY KEY REFERENCES catalog_inbounds(id),
 instance_id INTEGER NOT NULL UNIQUE REFERENCES instances(id),
 token_hash TEXT NOT NULL UNIQUE,
 allowed_cidrs TEXT NOT NULL,
 target_endpoint TEXT NOT NULL,
 target_tag TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1,
 confirmed INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 configured_at TEXT,
 last_callback_at TEXT,
 last_success_at TEXT
);
```

</details>

## identities

核心用户、入站或实例到用户资产的映射

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `user_id` | `INTEGER` | 否 | — | — |
| `instance_id` | `INTEGER` | 是 | — | — |
| `inbound_tag` | `TEXT` | 是 | '' | — |
| `core_user_key` | `TEXT` | 是 | — | — |
| `scope` | `TEXT` | 是 | — | — |
| `last_active_at` | `TEXT` | 否 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）；`user_id` → `users.id`（删除 NO ACTION）

索引：`sqlite_autoindex_identities_1`（唯一，`instance_id`, `inbound_tag`, `core_user_key`, `scope`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE identities(id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), instance_id INTEGER NOT NULL REFERENCES instances(id), inbound_tag TEXT NOT NULL DEFAULT '', core_user_key TEXT NOT NULL, scope TEXT NOT NULL CHECK(scope IN ('user','inbound','instance')), last_active_at TEXT, UNIQUE(instance_id,inbound_tag,core_user_key,scope));
```

</details>

## inbound_client_keys

稳定入站与客户端身份映射

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `inbound_id` | `TEXT` | 是 | — | 1 |
| `client_ref` | `TEXT` | 是 | — | 2 |
| `credential_id` | `TEXT` | 是 | — | — |

外键：`credential_id` → `credential_material.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：`sqlite_autoindex_inbound_client_keys_2`（唯一，`inbound_id`, `client_ref`）；`sqlite_autoindex_inbound_client_keys_1`（唯一，`credential_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE inbound_client_keys(inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),client_ref TEXT NOT NULL,credential_id TEXT NOT NULL UNIQUE REFERENCES credential_material(id) DEFERRABLE INITIALLY DEFERRED,PRIMARY KEY(inbound_id,client_ref));
```

</details>

## inbound_endpoint_bindings

入站与连接入口的绑定及覆盖

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `inbound_id` | `TEXT` | 是 | — | — |
| `endpoint_id` | `TEXT` | 是 | — | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `sort_order` | `INTEGER` | 是 | 0 | — |
| `overrides_json` | `TEXT` | 是 | '{}' | — |
| `validation_status` | `TEXT` | 是 | 'ready' | — |
| `validated_config_hash` | `TEXT` | 是 | '' | — |
| `revision` | `INTEGER` | 是 | 1 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |

外键：`endpoint_id` → `node_endpoints.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：`sqlite_autoindex_inbound_endpoint_bindings_2`（唯一，`inbound_id`, `endpoint_id`）；`sqlite_autoindex_inbound_endpoint_bindings_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE inbound_endpoint_bindings(
 id TEXT PRIMARY KEY,
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 endpoint_id TEXT NOT NULL REFERENCES node_endpoints(id),
 enabled INTEGER NOT NULL DEFAULT 1,
 sort_order INTEGER NOT NULL DEFAULT 0,
 overrides_json TEXT NOT NULL DEFAULT '{}',
 validation_status TEXT NOT NULL DEFAULT 'ready',
 validated_config_hash TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 UNIQUE(inbound_id,endpoint_id)
);
```

</details>

## inbound_identity_aliases

入站稳定身份别名

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `source_locator` | `TEXT` | 是 | — | 2 |
| `tag` | `TEXT` | 是 | — | 3 |
| `protocol` | `TEXT` | 是 | — | 4 |
| `inbound_id` | `TEXT` | 是 | — | — |
| `first_seen_at` | `TEXT` | 是 | — | — |
| `last_seen_at` | `TEXT` | 是 | — | — |

外键：`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）；`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_inbound_identity_aliases_1`（唯一，`instance_id`, `source_locator`, `tag`, `protocol`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE inbound_identity_aliases(instance_id INTEGER NOT NULL REFERENCES instances(id),source_locator TEXT NOT NULL,tag TEXT NOT NULL,protocol TEXT NOT NULL,inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),first_seen_at TEXT NOT NULL,last_seen_at TEXT NOT NULL,PRIMARY KEY(instance_id,source_locator,tag,protocol));
```

</details>

## inbound_identity_history

入站身份及来源变化历史

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `inbound_id` | `TEXT` | 是 | — | — |
| `instance_id` | `INTEGER` | 是 | — | — |
| `old_tag` | `TEXT` | 是 | — | — |
| `new_tag` | `TEXT` | 是 | — | — |
| `old_source` | `TEXT` | 是 | — | — |
| `new_source` | `TEXT` | 是 | — | — |
| `config_hash` | `TEXT` | 是 | — | — |
| `method` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE inbound_identity_history(id INTEGER PRIMARY KEY,inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),instance_id INTEGER NOT NULL REFERENCES instances(id),old_tag TEXT NOT NULL,new_tag TEXT NOT NULL,old_source TEXT NOT NULL,new_source TEXT NOT NULL,config_hash TEXT NOT NULL,method TEXT NOT NULL,created_at TEXT NOT NULL);
```

</details>

## inbound_identity_pending

等待确认的入站身份变更

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `source_locator` | `TEXT` | 是 | — | — |
| `tag` | `TEXT` | 是 | — | — |
| `protocol` | `TEXT` | 是 | — | — |
| `profile_json` | `TEXT` | 是 | — | — |
| `listen_address` | `TEXT` | 是 | — | — |
| `candidates_json` | `TEXT` | 是 | — | — |
| `config_hash` | `TEXT` | 是 | — | — |
| `status` | `TEXT` | 是 | 'needs_review' | — |
| `resolved_inbound_id` | `TEXT` | 否 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`resolved_inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）；`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_inbound_identity_pending_2`（唯一，`instance_id`, `source_locator`, `tag`, `protocol`）；`sqlite_autoindex_inbound_identity_pending_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE inbound_identity_pending(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),source_locator TEXT NOT NULL,tag TEXT NOT NULL,protocol TEXT NOT NULL,profile_json TEXT NOT NULL,listen_address TEXT NOT NULL,candidates_json TEXT NOT NULL,config_hash TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'needs_review',resolved_inbound_id TEXT REFERENCES catalog_inbounds(id),created_at TEXT NOT NULL,UNIQUE(instance_id,source_locator,tag,protocol));
```

</details>

## inbound_merge_audit

入站归并审计

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `primary_id` | `TEXT` | 是 | — | — |
| `secondary_id` | `TEXT` | 是 | — | — |
| `backup_path` | `TEXT` | 是 | — | — |
| `summary_json` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_inbound_merge_audit_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE inbound_merge_audit(id TEXT PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),primary_id TEXT NOT NULL,secondary_id TEXT NOT NULL,backup_path TEXT NOT NULL,summary_json TEXT NOT NULL,created_at TEXT NOT NULL);
```

</details>

## inbound_retirements

入站退役记录

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `inbound_id` | `TEXT` | 否 | — | 1 |
| `canonical_id` | `TEXT` | 否 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`canonical_id` → `catalog_inbounds.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：`sqlite_autoindex_inbound_retirements_1`（唯一，`inbound_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE inbound_retirements(inbound_id TEXT PRIMARY KEY REFERENCES catalog_inbounds(id),canonical_id TEXT REFERENCES catalog_inbounds(id),created_at TEXT NOT NULL);
```

</details>

## instance_epochs

核心启动代次估计

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 否 | — | 1 |
| `epoch_id` | `TEXT` | 是 | — | — |
| `boot_estimate` | `TEXT` | 是 | — | — |
| `observed_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE instance_epochs(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), epoch_id TEXT NOT NULL, boot_estimate TEXT NOT NULL, observed_at TEXT NOT NULL);
```

</details>

## instances

核心实例接入地址、类型、启停与能力

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `server_id` | `INTEGER` | 是 | — | — |
| `name` | `TEXT` | 是 | — | — |
| `core_type` | `TEXT` | 是 | — | — |
| `api_endpoint` | `TEXT` | 是 | — | — |
| `api_secret` | `TEXT` | 是 | '' | — |
| `config_path` | `TEXT` | 是 | '' | — |
| `version` | `TEXT` | 是 | '' | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `last_collected_at` | `TEXT` | 否 | — | — |
| `last_error` | `TEXT` | 是 | '' | — |
| `capabilities_json` | `TEXT` | 是 | '[]' | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |
| `control_endpoint` | `TEXT` | 是 | '' | — |
| `detected_version` | `TEXT` | 是 | '' | — |
| `clash_endpoint` | `TEXT` | 是 | '' | — |
| `clash_secret` | `TEXT` | 是 | '' | — |

外键：`server_id` → `servers.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE instances(id INTEGER PRIMARY KEY, server_id INTEGER NOT NULL REFERENCES servers(id), name TEXT NOT NULL, core_type TEXT NOT NULL CHECK(core_type IN ('xray','v2fly','singbox','hysteria2')), api_endpoint TEXT NOT NULL, api_secret TEXT NOT NULL DEFAULT '', config_path TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, last_collected_at TEXT, last_error TEXT NOT NULL DEFAULT '', capabilities_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, control_endpoint TEXT NOT NULL DEFAULT '', detected_version TEXT NOT NULL DEFAULT '', clash_endpoint TEXT NOT NULL DEFAULT '', clash_secret TEXT NOT NULL DEFAULT '');
```

</details>

## native_connection_cursors

原生连接差值采集游标

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `epoch_id` | `TEXT` | 是 | — | 2 |
| `connection_id` | `TEXT` | 是 | — | 3 |
| `inbound_tag` | `TEXT` | 是 | — | — |
| `user_key` | `TEXT` | 是 | — | — |
| `raw_upload` | `INTEGER` | 是 | — | — |
| `raw_download` | `INTEGER` | 是 | — | — |
| `observed_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_native_connection_cursors_1`（唯一，`instance_id`, `epoch_id`, `connection_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE native_connection_cursors(instance_id INTEGER NOT NULL REFERENCES instances(id),epoch_id TEXT NOT NULL,connection_id TEXT NOT NULL,inbound_tag TEXT NOT NULL,user_key TEXT NOT NULL,raw_upload INTEGER NOT NULL,raw_download INTEGER NOT NULL,observed_at TEXT NOT NULL,PRIMARY KEY(instance_id,epoch_id,connection_id));
```

</details>

## native_user_totals

原生连接观测累计账本

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `inbound_tag` | `TEXT` | 是 | — | 2 |
| `user_key` | `TEXT` | 是 | — | 3 |
| `upload` | `INTEGER` | 是 | — | — |
| `download` | `INTEGER` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_native_user_totals_1`（唯一，`instance_id`, `inbound_tag`, `user_key`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE native_user_totals(instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_tag TEXT NOT NULL,user_key TEXT NOT NULL,upload INTEGER NOT NULL,download INTEGER NOT NULL,PRIMARY KEY(instance_id,inbound_tag,user_key));
```

</details>

## node_endpoints

客户端公网连接入口定义

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `name` | `TEXT` | 是 | — | — |
| `profile_json` | `TEXT` | 是 | — | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `sort_order` | `INTEGER` | 是 | 0 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |
| `description` | `TEXT` | 是 | '' | — |
| `endpoint_type` | `TEXT` | 是 | 'custom' | — |

外键：无声明外键。

索引：`sqlite_autoindex_node_endpoints_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE node_endpoints(id TEXT PRIMARY KEY,name TEXT NOT NULL,profile_json TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 1,sort_order INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', endpoint_type TEXT NOT NULL DEFAULT 'custom');
```

</details>

## node_group_members

分组与节点的成员关系及顺序

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `group_id` | `TEXT` | 是 | — | 1 |
| `node_id` | `TEXT` | 是 | — | 2 |
| `sort_order` | `INTEGER` | 是 | — | — |

外键：`node_id` → `node_profiles.id`（删除 NO ACTION）；`group_id` → `node_groups.id`（删除 CASCADE）

索引：`sqlite_autoindex_node_group_members_1`（唯一，`group_id`, `node_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE node_group_members(group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,node_id TEXT NOT NULL REFERENCES node_profiles(id),sort_order INTEGER NOT NULL,PRIMARY KEY(group_id,node_id));
```

</details>

## node_groups

代理节点分组定义

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `name` | `TEXT` | 是 | — | — |
| `description` | `TEXT` | 是 | '' | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `sort_order` | `INTEGER` | 是 | 0 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |

外键：无声明外键。

索引：`sqlite_autoindex_node_groups_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE node_groups(id TEXT PRIMARY KEY,name TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,sort_order INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
```

</details>

## node_profiles

可交付代理节点与来源映射

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `inbound_id` | `TEXT` | 是 | — | — |
| `endpoint_id` | `TEXT` | 是 | — | — |
| `name` | `TEXT` | 是 | — | — |
| `description` | `TEXT` | 是 | '' | — |
| `source_type` | `TEXT` | 是 | — | — |
| `profile_json` | `TEXT` | 是 | '{}' | — |
| `overrides_json` | `TEXT` | 是 | '{}' | — |
| `tags_json` | `TEXT` | 是 | '[]' | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `sort_order` | `INTEGER` | 是 | 0 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |
| `binding_id` | `TEXT` | 是 | — | — |
| `variant_key` | `TEXT` | 是 | 'default' | — |

外键：`binding_id` → `inbound_endpoint_bindings.id`（删除 NO ACTION）；`endpoint_id` → `node_endpoints.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：`node_binding_variant`（唯一，`binding_id`, `variant_key`）；`node_profiles_inbound`（非唯一，`inbound_id`）；`sqlite_autoindex_node_profiles_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE node_profiles(id TEXT PRIMARY KEY,inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),endpoint_id TEXT NOT NULL REFERENCES node_endpoints(id),name TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',source_type TEXT NOT NULL CHECK(source_type IN ('discovered')),profile_json TEXT NOT NULL DEFAULT '{}',overrides_json TEXT NOT NULL DEFAULT '{}',tags_json TEXT NOT NULL DEFAULT '[]',enabled INTEGER NOT NULL DEFAULT 1,sort_order INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL, binding_id TEXT NOT NULL REFERENCES inbound_endpoint_bindings(id), variant_key TEXT NOT NULL DEFAULT 'default');
```

</details>

## online_snapshots

实例实时在线数据快照

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 否 | — | 1 |
| `records_json` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE online_snapshots(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), records_json TEXT NOT NULL, updated_at TEXT NOT NULL);
```

</details>

## preferred_generated_nodes

优选批次生成地址到节点的去重映射

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `template_node_id` | `TEXT` | 是 | — | 1 |
| `fingerprint` | `TEXT` | 是 | — | 2 |
| `node_id` | `TEXT` | 是 | — | — |

外键：`node_id` → `node_profiles.id`（删除 CASCADE）；`template_node_id` → `node_profiles.id`（删除 CASCADE）

索引：`sqlite_autoindex_preferred_generated_nodes_2`（唯一，`template_node_id`, `fingerprint`）；`sqlite_autoindex_preferred_generated_nodes_1`（唯一，`node_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE preferred_generated_nodes(template_node_id TEXT NOT NULL REFERENCES node_profiles(id) ON DELETE CASCADE,fingerprint TEXT NOT NULL,node_id TEXT NOT NULL UNIQUE REFERENCES node_profiles(id) ON DELETE CASCADE,PRIMARY KEY(template_node_id,fingerprint));
```

</details>

## preferred_generation_requests

优选节点批次预览及生成幂等记录

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `request_id` | `TEXT` | 否 | — | 1 |
| `request_hash` | `TEXT` | 是 | — | — |
| `result_json` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：无声明外键。

索引：`sqlite_autoindex_preferred_generation_requests_1`（唯一，`request_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE preferred_generation_requests(request_id TEXT PRIMARY KEY,request_hash TEXT NOT NULL,result_json TEXT NOT NULL,created_at TEXT NOT NULL);
```

</details>

## preferred_sync_jobs

定时优选多源任务、配置及缓存状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `name` | `TEXT` | 是 | — | — |
| `template_node_id` | `TEXT` | 是 | — | — |
| `group_id` | `TEXT` | 是 | — | — |
| `config_json` | `TEXT` | 是 | — | — |
| `state_json` | `TEXT` | 是 | — | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `revision` | `INTEGER` | 是 | 1 | — |
| `next_run` | `INTEGER` | 是 | — | — |
| `last_attempt` | `TEXT` | 是 | — | — |
| `last_success` | `TEXT` | 是 | — | — |
| `status` | `TEXT` | 是 | — | — |

外键：`group_id` → `node_groups.id`（删除 NO ACTION）；`template_node_id` → `node_profiles.id`（删除 NO ACTION）

索引：`preferred_sync_due`（非唯一，`enabled`, `next_run`）；`sqlite_autoindex_preferred_sync_jobs_3`（唯一，`group_id`）；`sqlite_autoindex_preferred_sync_jobs_2`（唯一，`name`）；`sqlite_autoindex_preferred_sync_jobs_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE preferred_sync_jobs(
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 template_node_id TEXT NOT NULL REFERENCES node_profiles(id),
 group_id TEXT NOT NULL UNIQUE REFERENCES node_groups(id),
 config_json TEXT NOT NULL,
 state_json TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1,
 revision INTEGER NOT NULL DEFAULT 1,
 next_run INTEGER NOT NULL,
 last_attempt TEXT NOT NULL,
 last_success TEXT NOT NULL,
 status TEXT NOT NULL
);
```

</details>

## preferred_sync_runs

优选同步历史：触发方式、时间、来源脱敏统计、发布数量和状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `job_id` | `TEXT` | 是 | — | — |
| `trigger` | `TEXT` | 是 | — | — |
| `started_at` | `TEXT` | 是 | — | — |
| `finished_at` | `TEXT` | 是 | — | — |
| `duration_ms` | `INTEGER` | 是 | — | — |
| `status` | `TEXT` | 是 | — | — |
| `candidate_count` | `INTEGER` | 是 | — | — |
| `active_count` | `INTEGER` | 是 | — | — |
| `slot_count` | `INTEGER` | 是 | — | — |
| `limit_value` | `INTEGER` | 是 | — | — |
| `sources_json` | `TEXT` | 是 | — | — |
| `error_code` | `TEXT` | 是 | '' | — |

外键：`job_id` → `preferred_sync_jobs.id`（删除 CASCADE）

索引：`preferred_sync_runs_job_time`（非唯一，`job_id`, `id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE preferred_sync_runs (
 id INTEGER PRIMARY KEY,
 job_id TEXT NOT NULL REFERENCES preferred_sync_jobs(id) ON DELETE CASCADE,
 trigger TEXT NOT NULL CHECK(trigger IN ('initial','manual','scheduled')),
 started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL,
 duration_ms INTEGER NOT NULL CHECK(duration_ms>=0),
 status TEXT NOT NULL,
 candidate_count INTEGER NOT NULL,
 active_count INTEGER NOT NULL,
 slot_count INTEGER NOT NULL,
 limit_value INTEGER NOT NULL,
 sources_json TEXT NOT NULL,
 error_code TEXT NOT NULL DEFAULT ''
);
```

</details>

## preferred_sync_slots

同步任务的稳定入口槽位和发布状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `job_id` | `TEXT` | 是 | — | 1 |
| `position` | `INTEGER` | 是 | — | 2 |
| `node_id` | `TEXT` | 是 | — | — |
| `endpoint_id` | `TEXT` | 是 | — | — |
| `binding_id` | `TEXT` | 是 | — | — |
| `expected_revision` | `TEXT` | 是 | — | — |
| `expected_profile` | `TEXT` | 是 | — | — |
| `active` | `INTEGER` | 是 | 1 | — |

外键：`binding_id` → `inbound_endpoint_bindings.id`（删除 NO ACTION）；`endpoint_id` → `node_endpoints.id`（删除 NO ACTION）；`node_id` → `node_profiles.id`（删除 NO ACTION）；`job_id` → `preferred_sync_jobs.id`（删除 NO ACTION）

索引：`sqlite_autoindex_preferred_sync_slots_4`（唯一，`job_id`, `position`）；`sqlite_autoindex_preferred_sync_slots_3`（唯一，`binding_id`）；`sqlite_autoindex_preferred_sync_slots_2`（唯一，`endpoint_id`）；`sqlite_autoindex_preferred_sync_slots_1`（唯一，`node_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE preferred_sync_slots(
 job_id TEXT NOT NULL REFERENCES preferred_sync_jobs(id),
 position INTEGER NOT NULL,
 node_id TEXT NOT NULL UNIQUE REFERENCES node_profiles(id),
 endpoint_id TEXT NOT NULL UNIQUE REFERENCES node_endpoints(id),
 binding_id TEXT NOT NULL UNIQUE REFERENCES inbound_endpoint_bindings(id),
 expected_revision TEXT NOT NULL,
 expected_profile TEXT NOT NULL,
 active INTEGER NOT NULL DEFAULT 1,
 PRIMARY KEY(job_id,position)
);
```

</details>

## provider_observations

流量来源观测及游标信息

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `provider_type` | `TEXT` | 是 | — | 2 |
| `metric_scope` | `TEXT` | 是 | — | 3 |
| `capability_status` | `TEXT` | 是 | — | — |
| `collected_at` | `TEXT` | 是 | — | — |
| `instance_version` | `TEXT` | 是 | — | — |
| `config_revision` | `TEXT` | 是 | — | — |
| `summary_json` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_provider_observations_1`（唯一，`instance_id`, `provider_type`, `metric_scope`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE provider_observations(instance_id INTEGER NOT NULL REFERENCES instances(id),provider_type TEXT NOT NULL,metric_scope TEXT NOT NULL,capability_status TEXT NOT NULL,collected_at TEXT NOT NULL,instance_version TEXT NOT NULL,config_revision TEXT NOT NULL,summary_json TEXT NOT NULL,PRIMARY KEY(instance_id,provider_type,metric_scope));
```

</details>

## remote_inbound_registry

远程入站登记与来源能力信息

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `inbound_id` | `TEXT` | 否 | — | 1 |
| `source_type` | `TEXT` | 是 | — | — |
| `verification` | `TEXT` | 是 | — | — |
| `manual_profile_json` | `TEXT` | 是 | '{}' | — |
| `revision` | `INTEGER` | 是 | 1 | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：`sqlite_autoindex_remote_inbound_registry_1`（唯一，`inbound_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE remote_inbound_registry(inbound_id TEXT PRIMARY KEY REFERENCES catalog_inbounds(id),source_type TEXT NOT NULL CHECK(source_type IN ('native_api','manual','agent','optional_config')),verification TEXT NOT NULL,manual_profile_json TEXT NOT NULL DEFAULT '{}',revision INTEGER NOT NULL DEFAULT 1,checked_at TEXT NOT NULL);
```

</details>

## remote_inventory_runs

远程资产清单采集运行记录

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `status` | `TEXT` | 是 | — | — |
| `summary` | `TEXT` | 是 | — | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE remote_inventory_runs(id INTEGER PRIMARY KEY,instance_id INTEGER NOT NULL REFERENCES instances(id),status TEXT NOT NULL,summary TEXT NOT NULL,checked_at TEXT NOT NULL);
```

</details>

## remote_runtime_users

远程核心运行时客户端的受控期望状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `inbound_id` | `TEXT` | 是 | — | 1 |
| `client_ref` | `TEXT` | 是 | — | 2 |
| `desired_sealed` | `TEXT` | 是 | — | — |
| `desired_enabled` | `INTEGER` | 是 | — | — |
| `restore_authorized` | `INTEGER` | 是 | 0 | — |
| `api_endpoint` | `TEXT` | 是 | — | — |
| `core_tag` | `TEXT` | 是 | — | — |
| `protocol` | `TEXT` | 是 | — | — |
| `revision` | `INTEGER` | 是 | 1 | — |
| `status` | `TEXT` | 是 | — | — |
| `persistence` | `TEXT` | 是 | 'runtime_only' | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）

索引：`sqlite_autoindex_remote_runtime_users_1`（唯一，`inbound_id`, `client_ref`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE remote_runtime_users(inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),client_ref TEXT NOT NULL,desired_sealed TEXT NOT NULL,desired_enabled INTEGER NOT NULL,restore_authorized INTEGER NOT NULL DEFAULT 0,api_endpoint TEXT NOT NULL,core_tag TEXT NOT NULL,protocol TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1,status TEXT NOT NULL,persistence TEXT NOT NULL DEFAULT 'runtime_only',checked_at TEXT NOT NULL,PRIMARY KEY(inbound_id,client_ref));
```

</details>

## schema_migrations

已执行迁移及时间，防止重复执行

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `version` | `TEXT` | 否 | — | 1 |
| `applied_at` | `TEXT` | 是 | — | — |

外键：无声明外键。

索引：`sqlite_autoindex_schema_migrations_1`（唯一，`version`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL);
```

</details>

## servers

服务器资源及展示信息

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `name` | `TEXT` | 是 | — | — |
| `address` | `TEXT` | 是 | — | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |

外键：无声明外键。

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE servers(id INTEGER PRIMARY KEY, name TEXT NOT NULL, address TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
```

</details>

## settings

系统与业务设置

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `key` | `TEXT` | 否 | — | 1 |
| `value` | `TEXT` | 是 | — | — |

外键：无声明外键。

索引：`sqlite_autoindex_settings_1`（唯一，`key`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

</details>

## subscriber_credentials

订阅用户在入站上的认证绑定及计费属性

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `subscriber_id` | `TEXT` | 是 | — | — |
| `inbound_id` | `TEXT` | 是 | — | — |
| `credential_id` | `TEXT` | 是 | — | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `billable` | `INTEGER` | 是 | 1 | — |
| `usage_mode` | `TEXT` | 是 | 'exclusive' | — |

外键：`credential_id` → `credential_material.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）；`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_credentials_2`（唯一，`subscriber_id`, `inbound_id`）；`sqlite_autoindex_subscriber_credentials_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_credentials(id TEXT PRIMARY KEY,subscriber_id TEXT NOT NULL REFERENCES subscribers(id),inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),credential_id TEXT NOT NULL REFERENCES credential_material(id),enabled INTEGER NOT NULL DEFAULT 1,created_at TEXT NOT NULL,billable INTEGER NOT NULL DEFAULT 1, usage_mode TEXT NOT NULL DEFAULT 'exclusive' CHECK(usage_mode IN ('exclusive','shared_native')),UNIQUE(subscriber_id,inbound_id));
```

</details>

## subscriber_group_grants

订阅用户对节点组的动态授权（010 新增）

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscriber_id` | `TEXT` | 是 | — | 1 |
| `group_id` | `TEXT` | 是 | — | 2 |
| `created_at` | `TEXT` | 是 | — | — |

外键：`group_id` → `node_groups.id`（删除 CASCADE）；`subscriber_id` → `subscribers.id`（删除 CASCADE）

索引：`subscriber_group_grants_group`（非唯一，`group_id`, `subscriber_id`）；`sqlite_autoindex_subscriber_group_grants_1`（唯一，`subscriber_id`, `group_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_group_grants (
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 created_at TEXT NOT NULL,
 PRIMARY KEY(subscriber_id,group_id)
);
```

</details>

## subscriber_node_edits

节点权限变更与重试任务

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `request_id` | `TEXT` | 否 | — | 1 |
| `request_hash` | `TEXT` | 是 | — | — |
| `subscriber_id` | `TEXT` | 是 | — | — |
| `addition_json` | `TEXT` | 是 | — | — |
| `status` | `TEXT` | 是 | — | — |
| `message` | `TEXT` | 是 | — | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_node_edits_1`（唯一，`request_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_node_edits (
 request_id TEXT PRIMARY KEY,
 request_hash TEXT NOT NULL,
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 addition_json TEXT NOT NULL,
 status TEXT NOT NULL,
 message TEXT NOT NULL,
 checked_at TEXT NOT NULL
);
```

</details>

## subscriber_node_grants

订阅用户逐节点明确授权

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscriber_id` | `TEXT` | 是 | — | 1 |
| `node_id` | `TEXT` | 是 | — | 2 |
| `inbound_id` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）；`node_id` → `node_profiles.id`（删除 NO ACTION）；`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_node_grants_1`（唯一，`subscriber_id`, `node_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_node_grants (
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 node_id TEXT NOT NULL REFERENCES node_profiles(id),
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(subscriber_id,node_id)
);
```

</details>

## subscriber_node_requests

节点授权及认证配置的父任务

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `request_id` | `TEXT` | 否 | — | 1 |
| `request_hash` | `TEXT` | 是 | — | — |
| `subscriber_id` | `TEXT` | 是 | — | — |
| `plan_json` | `TEXT` | 是 | — | — |
| `requested_enabled` | `INTEGER` | 是 | — | — |
| `activation_revision` | `INTEGER` | 是 | — | — |
| `status` | `TEXT` | 是 | 'pending' | — |
| `message` | `TEXT` | 是 | '' | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_node_requests_1`（唯一，`request_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_node_requests (
 request_id TEXT PRIMARY KEY,
 request_hash TEXT NOT NULL,
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 plan_json TEXT NOT NULL,
 requested_enabled INTEGER NOT NULL,
 activation_revision INTEGER NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending',
 message TEXT NOT NULL DEFAULT '',
 checked_at TEXT NOT NULL
);
```

</details>

## subscriber_node_revocations

订阅用户逐节点撤销记录

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscriber_id` | `TEXT` | 是 | — | 1 |
| `node_id` | `TEXT` | 是 | — | 2 |
| `created_at` | `TEXT` | 是 | — | — |

外键：`node_id` → `node_profiles.id`（删除 NO ACTION）；`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_node_revocations_1`（唯一，`subscriber_id`, `node_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_node_revocations (
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 node_id TEXT NOT NULL REFERENCES node_profiles(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(subscriber_id,node_id)
);
```

</details>

## subscriber_node_tasks

按入站拆分的认证配置子任务

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `request_id` | `TEXT` | 是 | — | 1 |
| `child_request_id` | `TEXT` | 是 | — | — |
| `inbound_id` | `TEXT` | 是 | — | 2 |
| `status` | `TEXT` | 是 | 'pending' | — |
| `message` | `TEXT` | 是 | '' | — |
| `checked_at` | `TEXT` | 是 | '' | — |

外键：`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）；`request_id` → `subscriber_node_requests.request_id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_node_tasks_2`（唯一，`request_id`, `inbound_id`）；`sqlite_autoindex_subscriber_node_tasks_1`（唯一，`child_request_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_node_tasks (
 request_id TEXT NOT NULL REFERENCES subscriber_node_requests(request_id),
 child_request_id TEXT NOT NULL UNIQUE,
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 status TEXT NOT NULL DEFAULT 'pending',
 message TEXT NOT NULL DEFAULT '',
 checked_at TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(request_id,inbound_id)
);
```

</details>

## subscriber_provisioning

用户认证配置任务及加密期望状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `request_id` | `TEXT` | 否 | — | 1 |
| `request_hash` | `TEXT` | 是 | — | — |
| `subscriber_id` | `TEXT` | 是 | — | — |
| `inbound_id` | `TEXT` | 是 | — | — |
| `client_ref` | `TEXT` | 是 | — | — |
| `target_endpoint` | `TEXT` | 是 | — | — |
| `target_tag` | `TEXT` | 是 | — | — |
| `target_protocol` | `TEXT` | 是 | — | — |
| `origin` | `TEXT` | 是 | — | — |
| `credential_id` | `TEXT` | 否 | — | — |
| `desired_sealed` | `TEXT` | 是 | '' | — |
| `status` | `TEXT` | 是 | — | — |
| `message` | `TEXT` | 是 | '' | — |
| `requested_enabled` | `INTEGER` | 是 | 1 | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`credential_id` → `credential_material.id`（删除 NO ACTION）；`inbound_id` → `catalog_inbounds.id`（删除 NO ACTION）；`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_provisioning_2`（唯一，`subscriber_id`, `inbound_id`）；`sqlite_autoindex_subscriber_provisioning_1`（唯一，`request_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_provisioning (
 request_id TEXT PRIMARY KEY,
 request_hash TEXT NOT NULL,
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
 inbound_id TEXT NOT NULL REFERENCES catalog_inbounds(id),
 client_ref TEXT NOT NULL,
 target_endpoint TEXT NOT NULL,
 target_tag TEXT NOT NULL,
 target_protocol TEXT NOT NULL,
 origin TEXT NOT NULL CHECK(origin IN ('managed','native')),
 credential_id TEXT REFERENCES credential_material(id),
 desired_sealed TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL,
 message TEXT NOT NULL DEFAULT '',
 requested_enabled INTEGER NOT NULL DEFAULT 1,
 checked_at TEXT NOT NULL,
 UNIQUE(subscriber_id,inbound_id)
);
```

</details>

## subscriber_quota_audit

订阅额度与权限变更审计

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `subscriber_id` | `TEXT` | 是 | — | — |
| `action` | `TEXT` | 是 | — | — |
| `before_json` | `TEXT` | 是 | — | — |
| `after_json` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`subscriber_id` → `subscribers.id`（删除 CASCADE）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_quota_audit(id INTEGER PRIMARY KEY,subscriber_id TEXT NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,action TEXT NOT NULL,before_json TEXT NOT NULL,after_json TEXT NOT NULL,created_at TEXT NOT NULL);
```

</details>

## subscriber_unified_links

订阅用户与统一订阅的映射

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscriber_id` | `TEXT` | 否 | — | 1 |
| `subscription_id` | `TEXT` | 是 | — | — |

外键：`subscription_id` → `catalog_subscriptions.id`（删除 CASCADE）；`subscriber_id` → `subscribers.id`（删除 NO ACTION）

索引：`sqlite_autoindex_subscriber_unified_links_2`（唯一，`subscription_id`）；`sqlite_autoindex_subscriber_unified_links_1`（唯一，`subscriber_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_unified_links (
 subscriber_id TEXT PRIMARY KEY REFERENCES subscribers(id),
 subscription_id TEXT NOT NULL UNIQUE REFERENCES catalog_subscriptions(id) ON DELETE CASCADE
);
```

</details>

## subscriber_usage_sources

订阅用户计费来源、去重和冻结状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscriber_id` | `TEXT` | 是 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | 2 |
| `inbound_id` | `TEXT` | 是 | '' | 3 |
| `client_ref` | `TEXT` | 是 | — | 4 |
| `baseline_upload` | `INTEGER` | 是 | 0 | — |
| `baseline_download` | `INTEGER` | 是 | 0 | — |
| `baseline_known` | `INTEGER` | 是 | 0 | — |
| `active` | `INTEGER` | 是 | 1 | — |
| `frozen_upload` | `INTEGER` | 否 | — | — |
| `frozen_download` | `INTEGER` | 否 | — | — |
| `frozen_state` | `TEXT` | 是 | '' | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）；`subscriber_id` → `subscribers.id`（删除 CASCADE）

索引：`meter_single_owner`（唯一，`instance_id`, `inbound_id`, `client_ref`）；`sqlite_autoindex_subscriber_usage_sources_1`（唯一，`subscriber_id`, `instance_id`, `inbound_id`, `client_ref`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscriber_usage_sources(subscriber_id TEXT NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,instance_id INTEGER NOT NULL REFERENCES instances(id),inbound_id TEXT NOT NULL DEFAULT '',client_ref TEXT NOT NULL,baseline_upload INTEGER NOT NULL DEFAULT 0,baseline_download INTEGER NOT NULL DEFAULT 0,baseline_known INTEGER NOT NULL DEFAULT 0,active INTEGER NOT NULL DEFAULT 1,frozen_upload INTEGER,frozen_download INTEGER,frozen_state TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,PRIMARY KEY(subscriber_id,instance_id,inbound_id,client_ref));
```

</details>

## subscribers

订阅用户、额度、到期与启用状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `TEXT` | 否 | — | 1 |
| `name` | `TEXT` | 是 | — | — |
| `external_ref` | `TEXT` | 是 | '' | — |
| `enabled` | `INTEGER` | 是 | 1 | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |
| `traffic_limit_bytes` | `INTEGER` | 是 | 0 | — |
| `expires_at` | `TEXT` | 否 | — | — |
| `activated_at` | `TEXT` | 否 | — | — |
| `quota_enabled` | `INTEGER` | 是 | 0 | — |
| `expiry_enabled` | `INTEGER` | 是 | 0 | — |
| `quota_revision` | `INTEGER` | 是 | 1 | — |
| `quota_initialized` | `INTEGER` | 是 | 0 | — |
| `quota_reset_at` | `TEXT` | 否 | — | — |
| `quota_policy` | `TEXT` | 是 | 'report_partial' | — |
| `node_authorization_required` | `INTEGER` | 是 | 0 | — |

外键：无声明外键。

索引：`sqlite_autoindex_subscribers_1`（唯一，`id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscribers(id TEXT PRIMARY KEY,name TEXT NOT NULL,external_ref TEXT NOT NULL DEFAULT '',enabled INTEGER NOT NULL DEFAULT 1,created_at TEXT NOT NULL,updated_at TEXT NOT NULL, traffic_limit_bytes INTEGER NOT NULL DEFAULT 0 CHECK(traffic_limit_bytes>=0), expires_at TEXT, activated_at TEXT, quota_enabled INTEGER NOT NULL DEFAULT 0, expiry_enabled INTEGER NOT NULL DEFAULT 0, quota_revision INTEGER NOT NULL DEFAULT 1, quota_initialized INTEGER NOT NULL DEFAULT 0, quota_reset_at TEXT, quota_policy TEXT NOT NULL DEFAULT 'report_partial', node_authorization_required INTEGER NOT NULL DEFAULT 0);
```

</details>

## subscription_access_logs

订阅请求的脱敏访问日志及来源 IP

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `subscription_id` | `TEXT` | 否 | — | — |
| `format` | `TEXT` | 是 | — | — |
| `accessed_at` | `TEXT` | 是 | — | — |
| `status_code` | `INTEGER` | 是 | — | — |
| `error_code` | `TEXT` | 是 | — | — |
| `user_agent` | `TEXT` | 是 | — | — |
| `client_ip_hash` | `TEXT` | 是 | — | — |
| `client_ip` | `TEXT` | 是 | '' | — |
| `peer_ip` | `TEXT` | 是 | '' | — |
| `ip_source` | `TEXT` | 是 | 'legacy_hash_only' | — |

外键：`subscription_id` → `catalog_subscriptions.id`（删除 SET NULL）

索引：`subscription_access_time`（非唯一，`accessed_at`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscription_access_logs(id INTEGER PRIMARY KEY,subscription_id TEXT REFERENCES catalog_subscriptions(id) ON DELETE SET NULL,format TEXT NOT NULL,accessed_at TEXT NOT NULL,status_code INTEGER NOT NULL,error_code TEXT NOT NULL,user_agent TEXT NOT NULL,client_ip_hash TEXT NOT NULL, client_ip TEXT NOT NULL DEFAULT '', peer_ip TEXT NOT NULL DEFAULT '', ip_source TEXT NOT NULL DEFAULT 'legacy_hash_only');
```

</details>

## subscription_admin_audit

订阅管理操作审计

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `subscription_id` | `TEXT` | 否 | — | — |
| `subscriber_id` | `TEXT` | 否 | — | — |
| `action` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`subscriber_id` → `subscribers.id`（删除 NO ACTION）；`subscription_id` → `catalog_subscriptions.id`（删除 SET NULL）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscription_admin_audit (
 id INTEGER PRIMARY KEY,
 subscription_id TEXT REFERENCES catalog_subscriptions(id) ON DELETE SET NULL,
 subscriber_id TEXT REFERENCES subscribers(id),
 action TEXT NOT NULL,
 created_at TEXT NOT NULL
);
```

</details>

## subscription_link_secrets

管理员可查看订阅令牌的加密密文及哈希

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `subscription_id` | `TEXT` | 否 | — | 1 |
| `token_hash` | `TEXT` | 是 | — | — |
| `token_sealed` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`subscription_id` → `catalog_subscriptions.id`（删除 CASCADE）

索引：`sqlite_autoindex_subscription_link_secrets_2`（唯一，`token_hash`）；`sqlite_autoindex_subscription_link_secrets_1`（唯一，`subscription_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE subscription_link_secrets (
 subscription_id TEXT PRIMARY KEY REFERENCES catalog_subscriptions(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE,
 token_sealed TEXT NOT NULL,
 created_at TEXT NOT NULL
);
```

</details>

## traffic_counter_observations

计数器原值、方向和增量观察

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `identity_id` | `INTEGER` | 是 | — | 1 |
| `collected_at` | `TEXT` | 是 | — | 2 |
| `provider_type` | `TEXT` | 是 | — | — |
| `metric_scope` | `TEXT` | 是 | — | — |
| `capability_status` | `TEXT` | 是 | — | — |
| `instance_version` | `TEXT` | 是 | — | — |
| `config_revision` | `TEXT` | 是 | — | — |
| `epoch_id` | `TEXT` | 是 | — | — |
| `source_upload_name` | `TEXT` | 是 | — | — |
| `source_download_name` | `TEXT` | 是 | — | — |
| `raw_upload` | `INTEGER` | 是 | — | — |
| `raw_download` | `INTEGER` | 是 | — | — |
| `traffic_direction` | `TEXT` | 是 | — | — |
| `mapped` | `INTEGER` | 是 | — | — |
| `coverage` | `TEXT` | 是 | — | — |
| `upload_delta` | `INTEGER` | 是 | — | — |
| `download_delta` | `INTEGER` | 是 | — | — |
| `date` | `TEXT` | 是 | — | — |

外键：`identity_id` → `identities.id`（删除 NO ACTION）

索引：`counter_observation_retention`（非唯一，`collected_at`）；`sqlite_autoindex_traffic_counter_observations_1`（唯一，`identity_id`, `collected_at`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE traffic_counter_observations(
 identity_id INTEGER NOT NULL REFERENCES identities(id), collected_at TEXT NOT NULL,
 provider_type TEXT NOT NULL, metric_scope TEXT NOT NULL, capability_status TEXT NOT NULL,
 instance_version TEXT NOT NULL, config_revision TEXT NOT NULL, epoch_id TEXT NOT NULL,
 source_upload_name TEXT NOT NULL, source_download_name TEXT NOT NULL,
 raw_upload INTEGER NOT NULL, raw_download INTEGER NOT NULL,
 traffic_direction TEXT NOT NULL, mapped INTEGER NOT NULL, coverage TEXT NOT NULL,
 upload_delta INTEGER NOT NULL, download_delta INTEGER NOT NULL, date TEXT NOT NULL,
 PRIMARY KEY(identity_id,collected_at));
```

</details>

## traffic_cursors

采集计数器游标及重置基线

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `identity_id` | `INTEGER` | 否 | — | 1 |
| `raw_upload` | `INTEGER` | 是 | — | — |
| `raw_download` | `INTEGER` | 是 | — | — |
| `epoch_id` | `TEXT` | 是 | '' | — |
| `updated_at` | `TEXT` | 是 | — | — |
| `provider_type` | `TEXT` | 是 | '' | — |
| `instance_version` | `TEXT` | 是 | '' | — |

外键：`identity_id` → `identities.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE traffic_cursors(identity_id INTEGER PRIMARY KEY REFERENCES identities(id), raw_upload INTEGER NOT NULL CHECK(raw_upload>=0), raw_download INTEGER NOT NULL CHECK(raw_download>=0), epoch_id TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, provider_type TEXT NOT NULL DEFAULT '', instance_version TEXT NOT NULL DEFAULT '');
```

</details>

## traffic_daily

按日汇总的流量账本

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `date` | `TEXT` | 是 | — | 1 |
| `identity_id` | `INTEGER` | 是 | — | 2 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `upload_bytes` | `INTEGER` | 是 | — | — |
| `download_bytes` | `INTEGER` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）；`identity_id` → `identities.id`（删除 NO ACTION）

索引：`daily_instance_date`（非唯一，`instance_id`, `date`）；`sqlite_autoindex_traffic_daily_1`（唯一，`date`, `identity_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE traffic_daily(date TEXT NOT NULL, identity_id INTEGER NOT NULL REFERENCES identities(id), instance_id INTEGER NOT NULL REFERENCES instances(id), upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0), PRIMARY KEY(date,identity_id));
```

</details>

## traffic_direction_repairs

流量方向修复审计

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `source_profile` | `TEXT` | 是 | — | 2 |
| `cutoff` | `TEXT` | 是 | — | 3 |
| `affected_records` | `INTEGER` | 是 | — | — |
| `backup_path` | `TEXT` | 是 | — | — |
| `evidence` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_traffic_direction_repairs_1`（唯一，`instance_id`, `source_profile`, `cutoff`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE traffic_direction_repairs(instance_id INTEGER NOT NULL REFERENCES instances(id),source_profile TEXT NOT NULL,cutoff TEXT NOT NULL,affected_records INTEGER NOT NULL,backup_path TEXT NOT NULL,evidence TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(instance_id,source_profile,cutoff));
```

</details>

## traffic_hourly

按小时汇总的流量账本

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `hour_start` | `TEXT` | 是 | — | 1 |
| `identity_id` | `INTEGER` | 是 | — | 2 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `upload_bytes` | `INTEGER` | 是 | — | — |
| `download_bytes` | `INTEGER` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）；`identity_id` → `identities.id`（删除 NO ACTION）

索引：`sqlite_autoindex_traffic_hourly_1`（唯一，`hour_start`, `identity_id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE traffic_hourly(hour_start TEXT NOT NULL, identity_id INTEGER NOT NULL REFERENCES identities(id), instance_id INTEGER NOT NULL REFERENCES instances(id), upload_bytes INTEGER NOT NULL CHECK(upload_bytes>=0), download_bytes INTEGER NOT NULL CHECK(download_bytes>=0), PRIMARY KEY(hour_start,identity_id));
```

</details>

## traffic_provenance

流量来源、方向和粒度证据

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `identity_id` | `INTEGER` | 否 | — | 1 |
| `provider_type` | `TEXT` | 是 | — | — |
| `metric_scope` | `TEXT` | 是 | — | — |
| `capability_status` | `TEXT` | 是 | — | — |
| `collected_at` | `TEXT` | 是 | — | — |
| `instance_version` | `TEXT` | 是 | — | — |
| `config_revision` | `TEXT` | 是 | — | — |
| `source_upload_name` | `TEXT` | 是 | — | — |
| `source_download_name` | `TEXT` | 是 | — | — |
| `raw_upload` | `INTEGER` | 是 | — | — |
| `raw_download` | `INTEGER` | 是 | — | — |
| `traffic_direction` | `TEXT` | 是 | — | — |
| `mapped` | `INTEGER` | 是 | — | — |
| `coverage` | `TEXT` | 是 | — | — |

外键：`identity_id` → `identities.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE traffic_provenance(identity_id INTEGER PRIMARY KEY REFERENCES identities(id), provider_type TEXT NOT NULL,metric_scope TEXT NOT NULL,capability_status TEXT NOT NULL,collected_at TEXT NOT NULL,instance_version TEXT NOT NULL,config_revision TEXT NOT NULL,source_upload_name TEXT NOT NULL,source_download_name TEXT NOT NULL,raw_upload INTEGER NOT NULL,raw_download INTEGER NOT NULL,traffic_direction TEXT NOT NULL,mapped INTEGER NOT NULL,coverage TEXT NOT NULL);
```

</details>

## traffic_samples

流量增量明细

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `identity_id` | `INTEGER` | 是 | — | — |
| `instance_id` | `INTEGER` | 是 | — | — |
| `inbound_tag` | `TEXT` | 是 | — | — |
| `upload_delta` | `INTEGER` | 是 | — | — |
| `download_delta` | `INTEGER` | 是 | — | — |
| `collected_at` | `TEXT` | 是 | — | — |
| `hour_start` | `TEXT` | 是 | — | — |
| `date` | `TEXT` | 是 | — | — |
| `batch_id` | `TEXT` | 是 | '' | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）；`identity_id` → `identities.id`（删除 NO ACTION）

索引：`samples_batch`（非唯一，`instance_id`, `batch_id`）；`samples_identity_time`（非唯一，`identity_id`, `collected_at`）；`samples_time`（非唯一，`collected_at`）；`sqlite_autoindex_traffic_samples_1`（唯一，`identity_id`, `collected_at`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE traffic_samples(id INTEGER PRIMARY KEY, identity_id INTEGER NOT NULL REFERENCES identities(id), instance_id INTEGER NOT NULL REFERENCES instances(id), inbound_tag TEXT NOT NULL, upload_delta INTEGER NOT NULL CHECK(upload_delta>=0), download_delta INTEGER NOT NULL CHECK(download_delta>=0), collected_at TEXT NOT NULL, hour_start TEXT NOT NULL, date TEXT NOT NULL, batch_id TEXT NOT NULL DEFAULT '', UNIQUE(identity_id,collected_at));
```

</details>

## users

采集侧统一用户资产

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `display_name` | `TEXT` | 是 | — | — |
| `created_at` | `TEXT` | 是 | — | — |
| `updated_at` | `TEXT` | 是 | — | — |

外键：无声明外键。

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE users(id INTEGER PRIMARY KEY, display_name TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
```

</details>

## xray_clients

Xray 客户端资产观察

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `inbound_tag` | `TEXT` | 是 | — | 2 |
| `asset_key` | `TEXT` | 是 | — | 3 |
| `email` | `TEXT` | 是 | — | — |
| `protocol` | `TEXT` | 是 | '' | — |
| `level` | `INTEGER` | 是 | 0 | — |
| `source` | `TEXT` | 是 | — | 4 |
| `present` | `INTEGER` | 是 | 1 | — |
| `observed_at` | `TEXT` | 是 | — | — |
| `credential_fingerprint` | `TEXT` | 是 | '' | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_xray_clients_1`（唯一，`instance_id`, `inbound_tag`, `asset_key`, `source`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE xray_clients(instance_id INTEGER NOT NULL REFERENCES instances(id), inbound_tag TEXT NOT NULL, asset_key TEXT NOT NULL, email TEXT NOT NULL, protocol TEXT NOT NULL DEFAULT '', level INTEGER NOT NULL DEFAULT 0, source TEXT NOT NULL, present INTEGER NOT NULL DEFAULT 1, observed_at TEXT NOT NULL, credential_fingerprint TEXT NOT NULL DEFAULT '', PRIMARY KEY(instance_id,inbound_tag,asset_key,source));
```

</details>

## xray_diagnostics

Xray API 诊断历史

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `id` | `INTEGER` | 否 | — | 1 |
| `instance_id` | `INTEGER` | 是 | — | — |
| `checked_at` | `TEXT` | 是 | — | — |
| `trigger` | `TEXT` | 是 | — | — |
| `report_json` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`xray_diagnostics_instance_time`（非唯一，`instance_id`, `id`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE xray_diagnostics(id INTEGER PRIMARY KEY, instance_id INTEGER NOT NULL REFERENCES instances(id), checked_at TEXT NOT NULL, trigger TEXT NOT NULL, report_json TEXT NOT NULL);
```

</details>

## xray_runtime

Xray 版本及运行证据

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 否 | — | 1 |
| `version` | `TEXT` | 是 | 'Unknown' | — |
| `build_info` | `TEXT` | 是 | 'Unknown' | — |
| `version_source` | `TEXT` | 是 | 'unavailable' | — |
| `verified_at` | `TEXT` | 否 | — | — |
| `evidence_json` | `TEXT` | 是 | '{}' | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：无。

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE xray_runtime(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), version TEXT NOT NULL DEFAULT 'Unknown', build_info TEXT NOT NULL DEFAULT 'Unknown', version_source TEXT NOT NULL DEFAULT 'unavailable', verified_at TEXT, evidence_json TEXT NOT NULL DEFAULT '{}');
```

</details>

## xray_user_states

Xray 用户流量与在线能力状态

| 字段 | 类型 | NOT NULL | 默认值 | 主键位置 |
|---|---|---:|---|---:|
| `instance_id` | `INTEGER` | 是 | — | 1 |
| `email` | `TEXT` | 是 | — | 2 |
| `stats_state` | `TEXT` | 是 | 'unknown' | — |
| `online_state` | `TEXT` | 是 | 'unknown' | — |
| `online_count` | `INTEGER` | 否 | — | — |
| `online_basis` | `TEXT` | 是 | '' | — |
| `checked_at` | `TEXT` | 是 | — | — |

外键：`instance_id` → `instances.id`（删除 NO ACTION）

索引：`sqlite_autoindex_xray_user_states_1`（唯一，`instance_id`, `email`）

<details><summary>SQLite 建表语句</summary>

```sql
CREATE TABLE xray_user_states(instance_id INTEGER NOT NULL REFERENCES instances(id), email TEXT NOT NULL, stats_state TEXT NOT NULL DEFAULT 'unknown', online_state TEXT NOT NULL DEFAULT 'unknown', online_count INTEGER, online_basis TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL, PRIMARY KEY(instance_id,email));
```

</details>
