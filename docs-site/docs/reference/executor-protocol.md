# Executor Protocol

Any container image can serve as a Purko executor. The operator creates a Kubernetes Job for each workflow step, injects configuration via environment variables, and reads the result from stdout. This page specifies the complete contract.

Set a custom executor on an agent with `spec.runtime.image`:

```yaml
spec:
  runtime:
    image: my-org/my-executor:latest
    config:
      verbose: "true"       # becomes EXECUTOR_VERBOSE=true
      chain_type: "stuff"   # becomes EXECUTOR_CHAIN_TYPE=stuff
```

## Input Contract

The operator passes all inputs as environment variables. Your executor reads what it needs and ignores the rest.

### Environment Variables

| Name | Source | Required | Example |
|------|--------|----------|---------|
| `STEP_INPUT` | Workflow step `spec.input` | Yes | `{"task":"Review PR #42","repository":"org/repo"}` |
| `STEP_NAME` | Workflow step name | Yes | `code-review` |
| `WORKFLOW_NAME` | Workflow metadata name | Yes | `sdlc-feature-development` |
| `MODEL_PROVIDER` | Agent `spec.model.provider` | Yes | `anthropic` |
| `MODEL_NAME` | Agent `spec.model.name` | Yes | `claude-sonnet-4-6` |
| `MODEL_API_KEY` | Resolved from `credentialsSecretRef` | No | `sk-ant-...` |
| `MODEL_TEMPERATURE` | Agent `spec.model.temperature` | No | `0.2` |
| `AGENT_SYSTEM_PROMPT` | Agent `spec.systemPrompt` | No | `You are a code reviewer...` |
| `AGENT_TOOLS` | Agent `spec.tools` serialized as JSON | No | `[{"name":"list_pods","type":"mcp"}]` |
| `MCP_SERVERS` | Active MCPServer endpoints | No | `[{"name":"github","url":"http://...","token":"..."}]` |
| `AUTONOMY_LEVEL` | Agent `spec.autonomyLevel` | No | `supervised` |
| `MAX_TOOL_CALLS` | From `guardrails.maxIterations` | No | `20` |
| `COST_LIMIT_USD` | From `guardrails.costLimitUSD` | No | `5.00` |
| `CONTENT_FILTERS` | From `guardrails.contentFilters` | No | `["pii","secrets"]` |
| `MEMORY_TYPE` | Agent `spec.memory.type` | No | `buffer` |
| `EXECUTOR_*` | Agent `spec.runtime.config` keys | No | `EXECUTOR_VERBOSE=true` |
| `ANTHROPIC_VERTEX_PROJECT_ID` | Vertex AI config | No | `my-gcp-project` |
| `CLOUD_ML_REGION` | Vertex AI config | No | `us-east5` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Mounted from GCP Secret | No | `/var/run/secrets/gcp/key.json` |
| `TRACEPARENT` | OpenTelemetry trace context | No | `00-4bf9...` |

`EXECUTOR_*` variables come from `spec.runtime.config`. Every key is uppercased and prefixed with `EXECUTOR_`. For example, `config.chain_type: stuff` becomes `EXECUTOR_CHAIN_TYPE=stuff`.

## Output Contract

Print exactly one line to stdout matching the `OUTPUT:` prefix followed by a JSON object:

```
OUTPUT:{"response":"your output text","_metrics":{"tokens_in":100,"tokens_out":500,"cost_usd":0.01},"_memory_update":"summary text"}
```

### OUTPUT JSON Fields

| Field | Required | Description |
|-------|----------|-------------|
| `response` | Yes | The agent's text output (string) |
| `_metrics.tokens_in` | No | Input tokens consumed |
| `_metrics.tokens_out` | No | Output tokens consumed |
| `_metrics.cost_usd` | No | Estimated cost in USD |
| `_metrics.autonomy` | No | Autonomy level used during execution |
| `_memory_update` | No | Summary text persisted for subsequent executions |

Any additional keys in the JSON are preserved as step output and are accessible to downstream steps via `inputFrom`:

```yaml
# In a downstream step:
inputFrom:
  - step: code-review
    outputKey: verdict
```

## Exit Codes

| Exit Code | Meaning |
|-----------|---------|
| `0` | Success — controller reads the `OUTPUT:` line from stdout |
| Non-zero | Failure — controller captures the last 20 lines of logs as the error message |

