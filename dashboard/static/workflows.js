// Purko Dashboard — Tab 3: Workflows

let wfBuilderSteps = [];
let wfBuilderStepId = 0;
let wfBuilderOpen = false;

function init_workflows(detail) {
  wfBuilderOpen = false;
  if (detail && detail.name) {
    viewWorkflow(detail.name);
    return;
  }
  if (state.data) render_workflows(state.data);
  else fetch('/api/overview').then(r => r.json()).then(d => { state.data = d; render_workflows(d); });
}

function update_workflows(d) {
  // Don't overwrite if viewing a detail, builder is open, or intent input has focus/text
  if (state.detail) return;
  const intentInput = document.getElementById('intent-input');
  if (intentInput && (intentInput === document.activeElement || intentInput.value.trim())) return;
  if (wfBuilderOpen) return;
  render_workflows(d);
}

function render_workflows(d) {
  const wfs = d.workflows || [];
  const el = document.getElementById('view-workflows');

  // Group by repository
  const groups = {};
  for (const w of wfs) {
    const repo = w.repository || 'general';
    if (!groups[repo]) groups[repo] = [];
    groups[repo].push(w);
  }

  // Sort groups: repos first (alphabetically), 'general' last
  const sortedGroups = Object.keys(groups).sort((a, b) => {
    if (a === 'general') return 1;
    if (b === 'general') return -1;
    return a.localeCompare(b);
  });

  let rows = '';
  for (const repo of sortedGroups) {
    const repoWfs = groups[repo];
    const repoIcon = repo === 'general' ? '\u{2699}' : '\u{1F4C1}';
    const repoLabel = repo === 'general' ? 'General' : repo;
    rows += `<tr class="group-header"><td colspan="6"><span class="group-label">${repoIcon} ${repoLabel}</span><span class="group-count">${repoWfs.length}</span></td></tr>`;

    for (const w of repoWfs) {
      const hasApproval = (w.steps || []).some(s => s.phase === 'Pending');
      rows += `<tr>
        <td>
          <span class="clickable" onclick="viewWorkflow('${w.name}')">${w.name}</span>
          ${w.triggerType ? `<span class="tag tag--amber" style="font-size:9px;margin-left:6px">${w.triggerSource || w.triggerType}</span>` : ''}
          ${hasApproval && w.phase === 'Running' ? '<span class="tag tag--amber" style="font-size:9px;margin-left:6px;animation:pulse-glow 2s infinite">APPROVAL NEEDED</span>' : ''}
        </td>
        <td>${phaseHTML(w.phase)}</td>
        <td>${w.completedSteps}/${w.totalSteps}</td>
        <td class="mono">${w.duration || '-'}</td>
        <td class="mono">${shortAge(w.age)}</td>
        <td style="display:flex;gap:6px">
          ${w.phase === 'Succeeded' || w.phase === 'Failed' ? `<button class="btn btn--rerun" onclick="rerunWorkflow('${w.name}')">re-run</button>` : ''}
          <button class="btn btn--danger" onclick="deleteWorkflow('${w.name}')">delete</button>
        </td>
      </tr>`;
    }
  }

  // Intent bar is Pro (Spec 28): in community builds the /api/intent handler
  // is not compiled in, so render an upgrade teaser instead of a dead control.
  const intentBarHTML = hasFeature('intent') ? `
    <div class="intent-bar">
      <span style="font-size:20px;flex-shrink:0">&#x1f4a1;</span>
      <input id="intent-input" type="text" placeholder="Describe what you need... e.g. &quot;Investigate pod crashes in production&quot;" spellcheck="false">
      <button class="intent-submit" onclick="processIntent()">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
      </button>
    </div>` : upgradeCard('Intent Bar', 'Design workflows from natural language — describe what you need and deploy in one click.');

  el.innerHTML = `
    ${intentBarHTML}
    <div id="intent-result"></div>

    <div class="section">
      <div class="section-title" style="justify-content:space-between">
        <span>Workflows <span class="badge">${wfs.length}</span></span>
        <button class="btn btn--primary btn--sm" onclick="showWorkflowBuilder()">+ Create Workflow</button>
      </div>
      <table>
        <thead><tr><th>Name</th><th>Phase</th><th>Steps</th><th>Duration</th><th>Age</th><th></th></tr></thead>
        <tbody>${rows || '<tr><td colspan="6" class="empty">No workflows</td></tr>'}</tbody>
      </table>
    </div>
    <div id="wf-detail-container"></div>
  `;

  // Re-attach enter key (only exists when the intent feature is available)
  const inp = document.getElementById('intent-input');
  if (inp) inp.addEventListener('keydown', e => { if (e.key === 'Enter') processIntent(); });
}

// Re-render the open workflow detail when a step or the workflow itself
// changes phase (driven by SSE). Transition-only: no flicker while nothing
// changes, and the open logs panel is restored after re-render.
let _wfDetailSnapshot = '';

function wfSnapshot(phase, steps) {
  return phase + '|' + (steps || []).map(s => s.name + ':' + s.phase).sort().join(',');
}

function update_workflows_detail(d) {
  if (!state.detail || !state.detail.name) return;
  const w = (d.workflows || []).find(x => x.name === state.detail.name);
  if (!w) return;
  const snap = wfSnapshot(w.phase, w.steps);
  if (snap === _wfDetailSnapshot) return;
  _wfDetailSnapshot = snap;
  viewWorkflow(state.detail.name);
}

