# Agent CRD

**API Version:** `purko.io/v1alpha1`
**Kind:** `Agent`
**Scope:** Namespaced

An Agent is a Kubernetes resource that defines an AI agent — its model, memory, tools, autonomy level, runtime container, and scaling behavior.

## Example

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: code-executor
  namespace: ai-agents
  labels:
    app.kubernetes.io/component: archetypes
spec:
  type: executor
  autonomyLevel: supervised
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.2
  role: code-executor
  runtime:
    image: localhost/purko-executor:codeact
    codeExecution:
      enabled: true
      languages:
        - python
        - bash
      sandbox:
        maxExecutionSeconds: 30
        maxOutputBytes: 100000
        networkAccess: false
  guardrails:
    maxIterations: 50
    maxExecutionTime: "15m"
    humanApprovalRequired: true
    costLimitUSD: 20.0
  systemPrompt: |
    You are a code execution agent. Implement the specified task
    following best practices. Write tests for all new code.
  tools:
    - name: code-sandbox
      type: function
    - name: get_file_contents
      type: mcp
    - name: push_files
      type: mcp
```

## Spec Fields

### AgentSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | No | Agent archetype: `planner`, `executor`, `reviewer`, `router`, `monitor`, `retriever` |
| `model` | [ModelSpec](#modelspec) | Yes | LLM model configuration |
| `role` | string | No | Short role identifier used in prompts and logs |
| `systemPrompt` | string | No | System prompt text injected at every execution |
| `instructions` | string | No | Additional instructions appended to the system prompt |
| `autonomyLevel` | string | No | Autonomy level: `manual`, `restricted`, `supervised`, `full` |
| `shuHaRi` | ShuHaRiSpec (see below) | No | Agent-side Shu-Ha-Ri level override |
| `approvalPolicy` | [ApprovalPolicy](#approvalpolicy) | No | Explicit approval policy (overrides autonomy level defaults) |
| `confidenceThreshold` | float64 | No | Minimum confidence score before acting autonomously (0.0–1.0) |
| `replicas` | int | No | Fixed replica count; mutually exclusive with `scaling` |
| `maxConcurrency` | int | No | Maximum concurrent task executions per replica |
| `timeout` | string | No | Default execution timeout (e.g. `10m`, `1h`) |
| `tools` | [][ToolSpec](#toolspec) | No | Tools available to this agent |
| `memory` | [MemorySpec](#memoryspec) | No | Memory backend configuration |
| `runtime` | [RuntimeSpec](#runtimespec) | No | Container image and execution environment |
| `scaling` | [ScalingSpec](#scalingspec) | No | HPA autoscaling configuration |
| `credentialsSecretRef` | [SecretRef](#secretref) | No | Secret containing agent-level credentials |
| `guardrails` | object | No | Free-form guardrails: `costLimitUSD`, `maxIterations`, `maxExecutionTime`, `humanApprovalRequired` |
| `observability` | object | No | Free-form observability config (tracing, metrics) |
| `lifecycle` | object | No | Free-form lifecycle hooks (preStart, postStop) |
| `schedule` | object | No | Free-form schedule configuration |
| `blastRadiusLimit` | object | No | Free-form blast radius constraints |
| `escalationPolicy` | object | No | Free-form escalation policy |
| `capabilities` | []object | No | Free-form capability declarations |

### ModelSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | string | Yes | LLM provider: `anthropic`, `openai`, `vertex-ai`, `ollama`, `custom` |
| `name` | string | Yes | Model identifier (e.g. `claude-sonnet-4-6`, `gpt-4o`) |
| `version` | string | No | Model version pin |
| `temperature` | float64 | No | Sampling temperature (0.0–1.0, default 0.2) |
| `maxTokens` | int | No | Maximum tokens per response |
| `topP` | float64 | No | Nucleus sampling probability |
| `frequencyPenalty` | float64 | No | Penalize repeated tokens |
| `presencePenalty` | float64 | No | Penalize tokens already in context |
| `credentialsSecretRef` | [SecretRef](#secretref) | No | Secret containing model API key; overrides provider-level credentials |
| `fallback` | object | No | Free-form fallback model configuration |

### ToolSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Tool name (must match MCP tool name or function name) |
| `type` | string | Yes | Tool type: `mcp`, `function`, `http` |
| `endpoint` | [EndpointSpec](#endpointspec) | No | HTTP endpoint for `http`-type tools |
| `credentialsSecretRef` | [SecretRef](#secretref) | No | Secret for tool authentication |
| `config` | object | No | Free-form tool configuration |

### EndpointSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | HTTP URL |
| `method` | string | No | HTTP method (default `POST`) |
| `headers` | map[string]string | No | Static HTTP headers |
| `timeoutSeconds` | int | No | Per-request timeout in seconds |
| `authScheme` | string | No | Authentication scheme: `Bearer`, `Token`, `Basic`, `ApiKey` |
| `authHeader` | string | No | Header name for auth token (default `Authorization`) |

### MemorySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | No | Memory strategy: `buffer`, `summary`, `vector`, `none` |
| `backend` | string | No | Storage backend identifier |
| `ttl` | string | No | Time-to-live for memory entries (e.g. `24h`) |
| `maxEntries` | int | No | Maximum number of memory entries |
| `maxContextTokens` | int | No | Maximum tokens to include from memory in each prompt |
| `retentionPolicy` | string | No | Retention strategy (e.g. `sliding_window`, `priority`) |
| `persistentStorage` | [PersistentStorage](#persistentstorage) | No | PVC-backed persistent memory |

### PersistentStorage

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | Yes | Enable PVC-backed storage |
| `volumeClaimRef` | string | No | Name of an existing PersistentVolumeClaim |

### RuntimeSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | string | No | Container image for the executor (default `purko-executor:latest`) |
| `serviceAccountName` | string | No | Kubernetes ServiceAccount for the executor pod |
| `env` | [][EnvVar](#envvar) | No | Additional environment variables |
| `config` | map[string]string | No | Key-value config passed as `EXECUTOR_*` environment variables |
| `resources` | object | No | Kubernetes resource requests and limits |
| `codeExecution` | [CodeExecutionSpec](#codeexecutionspec) | No | Code execution sandbox configuration |

### CodeExecutionSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | Yes | Enable the code execution sandbox |
| `languages` | []string | No | Allowed languages: `python`, `bash` |
| `sandbox` | [SandboxSpec](#sandboxspec) | No | Sandbox resource and network limits |
| `preinstalled` | []string | No | Documentation-only list of pre-installed packages |

### SandboxSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `maxExecutionSeconds` | int | No | Maximum wall-clock time per code execution (default 30) |
| `maxOutputBytes` | int | No | Maximum output size in bytes (default 100000) |
| `networkAccess` | bool | No | Allow network access from sandboxed code (default `false`) |
| `writablePaths` | []string | No | Paths writable inside the sandbox (default `["/tmp"]`) |

### ScalingSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `minReplicas` | int | No | Minimum replica count for the HPA |
| `maxReplicas` | int | No | Maximum replica count for the HPA |
| `targetUtilization` | int | No | Target CPU utilization percentage (default 70) |
| `metrics` | []object | No | Additional HPA metrics (custom/external) |
| `behavior` | object | No | HPA scale-up/scale-down behavior tuning |

### ShuHaRiSpec agent-side

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `level` | string | No | Initial Shu-Ha-Ri level: `shu`, `ha`, `ri` |
| `promotedAt` | timestamp | No | Timestamp of last promotion |

### ApprovalPolicy

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | No | Approval mode: `manual`, `semi-autonomous`, `autonomous` |
| `autoApproveRiskBelow` | string | No | Auto-approve actions with risk below this level: `low`, `medium`, `high` |
| `requireApprovalAbove` | string | No | Require human approval for risk at or above: `low`, `medium`, `high` |

### SecretRef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Kubernetes Secret name |
| `namespace` | string | No | Secret namespace (defaults to agent namespace) |
| `key` | string | No | Key within the Secret |

### EnvVar

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Environment variable name |
| `value` | string | Yes | Environment variable value |

## Credential References

There are two credential reference points, for two different things:

| Field | Credential for | Consumed by |
|-------|----------------|-------------|
| `spec.model.credentialsSecretRef` | The LLM provider API key | Passed to the executor as `MODEL_API_KEY` |
| `spec.tools[].credentialsSecretRef` | A specific tool's endpoint | Passed to the tool's MCP client |

Both reference a Secret in the agent's namespace (`name` + optional `key`,
default `api-key`). There is no agent-level credential field. When the
agent's provider resolves to an `LLMProvider` resource, credentials from
that provider's `spec.credentials` take precedence — prefer configuring
credentials once on the LLMProvider over per-agent secrets.

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current agent phase: `Pending`, `Ready`, `Error` |
| `message` | string | Human-readable status message |
| `observedGeneration` | int64 | Most recent generation reconciled by the controller |
| `availableReplicas` | int | Number of ready replicas |
| `totalTasksProcessed` | int64 | Total tasks executed since creation |
| `errorCount` | int64 | Total failed executions |
| `startTime` | timestamp | When the agent first became active |
| `completionTime` | timestamp | When the agent last completed a task |
| `lastActiveTime` | timestamp | Timestamp of the most recent execution |
| `conditions` | []Condition | Standard Kubernetes conditions |
| `metrics` | [AgentMetrics](#agentmetrics) | Aggregated execution metrics |
| `shuHaRi` | [ShuHaRiStatus](#shuharistatus) | Shu-Ha-Ri progression state |

### AgentMetrics

| Field | Type | Description |
|-------|------|-------------|
| `totalInvocations` | int64 | Total number of executions |
| `totalTokensUsed` | int64 | Cumulative token consumption |
| `averageLatencyMs` | int64 | Average execution latency in milliseconds |
| `totalCostUSD` | float64 | Cumulative cost in USD |
| `lastInvocationTime` | timestamp | Timestamp of the last invocation |
| `successCount` | int64 | Number of successful executions |
| `failureCount` | int64 | Number of failed executions |
| `consecutiveFailures` | int64 | Current streak of consecutive failures |

### ShuHaRiStatus

| Field | Type | Description |
|-------|------|-------------|
| `currentLevel` | string | Active level: `shu`, `ha`, `ri` |
| `readyForPromotion` | bool | Whether the agent meets promotion criteria |
| `promotionProgress` | [PromotionProgress](#promotionprogress) | Detailed promotion progress |

### PromotionProgress

| Field | Type | Description |
|-------|------|-------------|
| `actionsCompleted` | int64 | Actions completed at current level |
| `actionsRequired` | int64 | Actions required to qualify for promotion |
| `successRate` | float64 | Success rate over the qualifying window (0.0–1.0) |
| `daysInLevel` | int | Days spent at current level |
| `daysRequired` | int | Minimum days required at current level |

## Conditions

| Condition | Meaning |
|-----------|---------|
| `Ready` | Agent deployment is healthy and at least one replica is available |
| `Degraded` | Agent is partially available (fewer replicas than desired) |
| `Error` | Agent failed to reconcile; see `message` for details |

## Related Resources

- [AgentAutonomyPolicy CRD](crd-autonomypolicy.md) — cluster-wide Shu-Ha-Ri promotion criteria
- [MCPServer CRD](crd-mcpserver.md) — MCP tool servers agents can connect to
- [LLMProvider CRD](crd-llmprovider.md) — LLM provider configuration
- [Executor Protocol](executor-protocol.md) — container protocol for custom executor images
- [Concepts: Agents](../concepts/agents.md)
