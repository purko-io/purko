# Dashboard API

The Purko dashboard exposes a REST API used by the web UI and available for automation. All endpoints are served on the dashboard port (default `8080`). Responses are JSON. CORS headers are set to `*` on all endpoints.

Base URL: `http://<dashboard-host>:8080`

## Overview

### Overview

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/overview` | Aggregate counts and full lists of agents and workflows |
| `GET` | `/api/events` | Server-Sent Events stream; pushes updated overview data every 3 seconds |

### Agents

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/agents` | List all agents in the configured namespace |
| `GET` | `/api/agent/{name}` | Get agent detail, pod list, and HPA state |
| `POST` | `/api/create/agent` | Create a new agent |
| `POST` | `/api/update/agent` | Update an existing agent |
| `DELETE` | `/api/delete/agent/{name}` | Delete an agent |

### Workflows

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/workflows` | List all workflows in the configured namespace |
| `GET` | `/api/workflow/{name}` | Get workflow detail, step statuses, job list, and outputs |
| `POST` | `/api/create/workflow` | Create a new workflow |
| `DELETE` | `/api/delete/workflow/{name}` | Delete a workflow |
| `POST` | `/api/rerun/workflow/{name}` | Delete and recreate a workflow to trigger a re-run |

### MCP Servers

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/mcp/servers` | List all MCPServer CRs across namespaces |
| `POST` | `/api/mcp/server` | Create a new MCPServer |
| `GET` | `/api/mcp/server/{name}` | Get a specific MCPServer |
| `DELETE` | `/api/mcp/server/{name}` | Delete a specific MCPServer |
| `GET` | `/api/mcp/tools` | List all discovered tools across all MCP servers |
| `GET` | `/api/presets` | List agent presets from the `purko-presets` ConfigMap |

### LLM Providers

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/llm/providers` | List all LLMProvider CRs |
| `POST` | `/api/llm/provider` | Create a new LLMProvider |
| `GET` | `/api/llm/provider/{name}` | Get a specific LLMProvider |
| `DELETE` | `/api/llm/provider/{name}` | Delete a specific LLMProvider |

### Autonomy

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/autonomy/policy` | Get the active AgentAutonomyPolicy |
| `POST` | `/api/approve/{workflow}/{step}` | Approve a step pending human review |
| `POST` | `/api/deny/{workflow}/{step}` | Deny a step pending human review |

### Execution

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/logs/{workflow}/{step}` | Fetch the last 100 log lines from a step's Job pod |
| `POST` | `/api/intent` | Parse a natural-language intent and return suggested agents and workflow |
| `POST` | `/api/trigger/{namespace}/{workflow}` | Trigger a specific workflow via webhook |
| `POST` | `/api/trigger/{namespace}` | Auto-route a webhook payload to a workflow via trigger rules |

### System

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/trigger/rules` | Get configured trigger routing rules |
| `POST` | `/api/trigger/rules` | Save trigger routing rules |
| `GET` | `/api/schedules` | List active scheduled workflows |

---

## Endpoint Reference

### GET /api/overview

Returns aggregate platform state.

**Response:**

```json
{
  "agentCount": 5,
  "agentReady": 4,
  "workflowCount": 12,
  "wfSucceeded": 8,
  "wfRunning": 2,
  "wfFailed": 2,
  "deployCount": 5,
  "hpaCount": 3,
  "agents": [...],
  "workflows": [...],
  "timestamp": "2025-04-23T09:00:00Z"
}
```

### GET /api/agents

Returns a sorted list of agent summaries.

**Response:**

```json
[
  {
    "name": "code-executor",
    "namespace": "ai-agents",
    "type": "executor",
    "provider": "anthropic",
    "model": "claude-sonnet-4-6",
    "phase": "Ready",
    "replicas": 1,
    "autonomy": "supervised",
    "toolCount": 5,
    "age": "2h30m0s",
    "generation": 1,
    "group": "archetypes",
    "image": "localhost/purko-executor:codeact"
  }
]
```

### GET /api/agent/{name}

Returns full agent detail.

**Response:**

```json
{
  "agent": { /* full Agent CR */ },
  "pods": [
    {"name": "code-executor-abc12", "status": "Running", "ip": "10.0.0.5"}
  ]
}
```

### POST /api/create/agent

Creates an agent.

**Request body:**

```json
{
  "name": "my-agent",
  "namespace": "ai-agents",
  "type": "executor",
  "provider": "anthropic",
  "model": "claude-sonnet-4-6",
  "temperature": 0.2,
  "autonomy": "supervised",
  "memory": "buffer",
  "role": "code-reviewer",
  "image": "",
  "group": "sdlc",
  "costLimit": 10.0,
  "maxIterations": 20,
  "systemPrompt": "You are a code reviewer...",
  "tools": ["get_file_contents", "search_code"],
  "minReplicas": 1,
  "maxReplicas": 3,
  "targetCPU": 70
}
```

**Response:** `{"status": "created", "name": "my-agent"}`

### POST /api/create/workflow

Creates a workflow.

**Request body:**

