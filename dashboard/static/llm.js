// Purko Dashboard — Tab 5: LLM Providers

function init_llm() {
  loadLLMProviders();
}

function loadLLMProviders() {
  fetch('/api/llm/providers').then(r => r.json()).then(d => {
    const providers = d.providers || [];
    const el = document.getElementById('view-llm');

    let cardsHTML = '';
    for (const p of providers) {
      const sp = p.spec || {};
      const st = p.status || {};
      const models = sp.models || [];

      let modelsHTML = '';
      if (models.length > 0) {
        modelsHTML = `<table style="margin-top:10px"><thead><tr><th>Model</th><th>Max Tokens</th><th>Input $/MT</th><th>Output $/MT</th></tr></thead><tbody>`;
        for (const m of models) {
          modelsHTML += `<tr>
            <td class="mono">${m.name}${m.name === sp.model ? ' <span class="tag tag--green" style="font-size:9px">default</span>' : ''}</td>
            <td class="mono">${m.maxTokens || '-'}</td>
            <td class="mono">${m.pricing ? '$' + m.pricing.inputPerMToken : '-'}</td>
            <td class="mono">${m.pricing ? '$' + m.pricing.outputPerMToken : '-'}</td>
          </tr>`;
        }
        modelsHTML += '</tbody></table>';
      }

      cardsHTML += `<div class="mcp-card">
        <div class="mcp-card-header">
          <div class="mcp-card-title">
            <span class="mcp-card-name">${p.metadata.name}</span>
            <span class="tag tag--blue">${sp.type || 'unknown'}</span>
            ${sp.default ? '<span class="tag tag--green">Default</span>' : ''}
            ${phaseHTML(st.phase)}
          </div>
          <div class="mcp-card-meta">
            <span class="mono">Default model: ${sp.model || '-'}</span>
            <span class="mono">${st.availableModels || 0} models</span>
          </div>
        </div>

        ${st.conditions && st.conditions.length > 0 ? `<div style="margin:12px 0">${conditionsHTML(st.conditions)}</div>` : ''}
        ${modelsHTML}

        <div style="margin-top:14px;display:flex;gap:8px">
          <button class="btn btn--danger" onclick="deleteLLMProvider('${p.metadata.name}')">Delete</button>
        </div>
      </div>`;
    }

    el.innerHTML = `
      <div class="cards" style="margin-bottom:24px">
        <div class="card card--blue"><div class="card-value">${providers.length}</div><div class="card-label">Providers</div></div>
        <div class="card card--green"><div class="card-value">${providers.reduce((s, p) => s + (p.spec.models || []).length, 0)}</div><div class="card-label">Models</div></div>
      </div>
      <div class="section">
        <div class="section-title" style="justify-content:space-between">
          <span>LLM Providers <span class="badge">${providers.length}</span></span>
          <button class="btn btn--primary btn--sm" onclick="showAddLLMForm()">+ Add Provider</button>
        </div>
      </div>
      ${cardsHTML || emptyProvidersGuide()}
      <div id="llm-form-container"></div>
    `;
  });
}

const LLM_PRESETS = {
  'vertex-ai': { name: 'vertex-ai-claude', model: 'claude-sonnet-4-6', secretKey: 'credentials.json', secretPlaceholder: 'gcp-credentials', models: 'claude-sonnet-4-6, claude-opus-4-6, claude-haiku-4-5', hint: 'Claude models via Vertex AI. Requires GCP service account JSON in a Secret. Set projectId and region in config.' },
  'gemini': { name: 'gemini', model: 'gemini-2.5-pro', secretKey: 'credentials.json', secretPlaceholder: 'gcp-credentials', models: 'gemini-2.5-pro, gemini-2.5-flash, gemini-2.0-flash', hint: 'Google Gemini via Vertex AI. Same GCP credentials as Claude. Set projectId and region in config.' },
  'anthropic': { name: 'anthropic', model: 'claude-sonnet-4-6', secretKey: 'api-key', secretPlaceholder: 'anthropic-api-key', models: 'claude-sonnet-4-6, claude-opus-4-6, claude-haiku-4-5', hint: 'Direct Anthropic API. Requires API key in a Secret.' },
  'openai': { name: 'openai', model: 'gpt-4o', secretKey: 'api-key', secretPlaceholder: 'openai-api-key', models: 'gpt-4o, gpt-4o-mini, gpt-4-turbo, o1, o1-mini', hint: 'Requires OpenAI API key in a Secret.' },
  'ollama': { name: 'ollama', model: 'qwen3:8b', secretKey: '', secretPlaceholder: '', models: '', modelNote: 'use a model from `ollama list` on your Ollama host', endpoint: 'http://ollama.ai-agents:11434/v1', endpointRequired: true, hint: 'No credentials needed. Endpoint is required — the OpenAI-compatible URL of your Ollama server as reachable from inside the cluster (append /v1).' },
  'custom': { name: 'custom', model: '', secretKey: 'api-key', secretPlaceholder: 'custom-api-key', models: '', endpoint: '', endpointRequired: true, endpointPlaceholder: 'https://openrouter.ai/api/v1', hint: 'Custom OpenAI-compatible endpoint. Endpoint base URL is required.' },
};

