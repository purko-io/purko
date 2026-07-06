// Purko Dashboard — Tab 2: Agents

function init_agents(detail) {
  agentFormOpen = false;
  if (detail && detail.name) {
    viewAgent(detail.name);
    return;
  }
  if (state.data) render_agents(state.data);
  else fetch('/api/overview').then(r => r.json()).then(d => { state.data = d; render_agents(d); });
}

let agentFormOpen = false;

function update_agents(d) {
  if (state.detail || agentFormOpen) return;
  render_agents(d);
}

function render_agents(d) {
  const agents = d.agents || [];
  const el = document.getElementById('view-agents');

  // Group by component
  const groups = {};
  for (const a of agents) {
    const g = a.group || 'general';
    if (!groups[g]) groups[g] = [];
    groups[g].push(a);
  }

  const groupIcons = {
    'platform-health': '\u{1F3E5}', 'incident-management': '\u{1F6A8}', 'observability': '\u{1F50D}',
    'security-compliance': '\u{1F6E1}', 'sdlc': '\u{1F4BB}', 'ci-cd': '\u{2699}',
    'capacity-cost': '\u{1F4CA}', 'general': '\u{1F4E6}',
  };

  let rows = '';
  for (const g of Object.keys(groups).sort()) {
    const icon = groupIcons[g] || '\u{1F4E6}';
    rows += `<tr class="group-header"><td colspan="7"><span class="group-label">${icon} ${g.replace(/-/g, ' ')}</span><span class="group-count">${groups[g].length}</span></td></tr>`;
    for (const a of groups[g]) {
      rows += `<tr>
        <td><span class="clickable" onclick="viewAgent('${a.name}')">${a.name}</span></td>
        <td>${a.type ? `<span class="tag tag--blue">${a.type}</span>` : '-'}</td>
        <td>${phaseHTML(a.phase)}</td>
        <td>${a.autonomy ? `<span class="tag tag--${a.autonomy === 'full' ? 'green' : a.autonomy === 'supervised' ? 'blue' : 'amber'}">${a.autonomy}</span>` : '-'}</td>
        <td>${a.toolCount > 0 ? `<span class="tag tag--purple">${a.toolCount} tools</span>` : '-'}</td>
        <td class="mono">${a.provider}/${a.model}</td>
        <td class="mono">${shortAge(a.age)}</td>
      </tr>`;
    }
  }

  el.innerHTML = `
    <div class="section">
      <div class="section-title" style="justify-content:space-between">
        <span>Agents <span class="badge">${agents.length}</span></span>
        <button class="btn btn--primary btn--sm" onclick="showCreateAgentForm()">+ Create Agent</button>
      </div>
      <table>
        <thead><tr><th>Name</th><th>Type</th><th>Status</th><th>Autonomy</th><th>Tools</th><th>Model</th><th>Age</th></tr></thead>
        <tbody>${rows || '<tr><td colspan="7" class="empty">No agents</td></tr>'}</tbody>
      </table>
    </div>
    <div id="agent-detail-container"></div>
  `;
}

