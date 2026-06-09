# Controllers

The purko-operator registers five controllers against the Kubernetes API server using [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime). Each controller watches a specific CRD (or set of resources) and reconciles the desired state declared in the spec against the actual state in the cluster.

---

## Five Controllers — Overview

| Controller | CRD Watched | Resources Created | Key Responsibility |
|-----------|-------------|-------------------|--------------------|
| `AgentReconciler` | `Agent` | ServiceAccount, Role, RoleBinding | Validates model config, provisions per-agent RBAC, sets `phase=Ready` |
| `WorkflowReconciler` | `Workflow`, `Job` | `Job` (one per step) | DAG execution, condition evaluation, output capture, variable substitution |
| `AutonomyReconciler` | `AgentAutonomyPolicy`, `Agent` | (updates Agent status/spec) | Evaluates agents every 5 minutes, promotes/demotes Shu-Ha-Ri level |
| `MCPServerReconciler` | `MCPServer` | `Deployment`, `Service`, `ConfigMap` entry | Tool lifecycle: deploy, register in registry, clean up on delete |
| `LLMProviderReconciler` | `LLMProvider` | (status only) | Credential validation, health checks, available model tracking |

All five are registered in `cmd/operator/main.go` and share the same controller-runtime `Manager`, which provides a shared cache, scheme, and leader election lease.

---

## Agent Controller

The `AgentReconciler` treats agents as configuration objects — it never creates pods or deployments.

**Reconciliation steps:**

1. Fetch the `Agent` CR.
2. Validate model spec: provider, name, and credentials secret reference.
3. Validate archetype constraints (if an archetype is set, required fields must be present).
4. Set `status.phase = Ready` and update conditions.
5. Emit a Kubernetes event for any validation failure.

The controller is intentionally lightweight. Heavy work (tool calls, LLM inference) happens in executor pods created by the Workflow controller.

---

## Workflow Controller

`WorkflowReconciler` is the most complex controller. It implements a DAG scheduler on top of Kubernetes Jobs.

### Reconciliation Loop (Pseudocode)

```
Reconcile(workflow):
  if phase in {Succeeded, Failed, Cancelled}:
    return  # terminal state, nothing to do

  if workflow is being deleted:
    delete all child Jobs
    remove finalizer
    return

  if workflow has no run-id:
    generate run-id from UID + random suffix
    set phase = Running

  check global timeout → if exceeded, set phase = Failed

  # --- Step status sync ---
  for each Job owned by this workflow:
    find matching step status
    if Job succeeded:
      read pod logs → extract OUTPUT:{json}
      store output in {workflow-name}-outputs ConfigMap
      set step phase = Succeeded
    if Job failed:
      read error from pod logs
      set step phase = Failed
      increment failedSteps counter

  # --- Failure strategy ---
  if failedSteps > 0 and failureStrategy == "failFast":
    cancel pending/running steps → set phase = Failed

  if failedSteps > 0 and failureStrategy == "rollback":
    create rollback Jobs for completed steps (in reverse order)
    set phase = RollingBack

  if failedSteps > 0 and failureStrategy == "stop":
    cancel pending steps → set phase = Failed

  # --- Terminal check ---
  if completedSteps + failedSteps >= totalSteps:
    set phase = Succeeded or Failed

  # --- Schedule ready steps ---
  load {workflow}-outputs ConfigMap (for condition evaluation + inputFrom)
  executableSteps = findExecutableSteps(workflow, currentJobs)

  for each step in executableSteps:
    evaluate conditionExpr against outputs ConfigMap
      → if false: mark step Succeeded (skipped), continue

    check humanApprovalRequired in guardrails
      → if true and annotation purko.io/approve-{step} != "true":
        set step error = "Waiting for approval"
        continue

    resolve step input:
      substitute ${parameters.X} from workflow.spec.parameters
      resolve inputFrom: read {workflow}-outputs ConfigMap → inject as env vars

    call buildStepJob(workflow, step, agent, runID, input, mcpServers)
    create Job in agent namespace
    set step phase = Running

  requeue after 10s
```

