# v2.6.0 系统信息

系统导航新增“系统信息”。只读管理员接口为 `GET /api/v1/system/info`，使用现有管理员认证；不依赖数据库连接、采集器或核心 API，无迁移。页面打开且可见时每五秒刷新，支持暂停；服务端共享五秒采样缓存，首次 CPU 百分比为空，第二次采样后显示计数器差值。

## 采集与指标口径

- 原创采集模块 `pkg/systeminfo` 只依赖标准库及 MIT 许可的 gopsutil/v4，可在其他 Go 项目实例化 Collector，传入 Options 后调用 Snapshot(context)。HTTP、鉴权、配置及前端是独立适配层。
- 参考 1Panel 使用 gopsutil、aaPanel 使用 psutil 的做法，读取操作系统指标，不调用面板程序、不复制其代码。
- CPU 来自累计计数器差值；系统忙碌率单列 I/O 等待，不重复计算 guest。进程/容器 CPU 单核 100%，多核可超过 100%；另提供占可见配额比例。
- 系统内存已用 = Total − Available，区别于空闲 Free。缓存可回收，并非都属于不可用内存。
- Linux 同时读取 cgroup v1/v2 的内存、Swap、CPU 配额、CPU 限流和 OOM；在可见层级内取最严格父限额。工作集扣除 inactive_file，无法读取时为空，不伪装为零。宿主不可见的祖先限额不推断。
- TML 进程显示 RSS、虚拟内存、CPU、运行时间、线程、文件描述符、物理 I/O；Go 指标使用 runtime/metrics，展示堆对象、运行时管理内存、goroutine、GC 和 GOMAXPROCS。虚拟内存/Go 预留不等于物理内存占用。
- 磁盘使用 statfs 口径；可用是当前普通进程可用空间，包含系统保留块的影响。根目录、数据、备份可能位于同一文件系统，不可相加。数据库/WAL/SHM 仅 stat 文件长度，不查询或递归扫描数据。
- 网络区分当前容器网卡与宿主本地地址。公网 NAT 地址不能靠容器内网地址推断，`TML_SYSTEM_PUBLIC_IPS` 可声明逗号分隔的公网 IP，界面明确标记来源。没有外部探测、云元数据请求或任意命令执行。
- 指标缺失显示不可用/空值，部分失败仍返回其他有效指标。缓存和采样不写 SQLite，不生成历史统计。API 不输出环境变量、命令行、MAC 或认证材料。

## Docker 宿主元数据（可选）

默认容器保留网络命名空间隔离。需要宿主本地 IP/发行版时，在外层部署目录使用附加模板：

```sh
docker compose -f docker-compose.yml -f traffic-manager-lite/deploy/docker-compose.system-info.yml up -d --no-deps traffic-manager-lite
```

它只读挂载 `/proc/1/net/fib_trie`、`/proc/1/net/if_inet6` 和 `/etc/os-release` 三个文件；不挂 Docker Socket，不使用宿主 PID/网络命名空间，也不挂整个宿主 /proc 或根目录。网络文件明确锚定宿主 PID 1 所在的网络命名空间，不能使用随读取进程变化的 `/proc/net`。仅适用于存在这些文件的 Linux 宿主。

可在 `.env` 配置 `TML_SYSTEM_PUBLIC_IPS=35.198.225.153`，经现有 env_file 注入。修改环境或挂载后必须重建 TML 容器，单纯 restart 不加载新配置。长期部署应固定使用同一 Compose 文件组合，也可将这三项只读挂载及两个环境变量合并进外层 Compose。

## 参考

- [1Panel dashboard 源码](https://github.com/1Panel-dev/1Panel/blob/dev-v2/agent/app/service/dashboard.go)
- [aaPanel system 源码](https://github.com/aaPanel/aaPanel/blob/master/class/system.py)
- [gopsutil](https://github.com/shirou/gopsutil)
- [Docker 容器资源指标](https://docs.docker.com/engine/containers/runmetrics/)
- [Linux cgroup v2](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)
