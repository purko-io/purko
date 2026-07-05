# Execution History

Purko persists workflow execution history, step outputs, and a tool call
audit trail in an embedded SQLite database on a PersistentVolumeClaim. Unlike
live workflow status (which disappears when pods are garbage-collected or the
Workflow resource is deleted), the history archive survives pod restarts,
operator redeploys, and workflow deletion.

## What Gets Recorded

| Record | When | Contents |
|--------|------|----------|
| Workflow run | On workflow start and every phase transition | Name, namespace, phase, parameters, step counts, start/completion times, final message |
| Step execution | When a step's Job succeeds or terminally fails | Agent, phase, full output JSON, error, retry count, Job name, token usage, cost |
| Tool call | With each recorded step | Tool name, MCP server, input preview (500 chars max), result size, elapsed time |

Blocked tool calls (denied by autonomy policy) are excluded from the audit
trail; the executor records them separately in the step output's
`tool_call_log` field.

History writes never block workflow execution — a failed write is logged and
the workflow continues. ConfigMap-based step outputs remain the runtime
source for `${steps.X.output.response}` variable substitution; the database
is the persistent archive.

## Configuration

Enabled by default. Helm values under `operator`:

```yaml
operator:
  history:
    enabled: true
    storageSize: 1Gi
    storageClass: ""  # empty = cluster default storage class
```

The chart creates a `purko-history` PVC (no owner references — it survives
`helm uninstall` of the operator Deployment) mounted at `/var/lib/purko`.
The database file is `/var/lib/purko/history.db`, opened in WAL mode.

The PVC uses `ReadWriteOnce`, which is sufficient for the single-replica
operator. Multi-replica HA requires `ReadWriteMany` or a future PostgreSQL
backend.

## Retention by License Tier

Old records are cleaned up automatically — once at operator startup, then
every 24 hours. Retention is governed by the license tier:

| Tier | Retention |
|------|-----------|
| Dev mode (no license set) | Unlimited |
| Community (`PURKO_LICENSE=community`) | 7 days |
| Pro | 90 days |
| Enterprise | Unlimited |

Deleting a workflow run cascades to its step executions and tool calls.

## Querying History

See the [Dashboard API reference](../reference/api-endpoints.md#execution-history)
for the full endpoint list.

```bash
# Recent runs in a namespace
curl "http://localhost:8082/api/history/runs?namespace=ai-agents&limit=10" | jq

# A specific run with its steps
curl "http://localhost:8082/api/history/run/deploy-app-abc123" | jq

# Tool call audit trail for a step execution
curl "http://localhost:8082/api/history/step/deploy-app-abc123-build-0/tools" | jq
```

Step execution IDs include the retry count (`<run-id>-<step-name>-<retry>`),
so each retry attempt is archived separately.
