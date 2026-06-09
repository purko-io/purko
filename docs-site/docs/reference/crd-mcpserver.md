# MCPServer CRD

**API Version:** `purko.io/v1alpha1`
**Kind:** `MCPServer`
**Scope:** Namespaced

An MCPServer is a Kubernetes resource that deploys and registers a Model Context Protocol server, making its tools available to agents.

## Example

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
  category: code
  hostNetwork: true
  env:
    - name: GITHUB_PERSONAL_ACCESS_TOKEN
      value: ""
  resources:
    requests:
      memory: "128Mi"
      cpu: "50m"
    limits:
      memory: "256Mi"
      cpu: "200m"
---
apiVersion: v1
kind: Secret
metadata:
  name: github-mcp-token
  namespace: mcp-servers
type: Opaque
stringData:
  token: "YOUR_GITHUB_PAT_HERE"
```

## Spec Fields

### MCPServerSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | string | Yes | Container image for the MCP server |
| `port` | int | No | Port the MCP server listens on (default 8080) |
| `args` | []string | No | Command-line arguments passed to the container |
| `replicas` | int | No | Number of replicas to run (default 1) |
| `auth` | string | No | Authentication scheme: `none` or `bearer` |
| `secretRef` | string | No | Name of the Kubernetes Secret containing the bearer token (key: `token`) |
| `icon` | string | No | Display icon (Unicode emoji) shown in the dashboard |
| `category` | string | No | Category label for dashboard grouping (e.g. `code`, `data`, `infra`) |
| `hostNetwork` | bool | No | Use host networking for the pod (required for minikube/podman setups) |
| `env` | [][EnvVar](#envvar) | No | Environment variables injected into the server container |
| `resources` | [ResourceRequirements](#resourcerequirements) | No | Kubernetes resource requests and limits |

### ResourceRequirements

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `requests` | map[string]string | No | Resource requests (e.g. `cpu: "50m"`, `memory: "128Mi"`) |
| `limits` | map[string]string | No | Resource limits (e.g. `cpu: "200m"`, `memory: "256Mi"`) |

### EnvVar

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Environment variable name |
| `value` | string | Yes | Environment variable value |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Server phase: `Pending`, `Ready`, `Error` |
| `toolCount` | int | Number of tools discovered from this server |
| `lastDiscovery` | timestamp | When tools were last discovered from the server |
| `message` | string | Human-readable status or error message |
| `conditions` | []Condition | Standard Kubernetes conditions |

## Conditions

| Condition | Meaning |
|-----------|---------|
| `Ready` | Server is deployed and responding to tool discovery |
| `Error` | Server failed to deploy or respond |

## Related Resources

- [Agent CRD](crd-agent.md) — agents reference MCP tools via `spec.tools[].type: mcp`
- [Concepts: MCP Servers](../concepts/mcp-servers.md)
- [Guide: Connect MCP Servers](../getting-started/connect-mcp.md)
