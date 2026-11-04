# v2.2.2：订阅访问来源与原生账号复用

007 为追加迁移，不更改既有授权、Token、凭据和流量事实。

订阅访问日志增加来源 IP、直连代理 IP 和解析依据，只有已登录管理员可查看。UA 仍仅保留识别到的客户端类别/版本，不记录订阅 URL、Token 或任意请求头。旧记录只有哈希，显示“旧记录未保存”，不能反推真实地址；保留现有日志清理周期。

`TML_TRUSTED_PROXIES` 默认空，仅信任 socket。配置反向代理的准确 IP/CIDR 后，从右向左验证 X-Forwarded-For，遇到首个非可信来源停止；畸形或超长链退回 socket。CF-Connecting-IP 不由 TML 直接信任。Nginx 若位于 Cloudflare 后，应仅对官方 Cloudflare CIDR 启用 real_ip_header CF-Connecting-IP，再在两个 location（尤其独立的 /sub/）覆盖 X-Forwarded-For 为 $remote_addr。不得无条件信任公网请求的头。Nginx 代理地址变化需更新 TML 的信任配置；Cloudflare 网段变化需更新受控配置。依据：[Nginx realip](https://nginx.org/en/docs/http/ngx_http_realip_module.html)、[Cloudflare 请求头](https://developers.cloudflare.com/fundamentals/reference/http-headers/)、[官方网段](https://www.cloudflare.com/ips/)。记录的是请求进入信任边界时的公网出口 IP，不能穿透客户端自身的 VPN、NAT 或代理。

分配节点优先复用该用户已有认证，其次使用实际支持的独立动态配置；不支持动态配置时，只有入站下恰好存在一个可用的已登记原生账号，才在预览中提出“复用原生共享账号”。不新建、删除或恢复核心账号，不改核心配置；排除 TML 托管的他人私有账号。没有凭据或多个候选时不猜测，需先明确纳管账号。提交仍需确认计划；目标和候选变化会拒绝旧计划。

共享分发使用独立 usage_mode='shared_native' 的非计费绑定，保留既有账号所有者和 meter，不重复分配计费来源。共享账号的核心统计无法区分各订阅用户：涉及共享账号的用户标记 shared_native，不能显示精确剩余额度；原有账本不抹除或转移。require_complete 策略仍拒绝不完整统计，不为交付绕过管理员策略。撤销节点只影响后续订阅输出，不使已缓存的核心凭据失效。

Hosts 仍只创建连接定义和代理节点，不创建 Client；分组与同入站新增入口不会自动授权。原生资产登记仍不等于完整原生枚举或运行时验证。

对于已授权但没有凭据绑定的旧节点，保留选择并预览时会列出“补齐原生认证”；确认后补齐原生交付绑定，不要求先撤销再分配。已停用的绑定不会被自动恢复。

2026-10-30 上线镜像 `traffic-manager-lite:v2.2.2-20261030-151642`，一致性备份 `/data/traffic-manager-lite/backups/v2.2.2-upgrade-20261030-151642`。007 已应用，原有 5 个订阅、URL、用户权限、核心凭据及账本保留。按上线时实时权限验证，SG-5 的既有显式授权保留，未以历史状态覆盖用户最新选择。

Nginx 仅变更 TML server：信任当日核对的 22 个 Cloudflare 官方 IPv4/IPv6 网段，两个代理 location 覆盖 X-Forwarded-For；配置验证通过并平滑 reload。TML 仅信任当前 Nginx peer `172.18.0.3/32`。其他 Nginx 配置未变，代理核心未重启。Nginx 原配置与候选配置同目录备份在本次升级恢复目录中。

验证：`go test ./...`、`go vet ./...`、54 项响应式页面及 3 个表单尺寸检查通过。隔离生产库副本中创建临时订阅用户，确认 SG-3 原生共享交付成功，不重复 meter；真实生产仅做预览与既有订阅读取，未增加用户或节点授权。两个用户通过公网代理分别取得 v2rayN Base64 与 Clash Verge Mihomo 200 响应，节点与当前授权一致。4 次请求日志确认真实来源解析、Nginx peer、伪造 XFF 未采信；携带伪造 CF-Connecting-IP 的额外测试在 Cloudflare 边缘返回 403，未进入 TML 日志。