### Finding Executable Steps

`findExecutableSteps` returns steps where all dependencies in `dependsOn` have completed (either Succeeded or Failed, depending on `continueOnError` policy) and the step is not already Running or complete.

```go
// Simplified logic from workflow_controller.go
func findExecutableSteps(wf, jobMap) []string {
    finished := set of step names with phase Succeeded or Failed
    running  := set of step names with existing Job

    executable := []
    for each step:
        if step already running or finished: skip
        if all step.dependsOn are in finished: add to executable
    return executable
}
```

### Variable Substitution

Two substitution patterns are supported:

- `${parameters.X}` — replaced with values from `workflow.spec.parameters` before job creation.
- `${steps.X.output.Y}` — resolved by reading the `{workflow}-outputs` ConfigMap at job creation time.

Unresolved references cause the step input to contain the literal placeholder string, not a runtime error, so operators can check ConfigMap contents when debugging.

---

## Job Builder

`buildStepJob` in `controllers/job_builder.go` constructs the `batchv1.Job` spec for each workflow step.

### Job Naming

Job names are constrained to 63 characters. The builder uses:

```
{workflow-name}-{step-name}-{run-id}
```

If this exceeds 63 characters, the first 12 hex characters of `sha256(workflow/step)` replace the workflow+step portion.

### Environment Variables Injected

| Variable | Source |
|----------|--------|
| `STEP_NAME` | Step spec name |
| `WORKFLOW_NAME` | Workflow name |
| `MODEL_PROVIDER` | `agent.spec.model.provider` |
| `MODEL_NAME` | `agent.spec.model.name` |
| `MODEL_TEMPERATURE` | `agent.spec.model.temperature` (if set) |
| `MODEL_API_KEY` | Secret ref from `agent.spec.model.credentialsSecretRef` |
| `AGENT_SYSTEM_PROMPT` | `agent.spec.systemPrompt` |
| `AUTONOMY_LEVEL` | `agent.spec.autonomyLevel` |
| `STEP_INPUT` | Resolved step input JSON |
| `MCP_SERVERS` | JSON array from MCP registry (`GetServersJSON()`) |
| `MAX_TOOL_CALLS` | `agent.spec.guardrails.maxIterations` |
| `COST_LIMIT_USD` | `agent.spec.guardrails.costLimitUSD` |
| `CONTENT_FILTERS` | `agent.spec.guardrails.contentFilters` (JSON) |
| `MAX_EXECUTION_TIME` | `agent.spec.guardrails.maxExecutionTime` |
| `MEMORY_TYPE` | `agent.spec.memory.type` (default: `buffer`) |
| `MEMORY_CM_NAME` | `{agent-name}-memory` (for summary memory type) |
| `MAX_CONTEXT_TOKENS` | `agent.spec.memory.maxContextTokens` |
| `ANTHROPIC_VERTEX_PROJECT_ID` | Operator env (Vertex AI only) |
| `CLOUD_ML_REGION` | Operator env (Vertex AI only) |
| `GOOGLE_APPLICATION_CREDENTIALS` | `/var/run/secrets/gcp/credentials.json` (Vertex AI only) |
| `STEP_INPUT_{STEP}_{KEY}` | Resolved `inputFrom` references |

### Volume Mounts

The job builder conditionally mounts volumes based on agent configuration:

- **GCP credentials** — When `ANTHROPIC_VERTEX_PROJECT_ID` is set in the operator environment, the builder mounts the `gcp-credentials` Secret at `/var/run/secrets/gcp`. The executor reads this path to authenticate with Vertex AI.
- **Tool ConfigMaps** — For each tool in `agent.spec.tools` that has a `config.configMapName`, a read-only ConfigMap volume is mounted at `/etc/tool-config/{tool-name}`.
- **Vector memory PVC** — When `agent.spec.memory.type=vector` and `persistentStorage.enabled=true`, the named PVC is mounted at `/var/run/agent-memory`.

