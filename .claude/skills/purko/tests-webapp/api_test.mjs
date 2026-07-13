import test from 'node:test';
import assert from 'node:assert/strict';
import { fetchJSON, fetchText, login, approve, features, connectEvents, ApiError, artifacts, artifactsForRun, artifactContent } from '../webapp/assets/api.js';

function fakeFetch(routes) {
  const calls = [];
  const impl = async (url, opts = {}) => {
    calls.push({ url, opts });
    const hit = routes[url] ?? routes['*'];
    if (!hit) throw new Error(`no route: ${url}`);
    return {
      ok: hit.status >= 200 && hit.status < 300,
      status: hit.status,
      json: async () => hit.body,
      text: async () => hit.text ?? String(hit.body ?? ''),
    };
  };
  impl.calls = calls;
  return impl;
}

test('fetchJSON returns parsed body', async () => {
  const f = fakeFetch({ '/api/agents': { status: 200, body: [{ name: 'atlas' }] } });
  const data = await fetchJSON('/api/agents', { fetchImpl: f });
  assert.equal(data[0].name, 'atlas');
});

test('fetchJSON throws ApiError with status on failure', async () => {
  const f = fakeFetch({ '/api/agents': { status: 401, body: { error: 'unauthorized' } } });
  await assert.rejects(fetchJSON('/api/agents', { fetchImpl: f }),
    (e) => e instanceof ApiError && e.status === 401);
});

test('login posts token JSON and resolves on 204', async () => {
  const f = fakeFetch({ '/local/token': { status: 204, body: null } });
  await login('tok', { fetchImpl: f });
  const call = f.calls[0];
  assert.equal(call.opts.method, 'POST');
  assert.equal(JSON.parse(call.opts.body).token, 'tok');
});

test('approve posts to the exact path with empty body', async () => {
  const f = fakeFetch({ '/api/approve/campaign/copy-review': { status: 200, body: { status: 'approved' } } });
  const res = await approve('campaign', 'copy-review', { fetchImpl: f });
  assert.equal(res.status, 'approved');
  assert.equal(f.calls[0].opts.method, 'POST');
});

test('connectEvents parses each SSE message', () => {
  const seen = [];
  class FakeES {
    constructor(url) { this.url = url; FakeES.last = this; }
    close() { this.closed = true; }
  }
  const conn = connectEvents((snap) => seen.push(snap), { EventSourceImpl: FakeES });
  FakeES.last.onmessage({ data: '{"agentCount": 3}' });
  assert.equal(seen[0].agentCount, 3);
  conn.close();
  assert.equal(FakeES.last.closed, true);
});

test('features() fetches /api/features and returns the body', async () => {
  const body = { artifactsAllowHttp: false, autonomy: true, history: false, historyRetentionDays: 0, intent: false, sso: false };
  const f = fakeFetch({ '/api/features': { status: 200, body } });
  const result = await features({ fetchImpl: f });
  assert.equal(f.calls[0].url, '/api/features');
  assert.deepEqual(result, body);
});

test('connectEvents wires onOpen for reconnect signaling', () => {
  class FakeES {
    constructor(url) { this.url = url; FakeES.last = this; }
    close() {}
  }
  let opened = 0;
  connectEvents(() => {}, { EventSourceImpl: FakeES, onOpen: () => opened++ });
  FakeES.last.onopen();
  FakeES.last.onopen();  // auto-reconnect fires it again
  assert.equal(opened, 2);
});

test('fetchText returns text body from a 200 response', async () => {
  const f = fakeFetch({ '/api/test': { status: 200, text: 'hello world' } });
  const result = await fetchText('/api/test', { fetchImpl: f });
  assert.equal(result, 'hello world');
});

test('fetchText throws ApiError on non-200', async () => {
  const f = fakeFetch({ '/api/test': { status: 404 } });
  await assert.rejects(fetchText('/api/test', { fetchImpl: f }),
    (e) => e instanceof ApiError && e.status === 404);
});

test('artifacts fetches /api/artifacts and returns parsed body', async () => {
  const body = { artifacts: [{ id: 'a1', name: 'report.md' }] };
  const f = fakeFetch({ '/api/artifacts': { status: 200, body } });
  const result = await artifacts({ fetchImpl: f });
  assert.equal(f.calls[0].url, '/api/artifacts');
  assert.deepEqual(result, body);
});

test('artifactsForRun fetches /api/artifacts?run=<runId>', async () => {
  const body = { artifacts: [] };
  const f = fakeFetch({ '/api/artifacts?run=r1': { status: 200, body } });
  await artifactsForRun('r1', { fetchImpl: f });
  assert.equal(f.calls[0].url, '/api/artifacts?run=r1');
});

test('artifactContent fetches text from /api/artifacts/<id>/content', async () => {
  const f = fakeFetch({ '/api/artifacts/a1/content': { status: 200, text: '# markdown content' } });
  const result = await artifactContent('a1', { fetchImpl: f });
  assert.equal(f.calls[0].url, '/api/artifacts/a1/content');
  assert.equal(result, '# markdown content');
});
