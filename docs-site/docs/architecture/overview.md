# Architecture Overview

Purko is a Kubernetes-native platform built around a single Go binary (the operator) that manages five CRDs. Workflows execute as isolated Kubernetes Jobs, each running a Python ReAct executor. Tools are provided by MCP servers — separate processes discovered and registered via the `MCPServer` CRD. LLM providers are declared via the `LLMProvider` CRD and credentials are injected at job creation time.

---

## System Diagram

```
┌──────────────────────────────────────────────────────────┐
│                    CONTROL PLANE                         │
│                 (purko-system namespace)                 │
│                                                          │
│   purko-operator (single Go binary)                      │
│   ├── Agent Controller                                   │
│   ├── Workflow Controller                                │
│   ├── Autonomy Controller                               │
│   ├── MCPServer Controller                              │
│   ├── LLMProvider Controller                            │
│   ├── MCP Server Registry  (60s TTL cache)             │
│   ├── Webhook Trigger Router                           │
│   ├── Cron Scheduler                                   │
│   └── Dashboard (embedded HTTP server, :8082)          │
│                                                          │
│   :8080 metrics    :8081 health    :8082 dashboard       │
└──────────────────────────────────────────────────────────┘
                          │ creates Jobs
                          ▼
┌──────────────────────────────────────────────────────────┐
│                     DATA PLANE                           │
│                  (ai-agents namespace)                   │
│                                                          │
│   Job: step-1       Job: step-2       Job: step-3        │
│   (executor pod)    (executor pod)    (executor pod)     │
│        │                 │                 │             │
│        └─────────────────┼─────────────────┘             │
│                          │ MCP JSON-RPC 2.0              │
│                          ▼                               │
│              ┌─────────────────────┐                     │
│              │  MCP Servers        │  (mcp-servers ns)  │
│              │  GitHub │ Lumino    │                     │
│              │  PagerDuty │ Custom │                     │
│              └─────────────────────┘                     │
│                          │ HTTPS                         │
│                          ▼                               │
│              ┌─────────────────────┐                     │
│              │  LLM Providers      │                     │
│              │  Vertex AI          │                     │
│              │  Anthropic          │                     │
│              │  OpenAI             │                     │
│              └─────────────────────┘                     │
└──────────────────────────────────────────────────────────┘
```

**Agents are config-only.** There are no idle agent pods. An agent is a CRD object that holds model configuration, system prompt, guardrails, and autonomy level. Jobs are created on demand by the Workflow controller when a step referencing that agent is ready to execute. This keeps resource usage proportional to actual work.

---

## Components

### Operator

The operator is a single Go binary built on [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime). It registers five controllers against the Kubernetes API server and starts a background goroutine for the MCP registry and cron scheduler. Key flags:

| Flag | Default | Purpose |
|------|---------|---------|
| `--agent-namespace` | `ai-agents` | Namespace where Jobs are created |
| `--dashboard-port` | `8082` | Port for the embedded web UI |
| `--metrics-bind-address` | `:8080` | Prometheus metrics endpoint |
| `--health-probe-bind-address` | `:8081` | Liveness and readiness probes |
| `--llm-provider` | `` (auto) | Override LLM provider |
| `--llm-model` | `claude-sonnet-4-6` | Default model for intent bar |
| `--leader-elect` | `false` | Enable leader election for HA |
| `--enable-webhooks` | `false` | Enable admission webhooks |

The operator also embeds a dashboard server (`:8082`) with a REST API and SSE stream for real-time workflow status. The dashboard can be disabled with `--dashboard-enabled=false` when running a standalone dashboard service.

### Executor

