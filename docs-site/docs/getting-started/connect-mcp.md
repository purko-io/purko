# Connect MCP Servers

**Model Context Protocol (MCP)** is an open standard (JSON-RPC 2.0) that lets AI agents call external tools in a uniform way — regardless of whether the tool talks to GitHub, a Kubernetes cluster, a database, or a monitoring stack. Purko discovers tools from MCP servers automatically and makes them available to agents by name.

This page shows you how to deploy the GitHub MCP server, create the authentication secret, verify tool discovery, and assign tools to an agent.

---

## Prerequisites

- Purko installed and the operator running ([Installation](installation.md))
- A GitHub Personal Access Token (classic, with `repo` and `read:org` scopes)
- `kubectl` configured to point at your cluster

---

## How MCP works in Purko

When you deploy an `MCPServer` CR, the Purko controller:

1. Creates a `Deployment` for the MCP server container
2. Creates a `Service` (ClusterIP) on the configured port
3. Registers the server's URL in the `mcp-servers` ConfigMap
4. Polls the server's tool catalog and caches it

After registration, the dashboard's tool picker and the `curl http://localhost:8082/api/mcp/tools` endpoint list the server's tools. Agents reference tools by name in their `spec.tools` list — the executor resolves the name to the correct MCP server at runtime.

---

## Step 1 — Create the auth secret

The GitHub MCP server requires a Personal Access Token. Store it in a Kubernetes Secret:

```bash
kubectl create secret generic github-mcp-token \
  --namespace mcp-servers \
  --from-literal=token=YOUR_GITHUB_PAT_HERE
```

Replace `YOUR_GITHUB_PAT_HERE` with your actual token.

!!! warning "Never put tokens in YAML files committed to version control"
    The `Secret` definition in `examples/mcp-servers/github.yaml` contains a placeholder value. Always create secrets via `kubectl create secret` or a secrets management tool (Vault, External Secrets Operator) rather than embedding tokens in YAML.

If the `mcp-servers` namespace does not exist yet, create it first:

```bash
kubectl create namespace mcp-servers
```

---

## Step 2 — Deploy the MCPServer CR

Save the following to `github-mcp.yaml`:

```yaml
apiVersion: purko.io/v1alpha1
kind: MCPServer
metadata:
  name: github
  namespace: mcp-servers
spec:
  image: ghcr.io/github/github-mcp-server:latest
  port: 8002
  args:
    - http
    - --port
    - "8002"
    - --toolsets
    - all
  replicas: 1
  auth: bearer
  secretRef: github-mcp-token
  icon: "\U0001F419"
  category: code
  hostNetwork: true
  env:
    - name: GITHUB_PERSONAL_ACCESS_TOKEN
  resources:
    requests:
      memory: "128Mi"
      cpu: "50m"
    limits:
      memory: "256Mi"
      cpu: "200m"
```

!!! tip "hostNetwork for minikube / podman"
    `hostNetwork: true` is required when using the `podman` driver because pod-to-pod networking is broken in that environment. The server binds to the host network and is registered as `localhost:8002` instead of its ClusterIP. On a real cluster, set `hostNetwork: false`.

Apply it:

```bash
kubectl apply -f github-mcp.yaml
```

```
mcpserver.purko.io/github created
```

The Purko controller creates the backing `Deployment` and `Service` within a few seconds.

---

## MCPServer spec fields

| Field | Description |
|-------|-------------|
| `image` | Container image for the MCP server |
| `port` | Port the server listens on (also the `Service` port) |
| `args` | Command-line arguments passed to the container |
| `replicas` | Number of server replicas (default: 1) |
| `auth` | Authentication mode: `none` or `bearer` |
| `secretRef` | Name of the Secret containing the `token` key (required when `auth: bearer`) |
| `icon` | Unicode emoji shown in the dashboard |
| `category` | Tool category for filtering: `code`, `kubernetes`, `monitoring`, `custom` |
| `hostNetwork` | Bind to host network (minikube/podman only) |
| `env` | Environment variables injected into the container |
| `resources` | Standard Kubernetes resource requests/limits |

For servers that need no authentication:

```yaml
spec:
  auth: none
```

---

## Step 3 — Verify registration and tool discovery

Check that the `MCPServer` CR is registered:

```bash
kubectl get mcp -n mcp-servers
```

```
NAME     STATUS    TOOLS   AGE
github   Ready     42      60s
```

The `TOOLS` column shows how many tools were discovered from the server's catalog. Tool discovery happens within 60 seconds of the server becoming healthy.

Verify the discovered tools through the dashboard API:

```bash
curl http://localhost:8082/api/mcp/tools | jq '.servers[] | {name, toolCount, status}'
```

```json
{
  "name": "github",
  "toolCount": 42,
  "status": "healthy"
}
```

List available tool names:

```bash
curl http://localhost:8082/api/mcp/tools | jq '.servers[] | select(.name=="github") | .tools[].name'
```

