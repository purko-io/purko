# Purko

**Purko is an open source, Kubernetes-native platform for building and operating AI agent teams.**

Define AI agents as Kubernetes custom resources, wire them into DAG workflows, connect them to real tools via MCP, and use any LLM provider. Agents are managed through the CLI (`purkoctl`) and operate with configurable autonomy levels.

## Features

- **5 Custom Resources** — Agent, Workflow, MCPServer, LLMProvider, AgentAutonomyPolicy
- **6 Agent Types** — planner, executor, reviewer, router, monitor, retriever
- **DAG Workflows** — parallel execution, conditional branching, retry policies
- **Any LLM Provider** — Anthropic, OpenAI, Vertex AI, Ollama, OpenRouter, or any OpenAI-compatible API
- **MCP Tool Integration** — connect agents to GitHub, Slack, Jira, and any MCP-compatible tool server
- **Cost Tracking** — per-agent, per-workflow cost monitoring
- **CLI Management** — `purkoctl` for agent and workflow operations

## Quick Start

### Install

```bash
helm install purko deploy/helm/
```

### Create an Agent

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: my-writer
  namespace: ai-agents
spec:
  type: executor
  model:
    provider: anthropic
    name: claude-sonnet-4-6
  systemPrompt: |
    You are a content writer. You produce clear,
    well-structured written content.
  autonomyLevel: supervised
  tools:
    - name: github
      type: mcp
  guardrails:
    maxIterations: 20
    costLimit: "$3.00"
```

```bash
kubectl apply -f my-writer.yaml
purkoctl agent list
```

### Create a Workflow

```yaml
apiVersion: purko.io/v1alpha1
kind: Workflow
metadata:
  name: my-pipeline
  namespace: ai-agents
spec:
  steps:
    - name: research
      agentRef: { name: researcher }
      input:
        raw: "Research the topic: AI agent frameworks"
    - name: write
      agentRef: { name: my-writer }
      dependsOn: [research]
      input:
        raw: "Write a summary based on: ${steps.research.output.response}"
```

```bash
kubectl apply -f my-pipeline.yaml
purkoctl workflow trigger my-pipeline
purkoctl workflow get my-pipeline
```

## Agent Types

| Type | Role | Example |
|------|------|---------|
| planner | Strategic thinking, planning | Campaign strategist, architect |
| executor | Producing deliverables | Content writer, code generator |
| reviewer | Quality checks, compliance | Brand reviewer, code reviewer |
| router | Classification, dispatching | Ticket classifier, task router |
| monitor | Watching, alerting, reporting | System monitor, anomaly detector |
| retriever | Searching, fetching context | Knowledge retriever, researcher |

## Pre-Built Examples

- **7 starter agents** — router, researcher, writer, reviewer, analyst, communicator, coordinator
- **13 SDLC agents** — full software development lifecycle
- **6 SRE agents** — incident response, capacity planning, remediation
- **13 workflow templates** — review pipelines, research reports, SDLC automation

## Documentation

Full documentation: [purko-io.github.io/purko](https://purko-io.github.io/purko/)

## Available in Purko Pro

- Web dashboard with visual workflow builder
- Shu-Ha-Ri graduated autonomy (automatic agent trust progression)
- Intent Bar (natural language workflow design)
- Execution history database
- SSO/OIDC authentication

Learn more at [purko.io](https://purko.io)

## License

Apache License 2.0 — see [LICENSE](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, branch naming, and PR process.
