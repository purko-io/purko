# Shu-Ha-Ri Autonomy

**Shu-Ha-Ri** (守破離) is a concept from Japanese martial arts describing three stages of mastery: obey, break, and transcend. Purko uses it as a graduated autonomy model — agents earn the right to act independently by demonstrating reliability over time.

Most AI agent platforms offer a single autonomy setting: the agent either has full access or no access. This all-or-nothing approach is too risky for real business operations. Shu-Ha-Ri solves this by making autonomy a measurable, progressive property that can be granted, revoked, and audited.

---

## The Three Levels

| Level | Japanese | Meaning | Autonomy | Typical Use |
|-------|----------|---------|----------|-------------|
| **Shu** | 守 | Obey | Human approves every action | New agent, untested behaviour |
| **Ha** | 破 | Break | Human notified, can intervene | Agent with a track record |
| **Ri** | 離 | Transcend | Agent acts independently | Trusted, mature agent |

### Shu — human approves every action

An agent at the Shu level maps to `autonomyLevel: restricted`. It can only call read-only tools. Write operations (creating pull requests, modifying files, triggering alerts) are blocked by the executor before they reach the model.

This is the starting point for every new agent.

### Ha — human notified but can intervene

An agent at the Ha level maps to `autonomyLevel: supervised`. All tools are available. The platform emits an event for every consequential action, giving a human the option to review or roll back, but execution does not pause waiting for approval.

### Ri — agent acts independently

An agent at the Ri level maps to `autonomyLevel: full`. The agent operates within its configured guardrails (cost limits, blast radius) without any human-in-the-loop step. Suitable for mature agents with proven reliability in production.

---

## Real-World Analogy

Think of it like onboarding a new employee:

- **Shu** — new hire. Every significant decision goes through a manager for sign-off.
- **Ha** — trusted employee. They handle their work independently but keep the manager informed and accept overrides.
- **Ri** — autonomous team lead. They own their domain and are trusted to make decisions without supervision.

The same intuition applies: you would not give a new hire the ability to deploy to production on day one. You let them prove themselves first.

---

## Promotion Criteria

The autonomy controller evaluates every agent every 5 minutes against the `AgentAutonomyPolicy`. Promotion from Shu to Ha, and from Ha to Ri, requires meeting all three criteria simultaneously:

| Criterion | Shu → Ha (default) | Ha → Ri (default) |
|-----------|--------------------|-------------------|
| `minimumActionsCompleted` | 100 | 500 |
| `minimumSuccessRate` | 95% | 99% |
| `minimumDaysInLevel` | 14 days | 30 days |
| `incidentFreeStreak` | — | 20 consecutive successes |

When `requiredApprovals: 0` (the default), promotion is automatic. Set a non-zero value to require a human to approve the transition before it takes effect.

---

## Rollback Conditions

An agent is demoted if it degrades below the threshold configured in `spec.rollback`:

| Condition | Default |
|-----------|---------|
| `successRateBelow` | 90% (measured over at least 10 actions) |
| `consecutiveFailures` | 3 failures in a row |

When either condition is met, the controller immediately demotes the agent to the level specified in `rollback.rollbackLevel` (default: `shu`) and resets the promotion clock.

---

## AgentAutonomyPolicy CRD

A single `AgentAutonomyPolicy` CR in a namespace governs all agents in that namespace.

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
        minimumActionsCompleted: 100
        minimumSuccessRate: 0.95
        minimumDaysInLevel: 14
        requiredApprovals: 0       # 0 = auto-promote
      haToRi:
        minimumActionsCompleted: 500
        minimumSuccessRate: 0.99
        minimumDaysInLevel: 30
        incidentFreeStreak: 20
        requiredApprovals: 0
  rollback:
    enabled: true
    triggerConditions:
      successRateBelow: 0.90
      consecutiveFailures: 3
    rollbackLevel: shu             # demote all the way to shu on failure
```

### AgentAutonomyPolicy spec fields

| Field | Type | Description |
|-------|------|-------------|
| `shuHaRi.progressionCriteria.shuToHa.minimumActionsCompleted` | integer | Actions the agent must complete before Shu → Ha promotion |
| `shuHaRi.progressionCriteria.shuToHa.minimumSuccessRate` | float (0–1) | Required success rate (e.g., `0.95` = 95%) |
| `shuHaRi.progressionCriteria.shuToHa.minimumDaysInLevel` | integer | Days the agent must remain at Shu before being eligible |
| `shuHaRi.progressionCriteria.shuToHa.requiredApprovals` | integer | Human approvals needed (0 = auto) |
| `shuHaRi.progressionCriteria.haToRi.minimumActionsCompleted` | integer | Actions required for Ha → Ri |
| `shuHaRi.progressionCriteria.haToRi.minimumSuccessRate` | float (0–1) | Required success rate for Ha → Ri |
| `shuHaRi.progressionCriteria.haToRi.minimumDaysInLevel` | integer | Days required at Ha |
| `shuHaRi.progressionCriteria.haToRi.incidentFreeStreak` | integer | Consecutive successes required |
| `shuHaRi.progressionCriteria.haToRi.requiredApprovals` | integer | Human approvals needed |
| `rollback.enabled` | boolean | Enable automatic demotion |
| `rollback.triggerConditions.successRateBelow` | float (0–1) | Demote if success rate drops below this |
| `rollback.triggerConditions.consecutiveFailures` | integer | Demote after this many consecutive failures |
| `rollback.rollbackLevel` | string | Level to demote to: `shu` or `ha` |

---

## Checking Agent Progression

```bash
# View current level and progress
kubectl get agent my-agent -n ai-agents \
  -o jsonpath='{.status.shuHaRi}' | jq .

# View metrics
kubectl get agent my-agent -n ai-agents \
  -o jsonpath='{.status.metrics}' | jq .

# View the ShuHaRiProgression condition
kubectl get agent my-agent -n ai-agents \
  -o jsonpath='{.status.conditions[?(@.type=="ShuHaRiProgression")].message}'
```

Example output:

```json
{
  "currentLevel": "shu",
  "readyForPromotion": false,
  "promotionProgress": {
    "actionsCompleted": 43,
    "actionsRequired": 100,
    "successRate": 0.97,
    "daysInLevel": 6,
    "daysRequired": 14
  }
}
```

---

## Why This Matters

Most platforms treat agent autonomy as a boolean. You either trust the agent or you do not. This creates two bad outcomes:

- **Too cautious**: agents require human approval for every action and provide no real automation benefit.
- **Too permissive**: agents are given full access immediately and there is no safety net when they misbehave.

Shu-Ha-Ri provides a third path: agents accumulate a track record, earn trust incrementally, and are automatically constrained again if that trust is violated. The promotion and rollback are data-driven, auditable, and reversible.

!!! tip
    Create one `AgentAutonomyPolicy` named `default` in each namespace where you run agents. The controller will apply it to all agents in that namespace automatically.

!!! warning
    If no `AgentAutonomyPolicy` exists in a namespace, agents remain at their declared `spec.autonomyLevel` indefinitely — there is no automatic promotion or rollback.

---

## See Also

- [AgentAutonomyPolicy CRD Reference](../reference/crd-autonomypolicy.md)
- [Agents](agents.md) — `spec.shuHaRi` and `spec.autonomyLevel` fields
