# Tool Types

Tools are the actions an agent can take. When the language model decides to use a tool, the Purko executor routes the call to the right backend based on the tool's `type` field in `spec.tools[]`.

There are four tool types:

| Type | Backend | Best For |
|------|---------|---------|
| `mcp` | JSON-RPC call to an MCP server | External integrations (GitHub, PagerDuty, Kubernetes) |
| `function` | Python/bash execution in the CodeAct sandbox | Code execution, data processing, calculations |
| `api` | Direct HTTP call to an `EndpointSpec` | Simple REST integrations |
| `builtin` | In-process handler compiled into the executor | Static analysis, workflow chaining |

---

## mcp — MCP server integration

The `mcp` type routes the tool call as a JSON-RPC 2.0 request to a registered [MCP server](mcp-servers.md). The executor looks up the tool name in the MCP registry, finds the owning server, and calls it.

**When to use**: any integration where an MCP server already exists — GitHub, Jira, PagerDuty, Kubernetes, databases, etc.

```yaml
spec:
  tools:
    - name: list_pods_in_namespace
      type: mcp
    - name: create_pull_request
      type: mcp
    - name: get_incident
      type: mcp
```

The tool `name` must match a tool name discovered from a registered MCP server. Check available names:

```bash
curl http://localhost:8082/api/mcp/tools | jq '.servers[].tools[].name'
```

!!! tip
    If an agent has no `spec.tools[]` at all, it gets access to all tools from all registered MCP servers. This is convenient during development; restrict tools in production.

---

## function — CodeAct sandbox

The `function` type executes Python or bash code inside a sandboxed subprocess running in the executor pod. The executor captures stdout and returns it to the model as the tool result.

**When to use**: data processing, calculations, file manipulation, any logic that would be awkward to express as a REST call.

```yaml
spec:
  runtime:
    codeExecution:
      enabled: true
      languages: [python, bash]
      sandbox:
        maxExecutionSeconds: 30
        maxOutputBytes: 100000
        networkAccess: false
        writablePaths: ["/tmp"]
  tools:
    - name: execute_code
      type: function
    - name: code-sandbox     # alias for execute_code
      type: function
    - name: vector-search    # built-in function for memory search
      type: function
```

The `execute_code` and `code-sandbox` tools accept `code` and `language` arguments. The model writes the code, the executor runs it, and the output is fed back into the conversation.

!!! warning
    Code execution is disabled by default (`codeExecution.enabled: false`). Enable it only for agents that genuinely need it. The sandbox blocks network access and limits execution time to reduce risk.

---

## api — direct HTTP endpoint

The `api` type makes a direct HTTP call to a URL you specify in `endpoint`. No MCP server is involved — the executor calls the URL directly and returns the response body.

**When to use**: simple REST APIs that do not have an MCP server, internal services, one-off integrations.

```yaml
spec:
  tools:
    - name: prometheus_query
      type: api
      endpoint:
        url: http://prometheus.monitoring.svc:9090/api/v1/query
        method: POST
        headers:
          Content-Type: application/json
        timeoutSeconds: 30
    - name: send_slack_alert
      type: api
      endpoint:
        url: https://hooks.slack.com/services/T000/B000/xxx
        method: POST
        timeoutSeconds: 10
```

The executor serializes the tool arguments as JSON and sends them in the request body (POST) or as query parameters (GET).

### EndpointSpec fields

| Field | Type | Description |
|-------|------|-------------|
| `url` | string | Full URL of the endpoint |
| `method` | string | HTTP method: `GET`, `POST`, `PUT`, `DELETE`, `PATCH` |
| `headers` | map | Static HTTP headers to include in every request |
| `timeoutSeconds` | integer | Per-request timeout in seconds |
| `authScheme` | string | `Bearer`, `Token`, `Basic`, or `ApiKey` |
| `authHeader` | string | Header name for the auth token (default: `Authorization`) |

---

## builtin — in-process handlers

The `builtin` type invokes a handler that is compiled directly into the executor binary. No network call is made. These tools are always available regardless of which MCP servers are registered.

**When to use**: fast, deterministic operations that do not need external calls.

Two builtin tools ship with Purko:

### `static-analysis`

Analyzes Python or Go code for security issues and anti-patterns. Returns a list of findings with severity levels.

```yaml
spec:
  tools:
    - name: static-analysis
      type: builtin
```

Example findings:
- `eval()` usage — HIGH severity
- Hardcoded secrets — HIGH severity
- Bare `except:` clause — LOW severity

### `trigger-workflow`

Chains to another Purko workflow. Useful when a planner agent decides that a full incident-response workflow should be started based on what it found.

```yaml
spec:
  tools:
    - name: trigger-workflow
      type: builtin
```

The model calls the tool with `workflow` (name), `namespace`, and `payload` arguments. The executor posts to the Purko dashboard API, which starts the target workflow.

---

## How the Executor Routes Tool Calls

When the model requests a tool, the executor follows this routing order:

1. **Function tools** — check the `FUNCTION_TOOLS` registry (`execute_code`, `vector-search`, `code-sandbox`)
2. **Builtin tools** — check the `BUILTIN_TOOLS` registry (`static-analysis`, `trigger-workflow`)
3. **MCP tools** — look up the owning MCP client in the tool-to-client map; try all clients on cache miss
4. **API tools** — scan `spec.tools[]` for a matching name with an `endpoint` field and call it directly

If none of the above produces a result, the executor returns `Tool '{name}' returned no result`.

---

## Autonomy Enforcement

The executor filters tools before passing them to the model based on `autonomyLevel`:

| Autonomy Level | Tool Access |
|----------------|-------------|
| `manual` | No tools — analysis-only mode |
| `restricted` (Shu) | Read-only MCP tools only; write operations and code execution blocked |
| `supervised` (Ha) | All tools available |
| `full` (Ri) | All tools available |

Write-capable MCP tools that are blocked under `restricted` include: `push_files`, `create_pull_request`, `merge_pull_request`, `create_branch`, `add_issue_comment`, and similar mutation operations.

---

## Tool Definition in Agent Spec

Full example showing all four types:

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: multi-tool-agent
  namespace: ai-agents
spec:
  type: executor
  model:
    provider: anthropic
    name: claude-sonnet-4-6
  autonomyLevel: supervised
  runtime:
    codeExecution:
      enabled: true
      languages: [python, bash]
      sandbox:
        maxExecutionSeconds: 60
        networkAccess: false
  tools:
    # mcp — calls GitHub MCP server
    - name: search_code
      type: mcp

    # function — executes Python code in sandbox
    - name: execute_code
      type: function

    # api — calls Prometheus directly
    - name: query_metrics
      type: api
      endpoint:
        url: http://prometheus.monitoring.svc:9090/api/v1/query_range
        method: POST
        timeoutSeconds: 30

    # builtin — in-process code analysis
    - name: static-analysis
      type: builtin
```

---

## See Also

- [MCP Servers](mcp-servers.md) — deploying and registering MCP servers
- [Agents](agents.md) — full agent spec reference
- [Memory](memory.md) — the `vector-search` function tool and vector memory
