# Your First Workflow

A [workflow](../concepts/workflows.md) is a directed acyclic graph (DAG) of steps, where each step delegates a task to an [agent](../concepts/agents.md). Workflows let you compose agents into multi-step pipelines with dependency ordering, conditional branching, and data passing between steps.

This tutorial creates a two-agent analysis pipeline that demonstrates the core workflow primitives: sequential steps, `dependsOn`, and `inputFrom`.

---

## Prerequisites

- Purko installed and the operator running ([Installation](installation.md))
- `kubectl` and `purkoctl` configured
- Port-forward running: `kubectl port-forward -n purko-system deploy/purko-operator 8082:8082`

---

## Step 1 — Create the agents

Workflows reference existing agents by name. Create two agents first:

```yaml
# agents.yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: analyzer
  namespace: ai-agents
spec:
  model:
    provider: anthropic
    name: claude-sonnet-4-20250514
  role: "code-analyzer"
  autonomyLevel: supervised
---
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: reporter
  namespace: ai-agents
spec:
  model:
    provider: openai
    name: gpt-4o-mini
  role: "report-writer"
  autonomyLevel: supervised
```

```bash
kubectl apply -f agents.yaml
```

```
agent.purko.io/analyzer created
agent.purko.io/reporter created
```

Verify both are `Ready` before proceeding:

```bash
kubectl get ag -n ai-agents
```

```
NAME       MODEL                    STATUS   AGE
analyzer   claude-sonnet-4-20250514 Ready    15s
reporter   gpt-4o-mini              Ready    15s
```

---

## Step 2 — Create the workflow

```yaml
# analysis-pipeline.yaml
apiVersion: purko.io/v1alpha1
kind: Workflow
metadata:
  name: analysis-pipeline
  namespace: ai-agents
spec:
  description: "Analyze code, then generate a report"

  steps:
    - name: analyze
      agentRef:
        name: analyzer
      description: "Run code analysis"
      output:
        key: analysis_results
      retryPolicy:
        maxRetries: 2
        backoff: "10s"

    - name: report
      agentRef:
        name: reporter
      description: "Generate report from analysis"
      dependsOn:
        - analyze
      inputFrom:
        - step: analyze
          outputKey: analysis_results

  failureStrategy: failFast
  parallelism: 1
```

```bash
kubectl apply -f analysis-pipeline.yaml
```

```
workflow.purko.io/analysis-pipeline created
```

---

## Field-by-field explanation

### `spec.description`

```yaml
description: "Analyze code, then generate a report"
```

Human-readable description displayed in the dashboard and in `purkoctl workflow list`.

### `spec.steps`

The ordered list of steps. Each step is one node in the DAG. Steps run in dependency order; steps without `dependsOn` can run immediately.

### `steps[].agentRef`

```yaml
agentRef:
  name: analyzer
```

Points to an existing `Agent` CR in the same namespace. The workflow controller verifies the agent exists and is `Ready` before dispatching the step.

To reference an agent in a different namespace:

```yaml
agentRef:
  name: analyzer
  namespace: shared-agents
```

### `steps[].output`

```yaml
output:
  key: analysis_results
```

The key under which the step's output is stored. Subsequent steps reference this key via `inputFrom`. Supported formats: `json` (default), `text`, `binary`.

### `steps[].retryPolicy`

```yaml
retryPolicy:
  maxRetries: 2
  backoff: "10s"
```

If the step fails, the controller retries up to `maxRetries` times, waiting `backoff` between attempts. Set `maxRetries: 0` to disable retries.

### `steps[].dependsOn`

```yaml
dependsOn:
  - analyze
```

Lists steps that must complete successfully before this step begins. The controller builds a dependency graph from these declarations — cycles are rejected at admission time by the validating webhook.

### `steps[].inputFrom`

```yaml
inputFrom:
  - step: analyze
    outputKey: analysis_results
```

Passes the output of a previous step as input to this step. The executor resolves `analysis_results` from the `analyze` step's output and makes it available to the `reporter` agent.

### `spec.failureStrategy`

```yaml
failureStrategy: failFast
```

What to do when a step fails:

| Value | Behavior |
|-------|----------|
| `failFast` | Cancel remaining steps, mark workflow `Failed` |
| `continueOnError` | Continue remaining steps, mark workflow `Failed` at the end |
| `rollback` | Cancel remaining steps, run rollback hooks |

### `spec.parallelism`

```yaml
parallelism: 1
```

Maximum number of steps to run concurrently. Set to a higher number to allow independent steps (those without a shared `dependsOn` path) to run in parallel.

---

## Trigger the workflow

Apply the YAML to create (and queue) the workflow:

```bash
kubectl apply -f analysis-pipeline.yaml
```

If the workflow is already created and you want to trigger a new run:

```bash
purkoctl workflow trigger analysis-pipeline --namespace ai-agents
```

To pass parameters at trigger time:

```bash
purkoctl workflow trigger analysis-pipeline \
  --namespace ai-agents \
  --param repository=myorg/myrepo \
  --param branch=main
```

---

## Monitor execution

Watch workflow status:

```bash
purkoctl workflow get analysis-pipeline
```

```
Name:        analysis-pipeline
Namespace:   ai-agents
Phase:       Running
Steps:       2 total, 1 completed, 0 failed
Started:     2026-04-23T09:05:00Z

Steps:
  analyze    Succeeded   started 09:05:01  completed 09:06:00
  report     Running     started 09:06:01
```

Using `kubectl` directly:

```bash
kubectl get wf -n ai-agents
```

```
NAME                PHASE     STEPS   COMPLETED   AGE
analysis-pipeline   Running   2       1           1m
```

Wait for completion:

```bash
kubectl get wf -n ai-agents
```

```
NAME                PHASE       STEPS   COMPLETED   AGE
analysis-pipeline   Succeeded   2       2           2m
```

Check per-step status with JSONPath:

```bash
kubectl get wf analysis-pipeline -n ai-agents \
  -o jsonpath='{range .status.stepStatuses[*]}{.name}: {.phase} (retries: {.retryCount}){"\n"}{end}'
```

```
analyze: Succeeded (retries: 0)
report: Succeeded (retries: 0)
```

---

## View step outputs

```bash
purkoctl workflow logs analysis-pipeline analyze
```

This streams the output produced by the `analyze` step — the text the `analyzer` agent returned, which was passed as input to the `report` step.

```bash
purkoctl workflow logs analysis-pipeline report
```

!!! tip "Raw JSON output"
    For programmatic use, get step outputs as JSON:

    ```bash
    kubectl get wf analysis-pipeline -n ai-agents \
      -o jsonpath='{.status.stepStatuses[?(@.name=="analyze")].output}' | jq .
    ```

---

## A more realistic example — SDLC pipeline

The example at `examples/workflows/sdlc/01-feature-development.yaml` shows a production workflow with six steps, parameter templates, conditional execution, and step timeouts:

