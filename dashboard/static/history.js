// Purko Dashboard — Tab 6: Execution History (Spec 24/28)
// Reads the persistent SQLite archive via /api/history/* — unlike the
// Workflows tab (live K8s state), these records survive pod GC and
// workflow deletion. Retention is governed by the license tier.

function init_history(detail) {
  const el = document.getElementById('view-history');

  // History is a compiled-in capability (Spec 28). Without it, upsell.
  if (!hasFeature('history')) {
    el.innerHTML = upgradeCard(
      'Execution History',
      'Persistent archive of workflow runs, step outputs, token usage, and the tool call audit trail — survives pod restarts and workflow deletion.'
    );
    return;
  }

  if (detail && detail.runId) loadHistoryRun(detail.runId);
  else loadHistoryRuns();
}

function loadHistoryRuns() {
  const el = document.getElementById('view-history');
  fetch('/api/history/runs?limit=100').then(r => {
    if (r.status === 503) {
      el.innerHTML = '<div class="empty">Execution history is not enabled on this operator — set <code class="mono">operator.history.enabled: true</code> in the Helm values.</div>';
      return null;
    }
    return r.json();
  }).then(runs => {
    if (runs === null) return;
    runs = runs || [];

    const succeeded = runs.filter(r => r.phase === 'Succeeded').length;
    const failed = runs.filter(r => r.phase === 'Failed' || r.phase === 'Cancelled').length;

    const rowsHTML = runs.map(r => `<tr>
      <td><span class="clickable" onclick="router.go('history',{runId:'${esc(r.id)}'})">${esc(r.name)}</span></td>
      <td class="mono">${esc(r.namespace)}</td>
      <td>${phaseHTML(r.phase)}</td>
      <td>${r.completedSteps}/${r.totalSteps}</td>
      <td class="mono">${r.startTime && r.completionTime ? calcDuration(r.startTime, r.completionTime) : '-'}</td>
      <td class="mono">${r.startTime ? new Date(r.startTime).toLocaleString() : '-'}</td>
    </tr>`).join('');

    el.innerHTML = `
      <div class="cards" style="margin-bottom:24px">
        <div class="card card--blue"><div class="card-value">${runs.length}</div><div class="card-label">Archived Runs</div></div>
        <div class="card card--green"><div class="card-value">${succeeded}</div><div class="card-label">Succeeded</div></div>
        <div class="card card--red"><div class="card-value">${failed}</div><div class="card-label">Failed / Cancelled</div></div>
      </div>
      <div class="section">
        <div class="section-title"><span>Workflow Runs <span class="badge">${runs.length}</span></span></div>
      </div>
      ${runs.length ? `<table><thead><tr><th>Name</th><th>Namespace</th><th>Phase</th><th>Steps</th><th>Duration</th><th>Started</th></tr></thead><tbody>${rowsHTML}</tbody></table>`
        : '<div class="empty">No archived runs yet — history is recorded as workflows execute.</div>'}
    `;
  });
}

