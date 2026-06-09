# LLM Providers

An **LLMProvider** is a Kubernetes Custom Resource that configures which AI models power your agents. Instead of scattering API keys and model names across individual Agent CRs, you declare a provider once and reference it by name. The controller validates credentials, monitors health, and makes provider status visible to the rest of the platform.

---

## Supported Providers

| Provider type | Description | Auth |
|---------------|-------------|------|
| `anthropic` | Direct Anthropic API — Claude models | API key |
| `vertex-ai` | Claude models via Google Cloud Vertex AI | GCP service account JSON |
| `openai` | OpenAI API — GPT-4o and variants | API key |
| `ollama` | Local Ollama server — any supported model | None |
| `custom` | Any OpenAI-compatible API endpoint | Configurable |

---

## LLMProvider Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Provider type: `vertex-ai`, `anthropic`, `openai`, `ollama`, `custom` |
| `model` | string | Yes | Default model identifier for agents that do not specify one |
| `models[]` | list | No | Catalogue of available models with pricing metadata |
| `models[].name` | string | Yes | Model identifier |
| `models[].displayName` | string | No | Human-readable name for the dashboard |
| `models[].maxTokens` | integer | No | Maximum context length |
| `models[].pricing.inputPerMToken` | float | No | Input cost in USD per million tokens |
| `models[].pricing.outputPerMToken` | float | No | Output cost in USD per million tokens |
| `config` | map | No | Provider-specific configuration (see below) |
| `credentials.secretRef` | string | No | Name of the Kubernetes Secret with credentials |
| `credentials.secretKey` | string | No | Key inside the Secret (default: `api-key`) |
| `healthCheck.enabled` | boolean | No | Periodically verify the provider is reachable |
| `healthCheck.intervalSeconds` | integer | No | Health check interval in seconds (default: 300) |
| `healthCheck.timeoutSeconds` | integer | No | Health check timeout in seconds |
| `fallback.providerRef` | string | No | Name of another LLMProvider to use if this one is unhealthy |
| `default` | boolean | No | Mark this as the cluster-wide default provider |

---

## Provider-Specific Config

### Vertex AI

```yaml
config:
  projectId: my-gcp-project
  region: us-east5
```

The `credentials.secretRef` Secret must contain a GCP service account JSON file under the key specified by `credentials.secretKey` (default: `credentials.json`).

### Ollama

```yaml
config:
  endpoint: http://ollama.local:11434
```

No credentials needed. Ollama must be reachable from within the cluster.

---

## Model Definitions and Cost Tracking

Declaring models under `spec.models[]` serves two purposes:

1. The dashboard shows friendly names and lets users pick models from a dropdown.
2. The executor uses pricing data to track per-invocation and cumulative cost, which surfaces in agent status metrics (`status.metrics.totalCostUSD`).

Approximate pricing reference:

| Model | Input ($/M tokens) | Output ($/M tokens) |
|-------|-------------------|---------------------|
| `claude-sonnet-4-6` | $3.00 | $15.00 |
| `claude-opus-4-6` | $15.00 | $75.00 |
| `claude-haiku-4-5` | $0.80 | $4.00 |
| `gpt-4o` | $2.50 | $10.00 |
| `gpt-4o-mini` | $0.15 | $0.60 |

---

## Health Checking

When `healthCheck.enabled: true`, the LLMProvider controller sends a lightweight probe to the provider API on the configured interval. If the probe fails:

- `status.phase` becomes `Error`
- The `Ready` condition is set to `False`
- If a `fallback.providerRef` is configured, agents that reference this provider automatically route to the fallback

```bash
kubectl get llm -n purko-system
# NAME              TYPE        MODEL               DEFAULT   PHASE   MODELS   AGE
# vertex-ai         vertex-ai   claude-sonnet-4-6   true      Ready   3        5d
# anthropic-direct  anthropic   claude-sonnet-4-6   false     Ready   1        2d
```

