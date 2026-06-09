# Security

Purko's security model is layered: Kubernetes RBAC controls who can create and modify CRDs, pod security contexts isolate execution, autonomy levels control what agents do at runtime, and cost/iteration limits act as circuit breakers against runaway execution.

---

## Pod Isolation

Every workflow step runs as an isolated Kubernetes Job with its own pod. This means:

- **Blast radius is bounded per step.** A step that fails, hangs, or misbehaves affects only its own pod. It does not share a process namespace or filesystem with other steps.
- **No shared state between steps.** Output passes through the controller via pod logs → ConfigMap → environment variables. Steps cannot directly read each other's memory or filesystem.
- **Automatic cleanup.** Jobs are garbage collected 3600 seconds after completion (`ttlSecondsAfterFinished`). Pod logs are read by the controller before cleanup.
- **No retry amplification.** `backoffLimit: 0` means Kubernetes will not retry a failed job pod. Retry decisions are made by the Workflow controller, which can apply back-off and failure strategies.

---

## RBAC Model

### Operator ClusterRole

The operator runs as ServiceAccount `purko-operator` in the `purko-system` namespace. It is bound to a ClusterRole with the following permissions (from `deploy/helm/templates/rbac.yaml`):

| API Group | Resources | Verbs | Purpose |
|-----------|-----------|-------|---------|
| `purko.io` | `agents`, `agents/status`, `agents/finalizers` | all | Manage Agent CRs |
| `purko.io` | `workflows`, `workflows/status`, `workflows/finalizers` | all | Manage Workflow CRs |
| `batch` | `jobs` | all | Create and delete step Jobs |
| `""` (core) | `pods`, `pods/log` | get, list, watch | Read executor pod logs for output capture |
| `""` (core) | `configmaps` | all | Output ConfigMaps, MCP registry, presets, trigger rules |
| `""` (core) | `secrets` | get, list, watch | Read credentials for LLM providers and MCP auth tokens |
| `apps` | `deployments` | get, list, watch | Dashboard overview of MCP server deployments |
| `autoscaling` | `horizontalpodautoscalers` | get, list, watch | Dashboard status display |
| `coordination.k8s.io` | `leases` | all | Leader election |
| `""` (core) | `events` | create, patch | Emit Kubernetes events for audit trail |

The operator does not hold `create` on `secrets` — it reads existing secrets but never writes them. Credentials are managed by the user and referenced by name.

### Per-Agent RBAC

The Agent controller provisions a `Role` and `RoleBinding` in the agent namespace for each agent. This restricts what the agent's executor pod can do within the cluster. The scope is limited to the agent's own namespace and only to resources the agent needs.

### User-Facing Roles

Two example roles for human operators (from the Security guide):

```yaml
# Full access to manage agents and workflows
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: purko-operator
  namespace: ai-agents
rules:
  - apiGroups: ["purko.io"]
    resources: ["agents", "workflows"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

```yaml
# Read-only access for observers
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: purko-viewer
  namespace: ai-agents
