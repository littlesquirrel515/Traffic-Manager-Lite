'use strict';
titles.systeminfo = ['系统信息', '系统资源、容器限额与 TML 进程占用'];
let systemInfoRefreshing = false;
let systemInfoAutomatic = true;

const systemPercent = value => value == null ? '—' : value.toFixed(1) + '%';
const systemBytes = value => value == null ? '—' : fmt(value);
const systemNumber = value => value == null ? '—' : esc(value);

function systemDuration(seconds) {
  if (seconds == null) return '—';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor(seconds % 86400 / 3600);
  const minutes = Math.floor(seconds % 3600 / 60);
  return `${days ? days + ' 天 ' : ''}${hours} 小时 ${minutes} 分钟`;
}

function systemMeter(percent, label) {
  if (percent == null) return '<p class="muted">等待采样或指标不可用</p>';
  const width = Math.max(0, Math.min(100, percent));
  const severity = percent >= 90 ? 'critical' : percent >= 75 ? 'warning' : 'normal';
  return `<svg class="system-meter ${severity}" viewBox="0 0 100 5" role="img" aria-label="${esc(label)} ${systemPercent(percent)}"><rect class="system-meter-track" width="100" height="5" rx="2"/><rect class="system-meter-value" width="${width}" height="5" rx="2"/></svg>`;
}

function systemCard(title, value, description, percent) {
  return `<article class="card system-resource"><h2>${esc(title)}</h2><strong class="system-resource-value">${esc(value)}</strong><p class="muted">${esc(description)}</p>${systemMeter(percent, title)}</article>`;
}

function systemDetails(entries) {
  return `<dl class="system-details">${entries.map(([name, value]) => `<div><dt>${esc(name)}</dt><dd>${value}</dd></div>`).join('')}</dl>`;
}

function systemAddress(address) {
  const scopes = {private:'内网', global:'本地全局地址', link_local:'链路本地', loopback:'回环', other:'其他'};
  return `${esc(address.ip)} <small>${esc(scopes[address.scope] || address.scope)}</small>`;
}

