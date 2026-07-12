# Agent Authoring Guide

> **Judgment layer.** This guide is generated from the purko showcase library
> and CRD types at release time. It gives the skill the *why* — archetypes,
> model-tier guidance, guardrail semantics. The *what exists on your cluster*
> (real LLMProvider names, registered MCP servers) comes from live queries.

---

## Agent CRD Shape

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: <name>               # [a-zA-Z0-9_.-]{1,63}
  namespace: <namespace>
spec:
  # Required
  model:
    provider: <LLMProvider CR name>   # matches .metadata.name of an LLMProvider on the cluster
    name: <model name>                # e.g. claude-sonnet-4-6, gpt-4o
  role: <short role description>
  systemPrompt: |
    <multi-line prompt>

  # Optional — defaults shown
  type: ""                   # planner | executor | reviewer | router | monitor | retriever
  autonomyLevel: ""          # restricted | supervised | full  (maps to Shu / Ha / Ri)

  guardrails:                # free-form RawExtension; common keys:
    costLimitUSD: 5.00       # stop spending above this per run
    maxIterations: 20        # stop after N LLM calls
    humanApprovalRequired: false  # true gates this agent's step until approved

  memory:                    # optional
    type: buffer             # buffer | summary | vector | none  (legacy)
    # OR (preferred, Spec 34):
    behavior: session        # off | session | persistent
    scope: agent             # agent | group | namespace
    maxContextTokens: 2048   # cap on recalled context injected into prompt

  tools:                     # optional list
    - name: <tool name>
      type: mcp              # mcp | builtin
    - name: search-knowledge
      type: builtin

  skills:                    # optional, max 8 refs
    - name: <Skill CR name>