function viewAgent(name) {
  state.detail = { type: 'agent', name };
  fetch('/api/agent/' + name).then(r => r.json()).then(d => {
    const a = d.agent;
    const sp = a.spec;
    const st = a.status;

    const tools = sp.tools ? sp.tools.map(t => `<span class="tag tag--purple">${t.name}</span>`).join(' ') : 'none';

    // Guardrails
    let guardrailsHTML = '';
    if (sp.guardrails) {
      const g = sp.guardrails;
      guardrailsHTML = `<div class="detail-grid" style="margin-top:12px">
        <div class="label">Guardrails</div>
        <div class="tags">
          ${g.maxIterations ? `<span class="tag tag--amber">max ${g.maxIterations} iterations</span>` : ''}
          ${g.costLimitUSD ? `<span class="tag tag--amber">$${g.costLimitUSD} limit</span>` : ''}
          ${g.maxExecutionTime ? `<span class="tag tag--amber">${g.maxExecutionTime} timeout</span>` : ''}
          ${g.humanApprovalRequired ? `<span class="tag tag--red">approval required</span>` : ''}
          ${(g.contentFilters || []).map(f => `<span class="tag tag--dim">${f}</span>`).join(' ')}
        </div>
      </div>`;
    }

    // Shu-Ha-Ri
    let shrHTML = '';
    if (st.shuHaRi) {
      const s = st.shuHaRi;
      const pp = s.promotionProgress;
      shrHTML = `<div class="detail-grid" style="margin-top:12px">
        <div class="label">Shu-Ha-Ri</div>
        <div>
          <span class="tag tag--${s.currentLevel === 'ri' ? 'green' : s.currentLevel === 'ha' ? 'blue' : 'amber'}">${s.currentLevel}</span>
          ${s.readyForPromotion ? '<span class="tag tag--green">ready for promotion</span>' : ''}
          ${pp ? `<span class="mono" style="margin-left:8px">${pp.actionsCompleted}/${pp.actionsRequired} actions, ${Math.round(pp.successRate * 100)}% success, ${pp.daysInLevel}/${pp.daysRequired} days</span>` : ''}
        </div>
      </div>`;
    }

    // Metrics
    let metricsHTML = '';
    if (st.metrics) {
      const m = st.metrics;
      metricsHTML = `<div class="detail-grid" style="margin-top:12px">
        <div class="label">Metrics</div>
        <div class="tags">
          <span class="tag tag--blue">${m.totalInvocations} invocations</span>
          <span class="tag tag--purple">${(m.totalTokensUsed/1000).toFixed(0)}K tokens</span>
          <span class="tag tag--amber">$${m.totalCostUSD.toFixed(4)}</span>
          <span class="tag tag--dim">${Math.round(m.averageLatencyMs/1000)}s avg</span>
          ${m.totalInvocations > 0 ? `<span class="tag tag--green">${Math.round((m.successCount||0)/m.totalInvocations*100)}% success</span>` : ''}
        </div>
      </div>`;
    }

    // Conditions
    let condsHTML = '';
    if (st.conditions && st.conditions.length > 0) {
      condsHTML = `<div class="detail-grid" style="margin-top:12px">
        <div class="label">Conditions <span style="font-size:9px;color:var(--dim);font-weight:400">(computed health — not configurable)</span></div>
        <div>${conditionsHTML(st.conditions)}</div>
      </div>`;
    }

    // RBAC info (SA name follows convention)
    const saName = `agent-${a.metadata.name}-sa`;
    const roleName = `agent-${a.metadata.name}-role`;

    // Pods
    let podsHTML = '';
    if (d.pods && d.pods.length > 0) {
      podsHTML = `<div style="margin-top:16px"><h3>Pods</h3>
        <table style="margin-top:8px"><thead><tr><th>Pod</th><th>Status</th><th>IP</th></tr></thead><tbody>
        ${d.pods.map(p => `<tr><td class="mono">${p.name}</td><td>${phaseHTML(p.status)}</td><td class="mono">${p.ip}</td></tr>`).join('')}
        </tbody></table></div>`;
    }

    const container = document.getElementById('view-agents');
    container.innerHTML = `
      <span class="back-link" onclick="init_agents()">&larr; Back to agents</span>
      <div class="panel">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
          <h3>${a.metadata.name}</h3>
          <div style="display:flex;gap:8px">
            <button class="btn btn--secondary btn--sm" onclick="editAgent('${a.metadata.name}')">Edit</button>
            <button class="btn btn--danger" onclick="deleteAgent('${a.metadata.name}')">Delete</button>
          </div>
        </div>

        <div class="detail-grid">
          <div class="label">Type</div><div>${sp.type ? `<span class="tag tag--blue">${sp.type}</span>` : '-'}</div>
          <div class="label">Autonomy</div><div>${sp.autonomyLevel || '-'}</div>
          <div class="label">Model</div><div class="mono">${sp.model.provider}/${sp.model.name}</div>
          ${sp.memory ? `<div class="label">Memory</div><div>${sp.memory.type || 'buffer'}</div>` : ''}
          <div class="label">Temperature</div><div>${sp.model.temperature != null ? sp.model.temperature : '-'}</div>
          <div class="label">Image</div><div class="mono">${sp.runtime && sp.runtime.image ? sp.runtime.image : 'purko-executor:latest'}</div>
          <div class="label">Tools</div><div class="tags">${tools}</div>
        </div>

        ${condsHTML}
        ${metricsHTML}
        ${shrHTML}
        ${guardrailsHTML}

        <div class="detail-grid" style="margin-top:12px">
          <div class="label">RBAC</div>
          <div class="tags">
            <span class="tag tag--dim">SA: ${saName}</span>
            <span class="tag tag--dim">Role: ${roleName}</span>
          </div>
        </div>

        ${sp.systemPrompt ? `<div style="margin-top:16px"><h3>System Prompt</h3><div class="prompt-box">${esc(sp.systemPrompt)}</div></div>` : ''}
        ${podsHTML}
      </div>
    `;
  });
}

