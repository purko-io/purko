// Memory page (Spec 34 §8). Mirrors history.js: router entry init_memory, a search
// box over /api/memory (FTS), stats header, per-row Forget. Store-disabled -> 503
// renders an informational empty state (memory is a shared feature, not Pro-gated).
function init_memory(detail) {
  const el = document.getElementById('view-memory');
  if (!el) return;
  el.innerHTML = '<div class="empty">Loading memory…</div>';
  const preAgent = detail && detail.name ? detail.name : '';
  loadMemory(preAgent, '');
}

function loadMemory(agent, q) {
  const el = document.getElementById('view-memory');
  const params = new URLSearchParams();
  if (q) params.set('q', q);
  if (agent) params.set('agent', agent);
  params.set('limit', '100');
  Promise.all([
    fetch('/api/memory?' + params.toString())
      .then(r => r.status === 503 ? '503' : (r.ok ? r.json() : Promise.reject(new Error('HTTP ' + r.status))))
      .catch(() => 'error'),
    fetch('/api/memory/stats').then(r => (r.ok ? r.json() : null)).catch(() => null),
  ]).then(([entries, stats]) => {
    if (entries === '503') {
      el.innerHTML = '<div class="section-title">Memory</div><div class="empty">Memory is not enabled on this operator (set <span class="mono">operator.memory.enabled=true</span>).</div>';
      return;
    }
    if (entries === 'error') {
      el.innerHTML = '<div class="section-title">Memory</div><div class="empty">Failed to load memories — the operator may be unavailable. Retry shortly.</div>';
      return;
    }
    const rows = (entries || []).map(memRow).join('');
    el.innerHTML = `
      <div class="section-title">Memory</div>
      ${memStatsHTML(stats)}
      <div id="mem-forget-result"></div>
      <div class="mem-search">
        <input id="mem-q" class="mem-input" type="text" placeholder="Search memories…" value="${esc(q)}"
          oninput="memDebounce(this.value, '${esc(agent)}')">
        ${agent ? `<span class="tag tag--blue">agent: ${esc(agent)} <span class="clickable" onclick="loadMemory('', document.getElementById('mem-q').value)">✕</span></span>` : ''}
      </div>
      <div class="mem-list">${rows || '<div class="empty">No memories yet — run a persistent-memory agent to populate this.</div>'}</div>`;
  });
}

function memStatsHTML(stats) {
  if (!stats) return '';
  const perAgent = stats.perAgent || {};
  const chips = Object.keys(perAgent).sort((a, b) => perAgent[b] - perAgent[a]).slice(0, 8)
    .map(a => `<span class="clickable tag" onclick="loadMemory('${esc(a)}','')">${esc(a)} · ${perAgent[a]}</span>`).join('');
  // Provider health badge (Spec §4/§8). 'healthy' is present only when the status
  // reconciler has written a default MemoryProvider status; omit the badge otherwise.
  let badge = '';
  if (typeof stats.healthy === 'boolean') {
    badge = stats.healthy
      ? '<span class="mem-health mem-health--ok">healthy</span>'
      : `<span class="mem-health mem-health--bad" title="${esc(stats.lastError || '')}">unhealthy</span>`;
  }
  return `<div class="mem-stats">
    <span class="mem-stat"><b>${stats.totalEntries || 0}</b> entries</span>
    <span class="mem-stat">provider <b>${esc(stats.providerType || 'builtin')}</b>${badge}</span>
    <span class="mem-agents">${chips}</span>
  </div>`;
}

function memRow(e) {
  const when = e.createdAt ? new Date(e.createdAt).toLocaleString() : '';
  return `<div class="mem-item" data-id="${esc(e.id)}">
    <div class="mem-content">${esc(e.content)}</div>
    <div class="mem-meta">
      <span class="mono">${esc(e.agent)}</span>
      <span class="mono">${esc(e.workflow || '')}${e.step ? '/' + esc(e.step) : ''}</span>
      <span class="mono">${esc(when)}</span>
      <button class="mem-forget" onclick="memForget('${esc(e.id)}')" title="Forget this memory">Forget</button>
    </div>
  </div>`;
}

let _memTimer = null;
function memDebounce(q, agent) {
  clearTimeout(_memTimer);
  _memTimer = setTimeout(() => loadMemory(agent, q), 250);
}

function memForget(id) {
  if (!confirm('Forget this memory permanently? This cannot be undone.')) return;
  fetch('/api/memory/' + encodeURIComponent(id), { method: 'DELETE' }).then(r => {
    if (r.status === 403) {
      showResult('mem-forget-result', 'err', 'Forget is disabled on this operator (PURKO_MEMORY_FORGET_ENABLED=false).');
      return;
    }
    if (!r.ok) { showResult('mem-forget-result', 'err', 'Forget failed.'); return; }
    const item = document.querySelector(`.mem-item[data-id="${CSS.escape(id)}"]`);
    if (item) item.remove();
  });
}
