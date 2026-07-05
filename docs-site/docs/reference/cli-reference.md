# CLI Reference

`purkoctl` is the command-line interface for managing Purko agents and workflows. It reads from the current kubeconfig context and communicates directly with the Kubernetes API.

## Installation

```bash
# Download for your platform
curl -Lo purkoctl https://github.com/purko-io/purko/releases/latest/download/purkoctl-$(uname -s)-$(uname -m)
chmod +x purkoctl
mv purkoctl /usr/local/bin/
```

## Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--namespace` | `-n` | Current kubeconfig context namespace | Target namespace for all operations |
| `--output` | `-o` | `table` | Output format: `table` or `json` |
| `--kubeconfig` | | `$KUBECONFIG` or `~/.kube/config` | Path to kubeconfig file |

Global flags must be placed before the subcommand:

```bash
purkoctl -n ai-agents -o json agent list
```

---

## Commands

### purkoctl version

Show the purkoctl client version and the operator version running in the cluster.

**Usage:**

```
purkoctl version
```

**Example:**

```
$ purkoctl version
Client:    v0.3.1
Operator:  v0.3.1 (purko-system/purko-operator-65d9c8b74f)
```

If the cluster is unreachable or the operator is not installed, the operator line shows `not found`.

---

### purkoctl agent

Parent command for agent operations. Always requires a subcommand.

**Usage:**

```
purkoctl agent <subcommand>
```

---

### purkoctl agent list

List all agents in the namespace.

**Usage:**

```
purkoctl agent list [flags]
```

**Output columns (table format):**

| Column | Description |
|--------|-------------|
| `NAME` | Agent name |
| `TYPE` | Agent type (executor, planner, reviewer, etc.) |
| `MODEL` | `provider/model-name` |
| `PHASE` | Current phase (Ready, Pending, Error) |
| `AUTONOMY` | Autonomy level |
| `INVOCATIONS` | Total executions |
| `COST` | Cumulative cost |
| `AGE` | Time since creation |

**Example:**

```
$ purkoctl agent list
NAME                  TYPE       MODEL                      PHASE   AUTONOMY     INVOCATIONS  COST     AGE
code-executor         executor   anthropic/claude-sonnet-4-6  Ready   supervised   142          $1.23    2d5h
requirements-analyst  planner    anthropic/claude-sonnet-4-6  Ready   supervised   89           $0.87    1d12h
security-scanner      reviewer   anthropic/claude-sonnet-4-6  Ready   manual       34           $0.31    18h
```

**JSON output:**

```
$ purkoctl -o json agent list
[{"name":"code-executor","spec":{...},"status":{...}}, ...]
```

---

### purkoctl agent get

Show detailed information for a single agent.

**Usage:**

```
purkoctl agent get <name> [flags]
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Agent name |

**Example:**

```
$ purkoctl agent get code-executor
Name:         code-executor
Namespace:    ai-agents
Type:         executor
Phase:        Ready
Age:          2d5h

Model:
  Provider:     anthropic
  Name:         claude-sonnet-4-6
  Temperature:  0.2

Autonomy:
  Level:        supervised
  Shu-Ha-Ri:    shu
  Ready:        false
  Progress:     142/200 actions, 96.5% success rate, 2/7 days

Tools (5):
  NAME                TYPE
  code-sandbox        function
  get_file_contents   mcp
  search_code         mcp
  push_files          mcp
  create_branch       mcp

Metrics:
  Invocations:  142
  Tokens:       1420500
  Cost:         $1.23
  Avg Latency:  4823ms
  Success:      137 (96.5%)
  Failures:     5

Conditions:
  TYPE    STATUS  REASON  MESSAGE
  Ready   True    Ready   1/1 replicas available
```

---

### purkoctl workflow

Parent command for workflow operations. Always requires a subcommand.

**Usage:**

```
purkoctl workflow <subcommand>
```

---

### purkoctl workflow list

List all workflows in the namespace, sorted by creation time (newest first).

**Usage:**

```
purkoctl workflow list [flags]
```

**Output columns (table format):**

| Column | Description |
|--------|-------------|
| `NAME` | Workflow name |
| `PHASE` | Current phase (Pending, Running, Succeeded, Failed, Cancelled) |
| `STEPS` | Total steps defined |
| `COMPLETED` | Steps that reached Succeeded |
| `FAILED` | Steps that reached Failed |
| `DURATION` | Wall-clock time from start to completion |
| `AGE` | Time since creation |

**Example:**

```
$ purkoctl workflow list
NAME                                    PHASE       STEPS  COMPLETED  FAILED  DURATION  AGE
sdlc-feature-development-run-1a2b3c4d  Succeeded   7      7          0       14m23s    1h
sdlc-feature-development-run-9f8e7d6c  Running     7      3          0       -         12m
sdlc-pr-review-run-4b5c6d7e            Failed      4      2          1       6m11s     3h
```

---

### purkoctl workflow get

Show detailed workflow information including step DAG and conditions.

**Usage:**

```
purkoctl workflow get <name> [flags]
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Workflow name |

**Example:**

