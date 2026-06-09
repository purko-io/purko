# How to Design Agents for Your Business

Every business has people who play specific roles -- a strategist who plans, a writer who produces content, a reviewer who checks quality, a coordinator who routes work. Purko agents mirror these roles. This guide walks you through designing agents that fit your business, step by step.

## Start from Your Team, Not Technology

Before writing any YAML, answer one question: **what roles exist in the process you want to automate?**

Grab a whiteboard (or a napkin) and list every person involved in a business process. For a marketing campaign, that might be: strategist, copywriter, brand reviewer, performance analyst. For a legal review: contract analyst, compliance researcher, report writer.

Each of those roles becomes an agent.

!!! tip "The napkin test"
    If you can describe what someone does in two sentences, you can build an agent for it. "Sarah reads client briefs and produces campaign strategies with target audience analysis, key messages, and a content calendar." That is already 80% of an agent's system prompt.

## Map Roles to Agent Types

Purko has six agent types. Every business role maps to one of them:

| Business Role | Agent Type | Why |
|--------------|-----------|-----|
| Strategist, planner, analyst | `planner` | Thinks and plans; does not produce final output |
| Writer, coder, designer | `executor` | Produces deliverables |
| QA, compliance, editor | `reviewer` | Checks quality and gives structured verdicts |
| Dispatcher, classifier, triage | `router` | Routes work to the right agent |
| Watcher, alerter, reporter | `monitor` | Tracks metrics and flags issues |
| Researcher, search, knowledge | `retriever` | Finds and retrieves information |

The type determines how the platform treats the agent's output. A `reviewer` produces structured verdicts (approve/revise/reject) that drive conditional branching in workflows. A `router` classifies and dispatches work. Pick the type that matches the role's **function**, not their job title.

## Write the System Prompt

The system prompt is the most important field in an agent definition. It is the difference between a useful agent and a generic chatbot.

A good system prompt has four parts:

1. **Role** -- who the agent is
2. **Instructions** -- what to do and what to produce
3. **Output format** -- structured sections, tables, or JSON
4. **Constraints** -- what NOT to do, quality standards

Here is a vague prompt versus a specific one:

!!! example "Before: vague prompt"
    ```
    You are a helpful marketing assistant. Help with marketing tasks.
    ```

!!! example "After: specific prompt"
    ```
    You are a senior digital marketing strategist at a creative agency.
    You analyze client briefs and produce campaign strategies.

    When given a brief, produce:
    1. Target audience analysis with personas
    2. Key messages (3-5) with emotional hooks
    3. Channel recommendation (social, email, blog, paid)
    4. Content calendar (what pieces, when, which channel)
    5. KPIs and success metrics with specific targets

    Always consider the client's brand voice, competitive landscape,
    and current market trends. Be specific -- no generic advice.
    ```

The specific prompt tells the agent exactly what output format to produce, what quality bar to meet, and what to avoid. The vague prompt produces vague results.

## Choose Temperature

Temperature controls how creative or deterministic the agent's output is:

| Temperature | Best For | Examples |
|-------------|----------|----------|
| 0.1 - 0.2 | Analytical, precise work | Legal analysis, data reports, compliance, security scanning |
| 0.3 - 0.5 | Balanced reasoning | Strategic planning, research, architecture design |
| 0.7 - 0.9 | Creative work | Copywriting, brainstorming, content creation, scripts |

!!! warning
    Higher temperature means more variation between runs. For content that must be consistent (contracts, compliance reports), keep it low. For content that benefits from freshness (social posts, creative briefs), turn it up.

## Choose Autonomy Level

Autonomy determines how much human oversight the agent requires:

| Level | When to Use | Example |
|-------|------------|---------|
| `supervised` | Output goes to clients, or has legal/financial impact | Content writer, contract analyst, deployment manager |
| `restricted` | Internal work with guardrails | Strategist, researcher, code reviewer |
| `full` | Reporting, monitoring, low-risk analysis | Performance reporter, regulatory monitor, log analyzer |

Start with `supervised` for any agent whose output leaves your organization. Move to `restricted` once you trust the output quality. The [Shu-Ha-Ri system](../concepts/shu-ha-ri.md) can promote agents automatically based on their track record.

## Select Tools

Give agents only the tools they need -- the principle of least privilege.

A content writer needs access to your CMS and brand guides, not your Kubernetes cluster. A deployment manager needs cluster access, not your social media accounts.