function viewWorkflow(name) {
  state.detail = { type: 'workflow', name };
  const hash = '#/workflows/' + encodeURIComponent(name);
  if (location.hash !== hash) history.replaceState(null, '', hash);
  fetch('/api/workflow/' + name).then(r => r.json()).then(d => {
    const w = d.workflow;
    const steps = w.spec.steps || [];
    const statuses = {};
    (w.status.stepStatuses || []).forEach(s => statuses[s.name] = s);

    // DAG
    const wfName = w.metadata.name;
    let dagHTML = '<div class="dag-flow">';
    const groups = buildDAGGroups(steps);
    for (let gi = 0; gi < groups.length; gi++) {
      const g = groups[gi];
      if (gi > 0) dagHTML += '<div class="dag-arrow">&rarr;</div>';
      if (g.length > 1) {
        dagHTML += '<div class="dag-parallel">';
        for (const s of g) dagHTML += dagNode(s, statuses[s.name], wfName);
        dagHTML += '</div>';
      } else {
        dagHTML += dagNode(g[0], statuses[g[0].name], wfName);
      }
    }
    dagHTML += '</div>';

    // Approval banner — check if any step needs approval
    let approvalBanner = '';
    const pendingApprovals = Object.values(statuses).filter(s => s.phase === 'Pending' && s.error && s.error.includes('approval'));
    if (pendingApprovals.length > 0) {
      approvalBanner = `<div class="result result--err" style="margin-bottom:16px;border-color:var(--amber);background:var(--amber-glow);color:var(--amber)">
        <strong>Approval Required</strong> — ${pendingApprovals.length} step(s) waiting for human approval:
        ${pendingApprovals.map(s => `<span style="margin-left:8px"><button class="btn btn--primary btn--sm" onclick="approveStep('${wfName}','${s.name}')">Approve ${s.name}</button> <button class="btn btn--danger" onclick="denyStep('${wfName}','${s.name}')">Deny</button></span>`).join('')}
      </div>`;
    }

    // Outputs — store for follow-up
    window._workflowOutputs = d.outputs || {};
    let outputHTML = '';
    if (d.outputs && Object.keys(d.outputs).length > 0) {
      outputHTML = '<div style="margin-top:16px"><h3>Step Outputs</h3>';
      for (const [k, v] of Object.entries(d.outputs)) {
        let content = '';
        let responseText = '';
        try {
          const parsed = JSON.parse(v);
          if (parsed.response) {
            content = `<div class="report-content">${renderMarkdown(parsed.response)}</div>`;
            responseText = parsed.response;
          } else {
            content = `<div class="output-box">${esc(JSON.stringify(parsed, null, 2))}</div>`;
            responseText = JSON.stringify(parsed, null, 2);
          }
        } catch (e) {
          content = `<div class="output-box">${esc(v)}</div>`;
          responseText = v;
        }
        const escapedStep = esc(k);
        // Follow-up designs a new workflow via /api/intent — Pro only (Spec 28).
        const followUpBtn = hasFeature('intent')
          ? `<button class="btn btn--sm btn--outline" style="margin-left:8px" onclick="event.stopPropagation(); openFollowUp('${escapedStep}')">Follow-up</button>`
          : '';
        outputHTML += `<details><summary class="mono clickable" style="padding:6px 0">${escapedStep}
          ${followUpBtn}
        </summary>${content}</details>`;
      }
      outputHTML += '</div>';
    }

    const el = document.getElementById('view-workflows');
    el.innerHTML = `
      <span class="back-link" onclick="closeStepLogs();router.go('workflows')">&larr; Back to workflows</span>
      <div class="wf-split" id="wf-split">
        <div class="wf-main">
          <div class="panel">
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
              <h3>${w.metadata.name} ${phaseHTML(w.status.phase)}</h3>
              <div style="display:flex;gap:8px">
                ${w.status.phase === 'Succeeded' || w.status.phase === 'Failed' ? `<button class="btn btn--rerun" onclick="showRerunForm('${w.metadata.name}')">Re-run</button>` : ''}
                <button class="btn btn--secondary btn--sm" onclick="downloadReport('${w.metadata.name}')">Download</button>
                <button class="btn btn--danger" onclick="deleteWorkflow('${w.metadata.name}')">Delete</button>
              </div>
            </div>

            <div class="detail-grid">
              <div class="label">Steps</div><div>${(w.status.completedSteps||0)}/${w.status.totalSteps}</div>
              <div class="label">Duration</div><div class="mono">${w.status.completionTime && w.status.startTime ? calcDuration(w.status.startTime, w.status.completionTime) : '-'}</div>
              <div class="label">Strategy</div><div>${w.spec.failureStrategy || 'failFast'}</div>
              <div class="label">Parallelism</div><div>${w.spec.parallelism || 1}</div>
              ${w.spec.concurrency ? `<div class="label">Concurrency</div><div><span class="tag tag--purple">${w.spec.concurrency.policy}</span></div>` : ''}
              ${w.spec.parameters ? `<div class="label">Parameters</div><div class="tags">${Object.entries(w.spec.parameters).map(([k,v]) => `<span class="tag tag--dim">${k}=${v}</span>`).join(' ')} <button class="btn btn--sm btn--outline" style="margin-left:4px" onclick="showRerunForm('${w.metadata.name}')">Edit & Re-run</button></div>` : ''}
              ${w.spec.trigger && w.spec.trigger.schedule ? `<div class="label">Schedule</div><div class="mono">${w.spec.trigger.schedule.cron} ${w.spec.trigger.schedule.suspend ? '<span style="color:var(--amber)">(suspended)</span>' : '<span style="color:var(--green)">(active)</span>'}</div>` : ''}
            </div>

            <div class="trigger-box" style="margin-top:14px">
              <div class="trigger-box-label">Webhook Trigger URL</div>
              <div class="trigger-url-row">
                <code class="trigger-url">POST ${window.location.origin}/api/trigger/${w.metadata.namespace}/${w.metadata.name}</code>
                <button class="btn btn--sm btn--outline" onclick="navigator.clipboard.writeText('${window.location.origin}/api/trigger/${w.metadata.namespace}/${w.metadata.name}').then(()=>alert('Copied!'))">Copy</button>
              </div>
            </div>

            ${approvalBanner}
            <div id="rerun-form-container"></div>
            <div style="margin-top:16px"><h3>Execution DAG <span style="font-size:11px;color:var(--dim);font-weight:400">— click a step to view logs</span></h3></div>
            <div class="dag">${dagHTML}</div>
            ${outputHTML}
          </div>
        </div>
        <div id="logs-panel-container"></div>
      </div>
    `;

    // Store workflow data for rerun form
    window._lastViewedWorkflow = w;

    // Seed the SSE transition snapshot so the first event doesn't re-render
    _wfDetailSnapshot = wfSnapshot(w.status.phase, w.status.stepStatuses);

    // Restore the logs panel a re-render closed
    if (window._openLogs && window._openLogs.workflowName === name) {
      openStepLogs(window._openLogs.workflowName, window._openLogs.stepName, window._openLogs.agentName);
    }
  });
}

