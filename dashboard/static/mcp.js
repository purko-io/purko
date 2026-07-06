// Purko Dashboard — Tab 4: MCP Servers

function init_mcp() {
  loadMCPServers();
}

function loadMCPServers() {
  fetch('/api/mcp/tools').then(r => r.json()).then(d => {
    const el = document.getElementById('view-mcp');
    const servers = d.servers || [];
    const totalTools = d.totalTools || 0;

    let cardsHTML = '';
    for (const server of servers) {
      // Group tools by category
      const groups = {};
      for (const t of (server.tools || [])) {
        const cat = TOOL_CATS[t] || 'Other';
        if (!groups[cat]) groups[cat] = [];
        groups[cat].push(t);
      }

      let groupsHTML = '';
      for (const [cat, tools] of Object.entries(groups)) {
        const color = CAT_COLORS[cat] || '--dim';
        groupsHTML += `<div class="mcp-group">
          <div class="mcp-group-head">
            <span class="mcp-group-dot" style="background:var(${color})"></span>
            <span class="mcp-group-name">${cat}</span>
            <span class="mcp-group-count">${tools.length}</span>
          </div>
          <div class="mcp-group-tools">${tools.map(t => `<span class="mcp-tool">${t}</span>`).join('')}</div>
        </div>`;
      }

      cardsHTML += `<div class="mcp-card">
        <div class="mcp-card-header">
          <div class="mcp-card-title">
            <span class="mcp-card-icon">${server.icon || '\u{1F4E6}'}</span>
            <span class="mcp-card-name">${server.name}</span>
            <span class="badge">${server.toolCount || 0}</span>
            ${phaseHTML(server.status === 'connected' ? 'Ready' : 'Failed')}
          </div>
          <div class="mcp-card-meta">
            <code class="mono">${server.url}</code>
            <span class="mcp-card-cat">${server.category || ''}</span>
          </div>
        </div>
        <div class="mcp-grid">${groupsHTML}</div>
        <div style="margin-top:14px;display:flex;gap:8px">
          <button class="btn btn--danger" onclick="deleteMCPServer('${server.name}')">Delete</button>
        </div>
      </div>`;
    }

    el.innerHTML = `
      <div class="cards" style="margin-bottom:24px">
        <div class="card card--blue"><div class="card-value">${totalTools}</div><div class="card-label">Total Tools</div></div>
        <div class="card card--green"><div class="card-value">${servers.length}</div><div class="card-label">Servers</div></div>
      </div>
      <div class="section">
        <div class="section-title" style="justify-content:space-between">
          <span>MCP Servers <span class="badge">${servers.length}</span></span>
          <button class="btn btn--primary btn--sm" onclick="showDeployMCPForm()">+ Add MCP Server</button>
        </div>
      </div>
      ${cardsHTML || '<div class="empty">No MCP servers</div>'}
      <div id="mcp-form-container"></div>
    `;
  });
}

function showDeployMCPForm() {
  const el = document.getElementById('mcp-form-container');
  el.innerHTML = `<div class="panel">
    <h3>Add MCP Server</h3>
    <div class="form-grid" style="margin-top:12px">
      <label>Mode</label>
      <select id="mcp-mode" onchange="updateMCPFormMode()">
        <option value="connect">Connect existing server (URL)</option>
        <option value="deploy">Deploy new server (container image)</option>
      </select>
      <label>Name</label><input id="mcp-name" placeholder="my-server" spellcheck="false">
      <label id="mcp-url-label">URL</label>
      <div id="mcp-url-wrap">
        <input id="mcp-url" placeholder="http://my-server.ai-agents:8000/mcp" spellcheck="false">
        <div style="font-size:10px;color:var(--dim);margin-top:4px">Full MCP endpoint as reachable from inside the cluster — include the path (usually /mcp for streamable HTTP).</div>
      </div>
      <label id="mcp-image-label">Image</label><input id="mcp-image" placeholder="quay.io/org/image:tag" spellcheck="false">
      <label id="mcp-port-label">Port</label><input type="number" id="mcp-port" value="8000" min="1" max="65535" style="width:100px">
      <label>Category</label><input id="mcp-cat" placeholder="e.g. kubernetes, code, incident" spellcheck="false">
      <label>Icon</label><input id="mcp-icon" placeholder="emoji" spellcheck="false" style="width:100px">
      <label>Auth</label><select id="mcp-auth"><option value="none">None</option><option value="bearer">Bearer Token</option></select>
      <label>Secret Ref</label><input id="mcp-secret" placeholder="secret name (if bearer)" spellcheck="false">
      <label id="mcp-hostnet-label">Host Network</label><select id="mcp-hostnet"><option value="false">No</option><option value="true">Yes (minikube)</option></select>
      <label id="mcp-args-label">Args</label><textarea id="mcp-args" rows="2" placeholder="one arg per line" style="font-family:var(--mono);font-size:12px"></textarea>
    </div>
    <div class="form-actions">
      <button class="btn btn--primary" id="mcp-submit" onclick="createMCPServer()">Connect</button>
      <button class="btn btn--secondary" onclick="document.getElementById('mcp-form-container').innerHTML=''">Cancel</button>
    </div>
    <div id="mcp-result"></div>
  </div>`;
  updateMCPFormMode();
}

function updateMCPFormMode() {
  const connect = document.getElementById('mcp-mode').value === 'connect';
  const show = (id, on) => { const e = document.getElementById(id); if (e) e.style.display = on ? '' : 'none'; };
  ['mcp-url-label', 'mcp-url-wrap'].forEach(id => show(id, connect));
  ['mcp-image-label', 'mcp-image', 'mcp-port-label', 'mcp-port', 'mcp-hostnet-label', 'mcp-hostnet', 'mcp-args-label', 'mcp-args'].forEach(id => show(id, !connect));
  document.getElementById('mcp-submit').textContent = connect ? 'Connect' : 'Deploy';
}

function createMCPServer() {
  const connect = document.getElementById('mcp-mode').value === 'connect';
  const body = {
    name: document.getElementById('mcp-name').value.trim(),
    category: document.getElementById('mcp-cat').value.trim(),
    icon: document.getElementById('mcp-icon').value.trim(),
    auth: document.getElementById('mcp-auth').value,
    secretRef: document.getElementById('mcp-secret').value.trim(),
  };
  if (!body.name) { showResult('mcp-result', 'err', 'Name required'); return; }

  if (connect) {
    body.url = document.getElementById('mcp-url').value.trim();
    if (!body.url) { showResult('mcp-result', 'err', 'URL required'); return; }
  } else {
    body.image = document.getElementById('mcp-image').value.trim();
    if (!body.image) { showResult('mcp-result', 'err', 'Image required'); return; }
    body.port = parseInt(document.getElementById('mcp-port').value);
    body.hostNetwork = document.getElementById('mcp-hostnet').value === 'true';
    body.args = document.getElementById('mcp-args').value.trim().split('\n').filter(a => a.trim());
  }

  fetch('/api/mcp/server', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    .then(r => r.json())
    .then(d => {
      if (d.error) showResult('mcp-result', 'err', 'Error: ' + d.error);
      else { showResult('mcp-result', 'ok', connect ? `Connected "${d.name}" — discovering tools...` : `MCPServer "${d.name}" created!`); setTimeout(loadMCPServers, 3000); }
    });
}

function deleteMCPServer(name) {
  if (!confirm(`Delete MCP server "${name}"?`)) return;
  fetch('/api/mcp/server/' + name, { method: 'DELETE' })
    .then(r => r.json())
    .then(d => { if (d.error) alert('Error: ' + d.error); else loadMCPServers(); });
}
