# Production Deployment

The [installation guide](installation.md) gets Purko running on a local
minikube cluster. This page covers what changes when you deploy to a real
Kubernetes cluster — EKS, GKE, AKS, OpenShift, or self-managed.

## Prerequisites

- Kubernetes 1.27+ with a default StorageClass (for the execution history PVC)
- `kubectl` and `helm` 3.8+ (OCI registry support)
- Cluster-admin access for the CRDs and RBAC

## 1. Install the CRDs

```bash
kubectl apply -k "https://github.com/purko-io/purko/crds?ref=v0.2.0"
```

Pin `ref=` to the release you deploy. CRD upgrades are explicit — re-run
this on every Purko upgrade *before* `helm upgrade` (Helm never upgrades
CRDs itself).

## 2. Pin images

Never run `:latest` in production. Pin the chart and images to a release:

```bash
helm install purko oci://ghcr.io/purko-io/purko --version 0.2.0 \
  --namespace purko-system --create-namespace \
  --set operator.image=ghcr.io/purko-io/purko-operator:v0.2.0 \
  --set executor.image=ghcr.io/purko-io/purko-executor:v0.2.0
```

## 3. Production values

The minikube defaults need review before production use:

| Value | minikube | Production |
|-------|----------|------------|
| `operator.hostNetwork` | `true` (podman networking) | **`false`** — pods use cluster networking |
| `executor.hostNetwork` | `true` when MCP servers run on the node | **`false`** — executors reach MCP servers and LLM endpoints via cluster DNS |
| `webhooks.enabled` | `false` | **`true`** — reject invalid Agents/Workflows at admission |
| `webhooks.certManager.enabled` | `false` (chart-generated certs) | `true` if the cluster runs cert-manager |
| `operator.history.storageClass` | `""` (cluster default) | An SSD-backed class; history is a write-frequent SQLite file |
| `operator.resources` | defaults | Size for your workflow volume; the defaults (128Mi–512Mi) suit small fleets |

!!! warning "GitOps (ArgoCD / Flux)"
    The default webhook TLS path generates certificates with Helm's `lookup`,
    which does not run under `helm template`-based GitOps renderers — certs
    would regenerate on every sync. Use `webhooks.certManager.enabled: true`
    (requires cert-manager) or pre-provision your own certificate Secret via
    `webhooks.tls.secretName`.

## 4. Set the license tier

Without a license, the operator runs in **dev mode** (all limits
unlimited) — fine for evaluation, not for a deliberate community
deployment. Set the community tier explicitly:

```bash
kubectl create secret generic purko-license \
  -n purko-system --from-literal=license=community
```

or set `PURKO_LICENSE=community` in the operator environment. This applies
the community limits (7-day execution history retention, linear
workflows). Purko Pro licenses unlock DAG workflows, the intent bar,
Shu-Ha-Ri autonomy, SSO, and 90-day retention — see
[purko.io/pricing](https://purko.io/pricing).

## 5. Expose the dashboard — carefully

The community dashboard has **no authentication**. Do not expose port 8082
to the internet. Options, in order of preference:

1. Keep it internal and use `kubectl port-forward` (or your VPN/bastion).
2. Put it behind an authenticating reverse proxy or Ingress with SSO
   enforced by your platform (oauth2-proxy, Istio RequestAuthentication,
   cloud IAP).
3. Purko Pro ships an integrated OAuth2 Proxy sidecar (`auth.enabled`).

## 6. Storage and backups

Execution history lives in a SQLite database on the `purko-history` PVC
(`/var/lib/purko/history.db`, WAL mode). It intentionally has no owner
references, so it survives `helm uninstall`. Include the PVC in your
volume-snapshot or backup schedule if the audit trail matters to you.
Agent vector memory (e.g. the `knowledge-retriever` starter agent) uses
its own PVC per agent.

## 7. High availability notes

- The operator runs a single replica by default. Leader election is
  supported (`--leader-elect`), but the history PVC is `ReadWriteOnce` —
  multi-replica setups need the standby on the same node, or disable
  history until a PostgreSQL backend lands.
- Executor Jobs are independent pods; operator restarts do not interrupt
  running steps. Workflow state is reconstructed from the cluster, and
  archived history survives on the PVC.

## 8. Upgrades

```bash
kubectl apply -k "https://github.com/purko-io/purko/crds?ref=v0.3.0"   # CRDs first
helm upgrade purko oci://ghcr.io/purko-io/purko --version 0.3.0 \
  --namespace purko-system --reuse-values
```

Before uninstalling, delete workload resources first or their finalizers
will block namespace deletion:

```bash
kubectl delete agents,workflows --all -A
helm uninstall purko -n purko-system
```