```yaml
apiVersion: purko.io/v1alpha1
kind: Workflow
metadata:
  name: sdlc-feature-development
  namespace: ai-agents
  labels:
    app.kubernetes.io/component: sdlc
    purko.io/workflow-type: development
spec:
  description: "Full SDLC feature development: requirements → design → implement → test → review → deploy"
  parallelism: 2
  failureStrategy: failFast
  parameters:
    repository: "CHANGE_ME/repo-name"
    branch: "main"
    featureTicket: "PROJ-001"
    featureBranch: "feature/PROJ-001"
    objective: "Describe the feature to implement"
  steps:
    - name: analyze-requirements
      agentRef:
        name: requirements-analyst
      input:
        raw: '{"task": "Analyze the feature request: ${parameters.objective}. Repository: ${parameters.repository}, branch: ${parameters.branch}. Identify acceptance criteria, dependencies, complexity, and risks. Produce a structured requirements brief.", "repository": "${parameters.repository}", "ticket": "${parameters.featureTicket}"}'
      timeout:
        timeoutSeconds: 600

    - name: design-solution
      agentRef:
        name: architecture-designer
      dependsOn: [analyze-requirements]
      condition: 'steps.analyze-requirements.output.feasibility != rejected'
      input:
        raw: '{"task": "Design the technical solution for: ${parameters.objective}. Repository: ${parameters.repository}. Use the requirements brief from the previous step. Produce component design, API changes, data model updates, and an implementation plan with file paths.", "repository": "${parameters.repository}"}'
      timeout:
        timeoutSeconds: 900

    - name: implement-feature
      agentRef:
        name: code-generator
      dependsOn: [design-solution]
      input:
        raw: '{"task": "Implement the feature in repository ${parameters.repository}. Create branch ${parameters.featureBranch} from ${parameters.branch}. Follow the architecture design from the previous step. Write clean code following project conventions.", "repository": "${parameters.repository}", "branch": "${parameters.branch}", "featureBranch": "${parameters.featureBranch}"}'
      timeout:
        timeoutSeconds: 1800

    - name: run-tests
      agentRef:
        name: test-engineer
      dependsOn: [implement-feature]
      input:
        raw: '{"task": "Write and execute tests for the feature implemented in ${parameters.repository} on branch ${parameters.featureBranch}. Validate against acceptance criteria. Report coverage on new code.", "repository": "${parameters.repository}", "branch": "${parameters.featureBranch}"}'
      timeout:
        timeoutSeconds: 1200

    - name: security-scan
      agentRef:
        name: security-scanner
      dependsOn: [implement-feature]
      input:
        raw: '{"task": "Scan the code changes in ${parameters.repository} on branch ${parameters.featureBranch} for security vulnerabilities, dependency issues, and anti-patterns.", "repository": "${parameters.repository}", "branch": "${parameters.featureBranch}"}'
      timeout:
        timeoutSeconds: 600

    - name: code-review
      agentRef:
        name: sdlc-code-reviewer
      dependsOn: [run-tests, security-scan]
      input:
        raw: '{"task": "Review the code changes in ${parameters.repository} on branch ${parameters.featureBranch}. Consider test results and security scan findings from previous steps. Evaluate for correctness, quality, and adherence to project standards.", "repository": "${parameters.repository}", "branch": "${parameters.featureBranch}"}'
      timeout:
        timeoutSeconds: 600

    - name: chain-pr-review
      agentRef:
        name: sdlc-router
      dependsOn: [code-review]
      condition: 'steps.code-review.output.verdict != request_changes'
      input:
        raw: '{"task": "Code review passed. Chain to the next SDLC phase by triggering the PR Review pipeline. Use the trigger-workflow tool with workflow=sdlc-pr-review and payload containing repository=${parameters.repository} and branch=${parameters.featureBranch}."}'
      timeout:
        timeoutSeconds: 60
```

Key patterns in this example:

- **`spec.parameters`** — named variables substituted into `input.raw` using `${parameters.name}` syntax
- **`parallelism: 2`** — `run-tests` and `security-scan` both depend on `implement-feature` and run in parallel
- **`condition`** — `design-solution` is skipped if requirements analysis returns `feasibility: rejected`; `chain-pr-review` only runs if code review passes
- **`timeout.timeoutSeconds`** — per-step hard timeout (independent of `retryPolicy`)

---

## Trigger rules and webhooks

Workflows can be triggered by external events. Add a trigger rule to route GitHub push events to this workflow:

```bash
kubectl edit configmap trigger-rules -n ai-agents
```

Add:

```json
{"name": "github-push", "source": "github", "match": {"action": "push"}, "workflow": "analysis-pipeline"}
```

Trigger via webhook:

```bash
curl -X POST http://localhost:8082/api/trigger/ai-agents \
  -H "Content-Type: application/json" \
  -d '{"source": "github", "action": "push", "repository": "myorg/myrepo"}'
```

---

## Clean up

```bash
kubectl delete wf analysis-pipeline -n ai-agents
kubectl delete ag analyzer reporter -n ai-agents
```

---

## Next steps

- [Connect MCP Servers](connect-mcp.md) — give agents real tools (GitHub, PagerDuty, cluster monitoring)
- [Workflows concept page](../concepts/workflows.md) — full API reference including edges, concurrency policies, and schedule triggers
- [SDLC Automation guide](../guides/sdlc-workflow.md) — the complete SDLC pipeline walkthrough
