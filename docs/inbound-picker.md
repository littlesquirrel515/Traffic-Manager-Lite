# 连接入口的入站数据来源

入站管理与 Hosts 表单统一读取 `GET /api/v1/catalog/inbound-management`。该管理员接口只查询 SQLite 中的 `catalog_inbounds`、实例/服务器与引用信息；不调用核心 API，不触发自动发现或健康探测。下拉选项和连接绑定使用完整的持久化 `inbound_id`，不是 Tag、名称或列表下标。

数据库登记与核心实时观测分别维护。来源同步只能更新观察状态；失败不能删除已登记对象。真实来源缺失、协议变更待确认、管理员退役会影响能否新建发布入口，但对象仍保留。后台在保存事务中再次校验状态，避免表单打开后同步状态改变造成错误发布。

旧 Hosts 表单在构建选项前静默过滤 `present=0`、`review=1` 或 `retired=1`；筛选又通过隐藏 option 实现，没有数量和保留当前选择的提示。现改为默认展示全部已保存入站，不可用项显示原因并禁止新选；显示稳定 ID 前缀，筛选严格限制关联 Inbound 选项；当前选择不匹配时清空且要求重新选择，提供清空筛选和数据库/匹配数量。不会绕过后端有效性检查。

回归验证：核心接口故障时反复读取仍返回全部已保存入站及稳定 ID，远程调用次数为零；不可用对象不能新建发布入口。浏览器验证已退役项仍可见但禁选、跨协议或搜索筛选清空不匹配的当前选择、清空筛选恢复完整列表。浏览器仅使用临时数据库和 fixture API。

2026-10-16（Asia/Shanghai）已上线：`traffic-manager-lite:v2-inboundchoices-20261015-160943`，更新前一致性备份 `/data/traffic-manager-lite/backups/v2-inboundchoices-20261015-160943`。10 个已保存入站及其他既有资产保持，未清库，代理核心及 Nginx 未重启。后台 API 回归、Windows/Linux 构建、前端语法、新增下拉框专项和 54 个页面/尺寸浏览器检查通过；公网已返回新版脚本，TML healthy。

## XHTTP 与筛选修正（2026-10-16）

服务器、核心实例和协议只筛选已保存的入站，实例随服务器、协议随服务器/实例联动。保存只提交明确的稳定 inbound_id，不把筛选器当成新增服务器或核心配置。从入站详情打开或编辑既有绑定时固定原入站，并禁用筛选器，避免隐式换绑。

Xray v26.3.27 的 SplitHTTP Build 即使未提供 extra 也序列化默认范围、Xmux 和填充/路径参数。原四字段 wire 投影把这些字段识别为未知，造成正常 packet-up 入口无法保存。现使用覆盖 26.3.27—26.9.9 的 29 字段 wire 投影，识别官方默认值和服务端响应/缓冲设置；已有客户端白名单支持的 padding、noGRPCHeader 独立映射。真实未知字段、独立下载配置、自定义路由/混淆及尚未映射调优仍明确过滤；不输出原始配置或敏感 Header。

官方依据：[wire schema](https://github.com/XTLS/Xray-core/blob/v26.3.27/transport/internet/splithttp/config.proto)、[Build 默认值](https://github.com/XTLS/Xray-core/blob/v26.3.27/infra/conf/transport_internet.go)。隔离官方 Xray 进程从 ListInbounds 获取默认 XHTTP，并验证只设置四个外部参数能够建立 Ready 入口；未改代理配置文件。

线上核心实际为 Xray 26.9.9：新增 sessionIDTable/sessionIDLength 字段（空范围也会序列化），默认 Xmux 从 maxConcurrency=1 调整为 maxConnections=3。解析同时接受两个已核实版本的默认值；自定义 sessionID 生成配置仍明确过滤。[26.9.9 schema](https://github.com/XTLS/Xray-core/blob/v26.9.9/transport/internet/splithttp/config.proto)、[26.9.9 Build](https://github.com/XTLS/Xray-core/blob/v26.9.9/infra/conf/transport_method.go)。

最终上线验收（2026-10-16，Asia/Shanghai）：镜像 `traffic-manager-lite:v2-xhttp-20261016-004753`；一致性备份 `/data/traffic-manager-lite/backups/v2-xhttp-20261016-004753`。线上重新只读获取 ListInbounds 后，`vless-xhttp-packet` 的误判限制已清除，10 个入站 ID 不变。在线上数据库的隔离副本中，按实际 Address/443/Host/SNI 调用创建接口返回 Ready；不向生产库写入验证入口，也不调用核心增删接口。整体清单仍按其他未完整解析对象保留 Unknown，不把目标入站验证成功冒充全部核心资产完整性已验证。

本轮验证：Go 全量测试及 vet；两个官方 Xray 版本的隔离进程枚举/四参数创建（26.3.27 Windows、26.9.9 Linux）；额外版本默认值、未知字段、敏感 Header 和自定义参数过滤回归；浏览器筛选专项和 54 个页面尺寸检查；Windows/Linux 构建；生产健康/资产计数/数据库完整性与独立密钥备份。仅更新 TML，远程代理核心及 Nginx 启动时间不变。Ready 代表参数完整，未宣称完成真实客户端代理连通测试。
