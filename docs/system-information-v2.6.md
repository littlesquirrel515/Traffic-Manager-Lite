# v2.6.0 系统信息

系统导航新增“系统信息”。只读管理员接口为 `GET /api/v1/system/info`，使用现有管理员认证；不依赖数据库连接、采集器或核心 API，无迁移。页面打开且可见时每五秒刷新，支持暂停；服务端共享五秒采样缓存，首次 CPU 百分比为空，第二次采样后显示计数器差值。

## 采集与指标口径

- 原创采集模块 `pkg/systeminfo` 只依赖标准库及 MIT 许可的 gopsutil/v4，可在其他 Go 项目实例化 Collector，传入 Options 后调用 Snapshot(context)。HTTP、鉴权、配置及前端是独立适配层。
- 参考 1Panel 使用 gopsutil、aaPanel 使用 psutil 的做法，读取操作系统指标，不调用面板程序、不复制其代码。
- CPU 来自累计计数器差值。系统忙碌率扣除空闲、I/O 等待和虚拟机 Steal，不重复计算 guest。进程/容器 CPU 单核 100%，多核可超过 100%，另提供占可见配额比例。
- 系统内存已用 = Total − Available，区别于空闲 Free。缓存可回收，并非都属于不可用内存。
- Linux 同时读取 cgroup v1/v2 的内存、Swap、CPU 配额、CPU 限流和 OOM；在可见层级内取最严格父限额。工作集扣除 inactive_file，无法读取时为空，不伪装为零。宿主不可见的祖先限额不推断。
- TML 进程显示 RSS、虚拟内存、CPU、运行时间、线程、文件描述符、物理 I/O；Go 指标使用 runtime/metrics，展示堆对象、运行时管理内存、goroutine、GC 和 GOMAXPROCS。虚拟内存/Go 预留不等于物理内存占用。
- 磁盘使用 statfs 口径；可用是当前普通进程可用空间，包含系统保留块的影响。根目录、数据、备份可能位于同一文件系统，不可相加。数据库/WAL/SHM 仅 stat 文件长度，不查询或递归扫描数据。
- 网卡展示当前网络命名空间收发速率、累计字节、错误和丢包。设备 I/O 展示可见设备读写速率和 IOPS，分区与整盘不合计。速率需要两次有效采样，计数器回退时留空。
- Linux PSI 显示 CPU、内存和 I/O 的 `some avg10`，表示近 10 秒内至少一个任务因该资源等待的时间比例。TML 文件描述符同时展示当前数量和进程软限额。
- 指标缺失显示不可用/空值，部分失败仍返回其他有效指标。缓存和采样不写 SQLite，不生成历史统计。API 不输出环境变量、命令行、MAC 或认证材料。

## Docker 宿主元数据（可选）

默认容器保留网络命名空间隔离。需要宿主本地 IP/发行版时，在外层部署目录使用附加模板：

```sh
docker compose -f docker-compose.yml -f traffic-manager-lite/deploy/docker-compose.system-info.yml up -d --no-deps traffic-manager-lite
```

它只读挂载 `/proc/1/net/fib_trie`、`/proc/1/net/if_inet6` 和 `/etc/os-release` 三个文件；不挂 Docker Socket，不使用宿主 PID/网络命名空间，也不挂整个宿主 /proc 或根目录。网络文件明确锚定宿主 PID 1 所在的网络命名空间，不能使用随读取进程变化的 `/proc/net`。仅适用于存在这些文件的 Linux 宿主。

长期部署应固定使用同一 Compose 文件组合，也可将这三项只读挂载及两个环境变量合并进外层 Compose。修改环境或挂载后需重建 TML 容器。

## 公网 IP 与归属地

管理员打开系统信息页后，独立的 `pkg/ipgeo` 优先使用 `TML_SYSTEM_PUBLIC_IPS`（逗号分隔），其次使用可见宿主机的公网网卡地址。两者均缺失时，从 TML 服务端向 Cloudflare 发起 HTTPS 请求取得出口 IP 和国家或地区代码，Cloudflare 失败时使用 ipify 获取 IP。在 NAT、代理或多出口环境中，这个结果表示探测服务看到的出口。手工配置或网卡发现的地址可通过下面的本地库补充地区。

探测不阻塞系统资源 API。成功结果缓存一小时，失败五分钟后可重试，已有成功结果会带“上次成功”的状态提示。外部请求不会附带业务数据、凭据或浏览器 IP，可设置 `TML_SYSTEM_DISABLE_ONLINE_IP=true` 关闭。外部探测使用固定地址，不跟随重定向。

需要省州或城市时，可从 [DB-IP City Lite](https://db-ip.com/db/download/ip-to-city-lite) 或 [MaxMind GeoLite2](https://dev.maxmind.com/geoip/) 下载解压后的 Country/City MMDB。将文件只读挂载到容器内，例如 `/app/geoip/geoip.mmdb`，在 `.env` 配置 `TML_SYSTEM_GEOIP_DB=/app/geoip/geoip.mmdb`。只需要国家或地区可用体积更小的 [DB-IP Country Lite](https://db-ip.com/db/download/ip-to-country-lite)。数据库单独管理，无需随镜像分发。数据库替换请使用原子重命名，下一次查询刷新时生效。使用 DB-IP 时页面提供来源链接。GeoLite2 下载需要免费账户。

## 参考

- [1Panel dashboard 源码](https://github.com/1Panel-dev/1Panel/blob/dev-v2/agent/app/service/dashboard.go)
- [aaPanel system 源码](https://github.com/aaPanel/aaPanel/blob/master/class/system.py)
- [gopsutil](https://github.com/shirou/gopsutil)
- [Docker 容器资源指标](https://docs.docker.com/engine/containers/runmetrics/)
- [Linux cgroup v2](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)
