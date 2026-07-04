// Purko Dashboard — Tab 6: Settings

function init_settings() {
  render_settings();
}

function render_settings() {
  const el = document.getElementById('view-settings');
  el.innerHTML = `
    <div class="section">
      <div class="section-title">Autonomy Policy</div>
      <div id="settings-autonomy"><div class="empty" style="padding:20px">Loading...</div></div>
    </div>
    <div class="section">
      <div class="section-title">Presets</div>
      <div id="settings-presets"><div class="empty" style="padding:20px">Loading...</div></div>
    </div>
    <div class="section">
      <div class="section-title">Trigger Rules</div>
      <div id="settings-triggers"><div class="empty" style="padding:20px">Loading...</div></div>
    </div>
    <div class="section">
      <div class="section-title">Scheduled Workflows</div>
      <div id="settings-schedules"><div class="empty" style="padding:20px">Loading...</div></div>
    </div>
    <div class="section">
      <div class="section-title">Platform Info</div>
      <div id="settings-platform"></div>
    </div>
  `;

  loadAutonomyPolicy();
  loadPresets();
  loadTriggerRules();
  loadSettingsSchedules();
  loadPlatformInfo();
}

// ── Autonomy Policy ─────────────────────────────────────────────────

function loadAutonomyPolicy() {
  // Shu-Ha-Ri autonomy policy is Pro (Spec 28): in community builds the
  // /api/autonomy/policy handler is not compiled in. Per-agent autonomy
  // badges elsewhere stay — manual autonomy levels are a community feature.
  if (!hasFeature('autonomy')) {
    document.getElementById('settings-autonomy').innerHTML = upgradeCard(
      'Shu-Ha-Ri Autonomy Policy',
      'Automatic agent autonomy progression with promotion and demotion thresholds.'
    );
    return;
  }
  fetch('/api/autonomy/policy').then(r => r.json()).then(d => {
    const el = document.getElementById('settings-autonomy');
    if (d.error || !d.policy) {
      el.innerHTML = '<div class="empty" style="padding:20px">No autonomy policy found</div>';
      return;
    }
    const p = d.policy;
    const sp = p.spec || {};
    const shr = sp.shuHaRi || {};
    const pc = shr.progressionCriteria || {};
    const s2h = pc.shuToHa || {};
    const h2r = pc.haToRi || {};
    const rb = sp.rollback || {};

    el.innerHTML = `<div class="panel" style="margin-top:0">
      <div class="detail-grid">
        <div class="label">Name</div><div class="mono">${p.metadata.name}</div>
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:16px">
        <div>
          <h3 style="font-size:14px;margin-bottom:8px">Shu &rarr; Ha</h3>
          <div class="detail-grid">
            <div class="label">Actions</div><div>${s2h.minimumActionsCompleted || '-'}</div>
            <div class="label">Success Rate</div><div>${s2h.minimumSuccessRate ? Math.round(s2h.minimumSuccessRate * 100) + '%' : '-'}</div>
            <div class="label">Days</div><div>${s2h.minimumDaysInLevel || '-'}</div>
          </div>
        </div>
        <div>
          <h3 style="font-size:14px;margin-bottom:8px">Ha &rarr; Ri</h3>
          <div class="detail-grid">
            <div class="label">Actions</div><div>${h2r.minimumActionsCompleted || '-'}</div>
            <div class="label">Success Rate</div><div>${h2r.minimumSuccessRate ? Math.round(h2r.minimumSuccessRate * 100) + '%' : '-'}</div>
            <div class="label">Days</div><div>${h2r.minimumDaysInLevel || '-'}</div>
          </div>
        </div>
      </div>
      ${rb.enabled ? `<div class="detail-grid" style="margin-top:12px">
        <div class="label">Rollback</div><div class="tags">
          ${rb.triggerConditions && rb.triggerConditions.successRateBelow ? `<span class="tag tag--red">rate &lt; ${Math.round(rb.triggerConditions.successRateBelow * 100)}%</span>` : ''}
          ${rb.triggerConditions && rb.triggerConditions.consecutiveFailures ? `<span class="tag tag--red">${rb.triggerConditions.consecutiveFailures} consecutive fails</span>` : ''}
          <span class="tag tag--amber">to ${rb.rollbackLevel || 'shu'}</span>
        </div>
      </div>` : ''}
    </div>`;
  }).catch(() => {
    document.getElementById('settings-autonomy').innerHTML = '<div class="empty" style="padding:20px">Failed to load autonomy policy</div>';
  });
}

