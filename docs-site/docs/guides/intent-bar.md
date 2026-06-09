!!! info "Pro Feature"
    The Intent Bar is part of the Purko Pro web dashboard. Community edition users create workflows via YAML and kubectl. [Learn more →](../community/roadmap.md)

# Intent Bar: Natural Language Workflow Design

The Intent Bar lets you describe a workflow in plain English and have Purko generate the complete YAML for you. Instead of manually writing steps, dependencies, and agent references, you type what you want and the platform figures out how to build it.

## What the Intent Bar Does

You type a description like:

> "Create a campaign delivery pipeline that takes a client brief, creates a strategy, writes content for social, email, and blog in parallel, reviews for brand compliance, and packages the deliverables."

Purko's AI analyzes your description, looks at the agents available on your cluster, and generates a complete workflow with:

- Steps mapped to the right agents
- Dependencies set correctly (parallel where possible, sequential where required)
- Input wiring between steps
- Proper agent type matching (planners for strategy, executors for content, reviewers for compliance)

One click to deploy. That is going from idea to running workflow in seconds.

## Where to Find It

The Intent Bar is the text input field at the top of the Purko dashboard. It is available on every page -- you do not need to navigate to a specific section to use it.

Type your workflow description and press Enter. The generated workflow appears as a preview with a **Deploy** button.

## How It Works

Behind the scenes, the Intent Bar:

1. **Sends your description** to Claude Opus, along with a list of every agent currently registered on your cluster
2. **The LLM analyzes** your intent and maps each task to the most appropriate existing agent based on agent type, tools, and capabilities
3. **Generates a structured workflow** with step names, agent references, and dependency ordering
4. **Returns the result** as a preview showing suggested agents and steps

The system prompt constrains the LLM to use only agents that actually exist on your cluster. It cannot invent agents that are not deployed -- every agent reference in the generated workflow points to a real agent.

!!! tip "The more agents you have, the smarter it gets"
    The Intent Bar works best when you have a library of agents already deployed. It matches your description to available capabilities. If you describe a task that does not match any existing agent, the Intent Bar will tell you which agent types are missing.

## Agent Matching Rules

The Intent Bar follows specific rules when mapping your description to agents:

| Task Description | Matched Agent Type | Why |
|-----------------|-------------------|-----|
| "analyze", "plan", "design", "strategize" | planner | Thinking and planning tasks |
| "write", "create", "generate", "implement" | executor | Content or code production |
| "review", "check", "validate", "audit" | reviewer | Quality and compliance checks |
| "classify", "route", "triage", "dispatch" | router | Work classification and routing |
| "monitor", "watch", "track", "scan" | monitor | Ongoing observation and alerting |
| "search", "find", "retrieve", "lookup" | retriever | Information retrieval |

If multiple agents of the same type exist, the LLM picks the one whose system prompt and tools best match the specific task.

## Example: Campaign Pipeline

**Input:**

> "Take a client brief for Acme Corp, create a campaign strategy, then write social media content, email nurture sequence, and a blog post in parallel. Review everything for brand compliance, revise if needed, and package the final deliverables."

**Generated workflow:**

| Step | Agent | Depends On |
|------|-------|-----------|
| analyze-client-brief | project-planner | -- |
| create-campaign-strategy | campaign-strategist | analyze-client-brief |
| write-social-media-content | content-writer | create-campaign-strategy |
| write-email-content | content-writer | create-campaign-strategy |
| write-blog-content | content-writer | create-campaign-strategy |
| review-brand-compliance | brand-reviewer | write-social-media-content, write-email-content, write-blog-content |
| package-deliverables | campaign-reporter | review-brand-compliance |

The Intent Bar correctly identified:

- The campaign strategist (planner) for strategy work
- The content writer (executor) for all three content types, running in parallel
- The brand reviewer (reviewer) for compliance, waiting for all content to complete
- Proper fan-out/fan-in dependency structure

## Example: Incident Investigation

**Input:**

> "When an alert fires, triage the incident, check pod health, analyze logs for root cause, and generate a postmortem report."

**Generated workflow:**

| Step | Agent | Depends On |
|------|-------|-----------|
| triage-incident | incident-triage-prod | -- |
| check-pod-health | system-monitor | triage-incident |
| analyze-logs | knowledge-retriever | triage-incident |
| generate-postmortem | postmortem-generation-prod | check-pod-health, analyze-logs |

The Intent Bar matched operational agents: triage for classification, monitor for health checks, retriever for log analysis, and a report generator for the postmortem.

## When to Use Intent Bar vs Manual YAML

| Scenario | Use |
|----------|-----|
| Exploring what is possible with your current agents | Intent Bar |
| Quick prototyping of a new workflow | Intent Bar |
| Production workflow with precise parameter control | Manual YAML |
| Workflows with complex conditional branching | Manual YAML |
| Demos and presentations | Intent Bar |
| Integrating with CI/CD or GitOps | Manual YAML |

The Intent Bar is a starting point, not a replacement for manual configuration. Use it to generate a first draft, then refine the YAML for production use. You can always edit the generated workflow before deploying.

!!! warning
    The Intent Bar generates workflows using agents that exist on your cluster. If you have not deployed the agents yet, deploy them first using `kubectl apply` or the dashboard's agent creation form. See [Your First Agent](../getting-started/first-agent.md).

## Tips for Better Results

The Intent Bar works best when you give it clear structure:

**Be specific about sequence.** "First analyze, then write, then review" is better than "analyze, write, and review things."

**Mention parallelism explicitly.** "Write social, email, and blog content in parallel" tells the LLM to set up fan-out dependencies rather than sequential steps.

**Name the task, not the agent.** Say "review for brand compliance" instead of "run the brand-reviewer agent." The Intent Bar matches tasks to agents automatically.

**Include conditions.** "If the review passes, package deliverables; if it fails, revise" triggers conditional branching in the generated workflow.

!!! example "Good vs Poor Descriptions"
    **Good:** "Take a client brief, create a strategy, write social and email content in parallel, review for compliance, and package the deliverables."

    **Poor:** "Do marketing stuff."

    The more structure you provide, the more accurate the generated workflow will be.

## Webhook Integration

The same intent-parsing engine powers Purko's webhook endpoint. External systems can trigger workflow generation by sending a POST request:

```bash
curl -X POST http://localhost:8082/api/intent \
  -H "Content-Type: application/json" \
  -d '{"intent": "Investigate pod failures in namespace production"}'
```

The response includes the suggested agents, steps, and a deployable workflow definition. This enables integration with chat platforms, monitoring systems, or any tool that can send HTTP requests.

## Next Steps

- [How to Design Workflows](building-workflows.md) -- understand workflow structure for manual editing
- [How to Design Agents](building-agents.md) -- build the agent library that powers Intent Bar
- [Your First Workflow](../getting-started/first-workflow.md) -- hands-on quickstart
