# Installation

This page walks you through installing Purko on a Kubernetes cluster, from prerequisites to a running operator with the CLI tool `purkoctl`.

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| kubectl | >= 1.26 | Cluster interaction |
| Kubernetes | >= 1.26 | Target cluster |
| Helm | >= 3 | Install the Purko chart |
| Go | >= 1.21 | Build `purkoctl` (optional if you use pre-built binaries) |

For local development, [minikube](https://minikube.sigs.k8s.io/) is the recommended cluster. The Purko operator works best with at least **4 CPUs and 8 GB of RAM** allocated.

!!! tip "Minikube quick start"
    Start a local cluster with enough resources:

    ```bash
    minikube start --driver=podman --cpus=4 --memory=8192
    ```

    If you use the `podman` driver, you will also need to enable `hostNetwork` for the operator (covered below).

---

## Step 1 — Install the Helm chart

Clone the repository and install from the local chart:

```bash
git clone https://github.com/geored/purko.git
cd purko
helm install purko deploy/helm/ --create-namespace --namespace purko-system
```

The chart installs:

- The `Agent`, `Workflow`, `MCPServer`, `LLMProvider`, and `AgentAutonomyPolicy` CRDs
- The Purko operator Deployment in `purko-system`
- RBAC (ServiceAccount, ClusterRole, ClusterRoleBinding)
- A dashboard Service on port 8082
- The `ai-agents` namespace for your Agent and Workflow resources

### Key values

The chart is configured through `deploy/helm/values.yaml`. The most commonly changed values:

| Value | Default | Description |
|-------|---------|-------------|
| `llm.provider` | `""` (auto-detect) | LLM provider: `anthropic`, `openai`, `vertex-ai`, `ollama` |
| `llm.model` | `claude-sonnet-4-6` | Default model name |
| `operator.hostNetwork` | `false` | Set `true` for minikube/podman |
| `dashboard.port` | `8082` | Dashboard and API port |
| `webhooks.enabled` | `false` | Enable server-side webhook validation |

Override values at install time:

```bash
helm install purko deploy/helm/ \
  --namespace purko-system \
  --create-namespace \
  --set llm.provider=anthropic \
  --set llm.credentials.secretName=anthropic-api-key
```

!!! warning "minikube / podman networking"
    Pod-to-pod networking does not work correctly with the `podman` driver by default. Enable `hostNetwork` so the operator and MCP servers bind to the host network:

    ```bash
    helm install purko deploy/helm/ \
      --namespace purko-system \
      --create-namespace \
      --set operator.hostNetwork=true
    ```

    When `hostNetwork: true` is set, MCP servers are also registered as `localhost:<port>` instead of their ClusterIP address.

---

## Step 2 — Verify the operator is running

```bash
kubectl get pods -n purko-system
```

Expected output:

```
NAME                               READY   STATUS    RESTARTS   AGE
purko-operator-xxxxx-yyyyy         1/1     Running   0          30s
```

Check that the CRDs were installed:

```bash
kubectl get crds | grep purko
```

Expected output:

```
agents.purko.io                     2026-04-23T09:00:00Z
agentautonomypolicies.purko.io      2026-04-23T09:00:00Z
llmproviders.purko.io               2026-04-23T09:00:00Z
mcpservers.purko.io                 2026-04-23T09:00:00Z
workflows.purko.io                  2026-04-23T09:00:00Z
```

Check that the API resources are registered (note the short names):

```bash
kubectl api-resources | grep purko
```

```
agents                ag    purko.io/v1alpha1    true    Agent
workflows             wf    purko.io/v1alpha1    true    Workflow
mcpservers            mcp   purko.io/v1alpha1    true    MCPServer
llmproviders          llm   purko.io/v1alpha1    true    LLMProvider
```

The short names (`ag`, `wf`, `mcp`, `llm`) can be used anywhere you would type the full resource name.

---

## Step 3 — Access the dashboard

The Purko dashboard provides a UI for creating agents, monitoring workflows, and browsing MCP tool catalogs.

```bash
kubectl port-forward -n purko-system deploy/purko-operator 8082:8082
```

Then open [http://localhost:8082](http://localhost:8082) in your browser.

!!! tip
    Keep the port-forward running in a separate terminal while you follow the rest of the Getting Started guide.

The dashboard also exposes a REST API at `http://localhost:8082/api/`. The CLI (`purkoctl`) uses this API internally.

---

## Step 4 — Install purkoctl

`purkoctl` is a CLI for managing agents, workflows, and MCP servers from the terminal. Build it from source:

```bash
# From the repository root
go build \
  -ldflags "-X main.version=$(git describe --tags --always)" \
  -o bin/purkoctl \
  ./cmd/purkoctl/
```

Move the binary to a directory on your `PATH`:

```bash
mv bin/purkoctl /usr/local/bin/purkoctl
```

Verify the installation:

```bash
purkoctl version
```

Expected output:

```
purkoctl v0.1.0-alpha (build: abc1234)
API: http://localhost:8082
```

!!! tip
    `purkoctl` defaults to `http://localhost:8082` as the API endpoint. If you change the dashboard port or run it on a remote cluster, set the `PURKO_API` environment variable:

    ```bash
    export PURKO_API=http://my-cluster:8082
    ```

---

## Uninstall

Remove all agent and workflow resources before uninstalling, or their finalizers will block deletion:

```bash
kubectl delete agents,workflows --all -A
helm uninstall purko --namespace purko-system
kubectl delete namespace purko-system
```

To remove the CRDs as well (this deletes all stored Agent and Workflow data):

```bash
kubectl get crds | grep purko | awk '{print $1}' | xargs kubectl delete crd
```

---

## Next steps

- [Your First Agent](first-agent.md) — deploy an AI agent in under five minutes
- [Your First Workflow](first-workflow.md) — wire agents together into a multi-step pipeline
- [Connect MCP Servers](connect-mcp.md) — extend agents with GitHub, PagerDuty, and other integrations
