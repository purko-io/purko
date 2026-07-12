# Workflow Authoring Guide

> **Judgment layer.** Generated from the purko showcase library and CRD types.
> Gives the skill *how to compose steps* — structure, interpolation, gates,
> triggers. Live cluster queries supply real agent names and other facts.

---

## Workflow CRD Shape

```yaml
apiVersion: purko.io/v1alpha1
kind: Workflow
metadata:
  name: <name>
  namespace: <namespace>
spec:
  description: "<human summary>"

  parameters:               # named inputs; referenced as ${parameters.KEY}
    clientName: "Acme Corp"
    contractType: "SaaS agreement"

  parallelism: 2            # max simultaneous running steps (optional)
  failureStrategy: continueOnError  # failFast | continueOnError | rollback

  trigger:                  # optional; omit for on-demand workflows
    type: webhook           # webhook | schedule
    webhook:
      path: /custom-path    # default: /api/trigger/<ns>/<name>
      secret:
        name: wh-secret
    schedule:
      cron: "0 8 * * 1"    # standard cron; runs in UTC unless timezone set
      timezone: "America/New_York"

  steps:
    - name: step-one                  # [a-zA-Z0-9_.-]{1,63}, unique
      agentRef:
        name: <agent-name>            # must exist in the namespace (WF-005)
      input:
        raw: |
          Context: ${parameters.clientName}
      # No dependsOn = runs immediately (root step)

    - name: step-two
      agentRef:
        name: <another-agent>
      dependsOn: [step-one]           # wait for step-one to complete
      input:
        raw: |
          Previous output: ${steps.step-one.output.response}

    - name: step-three
      agentRef:
        name: <agent>
      dependsOn: [step-one]           # same dependsOn as step-two = parallel fan-out

    - name: step-join
      agentRef:
        name: <agent>
      dependsOn: [step-two, step-three]   # waits for BOTH = fan-in / join
      input:
        raw: |
          Two: ${steps.step-two.output.response}
          Three: ${steps.step-three.output.response}

  timeout: "2h"             # optional workflow-level timeout; gate-wait excluded
```

---

## Step Fields

| Field | Required | Notes |
|-------|----------|-------|
| `name` | Yes | Pattern `[a-zA-Z0-9_.-]{1,63}`. Must be unique within the workflow (WF-002). |
| `agentRef.name` | Yes* | Agent CR name in the same namespace. WF-005 rejects non-existent agents at admission. *Required unless `type` is set. |
| `dependsOn` | No | List of step names this step waits for. Referenced names must exist (WF-003). Omit for root steps (run immediately). Cannot depend on itself (WF-003). |
| `input.raw` | No | Free-form string passed to the agent as its task input. Supports `${parameters.X}` and `${steps.Y.output.response}` interpolation. |
| `timeout.timeoutSeconds` | No | Per-step timeout in seconds. Warning (not rejection) if it exceeds the workflow timeout (WF-006). |
| `retryPolicy` | No | `maxRetries`, `backoffSeconds`, `backoff` (fixed/exponential), `retryOn` (list of error codes). |
| `condition` | No | CEL expression for conditional execution. Must reference `steps.<name>.output`. |

---

## Variable Interpolation

Two interpolation namespaces are available inside `input.raw`:

```
${parameters.KEY}                    # workflow-level parameter
${steps.STEP_NAME.output.response}   # full text output of a completed step
```

**Rules:**
- `${parameters.KEY}` is resolved at workflow start from `spec.parameters`.
- `${steps.STEP_NAME.output.response}` is only available after the named step completes. Use `dependsOn` to enforce ordering.
- Step names in variable references must exist (WF-009 validates this in condition expressions; inputs are checked at runtime).
- Parameter keys and step names must match `[a-zA-Z0-9_.-]{1,63}` (WF-010, XSS defense).

**Example from real-estate showcase:**
```yaml
- name: create-listing
  agentRef:
    name: listing-writer
  dependsOn: [market-analysis]
  input:
    raw: |
      Address: ${parameters.propertyAddress}
      Specs: ${parameters.bedrooms}bd/${parameters.bathrooms}ba
      Market analysis: ${steps.market-analysis.output.response}
```

> **WARNING:** Many showcase YAMLs use the bare `${steps.<name>.output}` form (no key). The controller regex does **not** substitute that form — it passes the literal string unchanged. Always use `${steps.<name>.output.response}` (or another named key such as `.output.summary`). The bare `.output` form is a known bug in the showcase sources.

---

## Parallel Fan-Out

Multiple steps with the same `dependsOn` entry run in parallel (up to `spec.parallelism`):

