import { localStatus, login, whoami, approve, deny, features, version, connectEvents, ApiError, artifacts, artifactContent } from './api.js';
import {
  esc, findGates, diffSnapshots, renderStats, renderGate,
  renderWorkflows, renderAgents, renderLedger, renderEmptyState, renderNudge,
  renderArtifacts, renderArtifactContent,
} from './render.js';

const $ = (id) => document.getElementById(id);
let prevSnapshot = null;
let me = null;
let deepLinkApplied = false;
let artifactExpandOpen = false;
let lastArtifactSig = null;

function applyDeepLink() {
  const hash = location.hash;
  if (!hash) return;
  if (hash.startsWith('#workflow=')) {
    const name = decodeURIComponent(hash.slice('#workflow='.length));
    for (const el of document.querySelectorAll('.wf[data-name]')) {
      if (el.dataset.name === name) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        el.classList.add('hl');
        setTimeout(() => el.classList.remove('hl'), 2000);
        break;
      }
    }
  } else if (hash.startsWith('#agent=')) {
    const name = decodeURIComponent(hash.slice('#agent='.length));
    for (const el of document.querySelectorAll('.agent[data-name]')) {
      if (el.dataset.name === name) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        el.classList.add('hl');
        setTimeout(() => el.classList.remove('hl'), 2000);
        break;
      }
    }
  } else if (hash === '#outputs') {
    const el = $('outputs-slot');
    if (el) el.scrollIntoView({ behavior: 'smooth' });
  }
}

function refreshOutputs() {
  if (artifactExpandOpen) return;
  artifacts().then((data) => {
    const list = (data && data.artifacts) || [];
    const sig = list.map((a) => a.id).sort().join(',');
    if (sig === lastArtifactSig) return;
    lastArtifactSig = sig;
    $('outputs-slot').innerHTML = renderArtifacts(list);
  }).catch(() => {});
}

function applyRole() {
  const can = new Set(me?.permissions || []);
  document.querySelectorAll('[data-requires]').forEach((el) => {
    el.hidden = !can.has(el.dataset.requires);
  });
}

function renderSnapshot(snap) {
  $('stats').innerHTML = renderStats(snap);
  const empty = !(snap.agents || []).length && !(snap.workflows || []).length;
  $('workflows').innerHTML = empty ? renderEmptyState() : renderWorkflows(snap.workflows);
  $('agents').innerHTML = renderAgents(snap.agents);
  const gate = findGates(snap.workflows)[0] || null;
  $('gate-slot').innerHTML = renderGate(gate);
  const entries = diffSnapshots(prevSnapshot, snap);
  if (entries.length) $('ledger').insertAdjacentHTML('afterbegin', renderLedger(entries));
  while ($('ledger').children.length > 30) $('ledger').lastElementChild.remove();
  refreshOutputs();
  if (!deepLinkApplied) { deepLinkApplied = true; applyDeepLink(); }
  prevSnapshot = snap;
  applyRole();
}

document.addEventListener('click', async (e) => {
  const btn = e.target.closest('[data-action]');
  if (!btn) return;
  if (btn.dataset.action === 'dismiss-nudge') {
    localStorage.setItem('purko-nudge-dismissed', '1');
    $('nudge-slot').innerHTML = '';
    return;
  }
  if (btn.dataset.action === 'close-artifact') {
    const expand = btn.closest('.artifact-expand');
    if (expand) expand.remove();
    artifactExpandOpen = false;
    return;
  }
  if (btn.dataset.action === 'view-artifact') {
    const { id, name, mime } = btn.dataset;
    const row = btn.closest('[data-artifact-id]');
    if (!row) return;
    let expand = row.querySelector('.artifact-expand');
    if (!expand) {
      expand = document.createElement('div');
      expand.className = 'artifact-expand';
      row.appendChild(expand);
    }
    artifactExpandOpen = true;
    expand.innerHTML = '<span>Loading…</span>';
    const closeBtn = '<button class="artifact-expand-close" aria-label="Close" data-action="close-artifact">\xd7</button>';
    try {
      const text = await artifactContent(id);
      expand.innerHTML = closeBtn + renderArtifactContent(name, mime, text);
    } catch (err) {
      expand.innerHTML = closeBtn + `<span class="err">Failed to load: ${esc(err.message)}</span>`;
    }
    return;
  }
  const gate = btn.closest('.gate');
  if (!gate) return;
  const { wf, step } = gate.dataset;
  btn.disabled = true;
  try {
    if (btn.dataset.action === 'approve') await approve(wf, step);
    else await deny(wf, step);
  } catch (err) {
    btn.disabled = false;
    alert(`Action failed: ${err.message}`);
  }
});