// ── Presets ──────────────────────────────────────────────────────────

function loadPresets() {
  fetch('/api/presets').then(r => r.json()).then(d => {
    const presets = d.presets || [];
    const el = document.getElementById('settings-presets');
    if (presets.length === 0) {
      el.innerHTML = '<div class="empty" style="padding:20px">No presets configured</div>';
      return;
    }
    el.innerHTML = `<div class="preset-grid">
      ${presets.map(p => `<div class="preset-card" onclick="deployPreset(${JSON.stringify(p).replace(/"/g, '&quot;')})">
        <div class="preset-card-icon">${p.icon || '\u{2699}'}</div>
        <div class="preset-card-name">${esc(p.description || p.name)}</div>
        <div class="preset-card-meta">${p.type === 'workflow' ? (p.config.steps || []).length + ' steps' : (p.config.tools || []).length + ' tools'}</div>
      </div>`).join('')}
    </div>
    <div id="preset-result"></div>`;
  });
}

function deployPreset(preset) {
  const endpoint = preset.type === 'agent' ? '/api/create/agent' : '/api/create/workflow';
  fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(preset.config) })
    .then(r => r.json())
    .then(d => {
      if (d.error) showResult('preset-result', 'err', 'Error: ' + d.error);
      else showResult('preset-result', 'ok', `${preset.type === 'agent' ? 'Agent' : 'Workflow'} "${d.name}" deployed!`);
    });
}

// ── Trigger Rules ───────────────────────────────────────────────────

let settingsRules = [];

function loadTriggerRules() {
  fetch('/api/trigger/rules').then(r => r.json()).then(d => {
    settingsRules = d.rules || [];
    renderSettingsRules();
  });
}

function renderSettingsRules() {
  const el = document.getElementById('settings-triggers');
  const icons = { pagerduty: '\u{1F6A8}', github: '\u{1F419}', slack: '\u{1F4AC}', '*': '\u{1F310}' };

  if (settingsRules.length === 0) {
    el.innerHTML = '<div class="empty" style="padding:20px">No trigger rules</div><button class="btn btn--sm btn--outline" onclick="addSettingsRule()" style="margin-top:12px">+ Add Rule</button>';
    return;
  }

  let html = `<table><thead><tr><th>Rule</th><th>Source</th><th>Match</th><th>Workflow</th><th></th></tr></thead><tbody>`;
  for (let i = 0; i < settingsRules.length; i++) {
    const r = settingsRules[i];
    const icon = icons[r.source] || '\u{1F4E1}';
    const matchStr = Object.entries(r.match || {}).map(([k,v]) => `${k}=${v}`).join(', ') || '(any)';
    const wfDisplay = r.workflow === '_intent' ? '<span style="color:var(--purple)">LLM Auto-Design</span>' : `<span class="mono">${r.workflow}</span>`;
    html += `<tr>
      <td class="mono">${r.name}</td>
      <td>${icon} ${r.source}</td>
      <td class="mono" style="font-size:11px">${matchStr}</td>
      <td>${wfDisplay}</td>
      <td><button class="btn btn--danger" onclick="deleteSettingsRule(${i})">x</button></td>
    </tr>`;
  }
  html += '</tbody></table>';
  html += '<button class="btn btn--sm btn--outline" onclick="addSettingsRule()" style="margin-top:12px">+ Add Rule</button>';
  html += '<div id="rule-editor"></div>';
  el.innerHTML = html;
}

