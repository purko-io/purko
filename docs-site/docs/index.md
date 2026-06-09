# Purko

**Purko is a platform that lets any company — from law firms to creative agencies — build AI teams that handle their workflows end-to-end, earn trust over time, and cost pennies per task.**

---

## Why Purko?

### Kubernetes-Native
Declare agents and workflows as Kubernetes custom resources. Get RBAC, health checks, scaling, and observability for free.

### Graduated Autonomy
Agents start supervised and earn trust over time through the Shu-Ha-Ri model. If they regress, they're automatically demoted. No leap of faith.

### Industry-Agnostic
From SDLC automation to legal contract review to creative campaigns — Purko adapts to your business processes, not the other way around.

---

## How It Works

```
You declare WHAT (CRDs) → Purko handles HOW (controllers + executor)

Agent      = "who does the work"     (model + tools + personality + guardrails)
Workflow   = "how work flows"        (DAG of agent steps with conditions)
MCPServer  = "what tools exist"      (deploy + discover MCP tool servers)
LLMProvider= "which brain to use"    (model credentials + health)
Autonomy   = "how much to trust"     (Shu-Ha-Ri progression + rollback)
```

---

## Quick Start

1. **Install** — `helm install purko deploy/helm/`
2. **Create an agent** — define a YAML with model, tools, and system prompt
3. **Create a workflow** — wire agents into a DAG pipeline
4. **Watch it run** — `purkoctl workflow get <name>` or open the dashboard

[Get started →](getting-started/installation.md){ .md-button .md-button--primary }

---

## Explore by Use Case

| Industry | What Purko Does | Example |
|----------|----------------|---------|
| [Digital Agency](showcases/digital-agency.md) | Campaign delivery pipeline | Brief → Strategy → Content → Review → Package |
| [Legal & Compliance](showcases/legal-compliance.md) | Contract review pipeline | Upload → Analyze → Flag Risks → Research → Report |
| [Real Estate](showcases/real-estate.md) | Listing-to-close pipeline | Property → Market Analysis → Listing → Matching → Transaction |
| [Data Analytics](showcases/data-analytics.md) | Client analytics pipeline | Quality Check → Anomaly Detection → Report → Insights |
| [Video Production](showcases/video-production.md) | Video production pipeline | Brief → Script → Shot Plan → Post-Production → Distribution |

[See all showcases →](showcases/overview.md){ .md-button }
