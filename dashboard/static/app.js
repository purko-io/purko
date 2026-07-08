// Purko Dashboard — Core (router, SSE, shared utilities)

const state = {
  data: null,       // latest overview data from SSE
  mcpTools: [],     // all MCP tools [{name, source, cat}]
  currentTab: 'dashboard',
  detail: null,     // {type, name} when viewing a detail
  features: null,   // compiled-in capabilities from /api/features (Spec 28)
};

// Returns true when a Pro feature is available in this build.
// Defaults to true while features are unknown (older operator without
// /api/features) so a stale bundle preserves Pro behavior.
function hasFeature(name) {
  return !state.features || state.features[name] !== false;
}

// Shared upgrade teaser card for Pro features absent in this build.
function upgradeCard(title, desc) {
  return `<div class="panel" style="border-style:dashed;opacity:0.85">
    <div style="display:flex;align-items:center;gap:10px">
      <span style="font-size:18px">&#x2728;</span>
      <div>
        <div style="font-weight:600;color:var(--text-bright)">${title} <span class="tag tag--blue" style="margin-left:6px">Pro</span></div>
        <div style="font-size:12px;color:var(--dim);margin-top:2px">${desc}</div>
      </div>
      <a class="btn btn--secondary btn--sm" style="margin-left:auto;flex-shrink:0" href="https://purko.io/pricing" target="_blank" rel="noopener">Upgrade to Pro</a>
    </div>
  </div>`;
}

// One-line variant of upgradeCard (Spec 29 #2): keeps pitch + CTA, reclaims
// the vertical space a full card would burn next to real data.
function upgradeStrip(title, desc) {
  return `<div class="pro-strip">
    <span class="pro-strip-ic">&#x2728;</span>
    <span class="pro-strip-txt"><b>${title}</b> <span class="tag tag--blue">Pro</span> — ${desc}</span>
    <a href="https://purko.io/pricing" target="_blank" rel="noopener">Upgrade &rarr;</a>
  </div>`;
}

// Theme toggle (Spec 29). The pre-paint script in index.html applies the
// stored/OS choice before first render; this just flips and persists it.
function toggleTheme() {
  const root = document.documentElement;
  const next = root.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
  if (next === 'light') root.setAttribute('data-theme', 'light');
  else root.removeAttribute('data-theme');
  localStorage.setItem('purko-theme', next);
}

// ── Router ──────────────────────────────────────────────────────────

const router = {
  go(tab, detail) {
    state.currentTab = tab;
    state.detail = detail || null;

    // Keep the URL in sync so browser refresh returns to this view
    const hash = '#/' + tab + (detail && detail.name ? '/' + encodeURIComponent(detail.name) : '');
    if (location.hash !== hash) history.replaceState(null, '', hash);

    // Update nav
    document.querySelectorAll('.nav-tab').forEach(t => t.classList.remove('active'));
    const btn = document.querySelector(`.nav-tab[data-tab="${tab}"]`);
    if (btn) btn.classList.add('active');

    // Update views
    document.querySelectorAll('.view').forEach(v => v.classList.remove('active'));
    const view = document.getElementById('view-' + tab);
    if (view) view.classList.add('active');

    // Notify module
    if (typeof window['init_' + tab] === 'function') {
      window['init_' + tab](detail);
    }
  }
};

// ── SSE ─────────────────────────────────────────────────────────────

function connectSSE() {
  const es = new EventSource('/api/events');
  es.onmessage = function(e) {
    const d = JSON.parse(e.data);
    state.data = d;
    document.getElementById('header-time').textContent = new Date(d.timestamp).toLocaleTimeString();
    // Notify active module
    if (typeof window['update_' + state.currentTab] === 'function' && !state.detail) {
      window['update_' + state.currentTab](d);
    }
    // Detail views get their own updater (e.g. live workflow DAG progress)
    if (state.detail && typeof window['update_' + state.currentTab + '_detail'] === 'function') {
      window['update_' + state.currentTab + '_detail'](d);
    }
  };
  es.onerror = function() {
    setTimeout(connectSSE, 5000);
  };
}

// ── HTML Helpers ────────────────────────────────────────────────────

function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function phaseHTML(phase) {
  const p = phase || 'Pending';
  return `<span class="phase phase-${p}">${p}</span>`;
}

function shortAge(age) {
  if (!age) return '-';
  return age.replace(/h\d+m.*/, 'h').replace(/m\d+\.\d+s/, 'm').replace(/(\d+\.\d+)s$/, (_, v) => Math.round(parseFloat(v)) + 's');
}

function calcDuration(start, end) {
  const ms = new Date(end) - new Date(start);
  if (ms < 0) return '-';
  if (ms < 1000) return ms + 'ms';
  const s = Math.round(ms / 1000);
  if (s < 60) return s + 's';
  const m = Math.floor(s / 60);
  return m + 'm' + (s % 60 ? (s % 60) + 's' : '');
}