function deleteAgent(name) {
  if (!confirm(`Delete agent "${name}"?`)) return;
  fetch('/api/delete/agent/' + name, { method: 'POST' })
    .then(r => r.json())
    .then(d => { if (d.error) alert('Error: ' + d.error); else { state.detail = null; init_agents(); } });
}

function editAgent(name) {
  agentFormOpen = true;
  fetch('/api/agent/' + name).then(r => r.json()).then(d => {
    const a = d.agent;
    const sp = a.spec;
    const currentTools = (sp.tools || []).map(t => t.name);
    const image = sp.runtime && sp.runtime.image ? sp.runtime.image : 'localhost/purko-executor:latest';
    const gr = sp.guardrails || {};
    const group = a.metadata.labels && a.metadata.labels['app.kubernetes.io/component'] || 'general';

    const toolGroups = {};
    for (const t of state.mcpTools) {
      const key = t.source;
      if (!toolGroups[key]) toolGroups[key] = [];
      toolGroups[key].push(t);
    }

    let toolsHTML = '';
    for (const [src, tools] of Object.entries(toolGroups)) {
      toolsHTML += `<div style="font-size:10px;color:var(--dim);margin:8px 0 4px;font-weight:600">${src}</div>`;
      toolsHTML += '<div class="tool-checkboxes">';
      for (const t of tools) {
        toolsHTML += `<label class="cb"><input type="checkbox" value="${t.name}" ${currentTools.includes(t.name)?'checked':''}> ${t.name}</label>`;
      }
      toolsHTML += '</div>';
    }

    const el = document.getElementById('view-agents');
    el.innerHTML = `
      <span class="back-link" onclick="viewAgent('${name}')">&larr; Back to ${name}</span>
      <div class="panel">
        <h3>Edit: ${name}</h3>
        <div class="form-grid" style="margin-top:12px">
          <label>Provider</label>
          <select id="edit-provider"><option value="anthropic" ${sp.model.provider==='anthropic'?'selected':''}>Anthropic</option><option value="openai" ${sp.model.provider==='openai'?'selected':''}>OpenAI</option></select>
          <label>Model</label>
          <select id="edit-model">
            <option value="claude-sonnet-4-6" ${sp.model.name==='claude-sonnet-4-6'?'selected':''}>Claude Sonnet 4.6</option>
            <option value="claude-opus-4-6" ${sp.model.name==='claude-opus-4-6'?'selected':''}>Claude Opus 4.6</option>
            <option value="claude-haiku-4-5" ${sp.model.name==='claude-haiku-4-5'?'selected':''}>Claude Haiku 4.5</option>
            <option value="gpt-4o" ${sp.model.name==='gpt-4o'?'selected':''}>GPT-4o</option>
          </select>
          <label>Temperature</label>
          <div class="range-group"><input type="range" id="edit-temp" min="0" max="2" step="0.1" value="${sp.model.temperature||0.2}" oninput="document.getElementById('edit-temp-val').textContent=this.value"><span id="edit-temp-val" class="mono">${sp.model.temperature||0.2}</span></div>
          <label>Autonomy</label>
          <div><select id="edit-autonomy">
            <option value="restricted" ${sp.autonomyLevel==='restricted'?'selected':''}>Restricted</option>
            <option value="supervised" ${sp.autonomyLevel==='supervised'?'selected':''}>Supervised</option>
            <option value="full" ${sp.autonomyLevel==='full'?'selected':''}>Full</option>
          </select><div style="font-size:10px;color:var(--dim);margin-top:4px">Sets the Shu-Ha-Ri starting level: restricted &rarr; Shu, supervised &rarr; Ha, full &rarr; Ri. Agents then earn (or lose) autonomy via AgentAutonomyPolicy.</div></div>
          <label>Image</label>
          <select id="edit-image">
            <option value="localhost/purko-executor:latest" ${image.includes('latest')&&!image.includes('git')&&!image.includes('dev')?'selected':''}>Base</option>
            <option value="localhost/purko-executor:git" ${image.includes('git')?'selected':''}>Git</option>
            <option value="localhost/purko-executor:dev" ${image.includes('dev')?'selected':''}>Dev</option>
          </select>
          <label>Group</label>
          <select id="edit-group">
            ${['general','platform-health','incident-management','observability','security-compliance','sdlc','ci-cd','capacity-cost'].map(g => `<option value="${g}" ${group===g?'selected':''}>${g}</option>`).join('')}
          </select>
          <label>Role</label><input id="edit-role" value="${esc(sp.role||'')}" spellcheck="false">
          <label>System Prompt</label><textarea id="edit-prompt" rows="5">${esc(sp.systemPrompt||'')}</textarea>
          <label>Guardrails</label>
          <div>
            <div style="display:flex;gap:8px;flex-wrap:wrap">
              <div style="flex:1;min-width:120px"><div style="font-size:10px;color:var(--dim);margin-bottom:2px">Max iterations</div><input type="number" id="edit-gr-iter" min="0" value="${gr.maxIterations||''}" placeholder="e.g. 15"></div>
              <div style="flex:1;min-width:120px"><div style="font-size:10px;color:var(--dim);margin-bottom:2px">Cost limit USD</div><input type="number" id="edit-gr-cost" min="0" step="0.5" value="${gr.costLimitUSD||''}" placeholder="e.g. 8"></div>
              <div style="flex:1;min-width:120px"><div style="font-size:10px;color:var(--dim);margin-bottom:2px">Max execution time</div><input id="edit-gr-time" value="${esc(gr.maxExecutionTime||'')}" placeholder="e.g. 5m" spellcheck="false"></div>
              <div style="flex:1;min-width:120px"><div style="font-size:10px;color:var(--dim);margin-bottom:2px">Rollback on failure</div><select id="edit-gr-rollback"><option value="false" ${gr.rollbackOnFailure?'':'selected'}>No</option><option value="true" ${gr.rollbackOnFailure?'selected':''}>Yes</option></select></div>
            </div>
            <div style="font-size:10px;color:var(--dim);margin-top:4px">Safety caps enforced by the executor. Empty fields keep their current values.</div>
          </div>
          <label>MCP Tools</label><div id="edit-tools">${toolsHTML}</div>
        </div>
        <div class="form-actions">
          <button class="btn btn--primary" onclick="saveAgent('${name}')">Save Changes</button>
          <button class="btn btn--secondary" onclick="viewAgent('${name}')">Cancel</button>
        </div>
        <div id="edit-result"></div>
      </div>
    `;
  });
}

