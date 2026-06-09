# Your First Agent

An [agent](../concepts/agents.md) is the atomic unit in Purko — a single AI model instance configured with a role, tools, guardrails, and a memory mode. You define it as a Kubernetes Custom Resource; the Purko operator handles provisioning, health checks, and lifecycle.

This tutorial deploys a **code reviewer** agent and shows you how to inspect it with both `kubectl` and `purkoctl`.

---

## Prerequisites

- Purko installed and the operator running ([Installation](installation.md))
- `kubectl` configured to point at your cluster
- Port-forward to the dashboard running: `kubectl port-forward -n purko-system deploy/purko-operator 8082:8082`

---

## The agent YAML

Save the following to `code-reviewer.yaml`. This is the exact file from `examples/agents/archetypes/code-reviewer.yaml` in the Purko repository:

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
  namespace: ai-agents
  labels:
    app.kubernetes.io/component: archetypes
spec:
  type: reviewer
  autonomyLevel: restricted
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.1
  role: code-reviewer
  guardrails:
    maxIterations: 5
    maxExecutionTime: "5m"
    costLimitUSD: 3.0
    contentFilters:
      - no-secrets-in-output
  systemPrompt: |
    You are a senior code reviewer. Evaluate changes for:
    correctness, security vulnerabilities, performance,
    readability, and test coverage. Provide structured feedback.
    Use the static-analysis builtin tool to check code for common issues.
  tools:
    - name: static-analysis
      type: builtin
    - name: get_file_contents
      type: mcp
    - name: search_code
      type: mcp
    - name: list_pull_requests
      type: mcp
    - name: search_pull_requests
      type: mcp
    - name: list_commits
      type: mcp
```

---

## Field-by-field explanation

### `spec.type`

```yaml
type: reviewer
```

The agent archetype. Built-in types include `planner`, `executor`, `reviewer`, `router`, `monitor`, and `retriever`. The type influences how the executor schedules the agent's tasks and which default behaviors apply.

### `spec.autonomyLevel`

```yaml
autonomyLevel: restricted
```

Controls how much independent decision-making the agent can exercise:

| Level | Write tools | Approval required |
|-------|-------------|-------------------|
| `manual` | Blocked | All actions |
| `restricted` | Blocked | Read-only — safe default |
| `supervised` | Allowed | High-impact actions |
| `full` | Allowed | None (within guardrails) |

Start new agents at `restricted`. The Shu-Ha-Ri autonomy system can promote them automatically as they demonstrate reliability. See [Shu-Ha-Ri](../concepts/shu-ha-ri.md).

### `spec.model`

```yaml
model:
  provider: anthropic
  name: claude-sonnet-4-6
  temperature: 0.1
```

Selects the underlying language model. `temperature: 0.1` produces more deterministic, consistent code review output. Supported providers: `anthropic`, `openai`, `vertex-ai`, `ollama`, `huggingface`, `local`.

To use a different provider, update `provider` and `name`. Credentials are looked up from the matching `LLMProvider` CR in `purko-system`.

### `spec.role`

```yaml
role: code-reviewer
```

A free-text label describing the agent's function. Used for filtering, dashboard display, and audit logs.

### `spec.systemPrompt`

```yaml
systemPrompt: |
  You are a senior code reviewer. Evaluate changes for:
  correctness, security vulnerabilities, performance,
  readability, and test coverage. Provide structured feedback.
  Use the static-analysis builtin tool to check code for common issues.
```

The system-level instruction passed to the model on every request. This shapes the agent's persona, constraints, and output format. Multi-line YAML strings (the `|` block scalar) are fully supported.

### `spec.guardrails`

```yaml
guardrails:
  maxIterations: 5
  maxExecutionTime: "5m"
  costLimitUSD: 3.0
  contentFilters:
    - no-secrets-in-output
```

Safety boundaries for the agent:

| Field | Description |
|-------|-------------|
| `maxIterations` | Maximum tool-call cycles per task. Prevents runaway loops. |
| `maxExecutionTime` | Hard wall-clock timeout per task execution. |
| `costLimitUSD` | Maximum spend per task. The executor aborts if the limit is reached. |
| `contentFilters` | Named filters applied to output. `no-secrets-in-output` blocks tokens matching secret patterns (API keys, passwords, private keys). |

### `spec.tools`

```yaml
tools:
  - name: static-analysis
    type: builtin
  - name: get_file_contents
    type: mcp
  - name: search_code
    type: mcp
  - name: list_pull_requests
    type: mcp
  - name: search_pull_requests
    type: mcp
  - name: list_commits
    type: mcp
