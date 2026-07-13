import test from 'node:test';
import assert from 'node:assert/strict';
import {
  esc, stepState, wfState, findGates, diffSnapshots, sortWorkflows,
  renderWorkflows, renderAgents, renderEmptyState, renderNudge,
  bytesHuman, renderArtifacts, renderArtifactContent,
} from '../webapp/assets/render.js';

const wfRunning = {
  name: 'campaign', namespace: 'ai-agents', phase: 'Running',
  totalSteps: 3, completedSteps: 1, failedSteps: 0, duration: '2m',
  steps: [
    { name: 'draft', phase: 'Succeeded', agent: 'quill', dependsOn: [] },
    { name: 'copy-review', phase: 'Pending', agent: 'sage', dependsOn: ['draft'] },
    { name: 'publish', phase: 'Pending', agent: 'herald', dependsOn: ['copy-review'] },
  ],
};

test('esc escapes html-significant characters including quotes', () => {
  assert.equal(esc(`<img src=x onerror='a'>"&`),
    '&lt;img src=x onerror=&#39;a&#39;&gt;&quot;&amp;');
});

test('gate detected on first Pending step of a Running workflow', () => {
  const s = stepState(wfRunning, wfRunning.steps[1]);
  assert.equal(s.cls, 'gated');
  const gates = findGates([wfRunning]);
  assert.equal(gates.length, 1);
  assert.equal(gates[0].step, 'copy-review');
});

test('later Pending steps are queued, not gated', () => {
  assert.equal(stepState(wfRunning, wfRunning.steps[2]).cls, 'queued');
});

test('wfState prefers gate over running and includes a label', () => {
  const s = wfState(wfRunning);
  assert.equal(s.cls, 'waiting');
  assert.match(s.label, /copy-review/);
});

test('diffSnapshots reports phase transitions and nothing else', () => {
  const prev = { workflows: [wfRunning] };
  const next = {
    workflows: [{
      ...wfRunning,
      steps: [
        { ...wfRunning.steps[0] },
        { ...wfRunning.steps[1], phase: 'Succeeded' },
        { ...wfRunning.steps[2], phase: 'Running' },
      ],
    }],
  };
  const entries = diffSnapshots(prev, next);
  assert.equal(entries.length, 2);
  assert.match(entries[0].text, /copy-review/);
  assert.deepEqual(diffSnapshots(null, next), []);
});

test('sortWorkflows orders gate > running > failures > other > succeeded', () => {
  const ok = { name: 'done-wf', phase: 'Succeeded', failedSteps: 0, steps: [] };
  const failed = { name: 'failed-wf', phase: 'Failed', failedSteps: 1, steps: [] };
  const withErrors = { name: 'errors-wf', phase: 'CompletedWithErrors', failedSteps: 1, steps: [] };
  const running = {
    name: 'running-wf', phase: 'Running', failedSteps: 0,
    steps: [{ name: 's1', phase: 'Running' }],
  };
  const gated = {
    name: 'gated-wf', phase: 'Running', failedSteps: 0,
    steps: [{ name: 'approve-me', phase: 'Pending' }],
  };
  const sorted = sortWorkflows([ok, failed, withErrors, running, gated]);
  assert.deepEqual(sorted.map((w) => w.name),
    ['gated-wf', 'running-wf', 'failed-wf', 'errors-wf', 'done-wf']);
});

test('sortWorkflows is stable within a state group and does not mutate input', () => {
  const a = { name: 'a', phase: 'Succeeded', failedSteps: 0, steps: [] };
  const b = { name: 'b', phase: 'Succeeded', failedSteps: 0, steps: [] };
  const input = [a, b];
  const sorted = sortWorkflows(input);
  assert.deepEqual(sorted.map((w) => w.name), ['a', 'b']);
  assert.deepEqual(input.map((w) => w.name), ['a', 'b']);
  assert.deepEqual(sortWorkflows(null), []);
});

test('renderWorkflows puts a running workflow above succeeded ones', () => {
  const ok = { name: 'zz-done', namespace: 'ai-agents', phase: 'Succeeded', failedSteps: 0, steps: [] };
  const html = renderWorkflows([ok, wfRunning]);
  assert.ok(html.indexOf('data-name="campaign"') < html.indexOf('data-name="zz-done"'));
});

test('renderWorkflows escapes malicious names', () => {
  const evil = { ...wfRunning, name: `<script>x</script>` };
  const html = renderWorkflows([evil]);
  assert.ok(!html.includes('<script>x</script>'));
  assert.ok(html.includes('&lt;script&gt;'));
});

test('renderAgents shows status label text, never color-alone', () => {
  const html = renderAgents([{ name: 'atlas', model: 'sonnet', phase: 'Ready', toolCount: 2 }]);
  assert.match(html, /Ready/);
});

test('renderEmptyState offers starter team and first agent', () => {
  const html = renderEmptyState();
  assert.match(html, /starter team/i);
  assert.match(html, /first agent/i);
});

test('renderNudge returns empty string when features is null', () => {
  assert.equal(renderNudge(null), '');
});

test('renderNudge returns empty string when all relevant features are enabled (pro shape)', () => {
  assert.equal(renderNudge({
    artifactsAllowHttp: true, autonomy: true,
    history: true, historyRetentionDays: 0,
    intent: true, sso: true,
  }), '');
});