### Job Lifecycle Settings

| Setting | Value | Effect |
|---------|-------|--------|
| `backoffLimit` | `0` | Jobs do not retry on failure — the Workflow controller handles retry policy |
| `activeDeadlineSeconds` | Step timeout or guardrails `maxExecutionTime` or 1800s | Hard timeout for the pod |
| `ttlSecondsAfterFinished` | `3600` | Jobs and pods are garbage collected after 1 hour |
| `restartPolicy` | `Never` | Pod never restarts; a new Job is created for retries |

### Labels

Every Job and its pod template receive three labels used by the controller to correlate Jobs back to workflow steps:

```
purko.io/workflow: {workflow-name}
purko.io/step:     {step-name}
purko.io/agent:    {agent-name}
```

---

## Output Capture Flow

When a Job completes, the Workflow controller reads the pod logs using the Kubernetes `pods/log` API with a 1 MB scanner buffer. The executor writes structured output to stdout as:

```
OUTPUT:{"response": "...", "_metrics": {...}, "_memory_update": "..."}
```

The controller scans each log line for the `OUTPUT:` prefix, unmarshals the JSON, and performs two writes:

1. Stores the output value in the `{workflow-name}-outputs` ConfigMap under the key `{step-name}`.
2. Stores `_memory_update` in the `{agent-name}-memory` ConfigMap for the next execution of that agent.

Downstream steps read from this ConfigMap when resolving `inputFrom` or `${steps.X.output.*}` variables.

---

## Autonomy Controller

`AutonomyReconciler` watches `AgentAutonomyPolicy` CRs and evaluates every agent in the target namespace on a 5-minute cycle.

**Evaluation logic:**

1. Collect `status.metrics` from each agent (invocations, success rate, consecutive failures, days at current level).
2. Compare against `promotionCriteria` and `rollbackTriggers` in the policy.
3. If promotion criteria met and `requiredApprovals == 0`: auto-promote (`shu → ha` or `ha → ri`).
4. If rollback triggers met (e.g., success rate drops below threshold): demote to `shu`.
5. Update `agent.spec.autonomyLevel` and `agent.status.shuHaRi`.
6. Set `ShuHaRiProgression` condition on agent status.

See [Shu-Ha-Ri concept](../concepts/shu-ha-ri.md) and [AgentAutonomyPolicy CRD](../reference/crd-autonomypolicy.md).

---

## MCPServer Controller

`MCPServerReconciler` manages the full lifecycle of tool providers:

1. Create `Deployment` from `spec.image`, `spec.args`, and `spec.env`.
2. Create `Service` pointing at the deployment.
3. Write an entry to the `mcp-servers` ConfigMap in the agent namespace.
4. Register a finalizer on the CR; on deletion, remove all three resources atomically.

The URL written to the ConfigMap depends on networking mode:

- `hostNetwork: true` — uses `localhost:{port}` (minikube/dev environments)
- Standard ClusterIP — uses `http://{service-name}.{namespace}.svc.cluster.local:{port}`

---

## LLMProvider Controller

`LLMProviderReconciler` is a lightweight validation controller:

1. Read credentials from the referenced Secret.
2. Make a lightweight health-check request to the provider API.
3. Discover and record available models and pricing in `status.availableModels`.
4. Set `status.phase = Ready` or `Failed`.

The controller does not create any backing resources. Its output is used by the dashboard to populate the LLM provider status view.

---

## Related Pages

- [Overview](overview.md) — system diagram and namespace model
- [Security](security.md) — RBAC, pod security, autonomy as safety
- [Executor Protocol](../reference/executor-protocol.md) — OUTPUT format, env var reference
- [Workflow CRD](../reference/crd-workflow.md) — full workflow spec
- [Agent CRD](../reference/crd-agent.md) — full agent spec