```

The tools the agent can invoke. Tool types:

| Type | Description |
|------|-------------|
| `builtin` | Built into the executor image (e.g., `static-analysis`, `bash`, `grep`) |
| `mcp` | Provided by a registered MCP server; discovered automatically |
| `api` | Direct HTTP endpoint call |
| `cli` | Shell command with arguments |
| `kubernetes` | Kubernetes API operations |

The MCP tools (`get_file_contents`, `search_code`, etc.) come from the GitHub MCP server. See [Connect MCP Servers](connect-mcp.md) to install it.

!!! tip "Discover available MCP tools"
    ```bash
    curl http://localhost:8082/api/mcp/tools | jq '.servers[].tools[].name'
    ```

    Only tools listed in this response can be referenced by name in an agent spec.

### `spec.memory` (not shown — defaults apply)

When omitted, the agent uses `buffer` mode: each task execution is stateless and independent. For agents that need to carry context across runs, add:

```yaml
memory:
  type: summary   # buffer | summary | vector
```

See [Memory](../concepts/memory.md) for the full reference.

---

## Apply the agent

```bash
kubectl apply -f code-reviewer.yaml
```

Expected output:

```
agent.purko.io/code-reviewer created
```

---

## Verify with kubectl

List agents in the `ai-agents` namespace (using the short name `ag`):

```bash
kubectl get ag -n ai-agents
```

```
NAME            MODEL               STATUS    AGE
code-reviewer   claude-sonnet-4-6             10s
```

The `STATUS` column is populated once the operator reconciles the agent. Wait a few seconds and run again:

```bash
kubectl get ag -n ai-agents
```

```
NAME            MODEL               STATUS   AGE
code-reviewer   claude-sonnet-4-6   Ready    30s
```

Inspect the full resource:

```bash
kubectl get ag code-reviewer -n ai-agents -o yaml
```

Check the status conditions (the controller writes structured conditions following the Kubernetes convention):

```bash
kubectl get ag code-reviewer -n ai-agents \
  -o jsonpath='{.status.conditions}' | jq .
```

```json
[
  {
    "type": "Ready",
    "status": "True",
    "reason": "Reconciled",
    "message": "Agent is ready",
    "lastTransitionTime": "2026-04-23T09:01:00Z"
  }
]
```

---

## Verify with purkoctl

```bash
purkoctl agent list
```

```
NAMESPACE    NAME            MODEL               STATUS   AGE
ai-agents    code-reviewer   claude-sonnet-4-6   Ready    1m
```

Get details for a specific agent:

```bash
purkoctl agent get code-reviewer
```

```
Name:           code-reviewer
Namespace:      ai-agents
Type:           reviewer
Model:          anthropic / claude-sonnet-4-6
Autonomy:       restricted
Status:         Ready
Tools:          static-analysis, get_file_contents, search_code,
                list_pull_requests, search_pull_requests, list_commits
Guardrails:
  maxIterations:    5
  maxExecutionTime: 5m
  costLimitUSD:     3.00
  contentFilters:   no-secrets-in-output
```

---

## Validate before applying

You can test a YAML file against the server-side schema without creating anything:

```bash
kubectl apply --dry-run=server -f code-reviewer.yaml
```

This catches schema errors (invalid enum values, out-of-range temperatures, missing required fields) before the resource reaches the cluster.

---

## Troubleshooting

**Agent stays in `Pending`**

```bash
kubectl describe ag code-reviewer -n ai-agents
```

Common causes:

- The `LLMProvider` for the requested model is not configured in `purko-system`
- The operator cannot reach the model API (check network policies)
- A credentials Secret is missing

**Agent in `Error` phase**

```bash
kubectl get ag code-reviewer -n ai-agents \
  -o jsonpath='{.status.message}'
```

Then check operator logs:

```bash
kubectl logs -n purko-system deploy/purko-operator
```

---

## Clean up

```bash
kubectl delete ag code-reviewer -n ai-agents
```

---

## Next steps

- [Your First Workflow](first-workflow.md) — chain this agent into a multi-step pipeline
- [Connect MCP Servers](connect-mcp.md) — add GitHub tools so the code-reviewer can read pull requests
- [Agents concept page](../concepts/agents.md) — full API reference for all spec fields
