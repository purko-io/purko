# Memory

By default, each workflow step is stateless — the executor starts fresh with no knowledge of previous runs. **Memory** gives agents a way to carry context across invocations, enabling learning, continuity, and semantic search over past findings.

Memory is configured in `spec.memory` on the Agent CR.

---

## Memory Types

| Type | Storage | Scope | Best For |
|------|---------|-------|---------|
| `buffer` | In-memory (none) | Per invocation | Short tasks, stateless analysis |
| `summary` | Kubernetes ConfigMap | Across invocations | Learning from past runs, trend awareness |
| `vector` | Persistent Volume Claim (PVC) | Persistent | Semantic search over history |
| `none` | None | None | Deliberately stateless agents |

---

## buffer — in-memory, per invocation

The default mode. The executor starts each step with an empty context window. Nothing is saved after the step completes. The conversation history within a single step is still held in the model's context, but it is discarded when the pod exits.

**When to use**: agents that produce self-contained outputs and do not need to reference past work — a code generator, a one-shot report writer, a data transformer.

```yaml
spec:
  memory:
    type: buffer
```

---

## summary — ConfigMap across invocations

At the end of each step, the executor builds a one-line summary of the task and its result, and stores it in the ConfigMap `agent-memory-<agent-name>` in the agent's namespace. On the next invocation, that summary is prepended to the system prompt as `[Previous execution context]`.

**When to use**: agents that should remember what they did last time — a monitor that tracks whether an anomaly is new or recurring, an analyzer that builds understanding of a codebase over time.

```yaml
spec:
  memory:
    type: summary
    maxEntries: 10        # keep the last 10 summaries
    ttl: 7d               # discard entries older than 7 days
```

The summary format:

```
[Previous execution context]
[workflow-name/step-name] Task: <first 200 chars of input> | Result: <first 500 chars of output>

[Current task follows]
```

---

## vector — PVC-based persistent memory

At the end of each step, the executor writes a timestamped `.txt` file to a PVC-mounted directory at `/var/run/agent-memory`. On the next invocation, it reads the most recent files (newest first) and injects them into the context up to the `maxContextTokens` budget.

The built-in `vector-search` function tool (type: `function`) lets the model search the memory store by keyword, scoring entries by term frequency and returning the top matches.

**When to use**: agents that build up institutional knowledge over many runs — a security scanner that tracks vulnerability trends, a monitor that builds a baseline of normal behaviour, a retriever that accumulates domain knowledge.

```yaml
spec:
  memory:
    type: vector
    maxContextTokens: 8000
    persistentStorage:
      enabled: true
      volumeClaimRef: my-agent-memory   # PVC must exist in the same namespace
  tools:
    - name: vector-search
      type: function
```

Create the PVC before applying the agent:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-agent-memory
  namespace: ai-agents
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
```

!!! warning
    The executor reads memory files sequentially, newest first, and stops when `maxContextTokens` is reached. If the PVC accumulates a large number of files, the oldest entries may never be read. Set a `ttl` or periodically prune old files.

---

## none — stateless

Explicitly disables all memory loading and saving. Equivalent to `buffer` but communicates intent clearly — this agent is designed to be stateless.

```yaml
spec:
  memory:
    type: none
```

---

## Memory Spec Fields

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `buffer`, `summary`, `vector`, or `none` |
| `backend` | string | Storage backend hint (informational) |
| `ttl` | string | Time-to-live for memory entries: `1h`, `24h`, `7d` |
| `maxEntries` | integer | Maximum number of entries to retain (oldest are dropped) |
| `maxContextTokens` | integer | Token budget for context injected at the start of each step |
| `retentionPolicy` | string | How entries are evicted when `maxEntries` is reached |
| `persistentStorage.enabled` | boolean | Mount a PVC for vector memory |
| `persistentStorage.volumeClaimRef` | string | Name of the PVC in the same namespace |

---

## How Memory is Loaded and Saved

### Load (start of each step)

```
1. Executor starts in a Kubernetes Job pod
2. Reads MEMORY_TYPE environment variable (set by the workflow controller)
3. If summary: reads AGENT_MEMORY env var (loaded from ConfigMap by the controller)
4. If vector: reads .txt files from /var/run/agent-memory (PVC mount)
5. Prepends context to the system prompt as "[Previous execution context]"
```

### Save (end of each step)

```
1. Executor completes the ReAct loop and has a final output
2. If summary: builds "Task: <input> | Result: <output>" string, returns it in OUTPUT JSON
   The workflow controller writes it to the ConfigMap on the next reconcile
3. If vector: writes timestamped .txt file to /var/run/agent-memory (PVC mount)
4. If buffer or none: nothing is saved
```

---

## Decision Guide

| Scenario | Recommended Type |
|----------|-----------------|
| Report generator — each run is independent | `buffer` or `none` |
| Monitor — should remember if an alert was seen before | `summary` |
| Code reviewer — should track patterns across PRs over weeks | `summary` |
| Knowledge retriever — searches a growing corpus of findings | `vector` |
| Data pipeline step — intentionally stateless | `none` |
| Security scanner — builds a baseline of normal state | `vector` |

!!! tip
    Start with `buffer`. Add `summary` when you notice an agent repeatedly asking questions it has already answered. Move to `vector` only when the amount of history exceeds what a ConfigMap entry can usefully hold.

---

## Example — agent with vector memory and search

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: knowledge-retriever
  namespace: ai-agents
spec:
  type: retriever
  role: knowledge-retriever
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.1
  autonomyLevel: supervised
  systemPrompt: |
    You are a knowledge retrieval agent. Use vector-search to find
    relevant past findings before answering. Always cite the
    source entry from memory when referencing past data.
  memory:
    type: vector
    maxContextTokens: 8000
    persistentStorage:
      enabled: true
      volumeClaimRef: knowledge-retriever-memory
  tools:
    - name: vector-search
      type: function
    - name: list_resources
      type: mcp
```

---

## See Also

- [Agents](agents.md) — full `spec.memory` field reference
- [Tool Types](tool-types.md) — the `vector-search` function tool
