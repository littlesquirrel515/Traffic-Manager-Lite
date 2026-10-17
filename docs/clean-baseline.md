# TML v2：全新数据基线与生产重建

这是 breaking release。入口仅执行 `migrations/001_v2_initial.sql`，包含干净的现用领域和成熟流量/观测存储结构，不执行 001–017 旧升级链。旧 SQL 仅留作 docs/history 审计资料，不嵌入可执行迁移。新库使用 application_id=1414351922、user_version=20000、settings.baseline=tml-v2，并校验规范化初始 SQL 的 SHA256，拒绝不同私有测试版基线混用。

非空旧库或其他应用库会在修改 journal mode、迁移表和设置之前被拒绝。不会自动迁移、自动重建或删除旧数据。不要直接用 v2 镜像替换仍挂载旧库的生产服务。

## 准备工具

```sh
tml-baseline -source /data/traffic-manager-lite/data/traffic.db \
  -output /data/traffic-manager-lite/backups/v2-review-YYYYMMDD-HHMMSS \
  -timezone Asia/Shanghai
```

output 必须不存在，父目录必须已存在。工具只读打开源库；要求 TML 所有权标记或旧迁移/设置/领域表证据，并拒绝未知应用表、未知 application_id/version、非普通文件。它创建 SQLite 一致性备份、执行完整 integrity_check、统计备份中全部表记录、创建空白 v2 候选库、输出两个文件 SHA256 和 manifest。所有权检查仍须结合部署挂载确认它是 TML 独占数据库；任何不确定性都不得执行替换。

工具没有“执行 reset”开关，`reset_executed=false`。计数来自一致性备份，生产采集仍可能继续增长；批准后的最终停服备份需更新，不能将预备快照冒充最后一笔生产数据。

## 受影响范围

TML 的服务器、实例、入站/客户端登记/Desired、凭据、连接入口/绑定/节点/分组、订阅用户/订阅/Token、流量明细/汇总/游标/归档账本/来源证据、配额、在线与连接快照、健康/诊断/操作历史、设置与迁移记录全部从新库开始。旧 URL/Token 不保留；旧统计不自动导入或重新解释。全局管理用户名/密码仍从部署环境加载，聚合时区随新库重新固定。

不删除任何远程 Xray、V2Fly、sing-box、Hysteria2 配置、入站、账号、计数器；不调用 RemoveUser、DeleteInbound、Kick、clear=1，不重启代理核心，也不删除 Docker 卷。

## 最终切换（本次不得提前执行）

按用户 v2 需求 §1.1 第 7 项，代码/测试/备份/清理范围准备完成后须再次明确确认。确认前保持生产 v1 服务、旧 DB 和远程核心不变。

确认后仅停止 TML，生成最后一致性备份并核对完整性/归属；保存当前镜像、源码、外层 .env/Compose、Desired 独立密钥及挂载清单；只替换已确认的 TML 数据库文件，不能将新 DB 与旧 -wal/-shm 混用。使用新库和 v2 镜像启动 TML，验证健康/空数据/新接入流程。旧数据库及 sidecar/镜像/部署文件保留在独立恢复目录，禁止删除远程数据。

源码也须使用新的干净目录，不能直接覆盖解压到旧目录导致旧 migrations/*.sql 残留。先确认代理/反代容器没有挂载源码目录，将旧源码整体保留至恢复目录，再安装已验证归档；不得移动或删除核心配置挂载。当前五个代理/反代容器与 TML 源码目录的挂载交集已核对为零。

回滚必须先停止 TML，再整体恢复匹配的旧 DB 和旧镜像/外层部署文件；不能让旧镜像读取 v2 数据库。SQLite 运行备份应使用 backup/VACUUM INTO，不能只复制正在写入的主文件。

## 当前执行状态

用户已明确确认清空 TML 自有数据并切换。2026-10-15 11:16 UTC 完成生产 v2 切换，旧订阅失效，业务资产及流量为空。最终停服备份位于 `/data/traffic-manager-lite/backups/v2-cutover-20261015-111644`；该目录保存匹配的旧数据库、源码和部署文件。远程核心与 Nginx 未重启、未改挂载；默认配置目录挂载已从 TML 移除。详细验证及中途回滚记录见 docs/v2-verification.md。