function conditionsHTML(conditions) {
  if (!conditions || conditions.length === 0) return '';
  return '<div class="conditions">' +
    conditions.map(c => {
      const ok = c.status === 'True';
      return `<span class="cond ${ok ? 'cond-ok' : 'cond-fail'}">${c.type}</span>`;
    }).join('') +
    '</div>';
}

// Inline sparkline for insight cards (Spec 29 #1). points: array of numbers.
// Returns '' when there is nothing meaningful to draw.
function sparklineSVG(points, colorVar) {
  if (!points || points.length < 2) return '';
  const max = Math.max(...points);
  if (max === 0) return '';
  const w = 70, h = 22, min = Math.min(...points), span = (max - min) || 1;
  const step = w / (points.length - 1);
  const d = points.map((p, i) =>
    `${i ? 'L' : 'M'}${(i * step).toFixed(1)} ${(h - 2 - ((p - min) / span) * (h - 4)).toFixed(1)}`
  ).join(' ');
  return `<svg class="card-spark" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" fill="none" aria-hidden="true"><path d="${d}" stroke="var(${colorVar})" stroke-width="1.5" opacity="0.8"/></svg>`;
}

// Bucket archived runs into fixed windows for sparklines (Spec 29 #1).
function historyBuckets(runs, hours, buckets) {
  const now = Date.now(), span = hours * 3600 * 1000, step = span / buckets;
  const counts = new Array(buckets).fill(0), ok = new Array(buckets).fill(0);
  for (const r of (runs || [])) {
    if (!r.startTime) continue;
    const t = new Date(r.startTime).getTime();
    if (isNaN(t) || now - t > span || t > now) continue;
    const i = Math.min(buckets - 1, Math.floor((t - (now - span)) / step));
    counts[i]++;
    if (r.phase === 'Succeeded') ok[i]++;
  }
  return { counts, ok };
}