---

## Fallback Configuration

Providers form a fallback chain. If the primary provider is unhealthy, traffic routes to the fallback automatically:

```yaml
spec:
  type: anthropic
  fallback:
    providerRef: vertex-ai   # use vertex-ai if anthropic is down
```

Chains of arbitrary length are supported as long as they do not create cycles.

---

## Default Provider

Mark exactly one provider as the cluster default with `spec.default: true`. Agents that do not specify a `credentialsSecretRef` on their `spec.model` automatically use the default provider's credentials.

---

## Example YAML — Vertex AI (recommended for production)

```yaml
apiVersion: purko.io/v1alpha1
kind: LLMProvider
metadata:
  name: vertex-ai
  namespace: purko-system
spec:
  type: vertex-ai
  model: claude-sonnet-4-6
  default: true
  models:
    - name: claude-sonnet-4-6
      displayName: Claude Sonnet 4.6
      maxTokens: 200000
      pricing:
        inputPerMToken: 3.0
        outputPerMToken: 15.0
    - name: claude-opus-4-6
      displayName: Claude Opus 4.6
      maxTokens: 200000
      pricing:
        inputPerMToken: 15.0
        outputPerMToken: 75.0
    - name: claude-haiku-4-5
      displayName: Claude Haiku 4.5
      maxTokens: 200000
      pricing:
        inputPerMToken: 0.8
        outputPerMToken: 4.0
  config:
    projectId: my-gcp-project
    region: us-east5
  credentials:
    secretRef: gcp-credentials
    secretKey: credentials.json
  healthCheck:
    enabled: true
    intervalSeconds: 300
    timeoutSeconds: 10
  fallback:
    providerRef: anthropic-direct
```

### Anthropic direct API

```yaml
apiVersion: purko.io/v1alpha1
kind: LLMProvider
metadata:
  name: anthropic-direct
  namespace: purko-system
spec:
  type: anthropic
  model: claude-sonnet-4-6
  credentials:
    secretRef: anthropic-api-key
    secretKey: api-key
  fallback:
    providerRef: vertex-ai
```

### OpenAI

```yaml
apiVersion: purko.io/v1alpha1
kind: LLMProvider
metadata:
  name: openai
  namespace: purko-system
spec:
  type: openai
  model: gpt-4o
  models:
    - name: gpt-4o
      displayName: GPT-4o
      pricing:
        inputPerMToken: 2.5
        outputPerMToken: 10.0
    - name: gpt-4o-mini
      displayName: GPT-4o Mini
      pricing:
        inputPerMToken: 0.15
        outputPerMToken: 0.6
  credentials:
    secretRef: openai-api-key
    secretKey: api-key
```

### Ollama (local, no auth)

```yaml
apiVersion: purko.io/v1alpha1
kind: LLMProvider
metadata:
  name: local-ollama
  namespace: purko-system
spec:
  type: ollama
  model: llama3
  config:
    endpoint: http://ollama.local:11434
```

!!! tip
    For local development, Ollama is the easiest option — no API key, no cost. Switch to Vertex AI or Anthropic for production where reliability and model capability matter.

!!! warning
    Never put API keys in the LLMProvider spec directly. Always reference a Kubernetes Secret. Secrets can be rotated without changing the LLMProvider CR.

---

## LLMProvider Status

| Field | Description |
|-------|-------------|
| `phase` | `Ready` or `Error` |
| `message` | Human-readable status message |
| `lastHealthCheck` | Timestamp of the most recent health check |
| `availableModels` | Count of models declared in `spec.models[]` |
| `conditions` | Standard Kubernetes conditions including `Ready` and `CredentialsValid` |

---

## See Also

- [LLMProvider CRD Reference](../reference/crd-llmprovider.md)
- [Agents](agents.md) — referencing model providers in `spec.model`
- [Shu-Ha-Ri](shu-ha-ri.md) — autonomy levels that affect which model actions are permitted
