# Workflow CRD

**API Version:** `purko.io/v1alpha1`
**Kind:** `Workflow`
**Scope:** Namespaced

A Workflow is a Kubernetes resource that defines a DAG of agent steps, their dependencies, triggers, concurrency policy, and failure handling.

## Example

```yaml
apiVersion: purko.io/v1alpha1
kind: Workflow
metadata:
  name: sdlc-feature-development
  namespace: ai-agents
  labels:
    app.kubernetes.io/component: sdlc
spec:
  description: "Full SDLC: requirements → design → implement → test → review"
  parallelism: 2
  failureStrategy: failFast
  parameters:
    repository: "my-org/my-repo"
    branch: "main"
    featureTicket: "PROJ-001"
  steps:
    - name: analyze-requirements
      agentRef:
        name: requirements-analyst
      input:
        raw: '{"task": "Analyze: ${parameters.featureTicket}"}'
      timeout:
        timeoutSeconds: 600

    - name: design-solution
      agentRef:
        name: architecture-designer
      dependsOn: [analyze-requirements]
      condition: 'steps.analyze-requirements.output.feasibility != rejected'
      timeout:
        timeoutSeconds: 900

    - name: implement-feature
      agentRef:
        name: code-generator
      dependsOn: [design-solution]
      retryPolicy:
        maxRetries: 2
        backoffSeconds: 30
        backoff: exponential
      timeout:
        timeoutSeconds: 1800
```

## Spec Fields

### WorkflowSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | No | Human-readable description of the workflow |
| `steps` | [][WorkflowStep](#workflowstep) | Yes | Ordered list of agent steps forming the DAG |
| `parallelism` | int | No | Maximum number of steps running concurrently |
| `failureStrategy` | string | No | `failFast` (default) or `continueOnError` |
| `parameters` | map[string]string | No | Key-value parameters; referenced via `${parameters.key}` in step inputs |
| `edges` | [][Edge](#edge) | No | Explicit DAG edges with optional conditions (alternative to `dependsOn`) |
| `trigger` | [TriggerSpec](#triggerspec) | No | Automatic trigger configuration |
| `concurrency` | [ConcurrencySpec](#concurrencyspec) | No | Concurrency policy for overlapping runs |
| `errorHandling` | object | No | Free-form error handling configuration |
| `observability` | object | No | Free-form observability hooks |
| `timeout` | object | No | Free-form workflow-level timeout |
| `variables` | []object | No | Free-form variable declarations |
| `hooks` | object | No | Free-form lifecycle hooks (onStart, onComplete, onFailure) |

### WorkflowStep

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique step name within the workflow |
| `agentRef` | [AgentRef](#agentref) | No | Reference to the Agent that executes this step |
| `type` | string | No | Step type hint (e.g. `analysis`, `implementation`, `review`) |
| `description` | string | No | Human-readable step description |
| `dependsOn` | []string | No | Names of steps that must complete before this step runs |
| `inputFrom` | [][InputRef](#inputref) | No | Pull specific output keys from prior steps into this step's input |
| `retryPolicy` | [RetryPolicySpec](#retrypolicyspec) | No | Retry configuration for this step |
| `timeout` | [StepTimeoutSpec](#steptimeoutspec) | No | Per-step execution timeout |
| `condition` | string | No | CEL expression; step is skipped if it evaluates to `false` |
| `input` | object | No | Free-form step input (JSON); supports `${parameters.key}` substitution |
| `output` | object | No | Free-form output schema or transformation spec |
| `config` | object | No | Free-form step-level config |

!!! note "agentRef vs type"
    `agentRef` is optional **only when the step sets `type`** (a built-in
    step kind). Every step must have one or the other — the validating
    webhook rejects steps with neither (`step must have either
    agentRef.name or type`). A step with `agentRef` runs that agent in an
    executor Job; a step with `type` runs built-in behavior.


### AgentRef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Name of the Agent resource |
| `namespace` | string | No | Namespace of the Agent (defaults to workflow namespace) |

### InputRef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `step` | string | Yes | Name of the upstream step to pull output from |
| `outputKey` | string | Yes | Key within the upstream step's output JSON |

### RetryPolicySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `maxRetries` | int | No | Maximum retry attempts (default 0) |
| `backoffSeconds` | int | No | Initial backoff delay in seconds |
| `backoff` | string | No | Backoff strategy: `fixed`, `exponential` |
| `retryOn` | []string | No | Error classes that trigger a retry (e.g. `timeout`, `error`) |

### StepTimeoutSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `timeoutSeconds` | int | No | Maximum wall-clock time for the step in seconds |

### Edge

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `from` | string | Yes | Source step name |
| `to` | string | Yes | Target step name |
| `condition` | object | No | Free-form condition for this edge |

### TriggerSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Trigger type: `schedule`, `webhook`, `event` |
| `schedule` | [ScheduleTrigger](#scheduletrigger) | No | Cron schedule configuration |
| `webhook` | [WebhookTrigger](#webhooktrigger) | No | Webhook trigger configuration |
| `eventTriggers` | []object | No | Free-form event trigger configurations |

### ScheduleTrigger

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cron` | string | Yes | Cron expression (e.g. `0 9 * * 1-5`) |
| `timezone` | string | No | IANA timezone name (e.g. `America/New_York`) |
| `suspend` | bool | No | Suspend the schedule without deleting the workflow |

### WebhookTrigger

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | No | Webhook path suffix (appended to `/api/trigger/{namespace}/`) |
| `secret` | SecretRef | No | Secret containing HMAC signing key for webhook validation (`name`, `namespace`, `key`) |

### ConcurrencySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `policy` | string | No | `Forbid` (skip new run if one is active) or `Replace` (cancel active run) |
| `maxParallel` | int | No | Maximum concurrent workflow instances |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Overall workflow phase: `Pending`, `Running`, `Succeeded`, `Failed`, `Cancelled` |
| `message` | string | Human-readable status message |
| `observedGeneration` | int64 | Most recent spec generation reconciled |
| `totalSteps` | int | Total number of steps in the workflow |
| `completedSteps` | int | Number of steps that have reached `Succeeded` |
| `failedSteps` | int | Number of steps that reached `Failed` |
| `startTime` | timestamp | When the first step started |
| `completionTime` | timestamp | When the workflow reached a terminal phase |
| `stepStatuses` | [][StepStatus](#stepstatus) | Per-step execution state |
| `conditions` | []Condition | Standard Kubernetes conditions |
| `activeRuns` | int | Number of concurrently active workflow instances (template mode) |
| `lastTriggerTime` | timestamp | When the workflow was last triggered |
| `nextRunTime` | timestamp | Scheduled next run time (schedule triggers) |

### StepStatus

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Step name |
| `phase` | string | Step phase: `Pending`, `Running`, `Succeeded`, `Failed` |
| `jobName` | string | Name of the Kubernetes Job created for this step |
| `startTime` | timestamp | When the step started |
| `completionTime` | timestamp | When the step completed |
| `output` | object | JSON output from the executor's `OUTPUT:` line |
| `error` | string | Error message if the step failed |
| `retryCount` | int | Number of retries attempted |

## Conditions

| Condition | Meaning |
|-----------|---------|
| `Complete` | All steps succeeded |
| `Failed` | One or more steps failed (and `failureStrategy` is `failFast`) |
| `Cancelled` | Workflow was cancelled via the `purko.io/cancel` annotation |

## Annotations

The controller reads and writes these annotations:

| Annotation | Direction | Meaning |
|-----------|-----------|---------|
| `purko.io/approve-{step}` | Write to approve | Set to `"true"` to approve a pending step |
| `purko.io/deny-{step}` | Write to deny | Set to `"true"` to deny a pending step |
| `purko.io/cancel` | Write to cancel | Set to `"true"` to cancel a running workflow |
| `purko.io/rerun` | Write to rerun | RFC3339 timestamp; triggers a re-run |
| `purko.io/workflow-template` | Read-only | Name of the template workflow this run was created from |
| `purko.io/trigger-type` | Read-only | How the run was triggered (`webhook`, `schedule`, `webhook-auto`) |
| `purko.io/trigger-source` | Read-only | Source system (`github`, `pagerduty`, `slack`, `unknown`) |
| `purko.io/trigger-route` | Read-only | Route method (`explicit`, `rule:name`, `llm-intent`) |

## Related Resources

- [Agent CRD](crd-agent.md) — agents referenced in workflow steps
- [Executor Protocol](executor-protocol.md) — how step containers receive input and return output
- [Concepts: Workflows](../concepts/workflows.md)
- [Guide: Building Workflows](../guides/building-workflows.md)