function dagNode(step, status, workflowName) {
  const ss = status || {};
  let dur = '';
  if (ss.startTime && ss.completionTime) dur = calcDuration(ss.startTime, ss.completionTime);
  const needsApproval = ss.phase === 'Pending' && ss.error && ss.error.includes('approval');
  const isClickable = ss.phase === 'Running' || ss.phase === 'Succeeded' || ss.phase === 'Failed';
  const clickHandler = isClickable && workflowName ? `onclick="openStepLogs('${workflowName}','${step.name}','${step.agentRef && step.agentRef.name ? step.agentRef.name : ''}')"` : '';
  const cursorStyle = isClickable ? 'cursor:pointer' : '';
  return `<div class="dag-node" style="${needsApproval ? 'border-color:var(--amber);border-width:2px;' : ''}${cursorStyle}" ${clickHandler}>
    <div class="node-name">${step.name}</div>
    <div>${phaseHTML(ss.phase)}</div>
    <div class="node-agent">${step.agentRef && step.agentRef.name ? step.agentRef.name : step.type || 'auto'}</div>
    ${step.condition ? `<div style="font-size:10px;color:var(--amber);margin-top:2px">if: ${esc(step.condition).substring(0,40)}</div>` : ''}
    ${dur ? `<div class="node-dur">${dur}</div>` : ''}
    ${isClickable && !needsApproval ? `<div style="font-size:9px;color:var(--accent);margin-top:4px">click for logs</div>` : ''}
    ${needsApproval && workflowName ? `<div style="display:flex;gap:4px;margin-top:8px"><button class="btn btn--primary btn--sm" style="flex:1" onclick="event.stopPropagation();approveStep('${workflowName}','${step.name}')">Approve</button><button class="btn btn--danger" style="flex:1" onclick="event.stopPropagation();denyStep('${workflowName}','${step.name}')">Deny</button></div>` : ''}
  </div>`;
}

function approveStep(workflowName, stepName) {
  if (!confirm(`Approve step "${stepName}" in workflow "${workflowName}"?`)) return;
  fetch(`/api/approve/${workflowName}/${stepName}`, { method: 'POST' })
    .then(r => r.json())
    .then(d => {
      if (d.error) alert('Error: ' + d.error);
      else {
        alert(`Step "${stepName}" approved!`);
        setTimeout(() => viewWorkflow(workflowName), 2000);
      }
    });
}

// ── Live Logs Panel ─────────────────────────────────────────────────

let logsPollingInterval = null;

function openStepLogs(workflowName, stepName, agentName) {
  // Show split layout
  const split = document.getElementById('wf-split');
  if (split) split.classList.add('has-logs');

  const container = document.getElementById('logs-panel-container');
  container.innerHTML = `<div class="logs-panel">
    <div class="logs-header">
      <div>
        <div class="logs-header-title">${stepName}</div>
        <div class="logs-header-meta">${agentName || 'unknown agent'}</div>
      </div>
      <button class="logs-header-close" onclick="closeStepLogs()">&times;</button>
    </div>
    <div class="logs-body" id="logs-body">
      <div class="logs-spinner"><span class="dot"></span> Loading logs...</div>
    </div>
  </div>`;

  // Start polling
  window._openLogs = { workflowName, stepName, agentName };
  fetchStepLogs(workflowName, stepName);
  if (logsPollingInterval) clearInterval(logsPollingInterval);
  logsPollingInterval = setInterval(() => fetchStepLogs(workflowName, stepName), 3000);
}

function closeStepLogs() {
  window._openLogs = null;
  if (logsPollingInterval) { clearInterval(logsPollingInterval); logsPollingInterval = null; }
  const split = document.getElementById('wf-split');
  if (split) split.classList.remove('has-logs');
  const container = document.getElementById('logs-panel-container');
  if (container) container.innerHTML = '';
}