function renderMarkdown(md) {
  if (!md) return '';
  let html = esc(md);
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, '<pre><code>$2</code></pre>');
  html = html.replace(/^###\s+(.+)$/gm, '<h3>$1</h3>');
  html = html.replace(/^##\s+(.+)$/gm, '<h2>$1</h2>');
  html = html.replace(/^#\s+(.+)$/gm, '<h1>$1</h1>');
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
  html = html.replace(/^\|(.+)\|$/gm, (match) => {
    const cells = match.split('|').filter(c => c.trim());
    if (cells.every(c => /^[\s-:]+$/.test(c))) return '';
    return '<tr>' + cells.map(c => `<td>${c.trim()}</td>`).join('') + '</tr>';
  });
  html = html.replace(/(<tr>[\s\S]*?<\/tr>(\s*<tr>[\s\S]*?<\/tr>)*)/g, '<table>$1</table>');
  html = html.replace(/^&gt;\s+(.+)$/gm, '<blockquote>$1</blockquote>');
  html = html.replace(/^---$/gm, '<hr>');
  html = html.replace(/\n\n/g, '</p><p>');
  html = html.replace(/\n/g, '<br>');
  return '<p>' + html + '</p>';
}

function showResult(el, type, msg) {
  if (typeof el === 'string') el = document.getElementById(el);
  if (!el) return;
  el.innerHTML = `<div class="result result--${type}">${msg}</div>`;
}

// ── DAG Helper ──────────────────────────────────────────────────────

function buildDAGGroups(steps) {
  const depth = {};
  function getDepth(name) {
    if (depth[name] !== undefined) return depth[name];
    const step = steps.find(s => s.name === name);
    if (!step || !step.dependsOn || step.dependsOn.length === 0) { depth[name] = 0; return 0; }
    let maxD = 0;
    for (const dep of step.dependsOn) maxD = Math.max(maxD, getDepth(dep) + 1);
    depth[name] = maxD;
    return maxD;
  }
  steps.forEach(s => getDepth(s.name));
  const maxD = Math.max(...Object.values(depth), 0);
  const groups = [];
  for (let d = 0; d <= maxD; d++) {
    const g = steps.filter(s => depth[s.name] === d);
    if (g.length > 0) groups.push(g);
  }
  return groups;
}

// ── MCP Tools (shared for agents create/edit + MCP tab) ─────────────

function loadMCPToolsList() {
  return fetch('/api/mcp/tools').then(r => r.json()).then(d => {
    state.mcpTools = [];
    for (const server of (d.servers || [])) {
      const src = server.name.toLowerCase();
      for (const name of (server.tools || [])) {
        state.mcpTools.push({ name, source: src, cat: TOOL_CATS[name] || server.category || 'Other' });
      }
    }
  });
}

// Tool category mapping
const TOOL_CATS = {
  list_namespaces: 'Discovery', list_pods_in_namespace: 'Discovery', get_kubernetes_resource: 'Discovery',
  search_resources_by_labels: 'Discovery', conservative_namespace_overview: 'Discovery',
  smart_get_namespace_events: 'Events', progressive_event_analysis: 'Events', advanced_event_analytics: 'Events',
  analyze_logs: 'Logs', detect_log_anomalies: 'Logs', smart_summarize_pod_logs: 'Logs',
  stream_analyze_pod_logs: 'Logs', analyze_pod_logs_hybrid: 'Logs', semantic_log_search: 'Logs',
  detect_anomalies: 'Analysis', check_resource_constraints: 'Analysis',
  adaptive_namespace_investigation: 'Investigation', automated_triage_rca_report_generator: 'Investigation',
  live_system_topology_mapper: 'Topology', resource_bottleneck_forecaster: 'Topology',
  predictive_log_analyzer: 'Prediction', what_if_scenario_simulator: 'Prediction', manage_prediction_training_data: 'Prediction',
  prometheus_query: 'Metrics',
  check_cluster_certificate_health: 'Security', investigate_tls_certificate_issues: 'Security',
  get_etcd_logs: 'etcd',
  ci_cd_performance_baselining_tool: 'CI/CD', analyze_failed_pipeline: 'CI/CD', get_pipelinerun_logs: 'CI/CD',
  pipeline_tracer: 'CI/CD', find_pipeline: 'CI/CD', get_tekton_pipeline_runs_status: 'CI/CD',
  list_pipelineruns: 'CI/CD', list_recent_pipeline_runs: 'CI/CD', list_taskruns: 'CI/CD',
  get_machine_config_pool_status: 'OpenShift', get_openshift_cluster_operator_status: 'OpenShift',
  get_file_contents: 'Files', search_code: 'Search', list_commits: 'Git', list_branches: 'Git',
  list_tags: 'Git', get_commit: 'Git', create_branch: 'Git',
  list_issues: 'Issues', search_issues: 'Issues', add_issue_comment: 'Issues',
  list_pull_requests: 'PRs', create_pull_request: 'PRs', merge_pull_request: 'PRs',
  update_pull_request: 'PRs', search_pull_requests: 'PRs',
  search_repositories: 'Search', search_users: 'Search',
  push_files: 'Files', create_or_update_file: 'Files',
  fork_repository: 'Repos', create_repository: 'Repos',
  get_latest_release: 'Releases', list_releases: 'Releases',
  acknowledge_incident: 'Incidents', resolve_incident: 'Incidents', create_incident: 'Incidents',
  list_incidents: 'Incidents', get_incident: 'Incidents', add_incident_note: 'Incidents',
  list_services: 'Services', get_service: 'Services',
  get_oncall_users: 'OnCall', list_users: 'Users', get_user: 'Users',
  list_schedules: 'Schedules', get_schedule: 'Schedules',
};

const CAT_COLORS = {
  Discovery: '--accent', Events: '--amber', Logs: '--green', Analysis: '--purple',
  Investigation: '--accent', Topology: '--purple', Prediction: '--amber',
  Metrics: '--green', Security: '--red', etcd: '--amber', 'CI/CD': '--accent',
  OpenShift: '--red', Files: '--accent', Search: '--green', Git: '--purple',
  Issues: '--amber', PRs: '--accent', Repos: '--purple',
  Releases: '--green', Users: '--dim', Incidents: '--red', Services: '--amber',
  OnCall: '--green', Schedules: '--purple',
};

// ── Init ────────────────────────────────────────────────────────────

// Fetch capabilities alongside the overview so state.features is populated
// before the first render (Spec 28). On failure (older operator without
// /api/features), features stays null and hasFeature() defaults to Pro
// behavior — 404s surface instead of hiding working controls.
Promise.all([
  fetch('/api/overview').then(r => r.json()),
  fetch('/api/features').then(r => (r.ok ? r.json() : null)).catch(() => null),
]).then(([d, features]) => {
  state.data = d;
  state.features = features;
  document.getElementById('header-time').textContent = new Date(d.timestamp).toLocaleTimeString();
  routeFromHash();
}).catch(() => {
  // Backend unreachable at boot (rollout, flapping port-forward): route
  // anyway — tabs fetch their own data and SSE reconnects — instead of
  // leaving a permanently blank page (F32).
  routeFromHash();
});

// Route from the URL hash (#/tab or #/tab/detail) so refresh and shared
// links land on the same view instead of resetting to the dashboard.
function routeFromHash() {
  const parts = location.hash.replace(/^#\/?/, '').split('/');
  const known = ['dashboard', 'agents', 'workflows', 'mcp', 'llm', 'history', 'settings'];
  const tab = known.includes(parts[0]) ? parts[0] : 'dashboard';
  const detail = parts[1] ? { name: decodeURIComponent(parts[1]) } : null;
  router.go(tab, detail);
}
window.addEventListener('hashchange', routeFromHash);

connectSSE();
loadMCPToolsList();

// Identity chip (F28): the auth proxy forwards the user; without SSO the
// endpoint returns empty and the chip stays hidden.
fetch('/api/whoami').then(r => r.json()).then(d => {
  if (d.user) {
    document.getElementById('header-user-email').textContent = d.user;
    document.getElementById('header-user').style.display = '';
  }
}).catch(() => {});
