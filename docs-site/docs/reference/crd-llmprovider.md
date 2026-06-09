# LLMProvider CRD

**API Version:** `purko.io/v1alpha1`
**Kind:** `LLMProvider`
**Scope:** Namespaced

An LLMProvider is a Kubernetes resource that registers an LLM backend — its endpoint, credentials, available models, health check, and fallback — making it available to agents by name.

## Example

```yaml
apiVersion: purko.io/v1alpha1
kind: LLMProvider
metadata:
  name: anthropic-prod
  namespace: ai-agents
spec:
  type: anthropic
  model: claude-sonnet-4-6
  default: true
  credentials:
    secretRef: anthropic-api-key
    secretKey: api-key
  healthCheck:
    enabled: true
    intervalSeconds: 60
    timeoutSeconds: 10
  models:
    - name: claude-sonnet-4-6
      displayName: Claude Sonnet 4.6
      maxTokens: 200000
      pricing:
        inputPerMToken: 3.00
        outputPerMToken: 15.00
    - name: claude-opus-4-5
      displayName: Claude Opus 4.5
      maxTokens: 200000
      pricing:
        inputPerMToken: 15.00
        outputPerMToken: 75.00
  fallback:
    providerRef: openai-backup
```

```yaml
# Vertex AI example
apiVersion: purko.io/v1alpha1
kind: LLMProvider
metadata:
  name: vertex-ai
  namespace: ai-agents
spec:
  type: vertex-ai
  model: claude-sonnet-4-6
  config:
    project: my-gcp-project
    region: us-east5
  credentials:
    secretRef: gcp-sa-key
    secretKey: service-account.json
```

## Spec Fields

### LLMProviderSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Provider type: `anthropic`, `openai`, `vertex-ai`, `ollama`, `custom` |
| `model` | string | Yes | Default model identifier used when agents do not specify a model |
| `models` | [][ModelDefinition](#modeldefinition) | No | Catalog of available models with metadata and pricing |
| `config` | map[string]string | No | Provider-specific configuration (e.g. `project`, `region` for Vertex AI) |
| `credentials` | [CredentialSpec](#credentialspec) | No | Secret reference for API credentials |
| `healthCheck` | [HealthCheckSpec](#healthcheckspec) | No | Periodic health check configuration |
| `fallback` | [FallbackSpec](#fallbackspec) | No | Fallback provider when this provider is unhealthy |
| `default` | bool | No | Mark this as the default provider for agents that do not specify one |

### ModelDefinition

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Model identifier (must match the value passed to the provider API) |
| `displayName` | string | No | Human-readable model name for the dashboard |
| `maxTokens` | int | No | Maximum context window in tokens |
| `pricing` | [ModelPricing](#modelpricing) | No | Per-million-token pricing for cost tracking |

### ModelPricing

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `inputPerMToken` | float64 | Yes | Cost per million input tokens in USD |
| `outputPerMToken` | float64 | Yes | Cost per million output tokens in USD |

### CredentialSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `secretRef` | string | Yes | Name of the Kubernetes Secret |
| `secretKey` | string | No | Key within the Secret (default `api-key`) |

### HealthCheckSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | Yes | Enable periodic health checks |
| `intervalSeconds` | int | No | Interval between health checks in seconds (default 60) |
| `timeoutSeconds` | int | No | Health check request timeout in seconds (default 10) |

### FallbackSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `providerRef` | string | Yes | Name of the LLMProvider to use when this provider is unhealthy |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Provider phase: `Ready`, `Degraded`, `Error` |
| `message` | string | Human-readable status or error message |
| `lastHealthCheck` | timestamp | Timestamp of the most recent health check |
| `availableModels` | int | Number of models in the catalog |
| `conditions` | []Condition | Standard Kubernetes conditions |

## Conditions

| Condition | Meaning |
|-----------|---------|
| `Ready` | Provider is reachable and credentials are valid |
| `Degraded` | Provider is reachable but returning errors intermittently |
| `Error` | Provider is unreachable or credentials are invalid |

## Related Resources

- [Agent CRD](crd-agent.md) — agents reference providers via `spec.model.provider`
- [Concepts: LLM Providers](../concepts/llm-providers.md)
