# Agent Memory

Purko agents can accumulate knowledge across workflow runs through persistent memory. Two concepts work together: **behavior** controls what the agent remembers, and **provider** controls where memories are stored. These are owned by different personas — agent authors set behavior, platform operators configure providers.

## Behaviors

Set `spec.memory.behavior` to one of three values:

| Behavior | Promise | When to use |
|----------|---------|-------------|
| `persistent` | Remembers across runs — tasks, results, lessons learned | Monitors, reviewers, accumulating knowledge bases |
| `session` | Context available within one run; discarded at completion | Default when `spec.memory` is unset |
| `"off"` | Clean slate every run; no recall, no store | Stateless, audit-safe, or privacy-sensitive agents |

### persistent

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: incident-reviewer
  namespace: ai-agents
spec:
  memory:
    behavior: persistent
```

Before each step the operator recalls relevant past entries and injects them into the agent's context. After each step the agent's output is stored in the memory provider.

### session

```yaml
spec:
  memory:
    behavior: session
```

Maintains context within a single workflow run using an in-memory conversation buffer. Nothing is written to persistent storage.

### "off"

```yaml
spec:
  memory:
    behavior: "off"
```

!!! warning
    Always quote `"off"` in YAML. Without quotes, `off` is parsed as a boolean `false` by YAML 1.1 (used by most Kubernetes clients) and will fail CRD validation.

## Scope

`spec.memory.scope` controls the recall pool:

| Value | Recall pool |
|-------|-------------|
| `agent` | This agent's memories only (default) |
| `group` | All agents sharing the same `app.kubernetes.io/component` label |
| `namespace` | All agents in the same namespace |

```yaml
spec:
  memory:
    behavior: persistent
    scope: group
```

Memory is never shared across namespaces — the scope key is computed by the controller from the namespace and scope value and is never trusted from executor output.

!!! warning
    Group memory is keyed by the `app.kubernetes.io/component` label. Renaming a group orphans memories under the old key; they remain in the store and age out via retention but are no longer recalled.

## Provider Reference

A `MemoryProvider` CR configures the storage backend. The built-in SQLite+FTS5 provider ships with every install — no `MemoryProvider` resource needs to be created on a fresh install. The operator behaves as if a built-in default provider exists automatically.

```yaml
# From examples/memory-providers/builtin.yaml
# Create this only to set an explicit default or to tune retention.
apiVersion: purko.io/v1alpha1
kind: MemoryProvider
metadata:
  name: builtin
  namespace: purko-system
spec:
  type: builtin
  default: true
  retention:
    maxEntriesPerScope: 500
    recallLogMaxAgeDays: 90
```

To point an agent at a specific provider, set `spec.memory.providerRef`:

```yaml
spec:
  memory:
    behavior: persistent
    providerRef: builtin   # omit to use the platform default
```

Check provider health and entry counts:

```bash
kubectl get memoryproviders -n purko-system
```

!!! tip
    Retention limits (`maxEntriesPerScope`, `recallLogMaxAgeDays`) are governed by the `MemoryProvider` CR, not by Helm values. The values shown under `operator.memory.retention` in `values.yaml` are informational defaults that mirror the built-in provider's built-in behavior. To change retention, edit the `MemoryProvider` CR.

## Memory Spec Fields

| Field | Type | Description |
|-------|------|-------------|
| `behavior` | string | `"off"`, `session`, or `persistent`. Controls recall and store lifecycle. |
| `scope` | string | `agent` (default), `group`, or `namespace`. Controls the recall pool. |
| `providerRef` | string | Name of a `MemoryProvider` CR. Defaults to the platform default provider. |
| `maxContextTokens` | int | Maximum tokens injected as recalled context per step (default 2048). |

## Helm Configuration

Memory is enabled by default. Helm values under `operator`:

```yaml
operator:
  memory:
    enabled: true
```

Set `enabled: false` to prevent the operator from initializing the memory store. This renders `PURKO_MEMORY_ENABLED="false"`, which is the exact string the operator checks — any other value, including absent, leaves memory enabled.

!!! warning "Apply CRDs before upgrading"
    When upgrading to a release that introduces agent memory, apply CRDs before the chart upgrade:

    ```bash
    kubectl apply -f crds/
    helm upgrade purko purko/purko ...
    ```

    The operator watches the `MemoryProvider` CRD. Upgrading the chart without applying the new `crds/memoryprovider.crd.yaml` first leaves the operator unable to initialize its cache and it will crash-loop.

!!! note "PVC provisioning on upgrade"
    `operator.memory.enabled` defaults to `true`. If an existing install had `operator.history.enabled: false` (no PVC provisioned), upgrading to a release that includes agent memory will create the shared `purko-history` PVC. Set `operator.memory.enabled: false` before upgrading if you do not want memory storage provisioned.

Memory entries are stored at `/var/lib/purko/memory.db` on the same PVC as execution history. Override the path with the `PURKO_MEMORY_PATH` environment variable if you need to relocate the store.

## Legacy Compatibility

Agents that use the `spec.memory.type` field (`buffer`, `summary`, `vector`, `none`) continue to work unchanged.

| Legacy `type` | Display-equivalent `behavior` |
|---------------|-------------------------------|
| `none` | `"off"` |
| `buffer` | `session` |
| `summary` | `persistent` |
| `vector` | `persistent` |

This mapping is used for dashboard display only. The runtime code path is unchanged unless `behavior` is explicitly set — an agent with `type: vector` and no `behavior` keeps its existing PVC mount and file-based recall path. A new `behavior` value activates the new controller-mediated path and takes precedence over `type`.

!!! warning
    Do not set both `type` and `behavior`. When `behavior` is set, it drives the runtime path and `type` becomes display-only. New agents should use `behavior` only.

## See Also

- [Memory Concepts](../concepts/memory.md) — background on memory types and decision guide
- [Building Custom Executors](custom-executor.md) — memory protocol for custom containers
- [Agent CRD Reference](../reference/crd-agent.md) — full `spec.memory` field reference
