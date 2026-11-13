function historyLink(label, range, grain = 'day', extra = {}) {
  const query = new URLSearchParams({range, grain, ...extra});
  return `<a href="#traffic?${esc(query)}">${esc(label)} →</a>`;
}

function trafficHistoryPanel(users, subscribers, instances, servers) {
  const today = new Date();
  const localDate = new Date(today - today.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
  return `<section class="card panel spacer" id="traffic-timeline">
    <div class="panel-heading"><h2>历史流量</h2></div>
    <form id="traffic-history-form" class="history-filters">
      ${field('subject', '统计对象', 'all', 'select', [
        ['all', '全部核心用户'],
        ...[...new Set(instances.map(instance => instance.core_type))].map(core => ['core:' + core, '核心 · ' + core]),
        ...users.map(user => ['user:' + user.id, '核心用户 · ' + (user.operator_name || user.display_name) + ' · #' + user.id]),
        ...subscribers.map(subscriber => ['subscriber:' + subscriber.id, '订阅用户 · ' + subscriber.name])
      ])}
      ${field('server_id', '服务器', '', 'select', [['','全部服务器'], ...servers.map(server => [server.id, server.name])])}
      ${field('instance_id', '实例', '', 'select', [['','全部实例'], ...instances.map(instance => [instance.id, instance.name])])}
      ${field('grain', '统计粒度', 'day', 'select', [['day','天'],['hour','小时'],['half-hour','半小时']])}
      ${field('from', '开始日期', localDate, 'date')}
      ${field('to', '结束日期', localDate, 'date')}
      <button class="primary" type="submit">查询历史</button>
    </form>
    <div id="traffic-history-result" aria-live="polite"></div>
  </section>`;
}

const historyStates = {observed:'已记录', unobserved:'无增量记录', archived:'明细已归档'};

function timelineChart(buckets) {
  const height = 180;
  const width = 720;
  const step = 660 / buckets.length;
  const max = Math.max(1, ...buckets.map(bucket => Math.max(bucket.upload, bucket.download)));
  const marks = buckets.map((bucket, index) => {
    const x = 48 + index * step;
    const title = esc(bucket.label + ' · ' + historyStates[bucket.state]);
    if (bucket.state !== 'observed') {
      return `<rect x="${x}" y="${height + 2}" width="${Math.max(1,step - 1)}" height="5" fill="${bucket.state === 'archived' ? '#b7791f' : 'var(--muted)'}"><title>${title}</title></rect>`;
    }
    return [['upload','#15977e','上传'],['download','#6960e7','下载']].map(([key, color, label], direction) => {
      const size = bucket[key] / max * 145;
      return `<rect x="${x + direction * step / 2}" y="${height-size}" width="${step * 0.43}" height="${size}" fill="${color}"><title>${title} ${label} ${fmt(bucket[key])}</title></rect>`;
    }).join('');
  }).join('');
  const ticks = buckets.map((bucket, index) => index % Math.ceil(buckets.length / 5) === 0
    ? `<text class="chart-text" x="${48 + index * step}" y="211">${esc(bucket.label.includes(' ') ? bucket.label.slice(0,5) : bucket.label.slice(5))}</text>` : '').join('');
  const axes = [0,1,2,3].map(index => {
    const y = height - index * 145 / 3;
    return `<line class="chart-grid" x1="48" y1="${y}" x2="708" y2="${y}"/><text class="chart-text" x="0" y="${y + 3}">${fmt(max * index / 3)}</text>`;
  }).join('');
  return `<div class="chart-wrap"><svg viewBox="0 0 ${width} 225" role="img" aria-label="历史上传和下载流量">${axes}${marks}${ticks}</svg></div>
    <div class="chart-legend"><span><i class="legend-dot green"></i>上传</span><span><i class="legend-dot"></i>下载</span><span>灰色 · 无增量记录</span><span>棕色 · 明细已归档</span></div>`;
}

function updateHistoryDateFields(form) {
  const singleDay = form.elements.grain.value !== 'day';
  form.elements.to.disabled = singleDay;
  if (singleDay) form.elements.to.value = form.elements.from.value;
}

async function loadTrafficTimeline(form, range = '') {
  const target = $('#traffic-history-result');
  const data = new FormData(form);
  const subject = String(data.get('subject'));
  const [kind, id] = subject.split(':');
  const grain = String(data.get('grain'));
  const from = String(data.get('from'));
  const params = new URLSearchParams({grain, range, from, to: grain === 'day' ? String(data.get('to')) : from});
  for (const key of ['server_id', 'instance_id']) {
    if (data.get(key)) params.set(key, data.get(key));
  }
  if (kind === 'user') params.set('user_id', id);
  if (kind === 'subscriber') params.set('subscriber_id', id);
  if (kind === 'core') params.set('core', id);
  const request = String(Number(target.dataset.request || 0) + 1);
  target.dataset.request = request;
  target.textContent = '正在读取历史流量…';
  try {
    const result = await api('traffic/timeline?' + params);
    if (!target.isConnected || target.dataset.request !== request) return;
    form.elements.from.value = result.from;
    form.elements.to.value = result.to;
    const totals = result.buckets.reduce((sum, bucket) => ({
      upload: sum.upload + bucket.upload,
      download: sum.download + bucket.download
    }), {upload:0, download:0});
    const explanation = kind === 'subscriber'
      ? '当前专用计量来源的观测量，可能包含绑定前的流量。共享账号请在核心用户中查看。'
      : '采集到的增量按观测时间归入统计区间。';
    target.innerHTML = `<p class="muted">${esc(result.timezone)} · ${esc(explanation)}</p>
      <p>已记录上传 ${fmt(totals.upload)} · 已记录下载 ${fmt(totals.download)}</p>
      ${result.buckets.some(bucket => bucket.state === 'archived') ? '<p>所选日期有明细已归档，可切换小时或天查看保留的汇总。</p>' : ''}
      ${timelineChart(result.buckets)}
      <details><summary>逐区间明细</summary>${table(['时间','上传','下载','状态'],result.buckets.map(bucket => [
        esc(bucket.label), bucket.state === 'observed' ? fmt(bucket.upload) : '—',
        bucket.state === 'observed' ? fmt(bucket.download) : '—', historyStates[bucket.state]
      ]))}</details>`;
  } catch (error) {
    if (target.isConnected && target.dataset.request === request) target.textContent = error.message;
  }
}

async function initializeTrafficTimeline() {
  const form = $('#traffic-history-form');
  const params = new URLSearchParams(location.hash.split('?')[1]);
  const subject = params.get('subject');
  const grain = params.get('grain');
  if ([...form.elements.subject.options].some(option => option.value === subject)) form.elements.subject.value = subject;
  if (['day','hour','half-hour'].includes(grain)) form.elements.grain.value = grain;
  updateHistoryDateFields(form);
  await loadTrafficTimeline(form, params.get('range') || 'today');
  if (params.size && form.isConnected) form.closest('section').scrollIntoView();
}

$('#content').addEventListener('submit', event => {
  if (event.target.id !== 'traffic-history-form') return;
  event.preventDefault();
  loadTrafficTimeline(event.target);
});

$('#content').addEventListener('change', event => {
  const form = event.target.form;
  if (form?.id !== 'traffic-history-form') return;
  if (['grain','from'].includes(event.target.name)) updateHistoryDateFields(form);
});
