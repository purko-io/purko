// Pure derive/render helpers. No DOM, no fetch — testable in node.

export function bytesHuman(n) {
  n = +n || 0;
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`;
}

export function esc(s) {
  return String(s ?? '')
    .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;').replaceAll("'", '&#39;');
}

const STEP_STATES = {
  Succeeded: { cls: 'done', glyph: '✓', label: 'done' },
  Running:   { cls: 'active', glyph: '◐', label: 'running' },
  Failed:    { cls: 'failed', glyph: '✕', label: 'failed' },
  Pending:   { cls: 'queued', glyph: '○', label: 'queued' },
};

function gateStepName(wf) {
  if (wf.phase !== 'Running') return null;
  const pending = (wf.steps || []).find((s) => s.phase === 'Pending');
  return pending ? pending.name : null;
}

export function stepState(wf, step) {
  if (step.phase === 'Pending' && gateStepName(wf) === step.name) {
    return { cls: 'gated', glyph: '⏸', label: 'awaiting approval' };
  }
  return STEP_STATES[step.phase] || STEP_STATES.Pending;
}

export function wfState(wf) {
  const gate = gateStepName(wf);
  if (gate) return { cls: 'waiting', glyph: '⏸', label: `gate at ${gate}` };
  const map = {
    Running:   { cls: 'running', glyph: '◐', label: 'running' },
    Succeeded: { cls: 'ok', glyph: '✓', label: 'succeeded' },
    Failed:    { cls: 'failed', glyph: '✕', label: 'failed' },
  };
  return map[wf.phase] || { cls: 'idle', glyph: '◷', label: (wf.phase || 'pending').toLowerCase() };
}

// Attention order: gate-waiting first, then running, then failures
// (including CompletedWithErrors, which wfState buckets as 'idle'),
// then everything else, with succeeded last. Stable within groups.
const WF_ORDER = { waiting: 0, running: 1, failed: 2, idle: 3, ok: 4 };

function wfPriority(wf) {
  const { cls } = wfState(wf);
  if (cls === 'idle' && (wf.failedSteps | 0) > 0) return WF_ORDER.failed;
  return WF_ORDER[cls] ?? WF_ORDER.idle;
}

export function sortWorkflows(list) {
  return [...(list || [])].sort((a, b) => wfPriority(a) - wfPriority(b));
}

export function findGates(workflows) {
  const out = [];
  for (const wf of workflows || []) {
    const step = gateStepName(wf);
    if (step) out.push({ workflow: wf.name, step });
  }
  return out;
}

export function diffSnapshots(prev, next) {
  if (!prev || !next) return [];
  const prevPhase = {};
  for (const wf of prev.workflows || []) {
    for (const s of wf.steps || []) prevPhase[`${wf.name}/${s.name}`] = s.phase;
  }
  const entries = [];
  const GLYPHS = { Succeeded: ['✓', 'ok'], Running: ['◐', 'running'], Failed: ['✕', 'failed'], Pending: ['⏸', 'waiting'] };
  for (const wf of next.workflows || []) {
    for (const s of wf.steps || []) {
      const before = prevPhase[`${wf.name}/${s.name}`];
      if (before !== undefined && before !== s.phase) {
        const [glyph, cls] = GLYPHS[s.phase] || ['○', 'idle'];
        entries.push({ glyph, cls, text: `${wf.name} step ${s.name}: ${before} → ${s.phase}` });
      }
    }
  }
  return entries;
}

export function renderStats(o) {
  return `
    <div class="stat"><div class="k">Workflows</div><div class="v">${o.wfRunning | 0}<small>running · ${o.wfFailed | 0} failed</small></div></div>
    <div class="stat"><div class="k">Agents ready</div><div class="v">${o.agentReady | 0}<small>of ${o.agentCount | 0}</small></div></div>
    <div class="stat"><div class="k">Succeeded</div><div class="v">${o.wfSucceeded | 0}<small>workflows</small></div></div>`;
}

export function renderGate(gate) {
  if (!gate) return '';
  return `
  <div class="gate" data-wf="${esc(gate.workflow)}" data-step="${esc(gate.step)}">
    <div class="gate-body">
      <div class="gate-kicker">⏸ Human gate · needs a decision</div>
      <div class="gate-q">Approve step <b>${esc(gate.step)}</b>?</div>
      <div class="gate-meta">workflow <b>${esc(gate.workflow)}</b></div>
    </div>
    <div class="gate-actions">
      <button class="btn approve" data-requires="operate" data-action="approve">Approve</button>
      <button class="btn deny" data-requires="operate" data-action="deny">Decline</button>
    </div>
  </div>`;
}

export function renderWorkflows(list) {
  if (!list || !list.length) return '';
  return sortWorkflows(list).map((wf) => {
    const st = wfState(wf);
    const steps = (wf.steps || []).map((s) => {
      const ss = stepState(wf, s);
      return `<div class="tl-step ${ss.cls}">
        <div class="tl-name">${esc(s.name)}</div>
        <div class="tl-sub">${ss.glyph} ${esc(ss.label)}${s.agent ? ' · ' + esc(s.agent) : ''}</div>
      </div>`;
    }).join('');
    return `<div class="wf" data-name="${esc(wf.name)}">
      <div class="wf-head">
        <span class="wf-name">${esc(wf.name)}</span>
        <span class="wf-ns">${esc(wf.namespace)}${wf.duration ? ' · ' + esc(wf.duration) : ''}</span>
        <span class="st ${st.cls}"><i>${st.glyph}</i> ${esc(st.label)}</span>
      </div>
      <div class="tl">${steps}</div>
    </div>`;
  }).join('');
}

export function renderAgents(list) {
  return (list || []).map((a) => {
    const ready = a.phase === 'Ready';
    const st = ready
      ? { cls: 'ok', glyph: '✓', label: a.phase }
      : { cls: 'idle', glyph: '○', label: a.phase || 'Unknown' };
    return `<div class="agent" data-name="${esc(a.name)}">
      <div class="agent-avatar">${esc((a.name || '?')[0].toUpperCase())}</div>
      <div class="agent-main">
        <div class="agent-name">${esc(a.name)}<small>${esc(a.type || '')}</small></div>
        <div class="agent-task">${esc(a.model || '')} · ${a.toolCount | 0} tools</div>
      </div>
      <div class="agent-side"><span class="st ${st.cls}"><i>${st.glyph}</i> ${esc(st.label)}</span></div>
    </div>`;
  }).join('');
}

export function renderLedger(entries) {
  return (entries || []).map((e) => `<div class="ev">
    <span class="ev-t">${new Date().toLocaleTimeString([], { hour12: false })}</span>
    <i class="ev-i ${e.cls}">${e.glyph}</i><span class="ev-m">${esc(e.text)}</span>
  </div>`).join('');
}

export function renderEmptyState() {
  return `<div class="empty">
    <div class="empty-title">Your studio is empty.</div>
    <p>No agents or workflows yet. Two good first moves:</p>
    <ul>
      <li><b>Load a starter team</b> — ask the skill: <code>load the legal starter team</code></li>
      <li><b>Create your first agent</b> — ask the skill: <code>create a purko agent that …</code></li>
    </ul>
  </div>`;
}

export function renderArtifacts(artifacts) {
  if (!artifacts || !artifacts.length) {
    return '<p class="muted">No outputs yet — completed steps produce artifacts.</p>';
  }
  return artifacts.map((a) => {
    const badge = [a.kind, a.mime].filter(Boolean).join(' · ');
    const size = typeof a.size === 'number' ? bytesHuman(a.size) : '';
    const provParts = [a.step, a.agent].filter(Boolean).map(esc);
    const provHtml = provParts.length
      ? `<span class="artifact-provenance">${provParts.join(' · ')}</span>`
      : '';
    return `<div class="artifact-row" data-artifact-id="${esc(a.id)}">
  <div class="artifact-info">
    <span class="artifact-name">${esc(a.name)}</span>
    ${badge ? `<span class="artifact-badge">${esc(badge)}</span>` : ''}
    ${provHtml}
    ${size ? `<span class="artifact-size">${esc(size)}</span>` : ''}
  </div>
  <button class="btn" data-action="view-artifact" data-id="${esc(a.id)}" data-name="${esc(a.name)}" data-mime="${esc(a.mime || '')}">view</button>
</div>`;
  }).join('');
}

export function renderArtifactContent(name, mime, text) {
  const heading = `<div class="artifact-content-name">${esc(name)}</div>`;
  if (typeof mime === 'string' && mime.startsWith('text/')) {
    return `<div class="artifact-content">${heading}<pre class="artifact-pre">${esc(text)}</pre></div>`;
  }
  return `<div class="artifact-content">${heading}<p class="artifact-note">Binary content (${esc(mime || 'unknown type')}) — download to view.</p></div>`;
}

export function renderNudge(features) {
  if (!features) return '';
  const missing = [];
  if (features.history === false ||
      (typeof features.historyRetentionDays === 'number' &&
       features.historyRetentionDays > 0 &&
       features.historyRetentionDays < 90)) missing.push('full run history');
  if (features.sso === false) missing.push('team SSO');
  if (features.intent === false) missing.push('AI follow-ups');
  if (!missing.length) return '';
  return `<div class="nudge">
    <span class="nudge-msg">This studio runs Community Purko — hosted Purko adds ${esc(missing.join(' / '))}.</span>
    <a class="nudge-link" href="/onboarding.html#plans">See plans</a>
    <button class="nudge-dismiss" aria-label="Dismiss" data-action="dismiss-nudge">&#xD7;</button>
  </div>`;
}
