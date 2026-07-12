# Legal & Compliance starter team

Four agents (contract-analyst, regulatory-monitor, due-diligence-researcher,
compliance-reporter) and the contract-review workflow, from the purko
legal-compliance showcase.

Apply: `kubectl apply -f . -n ai-agents`

Resources appear immediately in Mission Control. To actually RUN the workflow
the cluster needs an LLM provider + API key — the skill offers this as a
separate, explicit step (never assumed).

## Prerequisites

### LLM provider

All four agents declare:

```yaml
model:
  provider: anthropic
  name: claude-sonnet-4-6
```

The cluster must have an Anthropic `LLMProvider` resource (or equivalent
purko provider CRD) configured with a valid API key before the agents can
execute. The YAML files themselves contain no `secretRef` or `apiKey` fields
— the key is expected to be wired at the provider level, not embedded in the
manifest.

### MCP tool: github

Every agent lists:

```yaml
tools:
  - name: github
    type: mcp
```

A `github` MCP tool must be registered in the cluster (typically via a
`MCPServer` or `Tool` CR in `ai-agents`). Without it agents will start but
any step that calls the tool will fail. Register a stub or real GitHub MCP
server before running the workflow end-to-end.

## Server-side validation

Dry-run validation (`kubectl apply --dry-run=server`) requires a live purko
cluster. No cluster was available at bundle time — server-side validation is
deferred to the Task 13 e2e test pass.
