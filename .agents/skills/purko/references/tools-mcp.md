# Tools and MCP Servers Guide

> **Judgment layer.** Generated from purko CRD types and known patterns.
> This file covers how to wire MCP tools to agents. The live layer (cluster
> queries) supplies what MCPServer CRs and tools are actually registered.

---

## MCP Overview

Purko agents call external tools via the **Model Context Protocol (MCP)**. Each
MCP server is a separate process or service that exposes a set of tools the
model can invoke. On purko, MCP servers are registered as `MCPServer` CRs (pod
mode) or as URL entries in the dashboard catalog (connect mode).

---

## MCPServer CR (Pod Mode)

```yaml
apiVersion: purko.io/v1alpha1
kind: MCPServer
metadata:
  name: github
  namespace: ai-agents
spec:
  image: ghcr.io/purko-io/mcp-github:latest   # must be in imageAllowlist if configured
  port: 3000
  args: []                   # container command args
  replicas: 1                # optional; default 1
  auth: bearer               # none | bearer
  secretRef: github-token    # Secret name in same namespace (for bearer auth)
  category: dev-tools        # dashboard filtering
  icon: "🐙"                 # dashboard display
  env:
    - name: GITHUB_ORG
      value: my-org
  resources:
    requests:
      cpu: "100m"
      memory: "128Mi"
    limits:
      cpu: "500m"
      memory: "256Mi"
```

### MCPServer fields

| Field | Required | Notes |
|-------|----------|-------|
| `spec.image` | Yes (pod mode) | Container image. Must pass the image allowlist check (AG-011 equivalent, enforced by dashboard create endpoint). Root images fail — see RunAsNonRoot caveat below. |
| `spec.port` | No | Port the MCP server listens on inside the container. |
| `spec.auth` | No | `none` or `bearer`. With `bearer`, purko reads the token from `spec.secretRef`. |
| `spec.secretRef` | No | Name of a Secret in the same namespace containing the auth token. |
| `spec.hostNetwork` | No | **Always `false` in production.** `hostNetwork: true` is blocked by the dashboard security layer. It was supported only for local minikube/podman dev environments. |

### URL / Connect Mode

Register an already-running MCP server by URL (no CR, no pod — stored in ConfigMap):

```
POST /api/mcp-servers   {"name": "my-server", "url": "http://mcp.svc:3000"}
```

URL mode bypasses the image allowlist (no image to check) but the server must already be reachable from within the cluster.

---

## RunAsNonRoot Caveat

All MCPServer pods run with `runAsNonRoot: true` (enforced by the mcpserver
controller, not configurable). This means:

- Images that run as root (UID 0) by default will **fail to start**.
- The pod will enter `CreateContainerConfigError`.
- Fix: use an image that declares a non-root USER in its Dockerfile, or use an
  image from the purko catalog (all catalog entries are pre-validated).

The GitHub catalog entry was previously affected by this — the catalog now
lists images that pass the non-root requirement. Always verify a custom image
with `docker inspect <image> | jq '.[].Config.User'`.

---

## How an Agent References a Tool

In the Agent CR, list tools by the name they are registered under:

```yaml
spec:
  tools:
    - name: github         # matches MCPServer CR .metadata.name (or catalog entry name)
      type: mcp
    - name: search-knowledge
      type: builtin
```

### Tool types

| `type` | Meaning |
|--------|---------|
| `mcp` | References a registered MCPServer CR (or catalog entry). The executor discovers the server's tool list at runtime. |
| `builtin` | A built-in tool provided by the purko executor runtime (e.g. `search-knowledge`, `web-search`). Available builtins depend on the executor image. |

**There is no static reference validation for tools at admission** — the agent manifest is accepted even if the named MCPServer CR does not exist. The failure surfaces at runtime when the executor tries to discover the server. Use the live catalog query to confirm tool availability before authoring.

---

## Dashboard Catalog

The dashboard exposes two catalogs:

### Skills Catalog
Pre-built Skill CRs (instruction packages) that can be attached to agents via
`spec.skills`. Browseable at Mission Control → Skills.

### MCP Catalog
Pre-validated MCPServer images and configurations. Browseable at Mission
Control → MCP Servers → Catalog. Installing from the catalog:
- Pre-fills `image`, `port`, `auth`, and `category`.
- Uses images that pass the `RunAsNonRoot` check and the image allowlist.
- Adds the MCPServer CR to the namespace.

---

## Tool Discovery Flow

1. Agent step starts; executor resolves the agent's `spec.tools` list.
2. For each `type: mcp` tool, the executor contacts the MCPServer pod at its
   in-cluster URL and requests its tool manifest (MCP `tools/list`).
3. The discovered tools are made available to the model during the step's
   conversation loop.
4. Tool calls made by the model are proxied through the executor to the MCP
   server and the results are returned to the model.

**Implication:** The MCPServer pod must be `Running` and `Ready` before the
step executes. If the server is starting up, tool calls fail with a connection
error and the step may retry (if `retryPolicy` is configured).

---

## Common Tool Patterns from Showcases

| Tool name | Type | Usage in showcases |
|-----------|------|--------------------|
| `github` | mcp | Universal in all 5 showcases — agents read/write GitHub content (docs, repos, issues) |
| `search-knowledge` | builtin | `campaign-strategist` (digital-agency) — built-in knowledge base search |

All showcase agents declare `github` as their sole MCP tool. In production,
replace or augment with tools matched to the agent's actual work (e.g. a
`jira` MCP server for issue tracking, a `slack` MCP server for notifications).

---

## MCP Transport

The `MCPServerSpec` does not expose a `transport` field directly — the
controller manages HTTP transport internally. Custom MCPServer deployments
must speak the standard MCP HTTP/SSE protocol on the declared `spec.port`.

For servers created in URL/connect mode, the URL must be an HTTP or HTTPS
endpoint reachable from within the cluster.