function loadHistoryRun(runId) {
  const el = document.getElementById('view-history');
  fetch('/api/history/run/' + encodeURIComponent(runId)).then(r => {
    if (!r.ok) { el.innerHTML = `<div class="empty">Run not found: ${esc(runId)}</div>`; return null; }
    return r.json();
  }).then(run => {
    if (!run) return;
    const steps = run.steps || [];

    const stepsHTML = steps.map(s => `<tr>
      <td>${esc(s.stepName)}${s.retryCount > 0 ? ` <span class="tag tag--amber">retry ${s.retryCount}</span>` : ''}</td>
      <td>${s.agentName ? `<span class="clickable" onclick="router.go('agents',{type:'agent',name:'${esc(s.agentName)}'})">${esc(s.agentName)}</span>` : '-'}</td>
      <td>${phaseHTML(s.phase)}</td>
      <td class="mono">${s.tokensIn || 0} / ${s.tokensOut || 0}</td>
      <td class="mono">$${(s.costUsd || 0).toFixed(4)}</td>
      <td class="mono">${s.startTime && s.completionTime ? calcDuration(s.startTime, s.completionTime) : '-'}</td>
      <td><button class="btn btn--secondary btn--sm" onclick="toggleHistoryStep('${esc(s.id)}')">Details</button></td>
    </tr>
    <tr id="hist-step-${esc(s.id)}" style="display:none"><td colspan="7">
      <div class="panel" style="margin:8px 0">
        ${s.error ? `<div style="color:var(--red);margin-bottom:8px"><b>Error:</b> ${esc(s.error)}</div>` : ''}
        ${s.output ? `<details><summary style="cursor:pointer;color:var(--dim)">Step output</summary><pre class="mono" style="white-space:pre-wrap;max-height:280px;overflow:auto;margin-top:8px">${esc(prettyJSON(s.output))}</pre></details>` : '<span class="empty">No output recorded</span>'}
        <div id="hist-tools-${esc(s.id)}" style="margin-top:10px"></div>
      </div>
    </td></tr>`).join('');

    el.innerHTML = `
      <div class="section">
        <div class="section-title" style="justify-content:space-between">
          <span><span class="clickable" onclick="router.go('history')">History</span> / ${esc(run.name)} ${phaseHTML(run.phase)}</span>
          <span class="mono" style="color:var(--dim);font-size:12px">${esc(run.id)}</span>
        </div>
      </div>
      <div class="cards" style="margin-bottom:24px">
        <div class="card card--blue"><div class="card-value">${run.completedSteps}/${run.totalSteps}</div><div class="card-label">Steps</div></div>
        <div class="card card--purple"><div class="card-value">${run.startTime && run.completionTime ? calcDuration(run.startTime, run.completionTime) : '-'}</div><div class="card-label">Duration</div></div>
        <div class="card card--green"><div class="card-value">${esc(run.namespace)}</div><div class="card-label">Namespace</div></div>
      </div>
      ${run.message ? `<div class="panel" style="margin-bottom:20px;color:var(--dim)">${esc(run.message)}</div>` : ''}
      ${run.parameters ? `<details style="margin-bottom:20px"><summary style="cursor:pointer;color:var(--dim)">Parameters</summary><pre class="mono" style="margin-top:8px">${esc(prettyJSON(run.parameters))}</pre></details>` : ''}
      ${steps.length ? `<table><thead><tr><th>Step</th><th>Agent</th><th>Phase</th><th>Tokens in/out</th><th>Cost</th><th>Duration</th><th></th></tr></thead><tbody>${stepsHTML}</tbody></table>`
        : '<div class="empty">No step executions recorded for this run.</div>'}
    `;
  });
}

function toggleHistoryStep(stepId) {
  const row = document.getElementById('hist-step-' + stepId);
  if (!row) return;
  const showing = row.style.display !== 'none';
  row.style.display = showing ? 'none' : '';
  if (!showing) loadHistoryTools(stepId);
}

function loadHistoryTools(stepId) {
  const el = document.getElementById('hist-tools-' + stepId);
  if (!el || el.dataset.loaded) return;
  el.dataset.loaded = '1';
  fetch('/api/history/step/' + encodeURIComponent(stepId) + '/tools')
    .then(r => (r.ok ? r.json() : []))
    .then(tools => {
      tools = tools || [];
      if (!tools.length) { el.innerHTML = '<span style="color:var(--dim);font-size:12px">No tool calls recorded</span>'; return; }
      el.innerHTML = `<div style="font-weight:600;margin-bottom:6px">Tool calls <span class="badge">${tools.length}</span></div>
        <table><thead><tr><th>Tool</th><th>Server</th><th>Input</th><th>Result</th><th>Elapsed</th></tr></thead><tbody>
        ${tools.map(t => `<tr>
          <td class="mono">${esc(t.toolName)}</td>
          <td>${esc(t.toolServer || '-')}</td>
          <td class="mono" style="max-width:320px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(t.inputPreview || '-')}</td>
          <td class="mono">${t.resultBytes || 0} B</td>
          <td class="mono">${t.elapsedMs || 0} ms</td>
        </tr>`).join('')}
        </tbody></table>`;
    });
}

// prettyJSON re-indents a JSON string for display; returns it unchanged when
// it is not valid JSON (plain-text outputs).
function prettyJSON(s) {
  try { return JSON.stringify(JSON.parse(s), null, 2); } catch (e) { return s; }
}