$('login-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  $('login-error').textContent = '';
  try {
    await login($('login-token').value.trim());
    $('login-overlay').hidden = true;
    boot();
  } catch (err) {
    $('login-error').textContent =
      err instanceof ApiError && err.status === 401 ? 'Invalid token.' : `Error: ${err.message}`;
  }
});

let events = null;

function showUnreachable() {
  $('live-dot').style.opacity = '0.4';
  $('workflows').innerHTML =
    '<div class="empty"><div class="empty-title">Can’t reach the cluster.</div>' +
    '<p>The connection to purko dropped — the port-forward may have died. ' +
    'Retrying automatically… (ask the skill to restart it: <code>/purko</code>)</p></div>';
  setTimeout(boot, 5000);
}

async function boot() {
  let status;
  try {
    status = await localStatus();
  } catch {
    return showUnreachable();   // local server itself gone
  }
  if (!status.upstream) {
    $('workflows').innerHTML = '<div class="empty"><div class="empty-title">No cluster connected.</div><p>Start me via the skill: <code>/purko</code> — or <a href="/onboarding.html">see your options</a> (demo, connect, install).</p></div>';
    return;
  }
  try {
    me = await whoami();
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      $('login-overlay').hidden = false;   // token/sso cluster, not logged in
      return;
    }
    return showUnreachable();   // 502/503: upstream down — keep retrying
  }
  $('live-dot').style.opacity = '';
  $('whoami-chip').textContent = `${me.user} · ${me.role}`;
  renderDashboardLink(status.dashboardUrl);
  if (!localStorage.getItem('purko-nudge-dismissed')) {
    let feats = null;
    try { feats = await features(); } catch { /* network error — skip nudge */ }
    $('nudge-slot').innerHTML = renderNudge(feats);
  }
  refreshOutputs();
  checkVersion();
  if (events) events.close();   // never hold two streams after retries/login
  events = connectEvents(renderSnapshot, {
    onError: () => { $('live-dot').style.opacity = '0.4'; },
    onOpen: () => { $('live-dot').style.opacity = ''; },
  });
}

// "Manage in dashboard" — Mission Control is watch + approve; authoring and
// per-user namespace management live in the full dashboard (Spec 40, scoped
// per-user in sso mode). Surface its URL so a connected user can find it.
function renderDashboardLink(url) {
  const slot = $('dashboard-slot');
  if (!slot) return;
  if (!url) { slot.innerHTML = ''; return; }
  slot.innerHTML = `<a class="dash-link" href="${url}" target="_blank" rel="noopener"
    title="Create & manage your agents, workflows, tokens">Manage in dashboard →</a>`;
}

// Skill self-version chip (interim, pre-marketplace): surfaces one line when
// the installed skill is behind the published version. Silent-fail — the
// server does the network check and caches it; a miss just shows nothing.
async function checkVersion() {
  const slot = $('version-slot');
  if (!slot) return;
  try {
    const v = await version();
    if (v && v.update_available) {
      slot.innerHTML = `<a class="update-chip" href="${v.changelog_url}" target="_blank" rel="noopener"
        title="A newer Purko skill is available">↑ update · ${v.latest}</a>`;
    } else {
      slot.innerHTML = '';
    }
  } catch { /* offline / no endpoint — show nothing */ }
}

boot();