views.systeminfo = async function() {
  systemInfoRefreshing = true;
  try {
    const info = await api('system/info');
    const memory = info.memory;
    const group = info.container;
    const process = info.process;
    const dataDisk = info.disks.find(disk => disk.name === 'data') || info.disks[0];
    const groupPercent = group.memory_current != null && group.memory_limit > 0 ? group.memory_current / group.memory_limit * 100 : null;
    const alerts = [];
    for (const disk of info.disks) {
      if (disk.status === 'available' && disk.total && disk.available / disk.total < 0.1) alerts.push(`${disk.path} 可用磁盘低于 10%`);
      if (disk.status === 'available' && disk.inodes_total && disk.inodes_free / disk.inodes_total < 0.1) alerts.push(`${disk.path} 可用 inode 低于 10%`);
    }
    if (memory?.total && memory.available / memory.total < 0.1) alerts.push('系统可用内存低于 10%');
    if (groupPercent >= 90) alerts.push('容器内存达到可见限额的 90%');
    if (group.throttle_delta > 0) alerts.push(`本次采样发生 ${group.throttle_delta} 次 CPU 限流`);
    if (group.oom_kills > 0) alerts.push(`资源组累计发生 ${group.oom_kills} 次 OOM 杀进程事件`);
    const platform = info.system;
    const limit = group.memory_limit_state === 'unlimited' ? '未设置有限额度' : systemBytes(group.memory_limit);
    const swap = info.swap;
    const issues = {platform:'运行系统',cpu_info:'CPU 信息',cpu_usage:'CPU 使用率',load:'负载',memory:'内存',swap:'Swap',process:'进程资源',host_platform:'宿主发行版',host_network:'宿主网络',network:'网卡',network_addresses:'网卡地址'};
    return `<div class="system-toolbar"><span class="muted">采样于 ${esc(date(info.observed_at))} · 耗时 ${info.duration_ms} ms${info.sample_seconds ? ' · 统计间隔 ' + info.sample_seconds.toFixed(1) + ' 秒' : ''}</span><label><input id="system-info-auto" type="checkbox" ${systemInfoAutomatic ? 'checked' : ''}> 每 5 秒刷新</label></div>
      ${alerts.length ? `<div class="system-alert" role="status">${[...new Set(alerts)].map(alert => `<p>${esc(alert)}</p>`).join('')}</div>` : ''}
      <div class="system-resource-grid">
        ${systemCard('系统 CPU', systemPercent(info.cpu.percent), `逻辑 CPU ${info.cpu.logical || '—'} · 负载 ${info.cpu.load1 == null ? '不可用' : info.cpu.load1.toFixed(2)}`, info.cpu.percent)}
        ${systemCard('系统内存', memory ? systemBytes(memory.used) + ' / ' + systemBytes(memory.total) : '不可用', memory ? '可用 ' + systemBytes(memory.available) : '当前环境无法读取', memory?.percent)}
        ${systemCard('TML 进程 RSS', systemBytes(process.rss), '进程占容器内存限额 · 容器总用量 ' + systemBytes(group.memory_current), process.rss != null && group.memory_limit > 0 ? process.rss / group.memory_limit * 100 : null)}
        ${systemCard('数据磁盘可用', dataDisk?.status === 'available' ? systemBytes(dataDisk.available) : '不可用', dataDisk?.status === 'available' ? '总容量 ' + systemBytes(dataDisk.total) : '路径无法读取', dataDisk?.status === 'available' ? dataDisk.percent : null)}
      </div>
      <section class="card panel spacer"><h2>系统与 CPU</h2>${systemDetails([
        ['部署环境', platform.containerized ? '容器' : '本机进程'], ['运行环境主机名', esc(platform.hostname)],
        ['运行系统', esc(platform.runtime_platform || platform.os)], ['宿主发行版', esc(platform.host_platform || (platform.containerized ? '需启用宿主信息只读挂载' : '不可用'))],
        ['内核 / 架构', esc(platform.kernel + ' / ' + platform.architecture)], ['系统运行时间', systemDuration(platform.kernel_uptime_seconds)],
        ['CPU 型号', esc(info.cpu.model || '不可用')], ['物理核 / 逻辑 CPU', `${info.cpu.physical || '—'} / ${info.cpu.logical || '—'}`],
        ['CPU 报告频率', info.cpu.mhz ? info.cpu.mhz.toFixed(0) + ' MHz' : '不可用'], ['CPU I/O 等待', systemPercent(info.cpu.iowait_percent)],
        ['系统负载 1 / 5 / 15 分钟', [info.cpu.load1,info.cpu.load5,info.cpu.load15].map(value => value == null ? '—' : value.toFixed(2)).join(' / ')]
      ])}<p class="muted">系统 CPU、内存和负载来自当前可见的内核视图。内存使用量按总量减可用量计算。CPU 使用率需要两次有效采样。</p></section>
      <div class="grid-two spacer">
        <section class="card panel"><h2>容器 / 进程资源组</h2><p class="muted">${group.status === 'available' ? 'cgroup v' + group.version : group.status === 'not_applicable' ? '当前没有可读取的独立资源组' : '当前环境无法读取资源组指标'}</p>${systemDetails([
          ['CPU 配额', group.cpu_quota_cores == null ? '未读取到有限配额' : group.cpu_quota_cores.toFixed(2) + ' 核'],
          ['可见 CPU 容量', group.effective_cpu_cores == null ? '—' : group.effective_cpu_cores.toFixed(2) + ' 核'],
          ['CPU 占可见容量', systemPercent(group.cpu_capacity_percent)], ['内存当前用量（含缓存）', systemBytes(group.memory_current)],
          ['内存工作集', systemBytes(group.memory_working_set)], ['可见内存限额', esc(limit)],
          ['容器 Swap 用量 / 限额', systemBytes(group.swap_current) + ' / ' + systemBytes(group.swap_limit)],
          ['CPU 限流累计 / 本次增量', systemNumber(group.throttle_count) + ' / ' + systemNumber(group.throttle_delta)], ['OOM 杀进程累计', systemNumber(group.oom_kills)]
        ])}<p class="muted">工作集扣除非活跃文件缓存。资源组限额按当前 cgroup 命名空间可见范围读取。</p></section>
        <section class="card panel"><h2>TML 当前进程</h2>${systemDetails([
          ['PID / 已运行', process.pid + ' / ' + systemDuration(process.uptime_seconds)],
          ['CPU 使用率（单核 100%）', systemPercent(process.cpu_single_core_percent)], ['CPU 占可见容量', systemPercent(process.cpu_capacity_percent)],
          ['驻留内存 RSS', systemBytes(process.rss)], ['虚拟地址空间', systemBytes(process.virtual_memory)],
          ['Go 堆对象内存', systemBytes(process.go_heap)], ['Go 管理内存（含预留）', systemBytes(process.go_runtime_memory)],
          ['Goroutine / 系统线程', process.goroutines + ' / ' + systemNumber(process.threads)], ['打开的文件描述符', systemNumber(process.file_descriptors)],
          ['GC 次数 / GOMAXPROCS', process.gc_cycles + ' / ' + process.gomaxprocs], ['进程累计物理读 / 写', systemBytes(process.read_bytes) + ' / ' + systemBytes(process.write_bytes)], ['Go 版本', esc(process.go_version)]
        ])}</section>
      </div>
      <section class="card panel spacer"><h2>磁盘空间</h2><p class="muted">按路径展示所在文件系统的容量。同一文件系统可能对应多个路径，容量无需相加。可用空间采用普通进程可使用的空间。</p>${table(['用途 / 路径','总容量','已使用','可用','使用率','可用 / 总 inode'],info.disks.map(disk => [
        `${esc({root:'运行根目录',data:'数据目录',backups:'备份目录'}[disk.name] || disk.name)}<small>${esc(disk.path)}</small>`, disk.status === 'available' ? systemBytes(disk.total) : '不可用', disk.status === 'available' ? systemBytes(disk.used) : '—', disk.status === 'available' ? systemBytes(disk.available) : '—', disk.status === 'available' ? systemPercent(disk.percent) : '—', disk.inodes_total ? disk.inodes_free + ' / ' + disk.inodes_total : '不适用'
      ]))}<h3>数据文件大小</h3>${table(['文件','逻辑长度'],info.files.map(file => [esc({database:'SQLite 数据库',wal:'SQLite WAL',shm:'SQLite 共享内存文件'}[file.name] || file.name), file.status === 'available' ? systemBytes(file.size) : file.status === 'absent' ? '尚未创建' : '不可读取']))}</section>
      <section class="card panel spacer"><h2>Swap</h2>${swap ? `<p>${swap.total ? '已使用 ' + systemBytes(swap.used) + ' / 总量 ' + systemBytes(swap.total) + ' · 可用 ' + systemBytes(swap.free) : '系统未配置 Swap'}</p>${swap.devices.length ? table(['设备 / 文件','已使用','可用'],swap.devices.map(device => [esc(device.name),systemBytes(device.used),systemBytes(device.free)])) : ''}` : '<p>当前环境无法读取 Swap 信息</p>'}</section>
      <section class="card panel spacer"><h2>网络地址</h2>${systemDetails([
        ['宿主机本地地址', info.host_ips.length ? info.host_ips.map(systemAddress).join('<br>') : platform.containerized ? '需启用宿主信息只读挂载' : '未取得地址'],
        ['配置的公网地址', info.declared_public_ips.length ? info.declared_public_ips.map(esc).join('<br>') + '<small>来源为管理员配置</small>' : '未配置']
      ])}<p class="muted">网卡列表来自${platform.network_scope === 'container' ? '容器网络命名空间' : '本机网络'}。NAT 后的公网地址需由管理员配置。</p>${table(['网卡','状态 / MTU','本地地址'],info.interfaces.map(network => [esc(network.name), (network.up ? '启用' : '关闭') + ' / ' + network.mtu, network.addresses.map(systemAddress).join('<br>') || '无地址']))}</section>
      ${info.issues.length ? `<p class="muted">部分指标不可用 · ${[...new Set(info.issues.map(issue => issues[issue.component] || issue.component))].map(esc).join('、')}</p>` : ''}`;
  } finally {
    systemInfoRefreshing = false;
  }
};

$('#content').addEventListener('change', event => {
  if (event.target.id === 'system-info-auto') systemInfoAutomatic = event.target.checked;
});

setInterval(() => {
  if (page === 'systeminfo' && systemInfoAutomatic && !systemInfoRefreshing && !document.hidden && !$('#editor').open) render(true);
}, 5000);