function fetchStepLogs(workflowName, stepName) {
  fetch(`/api/logs/${workflowName}/${stepName}`)
    .then(r => r.json())
    .then(d => {
      const body = document.getElementById('logs-body');
      if (!body) return;

      const lines = d.lines || [];
      const status = d.status || 'unknown';

      // No pods — fall back to tool_call_log from step output
      if (status === 'no pods' || status === 'no job') {
        if (logsPollingInterval) { clearInterval(logsPollingInterval); logsPollingInterval = null; }
        renderLogsFromOutput(workflowName, stepName, body);
        return;
      }

      // Parse and render formatted log entries
      body.innerHTML = renderLogEntries(lines, status);
      body.scrollTop = body.scrollHeight;

      // Stop polling if complete
      if (status === 'complete' || status === 'failed') {
        if (logsPollingInterval) { clearInterval(logsPollingInterval); logsPollingInterval = null; }
      }
    })
    .catch(() => {});
}

function renderLogsFromOutput(workflowName, stepName, body) {
  // Read tool_call_log from the step output in the outputs ConfigMap
  fetch('/api/workflow/' + workflowName).then(r => r.json()).then(d => {
    const outputs = d.outputs || {};
    const raw = outputs[stepName];
    if (!raw) {
      body.innerHTML = `<div style="color:var(--dim);text-align:center;padding:20px">
        <div style="font-size:16px;margin-bottom:8px">No logs available</div>
        <div style="font-size:11px">Pod was cleaned up and no output was captured.</div>
      </div>`;
      return;
    }

    let parsed;
    try { parsed = JSON.parse(raw); } catch (e) {
      body.innerHTML = `<div style="color:var(--dim);padding:12px">Raw output (no structured log):<br><pre style="margin-top:8px;font-size:10px">${esc(raw).substring(0, 2000)}</pre></div>`;
      return;
    }

    const toolLog = parsed.tool_call_log || [];
    const toolsCalled = parsed.tools_called || [];
    const metrics = parsed._metrics || {};

    if (toolLog.length === 0 && toolsCalled.length === 0) {
      body.innerHTML = `<div style="color:var(--dim);text-align:center;padding:20px">
        <div style="font-size:14px;margin-bottom:8px">Step completed (no tool call log)</div>
        <div style="font-size:11px">This step ran before per-tool logging was added, or used no tools.</div>
      </div>`;
      return;
    }

    let html = '<div style="font-size:10px;color:var(--amber);margin-bottom:12px;padding:4px 8px;background:var(--amber-glow);border-radius:4px">Reconstructed from step output (pod logs expired)</div>';

    // Render from tool_call_log (structured)
    if (toolLog.length > 0) {
      for (let i = 0; i < toolLog.length; i++) {
        const entry = toolLog[i];
        if (entry.status === 'blocked') {
          html += `<div class="log-blocked">Blocked: ${esc(entry.tool)} — ${esc(entry.reason || '')}</div>`;
          continue;
        }
        html += `<div class="log-tool-call">
          <div class="log-tool-header">
            <span class="log-tool-num">#${i + 1}</span>
            <span class="log-tool-name">${esc(entry.tool)}</span>
            <span class="log-tool-server">${esc(entry.server || '?')}</span>
          </div>
          ${entry.input_preview ? `<div class="log-tool-input">${esc(entry.input_preview)}</div>` : ''}
        </div>`;
        html += `<div class="log-tool-result">
          <div class="log-result-status"><span class="ok">&#10003;</span> ${esc(entry.tool)} <span class="dur">${entry.elapsed_s || '?'}s</span> <span class="dur">${entry.result_bytes || '?'} bytes</span></div>
        </div>`;
      }
    } else {
      // Fallback: just list tools_called
      html += '<div style="margin-bottom:8px">';
      for (const tool of toolsCalled) {
        html += `<div class="log-tool-call"><div class="log-tool-header"><span class="log-tool-name">${esc(tool)}</span></div></div>`;
      }
      html += '</div>';
    }

    // Metrics footer
    if (metrics.cost_usd || metrics.tokens_in) {
      html += `<div class="log-iteration" style="border-color:var(--green)">
        <span style="color:var(--green)">&#10003; Completed</span>
        ${metrics.cost_usd ? `<span class="log-cost">$${metrics.cost_usd.toFixed(4)}</span>` : ''}
        ${metrics.tokens_in ? `<span>${metrics.tokens_in}in/${metrics.tokens_out || 0}out</span>` : ''}
      </div>`;
    }

    body.innerHTML = html;
  }).catch(() => {
    body.innerHTML = `<div style="color:var(--dim);text-align:center;padding:20px">Failed to load step output</div>`;
  });
}

