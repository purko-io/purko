# Agents

An **Agent** is an AI worker defined as a Kubernetes Custom Resource. It combines a language model, a system prompt, a set of tools, and an autonomy level into a single declarative object that the Purko operator manages for you.

When you apply an Agent CR, the controller validates credentials, ensures the referenced model is reachable, and prepares a ServiceAccount so the agent can be invoked by [workflows](workflows.md). The agent itself does not run continuously — it is instantiated as a Kubernetes Job each time a workflow step calls it.

```
kubectl get agents -n ai-agents
NAME                   MODEL                  STATUS   AGE
campaign-strategist    claude-sonnet-4-6      Ready    3d
code-reviewer          claude-sonnet-4-6      Ready    3d
log-analyzer           claude-sonnet-4-6      Ready    1d
```

---

## Agent Types

The `spec.type` field describes what the agent is designed to do. Purko uses six archetypes:

| Type | Purpose | Examples |
|------|---------|---------|
| `planner` | Strategic thinking, breaking a goal into steps, designing solutions | Campaign strategist, architecture designer, sprint planner |
| `executor` | Producing concrete output — content, code, data transformations | Content writer, code generator, report builder |
| `reviewer` | Quality checks, validation, approval decisions | Brand reviewer, code reviewer, security auditor |
| `router` | Classification, dispatching work to the right downstream agent | Task router, ticket classifier, intent detector |
| `monitor` | Watching a system, detecting anomalies, raising alerts | System monitor, anomaly detector, SLO watcher |
| `retriever` | Searching and fetching context from external sources | Knowledge retriever, document searcher, log fetcher |

The type is metadata — it does not change how the executor runs. It influences dashboard filtering, preset matching, and documentation.

---

## Agent Lifecycle

| Phase | Meaning |
|-------|---------|
| `Pending` | Agent CR received; controller has not finished reconciling |
| `Ready` | Credentials valid, model reachable, ServiceAccount ready; agent can be invoked |
| `Error` | One or more conditions failed; see `.status.message` and `.status.conditions` |

### Conditions

Each agent exposes a set of standard Kubernetes conditions under `.status.conditions`:

| Condition | Meaning |
|-----------|---------|
| `CredentialsValid` | The referenced Secret exists and contains the expected key |
| `ToolsAvailable` | All referenced MCP tools are discoverable in the registry |
| `ServiceAccountReady` | The agent's Kubernetes ServiceAccount has been provisioned |
| `ModelAvailable` | The LLMProvider for this agent is in the Ready phase |
| `Ready` | All checks passed; agent is available for workflow invocation |
| `ShuHaRiProgression` | Current autonomy level and promotion progress |

---

## Agent Spec Fields

### `spec.model` (required)

