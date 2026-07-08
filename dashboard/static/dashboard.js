// Purko Dashboard — Tab 1: Platform Overview

function init_dashboard() {
  if (state.data) render_dashboard(state.data);
  else fetch('/api/overview').then(r => r.json()).then(d => { state.data = d; render_dashboard(d); });
}

function update_dashboard(d) {
  render_dashboard(d);
}

function render_dashboard(d) {
  // Collect metrics from agents (need detail calls)
  const el = document.getElementById('view-dashboard');

  // Fetch all agent details for metrics + shu-ha-ri, plus the provider
  // count for the demo-mode warning banner.
  Promise.all([
    fetch('/api/agents').then(r => r.json()),
    fetch('/api/llm/providers').then(r => r.json()).catch(() => ({ providers: [] })),
    fetch('/api/history/runs?limit=100').then(r => (r.ok ? r.json() : null)).catch(() => null),
  ]).then(([agents, provResp, histRuns]) => {
    const providerCount = (provResp.providers || []).length;
    const promises = agents.map(a => fetch('/api/agent/' + a.name).then(r => r.json()));
    Promise.all(promises).then(details => {
      let totalInv = 0, totalTokens = 0, totalCost = 0;
      let shr = { shu: 0, ha: 0, ri: 0 };
      let topAgents = [];

      for (const det of details) {
        const a = det.agent;
        const m = a.status && a.status.metrics;
        const s = a.status && a.status.shuHaRi;
        if (s) shr[s.currentLevel] = (shr[s.currentLevel] || 0) + 1;
        if (m && m.totalInvocations > 0) {
          totalInv += m.totalInvocations;
          totalTokens += m.totalTokensUsed;
          totalCost += m.totalCostUSD;
          topAgents.push({ name: a.metadata.name, cost: m.totalCostUSD, inv: m.totalInvocations, tokens: m.totalTokensUsed });
        }
      }
      topAgents.sort((a, b) => b.cost - a.cost);

      // MCP tool count
      const mcpCount = state.mcpTools.length;
      const mcpServers = new Set(state.mcpTools.map(t => t.source)).size;

      const hb = historyBuckets(histRuns, 24, 8);
      const done = d.wfSucceeded + d.wfFailed;
      const okPct = done > 0 ? Math.round((d.wfSucceeded / done) * 100) : null;

      el.innerHTML = `
        ${providerCount === 0 ? `<div class="panel" style="border-color:var(--amber);margin-bottom:20px;display:flex;align-items:center;gap:12px">
          <span style="font-size:18px">&#x26A0;&#xFE0F;</span>
          <div style="flex:1">
            <b>No LLM provider configured</b> — workflow steps run in demo mode (no real inference).
            <span style="color:var(--dim)">Add Ollama (local, no key), OpenRouter or any OpenAI-compatible gateway, Anthropic, OpenAI, or Vertex AI, and mark it default.</span>
          </div>
          <button class="btn btn--primary btn--sm" style="flex-shrink:0" onclick="router.go('llm')">Configure</button>
        </div>` : ''}
        <div class="cards">
          <div class="card card--insight card--blue">
            <div class="card-top"><span class="card-label">Agents</span></div>
            <div class="card-value">${d.agentCount}</div>
            <div class="card-sub">${d.agentReady} ready</div>
          </div>
          <div class="card card--insight card--purple">
            <div class="card-top"><span class="card-label">Workflows</span></div>
            <div class="card-value">${d.workflowCount}</div>
            <div class="card-sub">${d.wfRunning} running</div>
            ${sparklineSVG(hb.counts, '--purple')}
          </div>
          <div class="card card--insight card--green">
            <div class="card-top"><span class="card-label">MCP Servers</span></div>
            <div class="card-value">${mcpServers}</div>
            <div class="card-sub">${mcpCount} tools</div>
          </div>
          <div class="card card--insight card--amber">
            <div class="card-top"><span class="card-label">Total Cost</span>${totalCost === 0 && totalInv > 0 ? '<span class="card-delta card-delta--flat">$0 · local</span>' : ''}</div>
            <div class="card-value">$${totalCost.toFixed(2)}</div>
            <div class="card-sub">${(totalTokens / 1000).toFixed(1)}K tokens · ${totalInv} invocations</div>
          </div>
          <div class="card card--insight card--green">
            <div class="card-top"><span class="card-label">Succeeded</span>${okPct !== null ? `<span class="card-delta ${okPct >= 50 ? 'card-delta--up' : 'card-delta--flat'}">${okPct}%</span>` : ''}</div>
            <div class="card-value">${d.wfSucceeded}${done > 0 ? `<span class="card-frac"> / ${done}</span>` : ''}</div>
            <div class="card-sub">${d.wfFailed} failed</div>
            ${sparklineSVG(hb.ok, '--green')}
          </div>
          ${d.wfFailed > 0 ? `<div class="card card--insight card--red"><div class="card-top"><span class="card-label">Failed</span></div><div class="card-value">${d.wfFailed}</div></div>` : ''}
        </div>

        ${hasFeature('autonomy') ? '' : upgradeStrip('Shu-Ha-Ri Autonomy', 'agents earn trust from their track record.')}
        <div style="display:grid;grid-template-columns:${hasFeature('autonomy') ? '1fr 1fr' : '1fr'};gap:20px;margin-bottom:28px">
          ${hasFeature('autonomy') ? `<div class="panel" style="margin-top:0">
            <h3>Shu-Ha-Ri Distribution</h3>
            <div style="display:flex;gap:20px;margin-top:14px">
              <div style="text-align:center"><div class="card-value" style="color:var(--amber);font-size:24px">${shr.shu}</div><div class="card-label">Shu</div></div>
              <div style="text-align:center"><div class="card-value" style="color:var(--accent);font-size:24px">${shr.ha}</div><div class="card-label">Ha</div></div>
              <div style="text-align:center"><div class="card-value" style="color:var(--green);font-size:24px">${shr.ri}</div><div class="card-label">Ri</div></div>
            </div>
            <div class="progress" style="margin-top:14px">
              <div style="display:flex;height:100%">
                <div class="progress-fill shr-shu" style="width:${d.agentCount ? (shr.shu/d.agentCount*100) : 0}%"></div>
                <div class="progress-fill shr-ha" style="width:${d.agentCount ? (shr.ha/d.agentCount*100) : 0}%"></div>
                <div class="progress-fill shr-ri" style="width:${d.agentCount ? (shr.ri/d.agentCount*100) : 0}%"></div>
              </div>
            </div>
          </div>` : ''}
          <div class="panel" style="margin-top:0">
            <h3>Top Agents by Cost</h3>
            <div id="top-agents-body">${topAgents.length > 0 ? `<table style="margin-top:10px"><thead><tr><th>Agent</th><th style="width:45%">Cost share</th><th style="text-align:right">Invocations</th></tr></thead><tbody>
              ${topAgents.slice(0, 5).map(a => {
                const maxCost = topAgents[0].cost;
                const maxInv = Math.max(...topAgents.map(x => x.inv));
                const share = maxCost > 0 ? (a.cost / maxCost) * 100 : (maxInv > 0 ? (a.inv / maxInv) * 100 : 0);
                return `<tr>
                  <td><span class="clickable" onclick="router.go('agents',{type:'agent',name:'${a.name}'})">${a.name}</span></td>
                  <td><span class="mono" style="font-size:11px">$${a.cost.toFixed(4)}</span><div class="costbar"><span style="width:${share.toFixed(0)}%"></span></div></td>
                  <td class="mono" style="text-align:right">${a.inv}</td>
                </tr>`;
              }).join('')}
              ${topAgents.length < 3 ? '<tr><td colspan="3" style="color:var(--dim);font-size:12px;padding-top:12px">Run more workflows to rank agents by spend &rarr;</td></tr>' : ''}
            </tbody></table>` : '<div class="empty" style="padding:20px">No metrics yet</div>'}</div>
          </div>
        </div>

        <div class="section">
          <div class="section-title">
            Recent Workflows
            <span class="badge">${d.workflows.length}</span>
          </div>
          <table>
            <thead><tr><th>Name</th><th>Phase</th><th>Steps</th><th>Duration</th><th>Age</th></tr></thead>
            <tbody>
              ${d.workflows.slice(0, 10).map(w => `<tr>
                <td><span class="clickable" onclick="router.go('workflows',{type:'workflow',name:'${w.name}'})">${w.name}</span></td>
                <td>${phaseHTML(w.phase)}</td>
                <td>${w.completedSteps}/${w.totalSteps}${w.failedSteps > 0 ? ` <span class="tag tag--amber" style="font-size:9px">${w.failedSteps} failed</span>` : ''}</td>
                <td class="mono">${w.duration || '-'}</td>
                <td class="mono">${shortAge(w.age)}</td>
              </tr>`).join('')}
              ${d.workflows.length === 0 ? '<tr><td colspan="5" class="empty">No workflows</td></tr>' : ''}
            </tbody>
          </table>
        </div>
      `;
    });
  });
}