```yaml
tools:
  - name: search-knowledge    # Can search internal knowledge base
    type: builtin
  - name: github              # Can access GitHub repos
    type: mcp
```

Tools come from [MCP servers](../concepts/mcp-servers.md) you have connected to Purko. Each tool checkbox in the dashboard is an individual capability you grant or withhold.

## Set Guardrails

Guardrails prevent runaway costs and infinite loops:

```yaml
guardrails:
  maxIterations: 15      # How many reasoning steps before stopping
  costLimit: "$5.00"     # Maximum LLM spend per execution
```

Rules of thumb for `maxIterations`:

| Task Complexity | Max Iterations |
|----------------|---------------|
| Simple classification, routing | 5 - 10 |
| Analysis, review, reporting | 10 - 20 |
| Complex research, code generation | 25 - 50 |

The `costLimit` acts as a circuit breaker. If an agent burns through its budget, the execution stops rather than running up a surprise bill.

## Configure Memory

Memory determines whether the agent learns from past executions:

| Memory Type | When to Use |
|-------------|-------------|
| `summary` | Roles that improve with context -- strategists, analysts, researchers |
| `buffer` | Roles that need recent conversation history -- writers, coordinators |
| `none` | Stateless roles -- routers, simple classifiers |

For more details, see [Memory](../concepts/memory.md).

## Worked Example: Customer Support Ticket Responder

Let us build an agent from scratch. Your support team spends 3 hours per day writing initial responses to customer tickets. You want an agent to draft those responses.

**Step 1: Identify the role.** The agent drafts responses to support tickets. It produces text that a human reviews before sending. This is an `executor`.

**Step 2: Write the system prompt.**

```yaml
systemPrompt: |
  You are a Tier 1 support agent at a B2B SaaS company.
  You draft initial responses to customer support tickets.

  For each ticket:
  1. Acknowledge the customer's issue with empathy
  2. Classify the problem (bug, feature request, how-to, billing)
  3. If it matches a known solution in the knowledge base, provide steps
  4. If it does not match, escalate with a summary for Tier 2
  5. Always include a follow-up timeline ("We will update you within X hours")

  Tone: professional, empathetic, concise. No jargon.
  Never promise fixes you cannot verify. Never share internal details.
  Maximum response length: 200 words.
```

**Step 3: Choose settings.**

- Temperature: `0.3` -- friendly but consistent
- Autonomy: `supervised` -- responses go to customers, so a human reviews every draft
- Memory: `summary` -- learns from past tickets to improve responses over time

**Step 4: Select tools and guardrails.**

```yaml
tools:
  - name: search-knowledge      # Search the knowledge base for solutions
    type: builtin
  - name: list_issues            # Look up related tickets
    type: mcp
guardrails:
  maxIterations: 10
  costLimit: "$1.00"
```

**Step 5: Assemble the full agent.**

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: ticket-responder
  namespace: ai-agents
spec:
  type: executor
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.3
  role: "Tier 1 Support Agent"
  systemPrompt: |
    You are a Tier 1 support agent at a B2B SaaS company.
    You draft initial responses to customer support tickets.

    For each ticket:
    1. Acknowledge the customer's issue with empathy
    2. Classify the problem (bug, feature request, how-to, billing)
    3. If it matches a known solution in the knowledge base, provide steps
    4. If it does not match, escalate with a summary for Tier 2
    5. Always include a follow-up timeline

    Tone: professional, empathetic, concise. No jargon.
    Never promise fixes you cannot verify. Never share internal details.
    Maximum response length: 200 words.
  autonomyLevel: supervised
  tools:
    - name: search-knowledge
      type: builtin
    - name: list_issues
      type: mcp
  memory:
    type: summary
  guardrails:
    maxIterations: 10
    costLimit: "$1.00"
```

Apply it:

```bash
kubectl apply -f ticket-responder.yaml
```

The agent appears in the dashboard immediately. From there, you can add it to a [workflow](building-workflows.md) that triggers on new tickets, pairs it with a reviewer agent, and routes escalations to your human team.

## Next Steps

- [How to Design Workflows](building-workflows.md) -- connect agents into automated processes
- [Adapting Purko to Any Industry](industry-templates.md) -- see how this pattern applies across industries
- [Agent CRD Reference](../reference/crd-agent.md) -- full specification for all agent fields