test('renderNudge community shape (limited retention) nudges for full run history', () => {
  const html = renderNudge({
    history: true, historyRetentionDays: 7, sso: false, intent: false,
  });
  assert.match(html, /hosted Purko/);
  assert.match(html, /href="\/onboarding\.html#plans"/);
  assert.match(html, /full run history/);
  assert.match(html, /team SSO/);
  assert.match(html, /AI follow-ups/);
  assert.match(html, /aria-label="Dismiss"/);
  assert.match(html, /data-action="dismiss-nudge"/);
});

test('renderNudge pro shape (unlimited retention, sso, intent) returns empty string', () => {
  assert.equal(renderNudge({
    history: true, historyRetentionDays: 0, sso: true, intent: true,
  }), '');
});

test('renderNudge history===false always nudges regardless of retention days', () => {
  const html = renderNudge({
    history: false, historyRetentionDays: 0, sso: true, intent: true,
  });
  assert.match(html, /full run history/);
});

test('renderNudge absent historyRetentionDays field does not nudge history alone', () => {
  assert.equal(renderNudge({ history: true, sso: true, intent: true }), '');
});

test('bytesHuman formats bytes, KB, and MB correctly', () => {
  assert.equal(bytesHuman(0), '0 B');
  assert.equal(bytesHuman(500), '500 B');
  assert.equal(bytesHuman(1536), '1.5 KB');
  assert.equal(bytesHuman(2 * 1024 * 1024), '2.0 MB');
});

test('renderArtifacts empty state contains expected message', () => {
  const html = renderArtifacts([]);
  assert.match(html, /No outputs yet/);
  assert.match(html, /completed steps produce artifacts/);
});

test('renderArtifacts null is treated as empty', () => {
  const html = renderArtifacts(null);
  assert.match(html, /No outputs yet/);
});

test('renderArtifacts renders name, badge, provenance, and view button', () => {
  const artifact = {
    id: 'a1', name: 'report.md', kind: 'step-output', mime: 'text/markdown',
    step: 'draft', agent: 'quill', size: 1024, state: 'stored',
  };
  const html = renderArtifacts([artifact]);
  assert.match(html, /report\.md/);
  assert.match(html, /step-output/);
  assert.match(html, /text\/markdown/);
  assert.match(html, /draft/);
  assert.match(html, /quill/);
  assert.match(html, /data-action="view-artifact"/);
  assert.match(html, /data-id="a1"/);
  assert.match(html, /view/);
});

test('renderArtifacts escapes XSS in artifact name', () => {
  const artifact = {
    id: 'x', name: '<script>alert(1)</script>', kind: 'k', mime: 'text/plain',
    step: 's', agent: 'a', size: 0,
  };
  const html = renderArtifacts([artifact]);
  assert.ok(!html.includes('<script>alert(1)</script>'));
  assert.match(html, /&lt;script&gt;/);
});

test('renderArtifactContent text/markdown renders as pre with escaped content', () => {
  const html = renderArtifactContent('report.md', 'text/markdown', '# Hello\n<b>world</b>');
  assert.match(html, /<pre/);
  assert.match(html, /Hello/);
  assert.match(html, /&lt;b&gt;/);
  assert.ok(!html.includes('<b>world</b>'));
});

test('renderArtifactContent text/plain renders as pre with escaping', () => {
  const html = renderArtifactContent('output.txt', 'text/plain', '<b>test</b>');
  assert.match(html, /<pre/);
  assert.match(html, /&lt;b&gt;test&lt;\/b&gt;/);
  assert.ok(!html.includes('<b>test</b>'));
});

test('renderArtifactContent escapes XSS in name', () => {
  const html = renderArtifactContent('<img src=x onerror=alert(1)>', 'text/plain', 'ok');
  assert.ok(!html.includes('<img'));
  assert.match(html, /&lt;img/);
});

test('renderArtifactContent non-text mime shows download note', () => {
  const html = renderArtifactContent('image.png', 'image/png', '');
  assert.match(html, /download/i);
  assert.match(html, /image\/png/);
  assert.ok(!html.includes('<pre'));
});

test('renderArtifacts escapes XSS in attribute-context fields (id, step, agent, mime)', () => {
  const xss = '"><img src=x onerror=alert(1)>';
  const artifact = {
    id: xss, name: 'safe.txt', kind: 'step-output', mime: xss,
    step: xss, agent: xss, size: 0,
  };
  const html = renderArtifacts([artifact]);
  assert.ok(!html.includes('<img'), 'raw <img element must not appear in output');
  assert.ok(html.includes('&lt;img'), 'img must appear entity-encoded as &lt;img');
  assert.ok(html.includes('&quot;'), '"must be entity-encoded to prevent attribute breakout');
});

test('renderArtifacts omits provenance span when both step and agent are absent', () => {
  const artifact = {
    id: 'b1', name: 'out.bin', kind: 'binary', mime: 'application/octet-stream',
    size: 42,
  };
  const html = renderArtifacts([artifact]);
  assert.ok(!html.includes('artifact-provenance'), 'no provenance span when step and agent are absent');
});
