# Workflows

A **Workflow** is a directed acyclic graph (DAG) of agent steps executed as Kubernetes Jobs. Each step references one [Agent](agents.md), receives inputs, produces outputs, and can depend on the results of earlier steps.

When you apply a Workflow CR, the controller evaluates the DAG, determines which steps are ready to run, creates a Kubernetes Job for each, and tracks progress. Steps that have no unfulfilled `dependsOn` entries run immediately; the rest are queued until their dependencies complete.

```
kubectl get workflows -n ai-agents
NAME                PHASE       STEPS   COMPLETED   AGE
incident-response   Succeeded   4       4           2h
daily-report        Running     3       1           5m
```

---

## Isolation Model

Each workflow step runs as an **isolated Kubernetes Job** with its own:

- Pod and container (using the executor image)
- ServiceAccount scoped to the step's agent
- Environment variables injected by the controller (model config, step input, tool specs)

This means a single workflow run spawns multiple short-lived pods — one per step. Logs, resource usage, and failures are trackable per step through standard Kubernetes tooling.

---

## Key Concepts

### `dependsOn` — step ordering

`dependsOn` lists the steps that must complete (successfully, unless `failureStrategy: continueOnError`) before this step starts. Steps without `dependsOn` run as soon as the workflow begins.

```yaml
steps:
  - name: scan          # runs immediately
    agentRef:
      name: security-scanner
  - name: report
    agentRef:
      name: report-writer
    dependsOn:
      - scan            # waits for scan to complete
```

### `parallelism` — concurrent step limit

`spec.parallelism` caps how many steps the controller may start simultaneously. With `parallelism: 2`, at most two steps run at any given time, even if more are ready.

```yaml
spec:
  parallelism: 2
```

### `parameters` — input variables

Parameters are key-value pairs defined at the workflow level and substituted into step inputs using `${parameters.KEY}`.

```yaml
spec:
  parameters:
    namespace: production
    severity: critical
  steps:
    - name: investigate
      agentRef:
        name: cluster-investigator
      input:
        raw: '{"task": "Inspect pods in ${parameters.namespace}"}'
```

Parameters make workflows reusable: apply the same workflow twice with different parameter values to target different environments.

### Output passing — `${steps.X.output.response}`

Steps write their output to the Workflow status. Subsequent steps can read it using `${steps.STEP_NAME.output.KEY}` in their input.

```yaml
steps:
  - name: analyze
    agentRef:
      name: log-analyzer
    # analyze writes output.response

  - name: report
    agentRef:
      name: report-writer
    dependsOn: [analyze]
    input:
      raw: '{"findings": "${steps.analyze.output.response}"}'
```

### `condition` — CEL expressions for branching

A step's `condition` field is a CEL (Common Expression Language) expression evaluated before the step runs. If it evaluates to false, the step is marked `Skipped` and downstream steps that depend only on it are also skipped.

```yaml
steps:
  - name: triage
    agentRef:
      name: incident-triager
    dependsOn: [detect]
    condition: 'steps.detect.output.anomaly_count > 0'
```

Common patterns:

```yaml
# Equality check
condition: 'steps.scan.output.status == "critical"'

# Set membership
condition: 'steps.triage.output.severity in ["critical", "high"]'

# Boolean flag
condition: 'steps.check.output.issues_found'
```

### `failureStrategy` — handling step failures

| Value | Behaviour |
|-------|-----------|
| `failFast` | Stop the workflow on the first step failure. Downstream steps are cancelled. (default) |
| `continueOnError` | Mark the failed step and continue running independent downstream steps. |
| `rollback` | Attempt to roll back completed steps, then fail the workflow. |

### `trigger` — how workflows start

| Type | Description |
|------|-------------|
| `manual` | Started by `kubectl apply` or a direct API call. Default for most workflows. |
| `webhook` | Started by an inbound HTTP POST. Useful for GitHub, PagerDuty, or Slack alerts. |
| `schedule` | Started on a cron expression. Useful for daily reports or periodic checks. |

Schedule example:

```yaml
spec:
  trigger:
    type: schedule
    schedule:
      cron: "0 6 * * 1-5"   # 06:00 Mon-Fri
      timezone: UTC
      suspend: false
```

Webhook example:

```yaml
spec:
  trigger:
    type: webhook
    webhook:
      path: /api/trigger/ai-agents/incident-response
      secret:
        name: webhook-secret
```

### `retryPolicy` — step-level retries

Each step can define its own retry behaviour:

| Field | Type | Description |
|-------|------|-------------|
| `maxRetries` | integer | How many times to retry a failed step (default: 3) |
| `backoffSeconds` | integer | Fixed wait between retries |
| `backoff` | string | Duration string override: `10s`, `1m` |
| `retryOn` | []string | Conditions that trigger a retry |

```yaml
steps:
  - name: fetch-data
    agentRef:
      name: data-fetcher
    retryPolicy:
      maxRetries: 3
      backoffSeconds: 10
      retryOn: ["timeout", "rate_limit"]
```

---

## Workflow Lifecycle

| Phase | Meaning |
|-------|---------|
| `Pending` | Workflow CR received; controller has not started reconciling |
| `Running` | At least one step is active |
| `Succeeded` | All steps completed successfully |
| `Failed` | One or more steps failed and `failureStrategy: failFast` was in effect |
| `Cancelled` | A running workflow was explicitly stopped |

---

## Workflow Spec Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `description` | string | — | Human-readable workflow description |
| `steps[]` | list | required | Ordered list of steps (at least one) |
| `parallelism` | integer | 1 | Maximum steps to run simultaneously |
| `failureStrategy` | string | `failFast` | `failFast`, `continueOnError`, or `rollback` |
| `parameters` | map | — | Input variables substituted with `${parameters.X}` |
| `trigger` | object | — | How the workflow is activated |
| `concurrency.policy` | string | — | `allow`, `forbid`, or `replace` for concurrent runs |
| `concurrency.maxParallel` | integer | — | Maximum concurrent workflow instances |
| `edges[]` | list | — | Explicit DAG edges (alternative to `dependsOn`) |
| `errorHandling` | object | — | Extended error handling config |
| `observability` | object | — | Metrics and tracing configuration |
| `timeout` | object | — | Overall workflow timeout |
| `variables[]` | list | — | Workflow-scoped variable declarations |
| `hooks` | object | — | Lifecycle hooks: `onStart`, `onComplete`, `onFailure` |

---

## Example YAML — 4-step workflow with parallelism and conditions

```yaml
apiVersion: purko.io/v1alpha1
kind: Workflow
metadata:
  name: incident-response
  namespace: ai-agents
spec:
  description: "Automated incident response pipeline"
  parallelism: 2
  failureStrategy: continueOnError

  parameters:
    namespace: production
    severity: critical

  trigger:
    type: webhook
    webhook:
      path: /api/trigger/ai-agents/incident-response
      secret:
        name: pagerduty-webhook-secret

  steps:
    # Step 1: investigate — runs immediately
    - name: investigate
      agentRef:
        name: cluster-investigator
      input:
        raw: '{"task": "Inspect namespace ${parameters.namespace} for ${parameters.severity} issues"}'
      retryPolicy:
        maxRetries: 2
        backoffSeconds: 15

    # Step 2: analyze-logs — runs in parallel with step 3 once step 1 completes
    - name: analyze-logs
      agentRef:
        name: log-analyzer
      dependsOn:
        - investigate
      condition: 'steps.investigate.output.response != ""'
      input:
        raw: '{"task": "Analyze logs. Context: ${steps.investigate.output.response}"}'

    # Step 3: check-metrics — runs in parallel with step 2
    - name: check-metrics
      agentRef:
        name: metrics-monitor
      dependsOn:
        - investigate
      input:
        raw: '{"task": "Check Prometheus metrics for ${parameters.namespace}"}'

    # Step 4: generate-rca — runs only if analysis found something
    - name: generate-rca
      agentRef:
        name: rca-investigator
      dependsOn:
        - analyze-logs
        - check-metrics
      condition: 'steps.analyze-logs.output.issues_found'
      input:
        raw: |
          {
            "task": "Generate RCA report",
            "log_findings": "${steps.analyze-logs.output.response}",
            "metric_findings": "${steps.check-metrics.output.response}"
          }
```

!!! tip
    Steps `analyze-logs` and `check-metrics` both depend on `investigate` and have no dependency on each other. With `parallelism: 2`, the controller starts them both as soon as `investigate` completes.

!!! warning
    `${steps.X.output.Y}` substitution is done by the controller before the Job pod starts. If step X was skipped or failed, the substitution resolves to an empty string. Always guard downstream steps with a `condition` when the upstream output is required.

---

## See Also

- [Workflow CRD Reference](../reference/crd-workflow.md)
- [Agents](agents.md)
- [Tool Types](tool-types.md)
