# Changelog

## v0.2.0 (April 2026)

### Features

- **purkoctl CLI** — 11 commands across agent and workflow subcommands: `agent list`, `agent get`, `workflow list`, `workflow get`, `workflow trigger`, `workflow logs`, `workflow approve`, `workflow deny`, `workflow cancel`, `workflow rerun`, `version`
- **Helm chart hardening** — security contexts on operator deployment, PodDisruptionBudget (`minAvailable: 1`), ClusterIP Service for the operator, NOTES.txt post-install instructions, Helm test pod for operator health check
- **Industry showcases** — 5 end-to-end showcase implementations: digital agency campaign delivery, legal contract review, real estate listing generation, data analytics reporting, video production pipeline
- **Documentation site** — MkDocs Material site with 31 pages across Getting Started, Concepts, Guides, Reference, Architecture, Showcases, and Community sections

### Fixes

- **RBAC** — added `watch` verb for pods in operator ClusterRole; without it the operator could not stream pod logs
- **Showcase agents** — fixed LLM provider from `vertex-ai` to `anthropic`; the executor only matched `anthropic` and `openai`
- **Workflow variable substitution** — step inputs must use `${steps.X.output.response}` format; the regex in the workflow controller requires a key after `.output.`

---

## v0.1.0 (March 2026)

### Features

- **5 CRDs** — `Agent`, `Workflow`, `MCPServer`, `LLMProvider`, `AgentAutonomyPolicy`
- **Operator with 5 controllers** — Agent, Workflow, Autonomy, MCPServer, LLMProvider; each controller watches its CRD and reconciles the desired state
- **Embedded dashboard** — intent bar for natural language workflow design, live SSE event stream, agent and workflow status views
- **Python executor** — ReAct loop with MCP tool routing; supports `mcp`, `function`, `api`, and `builtin` tool types
- **Shu-Ha-Ri graduated autonomy** — three autonomy levels (Shu: supervised, Ha: semi-autonomous, Ri: fully autonomous) enforced by the AgentAutonomyPolicy controller
- **MCP server examples** — GitHub MCP, Lumino (Kubernetes/Tekton), PagerDuty MCP
- **Agent library** — 6 archetype agents (`project-planner`, `code-executor`, `code-reviewer`, `knowledge-retriever`, `system-monitor`, `github-agent`), 13 SDLC agents, 6 SRE agents
- **Workflow library** — 9 SDLC workflows, 3 operational workflows (incident response, deployment monitoring, on-call briefing)
- **Validation webhooks** — admission webhooks for Agent and Workflow CRDs enforce required fields and autonomy policy constraints
- **Helm chart** — full chart with CRDs, ClusterRole, namespaces, configmaps, and configurable values for operator image, executor image, LLM provider, and agent namespace
