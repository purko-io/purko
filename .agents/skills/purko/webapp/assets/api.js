// Data layer for Mission Control. All network access goes through here.

export class ApiError extends Error {
  constructor(status, message) {
    super(message || `HTTP ${status}`);
    this.status = status;
  }
}

export async function fetchJSON(path, { fetchImpl = globalThis.fetch, ...opts } = {}) {
  const r = await fetchImpl(path, opts);
  if (!r.ok) throw new ApiError(r.status);
  if (r.status === 204) return null;
  return r.json();
}

export async function fetchText(path, { fetchImpl = globalThis.fetch, ...opts } = {}) {
  const r = await fetchImpl(path, opts);
  if (!r.ok) throw new ApiError(r.status);
  return r.text();
}

export function localStatus(opts = {}) {
  return fetchJSON('/local/status', opts);
}

export function version(opts = {}) {
  return fetchJSON('/local/version', opts);
}

export function login(token, opts = {}) {
  return fetchJSON('/local/token', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
    ...opts,
  });
}

export function whoami(opts = {}) {
  return fetchJSON('/api/whoami', opts);
}

export function approve(workflow, step, opts = {}) {
  return fetchJSON(`/api/approve/${encodeURIComponent(workflow)}/${encodeURIComponent(step)}`,
    { method: 'POST', ...opts });
}

export function deny(workflow, step, opts = {}) {
  return fetchJSON(`/api/deny/${encodeURIComponent(workflow)}/${encodeURIComponent(step)}`,
    { method: 'POST', ...opts });
}

export function features(opts = {}) {
  return fetchJSON('/api/features', opts);
}

export function artifacts(opts = {}) {
  return fetchJSON('/api/artifacts', opts);
}

export function artifactsForRun(runId, opts = {}) {
  return fetchJSON('/api/artifacts?run=' + encodeURIComponent(runId), opts);
}

export function artifactContent(id, opts = {}) {
  return fetchText(`/api/artifacts/${encodeURIComponent(id)}/content`, opts);
}

export function connectEvents(onSnapshot, { EventSourceImpl = globalThis.EventSource, onError, onOpen } = {}) {
  const es = new EventSourceImpl('/api/events');
  es.onmessage = (e) => {
    let parsed;
    try { parsed = JSON.parse(e.data); } catch { return; /* skip bad frame */ }
    onSnapshot(parsed);
  };
  if (onError) es.onerror = onError;
  if (onOpen) es.onopen = onOpen;   // fires on connect AND every auto-reconnect
  return { close: () => es.close() };
}
