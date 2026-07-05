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

### 1. Install Purko

Install the CRDs, then the chart:

```bash
kubectl apply -k "https://github.com/purko-io/purko/crds?ref=main"
```

**Option A: Helm chart from OCI registry (recommended)**
```bash
helm install purko oci://ghcr.io/purko-io/purko \
  --namespace purko-system --create-namespace
```

**Option B: From source**
```bash
git clone https://github.com/purko-io/purko.git
cd purko
kubectl apply -f crds/
helm install purko deploy/helm/ --namespace purko-system --create-namespace
```

Running on minikube or a production cluster? See the
[installation guide](https://purko-io.github.io/purko/getting-started/installation/)
and the [production deployment guide](https://purko-io.github.io/purko/getting-started/production/).

Verify the operator is running:
```bash
kubectl get pods -n purko-system
```

### 2. Configure an LLM Provider

Purko needs an LLM provider to power your agents. Choose one and apply:

**Anthropic (direct API):**
```bash
kubectl create secret generic anthropic-key \
  -n purko-system --from-literal=api-key=YOUR_API_KEY

kubectl apply -f examples/llm-providers/anthropic-direct.yaml
```

**OpenAI:**
```bash
kubectl create secret generic openai-key \
  -n purko-system --from-literal=api-key=YOUR_API_KEY

cat <<EOF | kubectl apply -f -
apiVersion: purko.io/v1alpha1
kind: LLMProvider
metadata:
  name: openai
  namespace: purko-system
spec:
  type: openai
  apiFormat: openai
  model: gpt-4o
  credentials:
    secretRef: openai-key
    secretKey: api-key
  default: true
EOF
```

**Ollama (free, local):**
```bash
kubectl apply -f examples/llm-providers/ollama.yaml
```

See `examples/llm-providers/` for more options (Vertex AI, OpenRouter).

### 3. Create an Agent

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

### 4. Create a Workflow

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
purkoctl workflow logs my-pipeline write
```

### 5. Install purkoctl CLI

```bash
go install github.com/purko-io/purko/cmd/purkoctl@latest
```

Or build from source:
```bash
go build -o bin/purkoctl ./cmd/purkoctl/
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
- **17 workflow templates** — review pipelines, research reports, SDLC automation

Deploy the starter agents:
```bash
kubectl apply -f examples/agents/starter/
kubectl apply -f examples/workflows/starter/
```

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