function renderLogEntries(lines, status) {
  let html = '';
  let currentIteration = 0;

  for (const line of lines) {
    // TOOL_CALL line
    const tcMatch = line.match(/TOOL_CALL #(\d+): (\S+) \(server: (\S+)\) input: (.+)/);
    if (tcMatch) {
      const [, num, tool, server, inputRaw] = tcMatch;
      // Parse key params from input
      let inputDisplay = '';
      try {
        const inp = JSON.parse(inputRaw);
        const keys = Object.keys(inp).slice(0, 3);
        inputDisplay = keys.map(k => `<strong>${k}:</strong> ${esc(String(inp[k]).substring(0, 80))}`).join('<br>');
      } catch (e) {
        inputDisplay = esc(inputRaw.substring(0, 120));
      }
      html += `<div class="log-tool-call">
        <div class="log-tool-header">
          <span class="log-tool-num">#${num}</span>
          <span class="log-tool-name">${tool}</span>
          <span class="log-tool-server">${server}</span>
        </div>
        ${inputDisplay ? `<div class="log-tool-input">${inputDisplay}</div>` : ''}
      </div>`;
      continue;
    }

    // TOOL_RESULT line
    const trMatch = line.match(/TOOL_RESULT #(\d+): (\S+) \((\d+\.\d+)s\) result: (.+)/);
    if (trMatch) {
      const [, num, tool, dur, resultRaw] = trMatch;
      const preview = esc(resultRaw.substring(0, 150));
      html += `<div class="log-tool-result">
        <div class="log-result-status"><span class="ok">&#10003;</span> ${tool} <span class="dur">${dur}s</span></div>
        <div class="log-result-preview">${preview}</div>
      </div>`;
      continue;
    }

    // TOOL_BLOCKED line
    if (line.includes('TOOL_BLOCKED:')) {
      const msg = line.replace(/.*TOOL_BLOCKED:\s*/, '');
      html += `<div class="log-blocked">&#10007; ${esc(msg)}</div>`;
      continue;
    }

    // Iteration + Cost line
    const iterMatch = line.match(/Iteration (\d+), tool calls so far: (\d+)/);
    if (iterMatch) {
      currentIteration = parseInt(iterMatch[1]);
      continue; // will be combined with cost line
    }

    const costMatch = line.match(/Cost: \$([0-9.]+) \(\+\$([0-9.]+)\), tokens: (\d+)in\/(\d+)out/);
    if (costMatch) {
      const [, total, delta, tokIn, tokOut] = costMatch;
      html += `<div class="log-iteration">
        <span>Iteration ${currentIteration || '?'}</span>
        <span class="log-cost">$${total}</span>
        <span>+$${delta}</span>
        <span>${tokIn}in/${tokOut}out</span>
      </div>`;
      continue;
    }

    // Max tool calls warning
    if (line.includes('Max tool calls')) {
      html += `<div class="log-blocked">${esc(line.replace(/.*WARNING\s*/, ''))}</div>`;
      continue;
    }

    // Step completed line
    if (line.includes('Step completed:')) {
      html += `<div class="log-iteration" style="border-color:var(--green)"><span style="color:var(--green)">&#10003; ${esc(line.replace(/.*INFO\s*/, ''))}</span></div>`;
      continue;
    }

    // Executor ERROR lines — surface the actual failure reason
    if (/\sERROR\s/.test(line)) {
      html += `<div class="log-blocked">&#10007; ${esc(line.replace(/.*ERROR\s*/, ''))}</div>`;
      continue;
    }

    // Final OUTPUT json — show its error field if the step failed
    if (line.startsWith('OUTPUT:')) {
      try {
        const out = JSON.parse(line.slice(7));
        if (out.error) html += `<div class="log-blocked">&#10007; ${esc(String(out.error))}</div>`;
      } catch (e) { /* non-JSON output — nothing to surface */ }
      continue;
    }
  }

  // Status footer
  if (status === 'running') {
    html += `<div class="logs-spinner"><span class="dot"></span> Running...</div>`;
  } else if (status === 'complete') {
    html += `<div class="log-iteration" style="border-color:var(--green)"><span style="color:var(--green)">&#10003; Step completed</span></div>`;
  } else if (status === 'failed') {
    html += `<div class="log-iteration" style="border-color:var(--red)"><span style="color:var(--red)">&#10007; Step failed</span></div>`;
  }

  if (!html) {
    html = `<div class="logs-spinner"><span class="dot"></span> Waiting for logs...</div>`;
  }

  return html;
}

function openFollowUp(stepName) {
  const outputs = window._workflowOutputs || {};
  const raw = outputs[stepName] || '';
  let contextText = '';
  try {
    const parsed = JSON.parse(raw);
    contextText = parsed.response || JSON.stringify(parsed, null, 2);
  } catch (e) {
    contextText = raw;
  }

  // Truncate context for the intent (keep first 2000 chars for the LLM)
  const contextPreview = contextText.substring(0, 500);
  const contextFull = contextText.substring(0, 2000);

  // Store for submission
  window._followUpContext = contextFull;
  window._followUpStep = stepName;

  const container = document.getElementById('rerun-form-container');
  if (!container) return;

  container.innerHTML = `<div class="panel" style="margin-top:16px;border-color:var(--purple)">
    <h3>Follow-up on "${stepName}" output</h3>
    <div style="margin:12px 0">
      <div style="font-size:11px;color:var(--dim);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:6px">Context from step output</div>
      <div class="output-box" style="max-height:150px;font-size:11px">${esc(contextPreview)}${contextText.length > 500 ? '\n...(truncated)' : ''}</div>
    </div>
    <div style="margin:12px 0">
      <div style="font-size:11px;color:var(--dim);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:6px">What do you want to do with this?</div>
      <input id="followup-intent" type="text" style="width:100%;background:var(--inset-deep);border:1px solid var(--border);border-radius:var(--radius-xs);padding:10px 14px;color:var(--text);font-size:14px;font-family:var(--font)" placeholder="e.g. Create GitHub issues for each finding, Fix these bugs, Send summary to Slack..." spellcheck="false" autofocus>
    </div>
    <div class="form-actions">
      <button class="btn btn--primary" onclick="submitFollowUp()">Design Workflow</button>
      <button class="btn btn--secondary" onclick="document.getElementById('rerun-form-container').innerHTML=''">Cancel</button>
    </div>
    <div id="followup-result"></div>
  </div>`;

  // Focus the input
  setTimeout(() => document.getElementById('followup-intent')?.focus(), 100);
}

function submitFollowUp() {
  const intent = document.getElementById('followup-intent')?.value?.trim();
  if (!intent) { showResult('followup-result', 'err', 'Describe what you want to do'); return; }

  const context = window._followUpContext || '';
  const stepName = window._followUpStep || 'unknown';

  // Combine context + intent for the LLM
  const fullIntent = `Follow-up action on "${stepName}" output.\n\nPrevious step output (context):\n${context}\n\nUser request: ${intent}`;

  showResult('followup-result', '', '<div style="color:var(--dim)">Designing workflow with Opus...</div>');

  fetch('/api/intent', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ intent: fullIntent }),
  })
    .then(r => r.json())
    .then(d => {
      const steps = d.suggestedSteps || [];
      const agents = d.suggestedAgents || [];

      let dagHTML = '<div style="display:flex;flex-wrap:wrap;gap:10px;align-items:center;margin:12px 0">';
      for (let i = 0; i < steps.length; i++) {
        if (i > 0) dagHTML += '<div class="dag-arrow">&rarr;</div>';
        const s = steps[i];
        const ag = agents.find(a => a.name === s.agent) || {};
        dagHTML += `<div class="dag-node" style="border-color:${ag.exists ? 'var(--green)' : 'var(--amber)'}">
          <div class="node-name">${s.name}</div>
          <div class="node-agent">${s.agent}</div>
        </div>`;
      }
      dagHTML += '</div>';

      document.getElementById('followup-result').innerHTML = `
        <div style="margin-top:12px">
          <h3 style="font-size:14px">${steps.length}-step follow-up workflow</h3>
          ${dagHTML}
          <div style="display:flex;gap:10px;margin-top:12px">
            <button class="btn btn--primary" onclick="deployFollowUp(${JSON.stringify(d).replace(/"/g, '&quot;')}, '${esc(intent)}')">Deploy</button>
            <button class="btn btn--secondary" onclick="document.getElementById('followup-result').innerHTML=''">Cancel</button>
          </div>
        </div>`;
    })
    .catch(e => showResult('followup-result', 'err', 'Error: ' + e));
}