```
$ purkoctl workflow get sdlc-feature-development-run-1a2b3c4d
Name:         sdlc-feature-development-run-1a2b3c4d
Namespace:    ai-agents
Phase:        Succeeded
Duration:     14m23s
Parameters:
  repository:    org/my-repo
  featureTicket: PROJ-42

Steps:
  NAME                  AGENT                  PHASE      DURATION  RETRIES  JOB
  analyze-requirements  requirements-analyst   Succeeded  2m10s     0        sdlc-...analyze-abc
  design-solution       architecture-designer  Succeeded  3m45s     0        sdlc-...design-def
  implement-feature     code-generator         Succeeded  5m30s     1        sdlc-...impl-ghi
  run-tests             test-engineer          Succeeded  1m20s     0        sdlc-...tests-jkl
  security-scan         security-scanner       Succeeded  0m58s     0        sdlc-...scan-mno
  code-review           sdlc-code-reviewer     Succeeded  0m48s     0        sdlc-...review-pqr
  chain-pr-review       sdlc-router            Succeeded  0m12s     0        sdlc-...chain-stu

Conditions:
  TYPE      STATUS  REASON     MESSAGE
  Complete  True    Succeeded  All 7 steps succeeded
```

---

### purkoctl workflow trigger

Trigger a workflow execution. For template workflows (those with `spec.trigger`), creates a new workflow instance. For non-template workflows, resets the status to trigger a re-run.

**Usage:**

```
purkoctl workflow trigger <name> [flags]
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Workflow name |

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--param` | `-p` | | Parameter in `key=value` format; may be repeated |

**Example:**

```
$ purkoctl workflow trigger sdlc-feature-development \
  -p repository=org/my-repo \
  -p featureTicket=PROJ-43 \
  -p featureBranch=feature/PROJ-43
Workflow sdlc-feature-development-ab3cd triggered in namespace ai-agents
```

For template workflows, a new workflow instance is created with a random 5-character suffix. For non-template workflows, the existing workflow status is reset.

---

### purkoctl workflow logs

Show logs from a workflow step's Job pod.

**Usage:**

```
purkoctl workflow logs <name> [step] [flags]
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Workflow name |
| `step` | No | Step name. If omitted, uses the currently running or most recently completed step |

**Flags:**

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--follow` | `-f` | `false` | Stream logs live (tail the pod) |

**Example:**

```
$ purkoctl workflow logs sdlc-feature-development-run-1a2b3c4d implement-feature
[09:12:01] Starting executor...
[09:12:02] Received task: Implement the feature in repository org/my-repo
[09:12:03] Tool call: get_file_contents({"path": "src/api/handler.go"})
[09:14:31] OUTPUT:{"response":"Implementation complete. Created 3 files...","_metrics":{"tokens_in":8420,"tokens_out":2103,"cost_usd":0.09}}
```

```bash
# Stream logs live
purkoctl workflow logs my-workflow implement-feature --follow
```

If the pod has been garbage-collected, the command prints: `Logs unavailable — pod has been garbage collected`

---

### purkoctl workflow approve

Approve a step that is pending human review.

**Usage:**

```
purkoctl workflow approve <name> <step>
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Workflow name |
| `step` | Yes | Step name |

Sets the `purko.io/approve-{step}` annotation on the workflow. The controller detects this and allows the step to proceed.

**Example:**

```
$ purkoctl workflow approve sdlc-feature-development-run-1a2b3c4d deploy
Step deploy approved in workflow sdlc-feature-development-run-1a2b3c4d
```

Returns an error if the step is not found or is not in `Pending` phase.

---

### purkoctl workflow deny

Deny a step that is pending human review.

**Usage:**

```
purkoctl workflow deny <name> <step>
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Workflow name |
| `step` | Yes | Step name |

Sets the `purko.io/deny-{step}` annotation. The controller marks the step as `Failed` with error `"Denied by human"`.

**Example:**

```
$ purkoctl workflow deny sdlc-feature-development-run-1a2b3c4d deploy
Step deploy denied in workflow sdlc-feature-development-run-1a2b3c4d
```

Returns an error if the step is not found or is not in `Pending` phase.

---

### purkoctl workflow cancel

Cancel a running or pending workflow.

**Usage:**

```
purkoctl workflow cancel <name>
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Workflow name |

Sets the `purko.io/cancel: "true"` annotation on the workflow. The controller stops dispatching new steps and marks the workflow as `Cancelled`.

Returns an error if the workflow is not in `Running` or `Pending` phase.

**Example:**

```
$ purkoctl workflow cancel sdlc-feature-development-run-1a2b3c4d
Workflow sdlc-feature-development-run-1a2b3c4d cancelled
```

---

### purkoctl workflow rerun

Re-run a completed, failed, or cancelled workflow with the same spec.

**Usage:**

```
purkoctl workflow rerun <name>
```

**Arguments:**

| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Workflow name |

Resets `status.phase`, `status.stepStatuses`, timestamps, and counters. Clears all `purko.io/approve-*`, `purko.io/deny-*`, and `purko.io/cancel` annotations. Sets `purko.io/rerun` to the current timestamp to signal the controller.

Returns an error if the workflow is still `Running` or `Pending` (use `cancel` first).

**Example:**

```
$ purkoctl workflow rerun sdlc-feature-development-run-9f8e7d6c
Workflow sdlc-feature-development-run-9f8e7d6c restarted
```

---

## Output Formats

All list and get commands support `--output json` (`-o json`), which prints the raw Kubernetes resource object(s) as JSON. This is useful for scripting:

```bash
# Get all agent names
purkoctl -o json agent list | jq '.[].metadata.name'

# Get the phase of a workflow
purkoctl -o json workflow get my-workflow | jq '.status.phase'

# Find all failed workflow steps
purkoctl -o json workflow get my-workflow | jq '.status.stepStatuses[] | select(.phase=="Failed")'
```

## Related Resources

- [Dashboard API](api-endpoints.md) — REST API for the same operations
- [Concepts: Agents](../concepts/agents.md)
- [Concepts: Workflows](../concepts/workflows.md)