rules:
  - apiGroups: ["purko.io"]
    resources: ["agents", "workflows"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["purko.io"]
    resources: ["agents/status", "workflows/status"]
    verbs: ["get"]
```

---

## Operator Pod Security

The operator Deployment enforces strict security contexts (from `deploy/helm/values.yaml`):

### Pod-Level

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
```

UID 65532 is the `nonroot` user in [distroless/static](https://github.com/GoogleContainerTools/distroless) images, the standard base image for Go operators built with kubebuilder. Running as a known non-root UID satisfies most `PodSecurityAdmission` policies without requiring privilege escalation.

### Container-Level

```yaml
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop: ["ALL"]
```

- **`allowPrivilegeEscalation: false`** — the process cannot gain more privileges than its parent, even if the binary has setuid bits.
- **`readOnlyRootFilesystem: true`** — the container filesystem is mounted read-only. Temporary writes go to explicitly mounted volumes or `emptyDir` mounts.
- **`capabilities: drop: ALL`** — all Linux capabilities are removed. The operator binary does not require any capability (no raw sockets, no bind to privileged ports, no filesystem ownership changes).

### Additional Hardening

The Helm chart also supports:

- `RuntimeDefault` seccomp profile (system call filter)
- Pod Disruption Budget (`minAvailable: 1`) for HA deployments
- Liveness probe at `/healthz:8081` with 15s initial delay
- Readiness probe at `/readyz:8081` with 5s initial delay

---

## Autonomy as a Security Boundary

The Shu-Ha-Ri autonomy system is not just a UX feature — it is a runtime enforcement mechanism that limits what agents can do before humans have established trust.

### Three Levels

| Level | Tool Access | Human Gate |
|-------|-------------|-----------|
| `shu` | Read-only tools only | Every step requires `purko.io/approve-{step}=true` annotation |
| `ha` | All tools in agent spec | Runs automatically; human notified of results |
| `ri` | All tools in agent spec | Fully autonomous; no notification required |

At `shu` level, the executor filters out any tool that modifies state before presenting the tool list to the LLM. The LLM cannot call write tools even if it generates a call — the executor rejects it before making the MCP request. This is enforced inside the pod, independently of RBAC.

### Annotation-Based Approval (Shu Level)

The Workflow controller checks for a step approval annotation before creating the Job:

```
purko.io/approve-{step-name}: "true"
```

If the annotation is absent, the step status is set to `Waiting for approval` and the controller requeues. No Job is created. When an operator adds the annotation (via `kubectl annotate` or the dashboard), the next reconciliation cycle picks it up and creates the Job.

This creates a human-in-the-loop gate that does not require any out-of-band approval system — it uses the Kubernetes object itself as the approval record.

### Promotion and Rollback

The Autonomy controller automatically adjusts autonomy levels based on observed behavior:

- **Promotion** (`shu → ha`, `ha → ri`): triggered when success rate and action count thresholds in `AgentAutonomyPolicy.spec.promotionCriteria` are met.
- **Demotion** (`ha/ri → shu`): triggered when `rollbackTriggers` fire (e.g., success rate drops below 80%, three consecutive failures).

Automatic promotion requires `requiredApprovals: 0`. Setting `requiredApprovals: 1` means a human must explicitly approve the promotion, even if performance criteria are met.

See [Shu-Ha-Ri concept](../concepts/shu-ha-ri.md) for configuration examples.

---

## Cost and Iteration Limits

Cost limits and iteration caps are enforced inside the executor pod, not just declared in the spec. They act as circuit breakers:

| Guardrail | Env Var | Effect |
|-----------|---------|--------|
| `maxIterations` | `MAX_TOOL_CALLS` | Executor stops the ReAct loop after N tool calls, regardless of task completion |
| `costLimitUSD` | `COST_LIMIT_USD` | Executor accumulates per-call cost from LLM API responses; stops when limit exceeded |
| `maxExecutionTime` | `MAX_EXECUTION_TIME` | Passed as the Job `activeDeadlineSeconds`; Kubernetes kills the pod hard |

A runaway LLM that generates an infinite tool-call loop will be stopped by `maxIterations` before it exhausts the cost limit. The `activeDeadlineSeconds` on the Job acts as a final hard stop if the executor process itself hangs.

---

## MCP Server Authentication

MCP servers authenticate callers via bearer tokens injected from Kubernetes Secrets. The MCPServer controller reads the token from a referenced Secret and passes it to the executor via the `MCP_SERVERS` JSON array. The executor includes the token in the `Authorization: Bearer {token}` header on each JSON-RPC request.

Token rotation requires updating the Secret. The MCP registry re-reads the ConfigMap (and therefore the Secret reference) on each 60-second sync cycle, so updated tokens take effect within one cycle without restarting the operator.

---

## Credential Management

Credentials for LLM providers are stored in Kubernetes Secrets and referenced by name in the Agent or LLMProvider spec:

```yaml
model:
  credentialsSecretRef:
    name: anthropic-key
    key: api-key
```

The job builder injects the secret value as `MODEL_API_KEY` using a `secretKeyRef` env var — the value is never written to a ConfigMap or pod spec field as a plain string. The operator itself only reads secrets; it does not create or modify them.

For Vertex AI, a GCP service account JSON key is stored in a Secret and mounted as a file at `/var/run/secrets/gcp/credentials.json`. The executor reads this path via the standard `GOOGLE_APPLICATION_CREDENTIALS` environment variable.

---

## Network Considerations

MCP servers run as ClusterIP services in production. The executor pods connect to them via DNS (`http://{service}.{namespace}.svc.cluster.local:{port}`). No external ingress is required for MCP communication.

For local development (minikube), the operator and MCPServer controller support `hostNetwork: true`, which places pods on the host network. This simplifies local port access but should not be used in production.

The operator's dashboard (`:8082`) is exposed as a ClusterIP Service by default. In production, expose it via an Ingress or `kubectl port-forward` — do not expose it to the internet without authentication.

---

## Security Best Practices

1. **Use `autonomyLevel: restricted` for new agents.** Only promote after observing successful runs in staging.
2. **Set `blastRadiusLimit.maxAffectedResources` and `deniedActions`.** Prevents an agent from modifying more resources than intended.
3. **Always use `credentialsSecretRef`, never embed keys in Agent specs.** Secrets referenced by name are not included in `kubectl get agent -o yaml` output from the API server.
4. **Enable `guardrails.audit: true` for privileged agents.** Audit log entries are written as Kubernetes events and can be forwarded to your SIEM.
5. **Set network policies.** Restrict executor pod egress to only the LLM endpoints and MCP servers each agent needs.
6. **Do not expose the dashboard without authentication** in production environments.

---

## Related Pages

- [Overview](overview.md) — system diagram, namespace model
- [Controllers](controllers.md) — RBAC provisioning by Agent controller
- [Shu-Ha-Ri](../concepts/shu-ha-ri.md) — autonomy levels and progression
- [AgentAutonomyPolicy CRD](../reference/crd-autonomypolicy.md) — promotion and rollback criteria
- [Agent CRD](../reference/crd-agent.md) — guardrails and blast radius fields