```
"get_file_contents"
"search_code"
"list_pull_requests"
"create_pull_request"
"list_commits"
"search_pull_requests"
"get_issue"
"list_issues"
...
```

---

## Step 4 — Assign MCP tools to an agent

Once tools are discovered, reference them by name in an agent's `spec.tools` list:

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
  namespace: ai-agents
spec:
  type: reviewer
  autonomyLevel: restricted
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.1
  role: code-reviewer
  systemPrompt: |
    You are a senior code reviewer. Evaluate changes for correctness,
    security vulnerabilities, performance, readability, and test coverage.
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
  guardrails:
    maxIterations: 5
    maxExecutionTime: "5m"
    costLimitUSD: 3.0
    contentFilters:
      - no-secrets-in-output
```

The executor resolves each `type: mcp` tool by name against the registered server catalog at runtime. No URL or server address is needed in the agent spec — Purko handles routing.

---

## Manual registration (alternative)

If you are running an MCP server that was not deployed via the `MCPServer` CRD, register it manually:

```bash
kubectl edit configmap mcp-servers -n ai-agents
```

Add to the `servers` list:

```yaml
- name: my-custom-server
  url: http://my-server.my-namespace.svc:8000
  auth: none
  icon: "\U0001F527"
  category: custom
```

The platform polls this URL for tool discovery every 60 seconds.

---

## Available MCP servers

Purko ships with configuration examples for the following MCP servers:

### GitHub

Provides tools for repository browsing, pull requests, code search, issue tracking, and commit history.

| Tool | Description |
|------|-------------|
| `get_file_contents` | Read file content at a given path and ref |
| `search_code` | Search code across repositories |
| `list_pull_requests` | List open/closed PRs with filters |
| `create_pull_request` | Create a pull request |
| `list_commits` | List commits on a branch |
| `get_issue` | Fetch issue details |
| `list_issues` | List issues with label/milestone filters |

Server image: `ghcr.io/github/github-mcp-server:latest`
Auth: Bearer token (GitHub PAT)

### Lumino

Provides tools for Kubernetes cluster investigation — pod logs, events, namespace overview, pipeline status.

| Tool | Description |
|------|-------------|
| `list_pods_in_namespace` | List pods with status |
| `analyze_logs` | Pattern-match logs across pods |
| `get_pipelinerun_logs` | Fetch Tekton PipelineRun logs |
| `smart_summarize_pod_logs` | AI-summarized pod logs |
| `adaptive_namespace_investigation` | Full namespace health report |

Server is pre-registered at `http://localhost:8000` in the default Helm values.
Auth: none (internal cluster access)

### PagerDuty

Provides tools for on-call management, incident creation, and escalation.

| Tool | Description |
|------|-------------|
| `get_oncall_users` | Query current on-call schedule |
| `list_incidents` | List open incidents |
| `create_incident` | Create a PagerDuty incident |
| `resolve_incident` | Resolve an incident |

Auth: Bearer token (PagerDuty API key)

To add PagerDuty:

```bash
kubectl create secret generic pagerduty-token \
  --namespace mcp-servers \
  --from-literal=token=YOUR_PD_API_KEY
```

```yaml
apiVersion: purko.io/v1alpha1
kind: MCPServer
metadata:
  name: pagerduty
  namespace: mcp-servers
spec:
  image: ghcr.io/your-org/pagerduty-mcp-server:latest
  port: 8003
  auth: bearer
  secretRef: pagerduty-token
  icon: "\U0001F514"
  category: monitoring
  resources:
    requests:
      memory: "128Mi"
      cpu: "50m"
```

---

## Troubleshooting

**MCPServer stuck in `Pending`**

```bash
kubectl describe mcp github -n mcp-servers
```

Check that the Secret exists:

```bash
kubectl get secret github-mcp-token -n mcp-servers
```

**Tools not discovered after 60 seconds**

Check the server pod is running:

```bash
kubectl get pods -n mcp-servers
```

Check server logs:

```bash
kubectl logs -n mcp-servers -l app=github-mcp-server
```

Verify the server responds to its catalog endpoint:

```bash
# With port-forward if hostNetwork is false
kubectl port-forward -n mcp-servers svc/github-mcp-server 8002:8002
curl http://localhost:8002/mcp/tools
```

**Agent cannot use MCP tool**

Confirm the tool name in the agent spec exactly matches the name returned by the catalog:

```bash
curl http://localhost:8082/api/mcp/tools | jq '.servers[].tools[].name' | grep get_file
```

Check that the agent's `autonomyLevel` permits tool calls. `restricted` allows read-only tools; `supervised` or `full` is required for write tools.

---

## Next steps

- [MCP Servers concept page](../concepts/mcp-servers.md) — full MCPServer CRD reference
- [Building Agents guide](../guides/building-agents.md) — combining multiple MCP servers in one agent
- [Your First Workflow](first-workflow.md) — wire MCP-enabled agents into a pipeline
