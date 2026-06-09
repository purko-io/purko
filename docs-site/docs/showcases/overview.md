# Showcases

Purko showcases are complete, working examples of AI agent teams deployed for specific industries. Each showcase contains four specialized agents, one multi-step workflow, dashboard screenshots, and a walkthrough you can follow.

The goal is to give you something concrete to run, examine, and adapt — not a toy demo, but a production-ready starting point.

## What each showcase includes

| Component | Description |
|---|---|
| 4 agents | Each with a defined role, autonomy level, model, and system prompt |
| 1 workflow | A DAG that chains those agents with parallel steps and conditional logic |
| Screenshots | Dashboard views showing agents, workflow execution, and step outputs |
| Presentation guide | A walkthrough of the business value for stakeholders |

## Showcases

| Showcase | Industry | What the workflow does |
|---|---|---|
| [Digital Agency](digital-agency.md) | Marketing & Creative | Campaign brief → strategy → content (social, email, blog in parallel) → brand review → client package |
| [Legal & Compliance](legal-compliance.md) | Legal Services | Contract → clause extraction → regulatory check + due diligence (parallel) → compliance report |
| [Real Estate](real-estate.md) | Property & Brokerage | New listing → market analysis → listing copy + buyer matching (parallel) → transaction coordination |
| [Data Analytics](data-analytics.md) | Analytics Consulting | Client data → quality check → anomaly detection → report generation → strategic insights |
| [Video Production](video-production.md) | Content & Media | Creative brief → script → shot planning + distribution strategy (parallel) → post-production brief |

## Deploy a showcase

Each showcase deploys with a single kubectl command:

```bash
kubectl apply \
  -f docs/showcases/<name>/agents/ \
  -f docs/showcases/<name>/workflows/
```

For example, to deploy the digital agency showcase:

```bash
kubectl apply \
  -f docs/showcases/digital-agency/agents/ \
  -f docs/showcases/digital-agency/workflows/
```

This creates the agents and the workflow in the `ai-agents` namespace. The workflow can then be triggered from the dashboard or via the webhook endpoint.

!!! tip "Namespace"
    All showcases deploy into the `ai-agents` namespace by default. Create it first if it does not exist:
    ```bash
    kubectl create namespace ai-agents
    ```

## Trigger a workflow

Once deployed, trigger the workflow from the dashboard by opening the workflow, clicking **Run**, and filling in the parameters. You can also trigger it via the API:

```bash
curl -X POST http://localhost:8082/api/trigger/ai-agents/<workflow-name> \
  -H 'Content-Type: application/json' \
  -d '{"clientName": "Acme Corp", "briefContent": "..."}'
```

## Adapt a showcase to your business

The showcases are starting points. To adapt one:

1. Edit the system prompts in the agent YAML files to match your brand voice, industry terminology, and output format requirements.
2. Change the model (provider and name) per agent based on your cost and capability needs.
3. Adjust autonomy levels — start with `supervised` and promote agents as they earn trust.
4. Add or remove workflow steps to match your actual process.

See [Industry Templates](../guides/industry-templates.md) for a step-by-step guide to adapting a showcase into a production deployment.