function deployFollowUp(intentData, userIntent) {
  const steps = intentData.suggestedSteps || [];
  const context = window._followUpContext || '';

  const body = {
    name: 'followup-' + Date.now().toString(36),
    description: userIntent,
    parallelism: Math.min(steps.length, 3),
    strategy: 'continueOnError',
    steps: steps.map(s => ({
      name: s.name,
      agent: s.agent,
      type: '',
      dependsOn: s.dependsOn || [],
      input: userIntent + '\n\nContext from previous workflow:\n' + context,
    })),
  };

  fetch('/api/create/workflow', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
    .then(r => r.json())
    .then(d => {
      if (d.error) showResult('followup-result', 'err', 'Error: ' + d.error);
      else {
        showResult('followup-result', 'ok', `Follow-up workflow "${d.name}" deployed!`);
        setTimeout(() => { state.detail = null; viewWorkflow(d.name); }, 3000);
      }
    });
}

function showRerunForm(workflowName) {
  // Fetch the workflow to get current parameters
  fetch('/api/workflow/' + workflowName).then(r => r.json()).then(d => {
    const w = d.workflow;
    const params = w.spec.parameters || {};
    const container = document.getElementById('rerun-form-container');
    if (!container) {
      // If we're on the list view, navigate to detail first
      viewWorkflow(workflowName);
      setTimeout(() => showRerunForm(workflowName), 1000);
      return;
    }

    let fieldsHTML = '';
    for (const [key, value] of Object.entries(params)) {
      fieldsHTML += `<label>${key}</label><input id="rerun-param-${key}" value="${esc(value)}" spellcheck="false">`;
    }

    container.innerHTML = `<div class="panel" style="margin-top:16px;border-color:var(--accent)">
      <h3>Re-run with Parameters</h3>
      <p style="color:var(--dim);font-size:12px;margin-bottom:12px">Edit parameters below and launch a new run. The current workflow will be deleted and re-created.</p>
      <div class="form-grid">${fieldsHTML}</div>
      <div class="form-actions" style="margin-top:16px">
        <button class="btn btn--primary" onclick="executeRerun('${workflowName}')">Launch Re-run</button>
        <button class="btn btn--secondary" onclick="document.getElementById('rerun-form-container').innerHTML=''">Cancel</button>
      </div>
      <div id="rerun-result"></div>
    </div>`;
  });
}

function executeRerun(workflowName) {
  fetch('/api/workflow/' + workflowName).then(r => r.json()).then(d => {
    const params = (d.workflow.spec || {}).parameters || {};

    // Read updated parameter values from form
    const newParams = {};
    for (const key of Object.keys(params)) {
      const input = document.getElementById('rerun-param-' + key);
      newParams[key] = input ? input.value : params[key];
    }

    // Server-side rerun keeps the full spec (step input templates,
    // timeouts, retries) and only swaps the parameters — rebuilding the
    // spec here from the summary API loses step inputs.
    fetch('/api/rerun/workflow/' + workflowName, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ parameters: newParams }),
    }).then(r => r.json()).then(result => {
      if (result.error) {
        showResult('rerun-result', 'err', 'Re-run failed: ' + result.error);
      } else {
        showResult('rerun-result', 'ok', `Workflow "${workflowName}" re-launched with new parameters!`);
        setTimeout(() => viewWorkflow(workflowName), 3000);
      }
    });
  });
}

function denyStep(workflowName, stepName) {
  if (!confirm(`Deny step "${stepName}" in workflow "${workflowName}"? This will mark it as Failed.`)) return;
  fetch(`/api/deny/${workflowName}/${stepName}`, { method: 'POST' })
    .then(r => r.json())
    .then(d => {
      if (d.error) alert('Error: ' + d.error);
      else setTimeout(() => viewWorkflow(workflowName), 2000);
    });
}

function deleteWorkflow(name) {
  if (!confirm(`Delete workflow "${name}"?`)) return;
  fetch('/api/delete/workflow/' + name, { method: 'POST' })
    .then(r => r.json())
    .then(d => { if (d.error) alert('Error: ' + d.error); else { state.detail = null; init_workflows(); } });
}