```json
{
  "name": "my-workflow",
  "namespace": "ai-agents",
  "description": "My workflow",
  "steps": [
    {
      "name": "step-1",
      "agent": "my-agent",
      "type": "analysis",
      "dependsOn": [],
      "input": "Analyze the repository"
    }
  ],
  "parallelism": 1,
  "strategy": "failFast",
  "parameters": {"repository": "org/repo"},
  "concurrency": "Forbid",
  "cron": "0 9 * * 1-5"
}
```

**Response:** `{"status": "created", "name": "my-workflow"}`

### GET /api/workflow/{name}

Returns full workflow detail including step statuses, job list, and step outputs.

**Response:**

```json
{
  "workflow": { /* full Workflow CR */ },
  "outputs": {
    "analyze-requirements": "{\"feasibility\":\"approved\"}",
    "design-solution": "{\"components\":[...]}"
  },
  "jobs": [
    {"name": "my-wf-analyze-xyz", "step": "analyze-requirements", "status": "Complete", "duration": "2m15s"}
  ]
}
```

### POST /api/approve/{workflow}/{step}

Approves a step waiting for human review. Sets the `purko.io/approve-{step}` annotation on the workflow.

**Response:** `{"status": "approved", "workflow": "my-workflow", "step": "deploy"}`

### POST /api/deny/{workflow}/{step}

Denies a step and marks it as Failed. Sets the `purko.io/deny-{step}` annotation and updates step status.

**Response:** `{"status": "denied", "workflow": "my-workflow", "step": "deploy"}`

### GET /api/logs/{workflow}/{step}

Returns up to 100 log lines from the most recent pod of the specified step's Job.

**Response:**

```json
{
  "lines": ["[09:00:01] Starting analysis...", "OUTPUT:{...}"],
  "status": "complete",
  "pod": "my-wf-analyze-xyz-abcd1"
}
```

### POST /api/intent

Parses a natural-language intent and suggests agents and a workflow structure.

**Request body:**

```json
{"intent": "Monitor for pod OOM kills and send a Slack alert"}
```

**Response:**

```json
{
  "intent": "Monitor for pod OOM kills and send a Slack alert",
  "mode": "llm",
  "type": "workflow",
  "suggestedAgents": [
    {"name": "cluster-investigator", "group": "investigation", "exists": true, "tools": [...]}
  ],
  "suggestedSteps": [
    {"name": "investigate", "agent": "cluster-investigator"}
  ],
  "existingAgents": ["cluster-investigator (investigation)"]
}
```

`mode` is `"llm"` when the intent was parsed by the LLM, or `"keyword"` when falling back to pattern matching.

### POST /api/trigger/{namespace}/{workflow}

Creates a new workflow run from the named workflow template.

**Request body:** Any JSON payload; all keys are merged into `spec.parameters`.

```bash
curl -X POST http://localhost:8080/api/trigger/ai-agents/sdlc-feature-development \
  -H "Content-Type: application/json" \
  -d '{"repository": "org/repo", "featureTicket": "PROJ-101"}'
```

**Response (201):**

```json
{
  "status": "triggered",
  "run": "sdlc-feature-development-run-1a2b3c4d",
  "namespace": "ai-agents",
  "template": "sdlc-feature-development",
  "route": "explicit",
  "source": "unknown"
}
```

### POST /api/trigger/{namespace}

Auto-routes a webhook payload to a workflow based on trigger rules. The source system is detected from the `X-Trigger-Source` header or payload structure.

**Headers:**

| Header | Description |
|--------|-------------|
| `X-Trigger-Source` | Optional: `github`, `pagerduty`, `slack` |
| `Content-Type` | `application/json` |

**Source auto-detection:**

| Payload key present | Detected source |
|--------------------|----------------|
| `event` | `pagerduty` |
| `repository` | `github` |
| `command` | `slack` |
| (none matched) | `unknown` |

### GET /api/trigger/rules

Returns the current trigger routing rules from the `trigger-rules` ConfigMap.

**Response:** `{"rules": [{"name": "github-pr", "workflow": "sdlc-pr-review", ...}]}`

### POST /api/trigger/rules

Saves trigger routing rules to the `trigger-rules` ConfigMap. Replaces all existing rules.

**Request body:** JSON array of rule objects.

---

## Common curl Examples

```bash
# List all agents
curl http://localhost:8080/api/agents | jq '.[].name'

# Trigger a workflow with parameters
curl -X POST http://localhost:8080/api/trigger/ai-agents/sdlc-feature-development \
  -H "Content-Type: application/json" \
  -d '{"repository":"org/repo","featureTicket":"PROJ-42","featureBranch":"feature/PROJ-42"}'

# Approve a pending step
curl -X POST http://localhost:8080/api/approve/sdlc-feature-development/deploy

# Stream live events
curl -N http://localhost:8080/api/events

# Fetch step logs
curl http://localhost:8080/api/logs/sdlc-feature-development/implement-feature | jq '.lines[]'
```

## Related Resources

- [CLI Reference](cli-reference.md) — `purkoctl` commands for the same operations
- [Concepts: Workflows](../concepts/workflows.md)
