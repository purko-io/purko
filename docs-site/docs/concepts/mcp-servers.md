# MCP Servers

**MCP** (Model Context Protocol) is a JSON-RPC 2.0 standard that lets AI models call external tools through a uniform interface. Any service that implements the MCP specification can be connected to Purko agents — once registered, its tools are immediately available to every agent in the cluster.

---

## How Purko Manages MCP Servers

You do not need to manually wire up MCP servers. Purko provides an `MCPServer` CRD that automates the full lifecycle:

1. **Apply** an `MCPServer` CR pointing at your server image
2. The **MCPServer controller** creates a Kubernetes `Deployment` and `ClusterIP Service`
3. Within ~60 seconds the controller **discovers tools** by calling the server's `list_tools` endpoint
4. Discovered tools are written to the **`mcp-servers` ConfigMap** in the `ai-agents` namespace
5. Every executor pod reads this ConfigMap at startup, so all agents see the new tools without restarting

```
MCPServer CR
     |
     v
MCPServer controller
     |
     +---> Deployment (server pod)
     |
     +---> Service (ClusterIP)
     |
     +---> ConfigMap: mcp-servers
               |
               v
         Executor reads at runtime
```

### Manual registration

If you deploy MCP servers outside Purko (e.g., an existing service), register them manually by editing the ConfigMap:

```bash
kubectl edit configmap mcp-servers -n ai-agents
```

Add an entry to the `servers` list:

```yaml
- name: my-server
  url: http://my-server.my-namespace.svc:8000
  auth: none
  category: custom
```

---

## MCPServer Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | string | Yes | Container image for the MCP server |
| `port` | integer | No | Port the server listens on (default: 8000) |
| `args[]` | list | No | Command arguments passed to the container |
| `replicas` | integer | No | Number of server replicas |
| `auth` | string | No | Authentication scheme: `none` or `bearer` |
| `secretRef` | string | No | Name of a Secret containing the bearer token (key: `token`) |
| `icon` | string | No | Icon character for the dashboard UI |
| `category` | string | No | Tool category for UI grouping |
| `hostNetwork` | boolean | No | Use host networking (required for minikube/podman) |
| `env[]` | list | No | Environment variables for the server container |
| `resources.requests` | map | No | CPU and memory requests |
| `resources.limits` | map | No | CPU and memory limits |

---

## Available Servers

Purko ships with example MCPServer CRs for three servers:

| Server | Tools | Category | Auth | Description |
|--------|-------|----------|------|-------------|
| `github` | 41 | code | bearer token | GitHub API — repos, PRs, issues, code search |
| `lumino` | 38 | kubernetes | none | Kubernetes/OpenShift cluster operations and log analysis |
| `pagerduty` | 13 | alerting | bearer token | PagerDuty incidents, schedules, services, on-call |

Check tool counts at runtime:

```bash
curl http://localhost:8082/api/mcp/tools | jq '.servers[] | {name, toolCount, status}'
```

---

## Authentication

### No authentication

```yaml
spec:
  auth: none
```

All requests to the server are made without credentials. Suitable for internal cluster-only servers.

### Bearer token

```yaml
spec:
  auth: bearer
  secretRef: my-server-token
```

Create the Secret with a `token` key:

```bash
kubectl create secret generic my-server-token \
  --from-literal=token=your-bearer-token \
  -n mcp-servers
```

The controller mounts this token and passes it in the `Authorization: Bearer <token>` header on every MCP request.

---

## hostNetwork for minikube

Pod-to-pod DNS resolution can be broken on minikube and podman-based clusters. Use `hostNetwork: true` to bind the server directly to the host network:

```yaml
spec:
  hostNetwork: true
```

When `hostNetwork` is enabled, the MCPServer controller registers the server at `http://localhost:PORT` instead of the in-cluster Service URL.

!!! warning
    Do not use `hostNetwork: true` on production clusters. It bypasses Kubernetes network isolation and exposes the server on the node's network interface.

---

## Example YAML

### Internal server (no auth)

```yaml
apiVersion: purko.io/v1alpha1
kind: MCPServer
metadata:
  name: analytics-tools
  namespace: mcp-servers
spec:
  image: my-org/analytics-mcp:v1.2.0
  port: 8000
  auth: none
  icon: "@"
  category: analytics
  resources:
    requests:
      memory: "128Mi"
      cpu: "50m"
    limits:
      memory: "256Mi"
      cpu: "200m"
```

### External API with auth and custom args

```yaml
apiVersion: purko.io/v1alpha1
kind: MCPServer
metadata:
  name: github
  namespace: mcp-servers
spec:
  image: ghcr.io/github/github-mcp-server:latest
  port: 9090
  args: ["http", "--port", "9090", "--toolsets", "all"]
  auth: bearer
  secretRef: github-mcp-token
  icon: "G"
  category: code
  env:
    - name: GITHUB_HOST
      value: "github.com"
  resources:
    requests:
      memory: "64Mi"
      cpu: "50m"
```

### Verify

```bash
# Check MCPServer status
kubectl get mcp -n mcp-servers

# Describe a server to see conditions
kubectl describe mcp github -n mcp-servers

# List all discovered tools
curl http://localhost:8082/api/mcp/tools | jq '.servers[].tools[].name'
```

---

## MCPServer Status

| Field | Description |
|-------|-------------|
| `phase` | `Pending`, `Ready`, or `Error` |
| `toolCount` | Number of tools discovered on the last discovery run |
| `lastDiscovery` | Timestamp of the most recent tool discovery |
| `message` | Human-readable status message |
| `conditions` | Standard Kubernetes conditions |

---

## See Also

- [Tool Types](tool-types.md) — how agents route calls to MCP tools
- [Agents](agents.md) — referencing MCP tools in `spec.tools[]`