```

### Required fields

| Field | Meaning |
|-------|---------|
| `spec.model.provider` | Name of an **LLMProvider CR** on the cluster — this is a CR name, not a provider type string. The live layer supplies real values. |
| `spec.model.name` | The model identifier string the provider understands (e.g. `claude-sonnet-4-6`). |
| `spec.role` | One-line human label. Appears in the dashboard and in the agent's self-description. |
| `spec.systemPrompt` | The standing instruction given to the model at every invocation. |

### Optional fields of note

| Field | Default | Notes |
|-------|---------|-------|
| `spec.type` | `""` | Enum: `planner executor reviewer router monitor retriever`. Webhook AG-001 rejects unknown values. `monitor` agents are limited to `replicas ≤ 1` (AG-005). `retriever` agents must have memory enabled (AG-010). |
| `spec.autonomyLevel` | `""` | `restricted` / `supervised` / `full`. Maps to Shu/Ha/Ri (see below). Informs dashboard display but does NOT by itself gate steps — `guardrails.humanApprovalRequired: true` does the actual gating. |
| `spec.guardrails.humanApprovalRequired` | `false` | When `true`, the step that references this agent pauses and waits for a human to set the annotation `purko.io/approve-<step>=true` on the Workflow (via dashboard or kubectl). Gate-wait time is excluded from the workflow timeout. |
| `spec.guardrails.costLimitUSD` | unset | Billing guardrail per run (number, e.g. `5.00`). AG-007 warns when this field is **present and ≤ 0** — an absent cost limit does NOT trigger AG-007. **Note:** showcase templates use `costLimit: "$3.00"` (string) which the controller does not read. When generating, always use `costLimitUSD: <number>`. |
| `spec.guardrails.maxIterations` | unset | Hard cap on LLM calls per invocation. |
| `spec.memory.type` | unset | Legacy field: `buffer` (session-scoped), `summary` (compressed), `vector` (persistent, requires PVC), `none`. If `behavior` is also set, `behavior` wins at runtime. |
| `spec.memory.behavior` | unset | Preferred (Spec 34): `off`, `session`, `persistent`. |
| `spec.tools` | `[]` | References registered MCPServer CRs by name (type `mcp`) or built-in tools by name (type `builtin`). The live layer shows what's actually available. |
| `spec.skills` | `[]` | Up to 8 Skill CR names. Skills are passive instruction packages injected into the prompt. Webhook AG-013 rejects refs to non-existent Skills. |
| `spec.model.temperature` | unset | Range 0.0–2.0 (webhook enforced). |
| `spec.model.maxTokens` | unset | Range 1–2,000,000 (webhook enforced). |

---

## Archetype Table

These archetypes describe role patterns found across the showcase library. Use them to pick the right `spec.type` and `autonomyLevel`.

> **Type vs. role:** The `spec.type` column is the AG-001 enum value accepted by the webhook. The archetype name (Planner, Researcher, etc.) is a *role* label for human clarity — the two are independent. A researcher-role agent may have `spec.type: planner`.

| Archetype *(role)* | `spec.type` | Role pattern | Autonomy | Memory | Notes |
|--------------------|-------------|-------------|----------|--------|-------|
| **Planner** | `planner` | Reads a brief, produces a structured plan or strategy | `restricted` | `summary` or `session` | Entry point of most workflows. Temperature 0.3–0.7. |
| **Executor** | `executor` | Produces the primary artifact (content, code, report) | `supervised` | `buffer` | Temperature 0.8–0.9 for creative work; 0.3 for structured output. |
| **Reviewer** | `reviewer` | Quality-gates the executor's output; returns structured verdict | `restricted` | none | Should set `humanApprovalRequired: true` when review triggers a publish or irreversible action. Temperature ≤ 0.3 for consistency. |
| **Researcher** | `retriever` | Gathers external facts, due diligence, market data | `restricted` | `summary` or `persistent` | **No showcase agent uses `spec.type: retriever`.** Researcher-role agents in the showcases (e.g. `due-diligence-researcher`) are typed `planner`. `retriever` is a valid AG-001 value, but requires memory enabled (AG-010). Often has MCP tools for external lookup. |
| **Monitor** | `monitor` | Runs continuously; watches data, regulations, metrics | `full` | `summary` | One replica max (AG-005). Calls home via tool when threshold crossed. Temperature ≤ 0.1. |
| **Router** | `router` | Scores, ranks, matches, or dispatches between options | `full` | `summary` | No artifact production — pure decision. Temperature 0.1–0.3. |

### Showcase examples by archetype

| Archetype *(role)* | `spec.type` | Industry | Agent name |
|--------------------|-------------|----------|-----------|
| Planner | `planner` | Legal | `compliance-reporter`, `due-diligence-researcher` |
| Planner | `planner` | Digital Agency | `campaign-strategist` |
| Planner | `planner` | Real Estate | `transaction-coordinator` |
| Planner | `planner` | Video Production | `post-production-coordinator`, `shot-planner`, `distribution-strategist` |
| Planner | `planner` | Data Analytics | `insight-synthesizer` |
| Executor | `executor` | Digital Agency | `content-writer` |
| Executor | `executor` | Real Estate | `listing-writer` |
| Executor | `executor` | Video Production | `script-writer` |
| Executor | `executor` | Data Analytics | `report-generator` |
| Reviewer | `reviewer` | Legal | `contract-analyst` |
| Reviewer | `reviewer` | Digital Agency | `brand-reviewer` |
| Researcher | `planner` | Legal | `due-diligence-researcher` *(researcher role, typed planner)* |
| Monitor | `monitor` | Legal | `regulatory-monitor` |
| Monitor | `monitor` | Data Analytics | `anomaly-detector`, `data-quality-analyst` |
| Monitor | `monitor` | Real Estate | `market-analyst` |
| Monitor | `monitor` | Digital Agency | `campaign-reporter` |
| Router | `router` | Real Estate | `client-matcher` |
| Retriever | `retriever` | — | *(none — showcase type distribution: 8 planner, 5 monitor, 4 executor, 2 reviewer, 1 router, 0 retriever)* |

---

## Model-Tier Guidance

**The `spec.model.provider` field must match the `.metadata.name` of an LLMProvider CR on the cluster** — not a type like `anthropic` or `openai`. The live layer (cluster query) gives you the actual provider names. What follows is guidance on *which capability tier to request* for each role.

| Role / archetype | Capability tier needed | Showcase examples |
|-----------------|----------------------|-------------------|
| Planner, Reviewer, Researcher | High reasoning, long context | `claude-sonnet-4-6` in all showcases |
| Executor (creative) | Strong generation, nuanced instruction-following | Same tier — temperature does the differentiation |
| Monitor | Cheap, fast, low-temperature inference | Can down-tier to a smaller model if the cluster offers one |
| Router | Fast, structured JSON output | Smallest capable model; temperature ≤ 0.3 |

All showcase agents use `claude-sonnet-4-6` via an `anthropic` LLMProvider CR. On a real cluster, substitute the provider name and model available — the skill's live query flow resolves this.

---

## Autonomy Levels (Shu / Ha / Ri)

| `autonomyLevel` | Shu/Ha/Ri | Meaning | When to use |
|----------------|-----------|---------|-------------|
| `restricted` | Shu | Every significant action requires confirmation; follows rules precisely | New or high-risk agents; reviewers; researchers that touch sensitive data |
| `supervised` | Ha | Acts independently on routine tasks; flags edge cases for human review | Executors producing reviewable artifacts; mid-workflow agents |
| `full` | Ri | Fully autonomous; self-directs based on mastered principles | Monitors that must act in real-time; routers with well-bounded decision space |

`autonomyLevel` is informational in the dashboard and affects ShuHaRi progression tracking. It does **not** block execution by itself. To actually gate a step for human approval, set `guardrails.humanApprovalRequired: true` on the agent.

---

## When to Set `humanApprovalRequired`

Set `guardrails.humanApprovalRequired: true` when:

- The agent's output triggers an **irreversible or externally visible action** (publishing content, sending emails, committing code, closing a transaction).
- The agent is a **reviewer** and the workflow continues only if the review passes — a human must confirm before the next step proceeds.
- The agent operates in a **high-risk domain** (legal, financial, compliance) where incorrect automated output has liability consequences.
- The agent's `autonomyLevel` is `restricted` and the step is novel or high-stakes.

When `humanApprovalRequired: true` is set, the workflow step pauses and emits a gate event. A human approves via:
- The purko dashboard (Mission Control → workflow → Approve)
- `kubectl annotate workflow/<name> purko.io/approve-<step>=true`

Gate-wait time is **excluded from the workflow timeout** (Spec 39). The step resumes immediately upon approval.

**Showcase examples where you'd add this gate:**
- `contract-analyst` → before `compile-report` reaches the client
- `brand-reviewer` → before `revise-content` is sent back for a final pass
- `compliance-reporter` → before the report is delivered externally

---

## Webhook Validation Rules (What Gets Rejected)

| Code | Rule |
|------|------|
| AG-001 | `spec.type` must be one of `planner executor reviewer router monitor retriever` (or empty) |
| AG-004 | Tool names in `spec.tools` must be unique |
| AG-005 | `monitor` agents: `replicas ≤ 1` |
| AG-007 | Warning (not rejection) when `spec.guardrails.costLimitUSD` is **present and ≤ 0**. An absent cost limit does NOT trigger this warning. |
| AG-010 | `retriever` agents must have `spec.memory.type` (vector/buffer/summary) or `spec.memory.behavior` (session/persistent) |
| AG-011 | `spec.runtime.image` must be in the cluster's `security.imageAllowlist` (if configured) |
| AG-012 | `spec.skills` max 8 entries |
| AG-013 | Every `spec.skills[].name` must exist as a Skill CR in the same namespace |
| — | `spec.model.temperature` must be 0.0–2.0 |
| — | `spec.model.maxTokens` must be 1–2,000,000 |
| — | `spec.memory.scope=group` requires the `app.kubernetes.io/component` label |