function addSettingsRule() {
  const edEl = document.getElementById('rule-editor');
  if (!edEl) return;
  edEl.innerHTML = `<div class="panel" style="margin-top:12px">
    <h3>Add Rule</h3>
    <div class="form-grid" style="margin-top:8px">
      <label>Name</label><input id="rule-name" placeholder="my-rule" spellcheck="false">
      <label>Source</label>
      <select id="rule-source"><option value="pagerduty">PagerDuty</option><option value="github">GitHub</option><option value="slack">Slack</option><option value="*">Any</option></select>
      <label>Match</label><input id="rule-match" placeholder="severity=critical" spellcheck="false">
      <label>Workflow</label><input id="rule-wf" placeholder="_intent or workflow-name" spellcheck="false" value="_intent">
    </div>
    <div class="form-actions">
      <button class="btn btn--primary" onclick="saveNewRule()">Save</button>
      <button class="btn btn--secondary" onclick="document.getElementById('rule-editor').innerHTML=''">Cancel</button>
    </div>
  </div>`;
}

function saveNewRule() {
  const name = document.getElementById('rule-name').value.trim();
  if (!name) { alert('Name required'); return; }
  const matchStr = document.getElementById('rule-match').value.trim();
  const match = {};
  if (matchStr) matchStr.split(',').forEach(p => { const [k, v] = p.split('=').map(s => s.trim()); if (k && v) match[k] = v; });

  settingsRules.push({
    name,
    source: document.getElementById('rule-source').value,
    match,
    workflow: document.getElementById('rule-wf').value.trim() || '_intent',
  });
  saveTriggerRules();
}

function deleteSettingsRule(i) {
  if (!confirm(`Delete rule "${settingsRules[i].name}"?`)) return;
  settingsRules.splice(i, 1);
  saveTriggerRules();
}

function saveTriggerRules() {
  fetch('/api/trigger/rules', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settingsRules) })
    .then(r => r.json())
    .then(d => {
      if (d.error) alert('Error: ' + d.error);
      else renderSettingsRules();
    });
}

// ── Schedules ───────────────────────────────────────────────────────

function loadSettingsSchedules() {
  fetch('/api/schedules').then(r => r.json()).then(d => {
    const schedules = d.schedules || [];
    const el = document.getElementById('settings-schedules');
    if (schedules.length === 0) {
      el.innerHTML = '<div class="empty" style="padding:20px">No scheduled workflows</div>';
      return;
    }
    let html = `<table><thead><tr><th>Workflow</th><th>Schedule</th><th>Next Run</th><th>Last Run</th></tr></thead><tbody>`;
    for (const s of schedules) {
      html += `<tr>
        <td><span class="clickable" onclick="router.go('workflows',{type:'workflow',name:'${s.workflowName}'})">${s.workflowName}</span></td>
        <td class="mono">${s.cron || '-'}</td>
        <td class="mono" style="font-size:11px">${s.nextRun ? new Date(s.nextRun).toLocaleString() : '-'}</td>
        <td class="mono" style="font-size:11px">${s.lastRun && !s.lastRun.startsWith('0001') ? new Date(s.lastRun).toLocaleString() : 'never'}</td>
      </tr>`;
    }
    html += '</tbody></table>';
    el.innerHTML = html;
  });
}

// ── Platform Info ───────────────────────────────────────────────────

function loadPlatformInfo() {
  const el = document.getElementById('settings-platform');
  const d = state.data;
  el.innerHTML = `<div class="panel" style="margin-top:0">
    <div class="detail-grid">
      <div class="label">API Group</div><div class="mono">purko.io/v1alpha1</div>
      <div class="label">CRDs</div><div class="tags">
        <span class="tag tag--blue">Agent</span>
        <span class="tag tag--purple">Workflow</span>
        <span class="tag tag--green">MCPServer</span>
        <span class="tag tag--amber">LLMProvider</span>
        <span class="tag tag--dim">AgentAutonomyPolicy</span>
      </div>
      <div class="label">Agents</div><div>${d ? d.agentCount : '-'}</div>
      <div class="label">Workflows</div><div>${d ? d.workflowCount : '-'}</div>
    </div>
  </div>`;
}
