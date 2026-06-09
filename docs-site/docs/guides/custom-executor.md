# Building Custom Executors

By default, Purko runs agents using its built-in executor -- a Python-based ReAct loop that handles LLM calls, tool use, and output formatting. But any container image can serve as a Purko executor if it follows a simple protocol: read environment variables, do your work, print one line of JSON to stdout.

## The Executor Protocol

The contract between Purko and an executor is:

```
  Environment Variables (in) --> Your Container --> OUTPUT JSON (stdout)
```

1. The Purko operator creates a Kubernetes Job with your container image
2. It passes all task context as environment variables
3. Your container runs, does its work (call LLMs, run scripts, query APIs)
4. It prints exactly one line starting with `OUTPUT:` followed by JSON
5. Exit code 0 means success; non-zero means failure

That is the entire protocol. Everything else is optional.

## Input: Environment Variables

Your executor receives these environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `STEP_INPUT` | Yes | JSON string containing the task input |
| `STEP_NAME` | Yes | Name of the workflow step |
| `WORKFLOW_NAME` | Yes | Name of the parent workflow |
| `MODEL_PROVIDER` | Yes | LLM provider (`anthropic`, `openai`) |
| `MODEL_NAME` | Yes | Model identifier (`claude-sonnet-4-6`, `gpt-4o`) |
| `MODEL_API_KEY` | No | Direct API key (if not using Vertex AI) |
| `MODEL_TEMPERATURE` | No | Sampling temperature (default: 0.2) |
| `AGENT_SYSTEM_PROMPT` | No | System prompt text |
| `AGENT_TOOLS` | No | JSON array of tool specifications |
| `MCP_SERVERS` | No | JSON array of MCP server configurations |
| `AUTONOMY_LEVEL` | No | `supervised`, `restricted`, or `full` |
| `MAX_TOOL_CALLS` | No | Maximum reasoning iterations (default: 20) |
| `COST_LIMIT_USD` | No | Cost budget in USD (0 = no limit) |
| `MEMORY_TYPE` | No | `buffer`, `summary`, `vector`, or `none` |
| `EXECUTOR_*` | No | Custom config from `spec.runtime.config` |

Environment variables prefixed with `EXECUTOR_` come from the agent's `spec.runtime.config` map. If you set `config.verbose: "true"`, your executor receives `EXECUTOR_VERBOSE=true`.

## Output: The OUTPUT Line

Print exactly one line to stdout:

```
OUTPUT:{"response":"your output text","_metrics":{"tokens_in":100,"tokens_out":500,"cost_usd":0.01}}
```

| Field | Required | Description |
|-------|----------|-------------|
| `response` | Yes | The agent's text response |
| `_metrics.tokens_in` | No | Input tokens consumed |
| `_metrics.tokens_out` | No | Output tokens consumed |
| `_metrics.cost_usd` | No | Estimated cost in USD |
| `_memory_update` | No | Summary text to persist for the next execution |

Any additional keys in the JSON are preserved as step output and available to downstream steps via `${steps.<name>.output.<key>}`.

### Exit Codes

- **0** -- success. The controller reads the `OUTPUT:` line.
- **Non-zero** -- failure. The controller reads the last 20 lines of logs as the error message.

## Building a Custom Executor

### Minimal Example: Bash Health Check

A simple executor that checks pod health -- no LLM needed:

```bash
#!/bin/bash
# health-check-executor.sh

NAMESPACE=$(echo $STEP_INPUT | python3 -c \
  "import sys,json; print(json.load(sys.stdin).get('namespace','default'))")

RESULT=$(kubectl get pods -n $NAMESPACE -o json | python3 -c "
import sys, json
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

### LangChain Executor

Use any LLM framework. Here is a LangChain executor:

```python
#!/usr/bin/env python3
"""LangChain executor for Purko."""
import os, json

step_input = json.loads(os.environ.get('STEP_INPUT', '{}'))
model_name = os.environ.get('MODEL_NAME', 'claude-sonnet-4-6')
system_prompt = os.environ.get('AGENT_SYSTEM_PROMPT', '')
task = step_input.get('task', str(step_input))

from langchain_anthropic import ChatAnthropic
from langchain_core.messages import HumanMessage, SystemMessage

llm = ChatAnthropic(model=model_name)
messages = []
if system_prompt:
    messages.append(SystemMessage(content=system_prompt))
messages.append(HumanMessage(content=task))

result = llm.invoke(messages)

output = {
    "response": result.content,
    "_metrics": {
        "tokens_in": result.usage_metadata.get("input_tokens", 0),
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

The same approach works for CrewAI, AutoGen, or any other framework -- read environment variables, do your work, print the `OUTPUT:` line.

## Using a Custom Executor

Set `spec.runtime.image` on your agent to use your custom image:

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: my-langchain-agent
spec:
  model:
    provider: anthropic
    name: claude-sonnet-4-6
  runtime:
    image: my-registry/langchain-executor:latest
    config:
      verbose: "true"           # becomes EXECUTOR_VERBOSE=true
      chain_type: "stuff"       # becomes EXECUTOR_CHAIN_TYPE=stuff
  tools:
    - name: list_pods_in_namespace
      type: mcp
```

The operator creates Jobs using your image and passes all standard environment variables. Your executor reads what it needs and ignores the rest.

## Extending the Default Executor

If you only need to add Python packages or custom tools, build on top of the default image:

```dockerfile
FROM purko-executor:latest
RUN pip install pandas numpy scikit-learn
COPY my_custom_tools.py /opt/app-root/tools/
```

The default executor auto-loads Python tools from `/opt/app-root/tools/`:

```python
# /opt/app-root/tools/data_analyzer.py
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

def do_analysis(args):
    import pandas as pd
    df = pd.read_json(args['data'])
    return f"Rows: {len(df)}, Columns: {list(df.columns)}"
```

Your custom tools appear alongside the MCP tools -- the agent can call them during its ReAct loop.

## Testing Locally

Test your executor without deploying to Kubernetes:

```bash
# Set the minimum required environment variables
export STEP_INPUT='{"task": "Check pod health in namespace default"}'
export STEP_NAME="health-check"
export WORKFLOW_NAME="test"
export MODEL_PROVIDER="anthropic"
export MODEL_NAME="claude-sonnet-4-6"

# Run your executor
docker run --rm \
  -e STEP_INPUT="$STEP_INPUT" \
  -e STEP_NAME="$STEP_NAME" \
  -e WORKFLOW_NAME="$WORKFLOW_NAME" \
  -e MODEL_PROVIDER="$MODEL_PROVIDER" \
  -e MODEL_NAME="$MODEL_NAME" \
  my-registry/my-executor:latest
```

The output should be a single line starting with `OUTPUT:` followed by valid JSON. If the exit code is 0 and the JSON parses correctly, your executor is ready.

!!! warning
    When testing LLM-based executors locally, you need to provide authentication (API keys or GCP credentials). The Purko operator handles this automatically in-cluster via Kubernetes secrets and service accounts.

## Volumes

The operator optionally mounts these volumes:

| Path | Mounted When | Purpose |
|------|-------------|---------|
| `/var/run/secrets/gcp/` | Vertex AI is configured | GCP service account credentials |
| `/var/run/agent-memory/` | `memory.type=vector` | Persistent volume for vector memory |

## Next Steps

- [Executor Protocol Reference](../reference/executor-protocol.md) -- complete protocol specification
- [Agent CRD Reference](../reference/crd-agent.md) -- full `spec.runtime` configuration
- [How to Design Agents](building-agents.md) -- design agents that use custom executors