function saveAgent(name) {
  const tools = [];
  document.querySelectorAll('#edit-tools input:checked').forEach(cb => tools.push(cb.value));
  const body = {
    name,
    provider: document.getElementById('edit-provider').value,
    model: document.getElementById('edit-model').value,
    temperature: parseFloat(document.getElementById('edit-temp').value),
    autonomy: document.getElementById('edit-autonomy').value,
    role: document.getElementById('edit-role').value,
    image: document.getElementById('edit-image').value,
    group: document.getElementById('edit-group').value,
    systemPrompt: document.getElementById('edit-prompt').value,
    tools,
    maxIterations: parseInt(document.getElementById('edit-gr-iter').value) || 0,
    costLimit: parseFloat(document.getElementById('edit-gr-cost').value) || 0,
    maxExecutionTime: document.getElementById('edit-gr-time').value.trim(),
    rollbackOnFailure: document.getElementById('edit-gr-rollback').value === 'true',
  };
  fetch('/api/update/agent', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    .then(r => r.json())
    .then(d => {
      if (d.error) showResult('edit-result', 'err', 'Error: ' + d.error);
      else { showResult('edit-result', 'ok', 'Updated!'); setTimeout(() => viewAgent(name), 1500); }
    });
}

function showCreateAgentForm() {
  agentFormOpen = true;
  const toolGroups = {};
  for (const t of state.mcpTools) {
    const key = t.source;
    if (!toolGroups[key]) toolGroups[key] = [];
    toolGroups[key].push(t);
  }

  let toolsHTML = '';
  for (const [src, tools] of Object.entries(toolGroups)) {
    toolsHTML += `<div style="font-size:10px;color:var(--dim);margin:8px 0 4px;font-weight:600">${src}</div>`;
    toolsHTML += '<div class="tool-checkboxes">';
    for (const t of tools) {
      toolsHTML += `<label class="cb"><input type="checkbox" value="${t.name}"> ${t.name}</label>`;
    }
    toolsHTML += '</div>';
  }

  const el = document.getElementById('view-agents');
  el.innerHTML = `
    <span class="back-link" onclick="init_agents()">&larr; Back to agents</span>
    <div class="panel">
      <h3>Create Agent</h3>
      <div class="form-grid" style="margin-top:12px">
        <label>Name</label><input id="ca-name" placeholder="my-agent" spellcheck="false">
        <label>Type</label>
        <select id="ca-type"><option value="">-- none --</option><option value="monitor">Monitor</option><option value="reviewer">Reviewer</option><option value="planner">Planner</option><option value="executor">Executor</option><option value="router">Router</option><option value="retriever">Retriever</option></select>
        <label>Provider</label><select id="ca-provider"><option value="anthropic">Anthropic</option><option value="openai">OpenAI</option></select>
        <label>Model</label>
        <select id="ca-model"><option value="claude-sonnet-4-6">Claude Sonnet 4.6</option><option value="claude-opus-4-6">Claude Opus 4.6</option><option value="claude-haiku-4-5">Claude Haiku 4.5</option><option value="gpt-4o">GPT-4o</option></select>
        <label>Temperature</label><div class="range-group"><input type="range" id="ca-temp" min="0" max="2" step="0.1" value="0.1" oninput="document.getElementById('ca-temp-val').textContent=this.value"><span id="ca-temp-val" class="mono">0.1</span></div>
        <label>Autonomy</label><div><select id="ca-autonomy"><option value="restricted">Restricted</option><option value="supervised">Supervised</option><option value="full">Full</option></select><div style="font-size:10px;color:var(--dim);margin-top:4px">Sets the Shu-Ha-Ri starting level: restricted &rarr; Shu, supervised &rarr; Ha, full &rarr; Ri. Agents then earn (or lose) autonomy via AgentAutonomyPolicy.</div></div>
        <label>Memory</label><select id="ca-memory"><option value="buffer">Buffer</option><option value="summary">Summary</option><option value="vector">Vector</option><option value="none">None</option></select>
        <label>Image</label><select id="ca-image"><option value="localhost/purko-executor:latest">Base</option><option value="localhost/purko-executor:git">Git</option><option value="localhost/purko-executor:dev">Dev</option></select>
        <label>Group</label>
        <select id="ca-group"><option value="general">General</option><option value="platform-health">Platform Health</option><option value="incident-management">Incident Management</option><option value="observability">Observability</option><option value="security-compliance">Security</option><option value="sdlc">SDLC</option><option value="ci-cd">CI/CD</option><option value="capacity-cost">Capacity & Cost</option></select>
        <label>Role</label><input id="ca-role" placeholder="e.g. cluster-health-assessor" spellcheck="false">
        <label>Cost Limit ($)</label><input type="number" id="ca-cost" value="5" min="0" max="100" step="0.5" style="width:100px">
        <label>Max Iterations</label><input type="number" id="ca-iter" value="20" min="1" max="50" style="width:100px">
        <label>System Prompt</label><textarea id="ca-prompt" rows="5" placeholder="You are an expert agent that..."></textarea>
        <label>MCP Tools</label><div id="ca-tools">${toolsHTML}</div>
      </div>
      <div class="form-actions">
        <button class="btn btn--primary" onclick="createAgent()">Deploy Agent</button>
      </div>
      <div id="ca-result"></div>
    </div>
  `;
}

function createAgent() {
  const tools = [];
  document.querySelectorAll('#ca-tools input:checked').forEach(cb => tools.push(cb.value));
  const body = {
    name: document.getElementById('ca-name').value,
    type: document.getElementById('ca-type').value,
    provider: document.getElementById('ca-provider').value,
    model: document.getElementById('ca-model').value,
    temperature: parseFloat(document.getElementById('ca-temp').value),
    autonomy: document.getElementById('ca-autonomy').value,
    memory: document.getElementById('ca-memory').value,
    role: document.getElementById('ca-role').value,
    image: document.getElementById('ca-image').value,
    group: document.getElementById('ca-group').value,
    costLimit: parseFloat(document.getElementById('ca-cost').value),
    maxIterations: parseInt(document.getElementById('ca-iter').value),
    systemPrompt: document.getElementById('ca-prompt').value,
    tools,
    minReplicas: 1, maxReplicas: 3, targetCPU: 70,
  };
  if (!body.name) { showResult('ca-result', 'err', 'Name required'); return; }
  fetch('/api/create/agent', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    .then(r => r.json())
    .then(d => {
      if (d.error) showResult('ca-result', 'err', 'Error: ' + d.error);
      else showResult('ca-result', 'ok', `Agent "${d.name}" created!`);
    });
}