function rerunWorkflow(name) {
  if (!confirm(`Re-run "${name}"?`)) return;
  fetch('/api/rerun/workflow/' + name, { method: 'POST' })
    .then(r => r.json())
    .then(d => {
      if (d.error) alert('Error: ' + d.error);
      else setTimeout(() => viewWorkflow(name), 3000);
    });
}

function downloadReport(name) {
  fetch('/api/workflow/' + name).then(r => r.json()).then(d => {
    const w = d.workflow;
    const statuses = {};
    (w.status.stepStatuses || []).forEach(s => statuses[s.name] = s);

    let md = `# Workflow Report: ${w.metadata.name}\n\n`;
    md += `**Phase:** ${w.status.phase}\n**Steps:** ${w.status.completedSteps||0}/${w.status.totalSteps}\n`;
    if (w.status.startTime && w.status.completionTime) md += `**Duration:** ${calcDuration(w.status.startTime, w.status.completionTime)}\n`;
    md += `\n---\n\n## Steps\n\n`;
    for (const step of (w.spec.steps || [])) {
      const ss = statuses[step.name] || {};
      md += `### ${step.name}\n- **Phase:** ${ss.phase || 'Unknown'}\n`;
      if (step.agentRef && step.agentRef.name) md += `- **Agent:** ${step.agentRef.name}\n`;
      if (ss.error) md += `- **Error:** ${ss.error}\n`;
      md += '\n';
    }
    if (d.outputs && Object.keys(d.outputs).length > 0) {
      md += `---\n\n## Outputs\n\n`;
      for (const [k, v] of Object.entries(d.outputs)) {
        md += `### ${k}\n\n\`\`\`json\n${v}\n\`\`\`\n\n`;
      }
    }
    md += `\n---\n*Generated by Purko at ${new Date().toISOString()}*\n`;

    const blob = new Blob([md], { type: 'text/markdown' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `${name}-report.md`;
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(url);
  });
}

// ── Intent Bar ──────────────────────────────────────────────────────

function processIntent() {
  const input = document.getElementById('intent-input').value.trim();
  if (!input) return;
  document.getElementById('intent-result').innerHTML = '<div class="result" style="color:var(--dim)">Analyzing intent...</div>';

  fetch('/api/intent', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ intent: input }) })
    .then(r => r.json())
    .then(d => {
      const steps = d.suggestedSteps || [];
      const agents = d.suggestedAgents || [];

      let dagHTML = '<div style="display:flex;flex-wrap:wrap;gap:10px;align-items:center;margin:16px 0">';
      for (let i = 0; i < steps.length; i++) {
        if (i > 0) dagHTML += '<div class="dag-arrow">&rarr;</div>';
        const s = steps[i];
        const ag = agents.find(a => a.name === s.agent) || {};
        dagHTML += `<div class="dag-node" style="border-style:${ag.exists ? 'solid' : 'dashed'};border-color:${ag.exists ? 'var(--green)' : 'var(--amber)'}">
          <div class="node-name">${s.name}</div>
          <div class="node-agent">${s.agent} ${ag.exists ? '(exists)' : '(new)'}</div>
        </div>`;
      }
      dagHTML += '</div>';

      const mode = d.mode === 'llm' ? 'AI-designed' : 'Pattern-matched';
      document.getElementById('intent-result').innerHTML = `<div class="panel" style="margin-top:0">
        <h3>${mode}: ${steps.length}-step workflow</h3>
        <p style="color:var(--dim);font-size:13px">"${esc(input)}"</p>
        ${dagHTML}
        <div style="display:flex;gap:10px;margin-top:16px">
          <button class="btn btn--primary" onclick="deployIntentWorkflow(${JSON.stringify(d).replace(/"/g, '&quot;')})">Deploy</button>
          <button class="btn btn--secondary" onclick="document.getElementById('intent-result').innerHTML=''">Cancel</button>
        </div>
      </div>`;
    });
}