The executor is a Python process running inside a Kubernetes Job pod. It implements a [ReAct](https://arxiv.org/abs/2210.03629) loop:

1. Load memory context (buffer or summary from ConfigMap/PVC)
2. Filter available tools by autonomy level
3. Connect to all MCP servers listed in `MCP_SERVERS`
4. Run the ReAct loop, tracking cost per LLM call
5. Apply content filters to the output
6. Save memory summary
7. Write `OUTPUT:{json}` to stdout

The controller reads pod logs (1 MB buffer), extracts the `OUTPUT:` line, stores the result in a ConfigMap, and uses it as input for downstream steps.

### MCP Servers

MCP servers are HTTP processes that implement [JSON-RPC 2.0](https://www.jsonrpc.org/specification) over the [Model Context Protocol](https://modelcontextprotocol.io). Each is declared as an `MCPServer` CRD; the controller creates a Deployment and Service, then registers the endpoint in the `mcp-servers` ConfigMap. The operator's MCP registry reads that ConfigMap, calls `tools/list` on each server every 60 seconds, and caches the result. Job pods receive the current server list as the `MCP_SERVERS` environment variable (JSON array).

See [MCP Servers concept](../concepts/mcp-servers.md) and [MCPServer CRD reference](../reference/crd-mcpserver.md).

### purkoctl

`purkoctl` is a Go CLI (built with [cobra](https://github.com/spf13/cobra)) for managing agents, workflows, MCP servers, and LLM providers from the terminal. It wraps `kubectl` conventions and adds purko-specific commands such as `purkoctl workflow run` and `purkoctl agent promote`.

See [CLI Reference](../reference/cli-reference.md).

---

## Namespace Model

| Namespace | Contents | Who manages it |
|-----------|----------|---------------|
| `purko-system` | Operator Deployment, ServiceAccount, ClusterRole, ConfigMaps | Helm chart |
| `ai-agents` | Workflow and Agent CRs, Job pods, output ConfigMaps | Operator / users |
| `mcp-servers` | MCP server Deployments and Services | MCPServer controller |

The `--agent-namespace` flag controls where Jobs are created. In multi-tenant setups you can deploy multiple operator instances pointing at different namespaces.

---

## Data Flow

The full path from CRD creation to output:

```
User creates Workflow CR
        │
        ▼
Workflow Controller — check concurrency policy, set phase=Running
        │
        ▼
findExecutableSteps() — resolve DAG, check conditions, check approvals
        │
        ▼
buildStepJob() — inject env vars, credentials, MCP config, guardrails
        │
        ▼
Kubernetes Job created in ai-agents namespace
        │
        ▼
Executor pod — ReAct loop → MCP tool calls → LLM calls
        │ stdout: OUTPUT:{json}
        ▼
Controller reads pod logs — extracts output — stores in ConfigMap
        │
        ▼
Variable substitution: ${steps.X.output.response} → next step input
        │
        ▼
Repeat for each ready step until all done → set phase=Succeeded/Failed
```

Step dependencies are declared with `dependsOn`. Conditions (`conditionExpr`) allow a step to be skipped based on upstream output values. Human approval gates are implemented via annotations on the Workflow object.

See [Workflow CRD reference](../reference/crd-workflow.md) for the full spec.

---

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Operator | Go, controller-runtime, client-go |
| Dashboard | Go HTTP server, SSE, embedded static assets |
| CLI | Go, cobra |
| Executor | Python, MCP SDK |
| CRDs | Kubernetes v1alpha1, kubebuilder markers |
| Packaging | Helm chart |
| Metrics | Prometheus (`:8080/metrics`) |
| LLM providers | Vertex AI (Claude via Anthropic API), Anthropic direct, OpenAI |
| MCP protocol | JSON-RPC 2.0 over HTTP |

---

## Related Pages

- [Controllers](controllers.md) — reconciliation loops, job builder details
- [Security](security.md) — RBAC, pod security, autonomy as safety
- [Executor Protocol](../reference/executor-protocol.md) — OUTPUT format, env vars
- [Agent CRD](../reference/crd-agent.md) — full spec reference
- [Workflow CRD](../reference/crd-workflow.md) — full spec reference