| Field | Type | Description |
|-------|------|-------------|
| `provider` | string | LLM provider: `anthropic`, `openai`, `meta`, `huggingface`, `local` |
| `name` | string | Model identifier: `claude-sonnet-4-6`, `gpt-4o`, `llama-3`, etc. |
| `version` | string | Model version or variant |
| `temperature` | float (0–2) | Sampling temperature. Lower values are more deterministic. |
| `maxTokens` | integer | Maximum tokens per response |
| `topP` | float (0–1) | Nucleus sampling parameter |
| `frequencyPenalty` | float (-2–2) | Reduces repetition of token sequences |
| `presencePenalty` | float (-2–2) | Encourages topic diversity |
| `credentialsSecretRef.name` | string | Name of the Secret with API credentials |
| `credentialsSecretRef.namespace` | string | Namespace of the Secret (defaults to agent's namespace) |
| `fallback.provider` | string | Provider to use if primary is unavailable |
| `fallback.name` | string | Model name for the fallback |

### Top-level spec fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | — | Agent archetype: `planner`, `executor`, `reviewer`, `router`, `monitor`, `retriever` |
| `role` | string | — | Free-text role description, used for filtering and documentation |
| `systemPrompt` | string | — | System-level instructions defining the agent's behaviour and constraints |
| `instructions` | string | — | Operational instructions, complementary to `systemPrompt` |
| `autonomyLevel` | string | `restricted` | `manual`, `restricted`, `supervised`, or `full`. See [Shu-Ha-Ri](shu-ha-ri.md) |
| `confidenceThreshold` | float (0–1) | — | Actions below this score are escalated for human review |
| `replicas` | integer | 1 | Number of agent replicas (0–100) |
| `maxConcurrency` | integer | 10 | Maximum concurrent tasks |
| `timeout` | string | `5m` | Default operation timeout: `30s`, `5m`, `1h` |

### `spec.tools[]`

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique tool identifier |
| `type` | string | `mcp`, `function`, `api`, or `builtin`. See [Tool Types](tool-types.md) |
| `endpoint.url` | string | HTTP endpoint URL (for `api` type) |
| `endpoint.method` | string | HTTP method: `GET`, `POST`, `PUT`, `DELETE`, `PATCH` |
| `endpoint.headers` | map | HTTP headers to send |
| `endpoint.timeoutSeconds` | integer | Per-request timeout |
| `credentialsSecretRef` | object | Secret reference for tool authentication |
| `config` | object | Tool-specific configuration (free-form) |

### `spec.memory`

See [Memory](memory.md) for full details.

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `buffer`, `summary`, `vector`, or `none` |
| `backend` | string | Storage backend |
| `ttl` | string | Time-to-live for entries: `1h`, `24h`, `7d` |
| `maxEntries` | integer | Maximum entries to retain |
| `maxContextTokens` | integer | Token budget for loaded context |
| `retentionPolicy` | string | How entries are evicted |
| `persistentStorage.enabled` | boolean | Mount a PVC for vector memory |
| `persistentStorage.volumeClaimRef` | string | Name of the PVC |

### `spec.runtime`

| Field | Type | Description |
|-------|------|-------------|
| `image` | string | Executor container image |
| `serviceAccountName` | string | Kubernetes ServiceAccount for the agent pod |
| `env[]` | list | Extra environment variables |
| `config` | map | Passed as `EXECUTOR_*` environment variables |
| `codeExecution.enabled` | boolean | Enable the CodeAct sandbox |
| `codeExecution.languages[]` | list | Supported languages: `python`, `bash` |
| `codeExecution.sandbox.maxExecutionSeconds` | integer | Sandbox timeout (default: 30) |
| `codeExecution.sandbox.maxOutputBytes` | integer | Max sandbox output (default: 100000) |
| `codeExecution.sandbox.networkAccess` | boolean | Allow network in sandbox (default: false) |
| `codeExecution.sandbox.writablePaths[]` | list | Writable directories (default: `/tmp`) |

### `spec.scaling`

| Field | Type | Description |
|-------|------|-------------|
| `minReplicas` | integer | Minimum replicas |
| `maxReplicas` | integer | Maximum replicas |
| `targetUtilization` | integer (1–100) | Target CPU utilisation percentage for HPA |

### `spec.shuHaRi`

| Field | Type | Description |
|-------|------|-------------|
| `level` | string | Current level: `shu`, `ha`, or `ri` |
| `promotedAt` | datetime | When the agent was last promoted |

### `spec.approvalPolicy`

| Field | Type | Description |
|-------|------|-------------|
| `mode` | string | `manual`, `semi-autonomous`, or `autonomous` |
| `autoApproveRiskBelow` | string | Auto-approve actions below: `low`, `medium`, `high` |
| `requireApprovalAbove` | string | Require approval above: `low`, `medium`, `high` |

### Free-form fields

These fields accept arbitrary structured data and are schema-flexible:

| Field | Purpose |
|-------|---------|
| `guardrails` | Cost limits, iteration limits, content filters |
| `observability` | Prometheus metrics, tracing, logging config |
| `lifecycle` | Pre-start and post-stop hooks |
| `schedule` | Cron schedule for periodic activation |
| `blastRadiusLimit` | Caps on affected resources and allowed namespaces |
| `escalationPolicy` | Where to send decisions that exceed the autonomy level |
| `capabilities[]` | Agent capability declarations |

---

## Example YAML

The following example deploys two agents that work together: a planner that designs a campaign and an executor that writes the copy.

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: campaign-strategist
  namespace: ai-agents
  labels:
    app.kubernetes.io/component: marketing
spec:
  type: planner
  role: marketing-strategist
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.5
    maxTokens: 4096
  autonomyLevel: supervised
  systemPrompt: |
    You are a senior marketing strategist. Given a product brief,
    design a multi-channel campaign plan with clear objectives,
    target audiences, and key messages per channel.
  memory:
    type: summary
  tools:
    - name: search_web
      type: mcp
    - name: competitor_analysis
      type: api
      endpoint:
        url: http://analytics.internal/api/competitor
        method: POST
        timeoutSeconds: 30
---
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: content-writer
  namespace: ai-agents
  labels:
    app.kubernetes.io/component: marketing
spec:
  type: executor
  role: content-writer
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.7
    maxTokens: 8192
  autonomyLevel: supervised
  systemPrompt: |
    You are a skilled content writer. Given a campaign strategy,
    produce polished copy for each channel. Match the brand voice.
    Provide a JSON response with keys: headline, body, cta.
  memory:
    type: buffer
```

!!! tip
    Wire these agents together in a [Workflow](workflows.md) by referencing them in `spec.steps[].agentRef`.

!!! warning
    New agents always start at `restricted` autonomy. Do not set `autonomyLevel: full` unless you have reviewed the agent's behaviour in a lower mode first. The [Shu-Ha-Ri](shu-ha-ri.md) system will promote the agent automatically once it has demonstrated reliability.

---

## See Also

- [Agent CRD Reference](../reference/crd-agent.md)
- [Shu-Ha-Ri Autonomy](shu-ha-ri.md)
- [Tool Types](tool-types.md)
- [Memory](memory.md)
- [Workflows](workflows.md)