function showAddLLMForm() {
  const el = document.getElementById('llm-form-container');
  el.innerHTML = `<div class="panel">
    <h3>Add LLM Provider</h3>
    <div class="form-grid" style="margin-top:12px">
      <label>Type</label>
      <select id="llm-type" onchange="updateLLMFormPreset()">
        <option value="vertex-ai">Vertex AI (Claude via GCP)</option>
        <option value="gemini">Gemini (Google AI via Vertex)</option>
        <option value="anthropic">Anthropic (Direct API)</option>
        <option value="openai">OpenAI</option>
        <option value="ollama">Ollama (Local)</option>
        <option value="custom">Custom (OpenAI-compatible)</option>
      </select>
      <label>Name</label><input id="llm-name" placeholder="my-provider" spellcheck="false">
      <label>Default Model</label>
      <div>
        <input id="llm-model" placeholder="model name" spellcheck="false">
        <div id="llm-model-hint" style="font-size:10px;color:var(--dim);margin-top:4px"></div>
      </div>
      <label>Endpoint</label>
      <div>
        <input id="llm-endpoint" placeholder="(optional — provider default)" spellcheck="false">
        <div id="llm-endpoint-hint" style="font-size:10px;color:var(--dim);margin-top:4px"></div>
      </div>
      <label>Set as Default</label><select id="llm-default"><option value="false">No</option><option value="true">Yes</option></select>
      <label>Credentials</label>
      <div id="llm-creds-section">
        <div style="display:flex;gap:8px">
          <input id="llm-secret" placeholder="secret name" spellcheck="false" style="flex:1">
          <input id="llm-key" placeholder="key" spellcheck="false" style="width:140px">
        </div>
      </div>
      <label>Config</label>
      <div>
        <textarea id="llm-config" rows="3" placeholder="key=value (one per line)&#10;e.g. endpoint=http://ollama:11434&#10;projectId=my-gcp-project" spellcheck="false" style="font-family:var(--mono);font-size:11px"></textarea>
      </div>
      <label></label>
      <div id="llm-hint" style="font-size:11px;color:var(--dim);background:rgba(0,180,255,0.04);border:1px solid rgba(0,180,255,0.1);border-radius:var(--radius-xs);padding:8px 12px"></div>
    </div>
    <div class="form-actions">
      <button class="btn btn--primary" onclick="createLLMProvider()">Create Provider</button>
      <button class="btn btn--secondary" onclick="document.getElementById('llm-form-container').innerHTML=''">Cancel</button>
    </div>
    <div id="llm-result"></div>
  </div>`;
  updateLLMFormPreset();
}