```yaml
steps:
  - name: strategy         # root step
    agentRef:
      name: campaign-strategist
    input:
      raw: Brief: ${parameters.briefContent}

  - name: write-social     # these three run in parallel
    agentRef:
      name: content-writer
    dependsOn: [strategy]

  - name: write-email
    agentRef:
      name: content-writer
    dependsOn: [strategy]

  - name: write-blog
    agentRef:
      name: content-writer
    dependsOn: [strategy]

  - name: review-content   # waits for all three
    agentRef:
      name: brand-reviewer
    dependsOn: [write-social, write-email, write-blog]
```

This pattern appears in `digital-agency/campaign-delivery` and `legal-compliance/contract-review`.

---

## Human Approval Gates

When an agent has `guardrails.humanApprovalRequired: true`, its step **pauses** and waits for a human action before the executor runs.

**How it works:**
1. The workflow reaches the gated step.
2. The step status becomes `Pending` / waiting for approval.
3. A human approves via:
   - Dashboard (Mission Control → workflow run → Approve button on the step)
   - `kubectl annotate workflow/<name> -n <ns> purko.io/approve-<step-name>=true`
4. The workflow resumes and the step executes.

**Timeout note:** Gate-wait time is excluded from `spec.timeout` and per-step `timeout.timeoutSeconds`. The workflow clock pauses while the gate is open. (Spec 39 §39.)

**When to add a gate:**
- Any step whose output is externally delivered (report sent to client, content published, API called with side effects).
- Any step whose failure would cause legal or financial liability.
- Reviewer steps in high-stakes pipelines (see `agents.md` §humanApprovalRequired).

---

## Triggers

### On-demand (default)

Omit `spec.trigger`. Trigger manually:
- Dashboard: Mission Control → Workflow → Run
- API: `POST /api/trigger/<namespace>/<workflow-name>` with JSON body `{"parameters": {...}}`

### Webhook trigger

```yaml
trigger:
  type: webhook
  webhook:
    secret:
      name: my-webhook-secret   # Secret in the same namespace; purko validates the HMAC
```

Purko registers the endpoint at `POST /api/trigger/<namespace>/<workflow-name>`.
Send parameters in the JSON body: `{"parameters": {"clientName": "Acme"}}`.

### Schedule trigger

```yaml
trigger:
  type: schedule
  schedule:
    cron: "0 8 * * 1"         # Every Monday at 08:00 UTC
    timezone: "America/New_York"
    suspend: false             # set true to pause without deleting
```

Standard cron syntax (5 fields). The `concurrency` field controls what happens when a new run fires while the previous is still executing:

```yaml
concurrency:
  policy: Forbid              # skip new run if previous is still active
  # OR:
  policy: Replace             # cancel previous, start new
```

---

## Failure Strategy

| Value | Behaviour |
|-------|-----------|
| `failFast` | Stop the workflow on first step failure. Other running steps are cancelled. |
| `continueOnError` | Continue executing independent steps even when one fails. Default in all showcases. |
| `rollback` | Stop and attempt rollback (platform-dependent). |

The showcases use `continueOnError` universally — downstream steps that reference a failed step's output receive an empty string and should handle that gracefully in their prompts.

---

## Parallelism

`spec.parallelism` limits how many steps run concurrently, independent of the dependency graph. Set it to avoid saturating the cluster or the model's rate limits.

All showcase workflows use `parallelism: 2` or `parallelism: 3` as a conservative starting point.

---

## Step Timeout

```yaml
steps:
  - name: slow-step
    agentRef:
      name: my-agent
    timeout:
      timeoutSeconds: 300     # 5 minutes max; the step is killed if exceeded
```

If per-step timeout exceeds the workflow-level `spec.timeout`, the webhook emits a warning (WF-006) but does not reject the manifest.

---

## Validation Rules (What Gets Rejected)

| Code | Rule |
|------|------|
| WF-001 | At least one step required |
| WF-002 | Step names must be non-empty and unique |
| WF-003 | `dependsOn` entries must reference existing step names; a step cannot depend on itself |
| WF-004 | No dependency cycles (topological sort) |
| WF-005 | Every `agentRef.name` must exist as an Agent CR in the same namespace |
| WF-006 | Warning (not rejection) if step timeout > workflow timeout |
| WF-007 | Condition expressions must reference `steps.<name>.output` |
| WF-008 | Max 50 steps |
| WF-009 | Variable references in conditions must name existing steps |
| WF-010 | Step names and parameter keys must match `[a-zA-Z0-9_.-]{1,63}` |
| — | `failureStrategy` must be `failFast`, `continueOnError`, or `rollback` |
| — | `agentRef.name` or `type` required per step |

**WF-005 is the most common authoring error.** Every agent referenced in a step must be applied to the cluster before applying the workflow. The skill's guided-create flow applies agents first, then the workflow.