When a step exits non-zero and `retryPolicy` is configured, the controller retries up to `maxRetries` times before marking the step as `Failed`.

## Volume Mounts

| Mount Path | When Mounted | Purpose |
|------------|-------------|---------|
| `/var/run/secrets/gcp/` | Vertex AI provider is configured | GCP service account JSON; set `GOOGLE_APPLICATION_CREDENTIALS` to point here |
| `/var/run/agent-memory/` | `spec.memory.type: vector` on the agent | PersistentVolumeClaim for vector memory storage |

## Tool Routing

The `AGENT_TOOLS` environment variable contains the serialized tool list. The executor is responsible for connecting to the tools it needs:

- **`type: mcp`** — Connect to the MCP server URL provided in `MCP_SERVERS`. The URL and bearer token are injected automatically.
- **`type: function`** — Built-in tool in the executor image (e.g. `code-sandbox` in `purko-executor:codeact`).
- **`type: http`** — Call the endpoint defined in `spec.tools[].endpoint`.

## Building a Minimal Executor (bash)

```bash
#!/bin/bash
# health-check-executor.sh

NAMESPACE=$(echo $STEP_INPUT | python3 -c "import sys,json; print(json.load(sys.stdin).get('namespace','default'))")
RESULT=$(kubectl get pods -n $NAMESPACE -o json | python3 -c "
import sys,json
pods = json.load(sys.stdin)['items']
print(json.dumps({
  'total': len(pods),
  'running': len([p for p in pods if p['status']['phase']=='Running']),
  'failed': len([p for p in pods if p['status']['phase']=='Failed'])
}))
")

echo "OUTPUT:{\"response\":\"Health check for $NAMESPACE: $RESULT\",\"_metrics\":{}}"
```

```dockerfile
FROM bitnami/kubectl:latest
COPY health-check-executor.sh /app/
RUN chmod +x /app/health-check-executor.sh
ENTRYPOINT ["/app/health-check-executor.sh"]
```

## Building an LLM Executor (Python / LangChain)

```python
#!/usr/bin/env python3
import os, json
from langchain_anthropic import ChatAnthropic
from langchain_core.messages import HumanMessage, SystemMessage

step_input = json.loads(os.environ.get('STEP_INPUT', '{}'))
model_name  = os.environ.get('MODEL_NAME', 'claude-sonnet-4-6')
system_prompt = os.environ.get('AGENT_SYSTEM_PROMPT', '')
task = step_input.get('task', str(step_input))

llm = ChatAnthropic(model=model_name)
messages = []
if system_prompt:
    messages.append(SystemMessage(content=system_prompt))
messages.append(HumanMessage(content=task))

result = llm.invoke(messages)
output = {
    "response": result.content,
    "_metrics": {
        "tokens_in":  result.usage_metadata.get("input_tokens", 0),
        "tokens_out": result.usage_metadata.get("output_tokens", 0),
    },
}
print(f"OUTPUT:{json.dumps(output)}")
```

```dockerfile
FROM python:3.12-slim
RUN pip install langchain langchain-anthropic
COPY langchain_executor.py /app/
ENTRYPOINT ["python", "/app/langchain_executor.py"]
```

## Extending the Default Executor

Add packages and custom tools on top of the default image:

```dockerfile
FROM purko-executor:latest
RUN pip install pandas numpy scikit-learn
COPY my_custom_tools.py /opt/app-root/tools/
```

The default executor auto-loads tool modules from `/opt/app-root/tools/`. Each module must export a `TOOLS` list:

```python
# /opt/app-root/tools/my_analyzer.py
TOOLS = [
    {
        'name': 'analyze_dataframe',
        'description': 'Load and analyze a CSV/JSON dataset',
        'input_schema': {
            'type': 'object',
            'properties': {
                'data': {'type': 'string', 'description': 'JSON data to analyze'},
                'analysis': {'type': 'string', 'description': 'What to analyze'}
            },
            'required': ['data']
        },
        'handler': lambda args: do_analysis(args)
    }
]
```

## Related Resources

- [Agent CRD](crd-agent.md) — `spec.runtime` and `spec.tools`
- [Guide: Custom Executors](../guides/custom-executor.md)
- [Concepts: Agents](../concepts/agents.md)