function deployIntentWorkflow(intentData) {
  const steps = intentData.suggestedSteps || [];
  const body = {
    name: 'intent-' + Date.now().toString(36),
    description: intentData.intent,
    parallelism: Math.min(steps.length, 3),
    strategy: 'continueOnError',
    steps: steps.map(s => ({ name: s.name, agent: s.agent, type: '', dependsOn: s.dependsOn || [], input: intentData.intent })),
  };
  fetch('/api/create/workflow', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    .then(r => r.json())
    .then(d => {
      if (d.error) { document.getElementById('intent-result').innerHTML = `<div class="result result--err">Error: ${d.error}</div>`; }
      else {
        document.getElementById('intent-result').innerHTML = `<div class="result result--ok">Workflow "${d.name}" deployed!</div>`;
        document.getElementById('intent-input').value = '';
        setTimeout(() => viewWorkflow(d.name), 3000);
      }
    });
}

// ── Workflow Builder ────────────────────────────────────────────────

function showWorkflowBuilder() {
  wfBuilderOpen = true;
  const agents = state.data ? state.data.agents || [] : [];
  const groups = {};
  for (const a of agents) {
    const g = a.group || 'general';
    if (!groups[g]) groups[g] = [];
    groups[g].push(a);
  }

  let palette = '';
  for (const [g, items] of Object.entries(groups)) {
    palette += `<div style="font-size:10px;color:var(--dim);text-transform:uppercase;letter-spacing:0.5px;margin:8px 0 4px">${g}</div>`;
    for (const a of items) {
      palette += `<div class="palette-item" onclick="addBuilderStep('${a.name}')"><div class="palette-item-name">${a.name}</div><div class="palette-item-sub">${a.toolCount} tools</div></div>`;
    }
  }

  const el = document.getElementById('view-workflows');
  el.innerHTML = `
    <span class="back-link" onclick="init_workflows()">&larr; Back to workflows</span>
    <div class="section"><div class="section-title">Workflow Builder</div></div>
    <div class="builder">
      <div class="builder-palette">
        <div class="builder-palette-title">Available Agents</div>
        ${palette || '<div style="color:var(--dim);font-size:12px">No agents</div>'}
      </div>
      <div class="builder-canvas">
        <div style="display:flex;gap:12px;align-items:center;margin-bottom:16px;flex-wrap:wrap">
          <input id="wfb-name" placeholder="workflow-name" spellcheck="false" style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-xs);padding:6px 12px;color:var(--text);font-size:14px;font-weight:600;width:200px;font-family:var(--mono)">
          <div style="display:flex;gap:8px;align-items:center;margin-left:auto">
            <span style="font-size:10px;color:var(--dim);text-transform:uppercase">Parallelism</span>
            <input type="number" id="wfb-par" value="2" min="1" max="10" style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-xs);padding:4px 8px;color:var(--text);font-size:12px;width:50px;text-align:center">
            <span style="font-size:10px;color:var(--dim);text-transform:uppercase">Strategy</span>
            <select id="wfb-strategy" style="background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-xs);padding:4px 8px;color:var(--text);font-size:12px"><option value="continueOnError">Continue</option><option value="failFast">Fail Fast</option></select>
          </div>
        </div>
        <div style="margin-bottom:14px">
          <textarea id="wfb-task" rows="2" placeholder="Task — what should this workflow do? Passed to every step as its input." spellcheck="false" style="width:100%;background:var(--bg);border:1px solid var(--border);border-radius:var(--radius-xs);padding:8px 12px;color:var(--text);font-size:13px;font-family:var(--font);resize:vertical"></textarea>
        </div>
        <div id="wfb-blocks" class="builder-blocks"><div style="color:var(--dim);font-size:13px;margin:auto">Click agents to add steps</div></div>
        <div style="display:flex;gap:10px;margin-top:14px">
          <button class="btn btn--primary" onclick="deployBuilderWf()">Deploy Workflow</button>
          <button class="btn btn--secondary" onclick="wfBuilderSteps=[];wfBuilderStepId=0;renderBuilderBlocks()">Clear</button>
        </div>
        <div id="wfb-result"></div>
      </div>
    </div>
  `;
}

function addBuilderStep(agentName) {
  wfBuilderStepId++;
  const deps = wfBuilderSteps.length > 0 ? [wfBuilderSteps[wfBuilderSteps.length - 1].name] : [];
  wfBuilderSteps.push({ id: wfBuilderStepId, name: agentName.replace(/[^a-z0-9-]/g, '-'), agent: agentName, dependsOn: deps });
  renderBuilderBlocks();
}

function removeBuilderStep(id) {
  const removed = wfBuilderSteps.find(s => s.id === id);
  wfBuilderSteps = wfBuilderSteps.filter(s => s.id !== id);
  if (removed) wfBuilderSteps.forEach(s => s.dependsOn = s.dependsOn.filter(d => d !== removed.name));
  renderBuilderBlocks();
}

function renderBuilderBlocks() {
  const el = document.getElementById('wfb-blocks');
  if (!el) return;
  if (wfBuilderSteps.length === 0) { el.innerHTML = '<div style="color:var(--dim);font-size:13px;margin:auto">Click agents to add steps</div>'; return; }
  let html = '';
  for (let i = 0; i < wfBuilderSteps.length; i++) {
    const s = wfBuilderSteps[i];
    if (i > 0) html += '<div class="dag-arrow">&rarr;</div>';
    html += `<div class="builder-block">
      <button style="position:absolute;top:4px;right:6px;background:none;border:none;color:var(--dim);cursor:pointer;font-size:14px" onclick="removeBuilderStep(${s.id})">&times;</button>
      <div style="font-weight:600;font-size:13px;color:var(--text-bright)">${s.name}</div>
      <div style="font-size:11px;color:var(--dim);font-family:var(--mono);margin-top:2px">${s.agent}</div>
      <div style="font-size:10px;color:var(--purple);margin-top:4px">${s.dependsOn.length > 0 ? 'after: ' + s.dependsOn.join(', ') : 'no deps'}</div>
    </div>`;
  }
  el.innerHTML = html;
}

async function deployBuilderWf() {
  const name = document.getElementById('wfb-name').value.trim();
  if (!name) { showResult('wfb-result', 'err', 'Name required'); return; }
  if (wfBuilderSteps.length === 0) { showResult('wfb-result', 'err', 'Add at least one step'); return; }
  const task = document.getElementById('wfb-task').value.trim();
  if (!task) { showResult('wfb-result', 'err', 'Task required — agents need an input to act on'); return; }

  const body = {
    name,
    description: task,
    parallelism: parseInt(document.getElementById('wfb-par').value) || 2,
    strategy: document.getElementById('wfb-strategy').value,
    steps: wfBuilderSteps.map(s => ({ name: s.name, agent: s.agent, type: '', dependsOn: s.dependsOn, input: task })),
  };

  const resp = await fetch('/api/create/workflow', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const d = await resp.json();
  if (d.error) showResult('wfb-result', 'err', 'Error: ' + d.error);
  else {
    showResult('wfb-result', 'ok', `Workflow "${d.name}" deployed!`);
    setTimeout(() => viewWorkflow(d.name), 3000);
  }
}