function updateLLMFormPreset() {
  const type = document.getElementById('llm-type').value;
  const preset = LLM_PRESETS[type] || {};
  document.getElementById('llm-name').value = preset.name || '';
  document.getElementById('llm-model').value = preset.model || '';
  document.getElementById('llm-secret').placeholder = preset.secretPlaceholder || 'secret name';
  document.getElementById('llm-key').value = preset.secretKey || '';
  document.getElementById('llm-model-hint').textContent = preset.models ? 'Available: ' + preset.models : (preset.modelNote || '');
  document.getElementById('llm-hint').textContent = preset.hint || '';

  const endpointEl = document.getElementById('llm-endpoint');
  endpointEl.value = preset.endpoint || '';
  endpointEl.placeholder = preset.endpointPlaceholder || (preset.endpointRequired ? 'http://host:port/v1' : '(optional — provider default)');
  document.getElementById('llm-endpoint-hint').textContent = preset.endpointRequired ? 'Required for this provider type' : '';

  // Hide credentials for ollama
  const credsSection = document.getElementById('llm-creds-section');
  if (type === 'ollama') {
    credsSection.innerHTML = '<span style="color:var(--dim);font-size:12px">No credentials required for local Ollama</span>';
  } else {
    credsSection.innerHTML = `<div style="display:flex;gap:8px">
      <input id="llm-secret" placeholder="${preset.secretPlaceholder || 'secret name'}" spellcheck="false" style="flex:1">
      <input id="llm-key" value="${preset.secretKey || 'api-key'}" placeholder="key" spellcheck="false" style="width:140px">
    </div>`;
  }
}

function createLLMProvider() {
  const type = document.getElementById('llm-type').value;
  const body = {
    name: document.getElementById('llm-name').value.trim(),
    type: type,
    model: document.getElementById('llm-model').value.trim(),
    endpoint: document.getElementById('llm-endpoint').value.trim(),
    default: document.getElementById('llm-default').value === 'true',
  };

  // Credentials (not for ollama)
  const secretEl = document.getElementById('llm-secret');
  const keyEl = document.getElementById('llm-key');
  if (secretEl && secretEl.value.trim()) {
    body.secretRef = secretEl.value.trim();
    body.secretKey = keyEl ? keyEl.value.trim() : 'api-key';
  }

  // Config from textarea
  const configText = document.getElementById('llm-config').value.trim();
  if (configText) {
    const config = {};
    configText.split('\n').forEach(line => {
      const [k, ...v] = line.split('=');
      if (k && v.length) config[k.trim()] = v.join('=').trim();
    });
    body.config = config;
  }

  if (!body.name) { showResult('llm-result', 'err', 'Name required'); return; }
  if (!body.model) { showResult('llm-result', 'err', 'Model required'); return; }
  if (!body.endpoint && (LLM_PRESETS[type] || {}).endpointRequired) { showResult('llm-result', 'err', 'Endpoint required for type "' + type + '"'); return; }

  fetch('/api/llm/provider', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
    .then(r => r.json())
    .then(d => {
      if (d.error) showResult('llm-result', 'err', 'Error: ' + d.error);
      else { showResult('llm-result', 'ok', `LLMProvider "${d.name}" created!`); setTimeout(loadLLMProviders, 2000); }
    });
}

function deleteLLMProvider(name) {
  if (!confirm(`Delete LLM provider "${name}"?`)) return;
  fetch('/api/llm/provider/' + name, { method: 'DELETE' })
    .then(r => r.json())
    .then(d => { if (d.error) alert('Error: ' + d.error); else loadLLMProviders(); });
}

// Rendered when no LLMProvider exists — without one, workflow steps run in
// demo mode (no real inference), so guide the user to their options.
function emptyProvidersGuide() {
  return `<div class="panel" style="border-style:dashed">
    <h3>No LLM providers yet</h3>
    <div style="color:var(--dim);margin-top:8px;line-height:1.6">
      Agents need an LLM provider to run real inference — without one,
      workflow steps run in <b>demo mode</b> and return placeholder output.
      Pick whichever you have access to:
      <ul style="margin:10px 0 10px 18px;line-height:1.8">
        <li><b>Ollama</b> — local models, no API key (run Ollama in-cluster or on your network)</li>
        <li><b>OpenRouter / any OpenAI-compatible gateway</b> — use the <i>custom</i> type with your base URL</li>
        <li><b>Anthropic</b> or <b>OpenAI</b> — direct APIs, key in a Secret</li>
        <li><b>Vertex AI / Gemini</b> — GCP service-account credentials</li>
      </ul>
      Mark your provider <code class="mono">default: true</code> and every agent
      (including the starter agents, whose spec says <code class="mono">provider: anthropic</code>)
      resolves to it automatically — the provider name on an agent only needs to
      match when you run several providers side by side.
    </div>
    <div style="margin-top:14px">
      <button class="btn btn--primary" onclick="showAddLLMForm()">+ Add Provider</button>
    </div>
  </div>`;
}
