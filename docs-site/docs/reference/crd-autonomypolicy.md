# AgentAutonomyPolicy CRD

**API Version:** `purko.io/v1alpha1`
**Kind:** `AgentAutonomyPolicy`
**Scope:** Namespaced

An AgentAutonomyPolicy defines the cluster-wide Shu-Ha-Ri progression criteria — the thresholds an agent must meet to advance from supervised (Shu) to semi-autonomous (Ha) to fully autonomous (Ri), and the conditions that trigger automatic rollback.

## Example

```yaml
apiVersion: purko.io/v1alpha1
kind: AgentAutonomyPolicy
metadata:
  name: default
  namespace: ai-agents
spec:
  shuHaRi:
    progressionCriteria:
      shuToHa:
        minimumActionsCompleted: 50
        minimumSuccessRate: 0.95
        minimumDaysInLevel: 7
        requiredApprovals: 10
        incidentFreeStreak: 5
      haToRi:
        minimumActionsCompleted: 200
        minimumSuccessRate: 0.98
        minimumDaysInLevel: 30
        incidentFreeStreak: 14
  rollback:
    enabled: true
    triggerConditions:
      successRateBelow: 0.85
      consecutiveFailures: 3
    rollbackLevel: shu
```

## Spec Fields

### AgentAutonomyPolicySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `shuHaRi` | [ShuHaRiPolicySpec](#shuharipolicyspec) | Yes | Shu-Ha-Ri progression criteria |
| `rollback` | [RollbackSpec](#rollbackspec) | No | Automatic rollback configuration |

### ShuHaRiPolicySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `progressionCriteria` | [ProgressionCriteria](#progressioncriteria) | Yes | Criteria for each level transition |

### ProgressionCriteria

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `shuToHa` | [TransitionCriteria](#transitioncriteria) | Yes | Criteria to advance from Shu to Ha |
| `haToRi` | [TransitionCriteria](#transitioncriteria) | Yes | Criteria to advance from Ha to Ri |

### TransitionCriteria

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `minimumActionsCompleted` | int | Yes | Minimum number of actions the agent must have completed at its current level |
| `minimumSuccessRate` | float64 | Yes | Minimum success rate required (0.0–1.0; e.g. `0.95` = 95%) |
| `minimumDaysInLevel` | int | Yes | Minimum number of days the agent must have spent at its current level |
| `requiredApprovals` | int | No | Minimum number of human-approved actions at the current level |
| `incidentFreeStreak` | int | No | Minimum consecutive incident-free days required |

### RollbackSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | Yes | Enable automatic rollback when trigger conditions are met |
| `triggerConditions` | [RollbackTriggers](#rollbacktriggers) | No | Conditions that trigger rollback |
| `rollbackLevel` | string | No | Target level after rollback: `shu` or `ha` (default `shu`) |

### RollbackTriggers

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `successRateBelow` | float64 | No | Trigger rollback when success rate drops below this value (0.0–1.0) |
| `consecutiveFailures` | int | No | Trigger rollback after this many consecutive failures |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `observedGeneration` | int64 | Most recent spec generation reconciled |
| `conditions` | []Condition | Standard Kubernetes conditions |

## Conditions

| Condition | Meaning |
|-----------|---------|
| `Ready` | Policy is valid and active |
| `Error` | Policy has a configuration error |

## How It Works

The controller evaluates each agent's `status.metrics` against the active AgentAutonomyPolicy every reconciliation cycle:

1. An agent at `shu` whose metrics meet `shuToHa` criteria has `status.shuHaRi.readyForPromotion` set to `true`. A human (or automated process) then updates `spec.shuHaRi.level` to `ha`.
2. An agent at `ha` whose metrics meet `haToRi` criteria is similarly flagged for promotion to `ri`.
3. If rollback is enabled and an agent's metrics breach the trigger conditions, the controller automatically demotes the agent to `rollbackLevel`.

## Related Resources

- [Agent CRD](crd-agent.md) — `spec.shuHaRi` and `status.shuHaRi` fields
- [Concepts: Shu-Ha-Ri Graduated Autonomy](../concepts/shu-ha-ri.md)
