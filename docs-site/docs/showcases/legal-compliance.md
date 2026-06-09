# Legal & Compliance

Contract intake to client-ready compliance report — clause extraction, regulatory check, due diligence, and final analysis in a single automated pipeline.

## Business Context

Legal and compliance firms handle high volumes of contracts, regulatory filings, and due diligence requests where precision and defensibility are non-negotiable. A missed clause or an untracked regulatory change can create significant liability. At the same time, routine first-pass review work consumes expensive attorney and analyst time that could be spent on judgment calls only humans can make.

## The Agents

| Agent | Type | Autonomy | Role |
|---|---|---|---|
| contract-analyst | reviewer | supervised | Extracts key terms, obligation matrix, and risk flags from contracts |
| regulatory-monitor | monitor | full | Tracks regulatory changes 24/7 and checks contracts against current requirements |
| due-diligence-researcher | planner | restricted | Investigates counterparties — corporate structure, litigation history, sanctions |
| compliance-reporter | planner | restricted | Compiles all findings into an executive-ready compliance report |

The autonomy levels match the risk profile of each role. The contract-analyst produces analysis that influences legal decisions, so every output requires attorney sign-off. The regulatory-monitor produces informational summaries, so it runs independently.

## The Workflow

The `contract-review` workflow runs four steps to take a contract from intake to a complete compliance report:

1. **analyze-contract** — the contract-analyst reads the document and extracts: executive summary, key terms table, obligation matrix, risk flags (Critical/Warning/Info), and deviations from your standard template.
2. **check-regulatory** and **research-counterparty** — run in parallel once the initial analysis is complete. The regulatory-monitor checks the contract against current regulations for the jurisdiction. The due-diligence-researcher investigates the counterparty.
3. **compile-report** — the compliance-reporter takes all three inputs and produces an executive-ready report with an overall risk rating and remediation recommendations.

A contract review that takes a junior associate 4-6 hours produces a structured first-pass in minutes. The senior attorney still reviews and approves, but they review a structured analysis rather than reading 40 pages from scratch.

## Screenshots


The agents list shows all four legal agents with their types, autonomy levels, and current status.


The contract-analyst detail shows supervised autonomy, low temperature (0.2) for precision, and the Shu-Ha-Ri trust tracker.


The workflow DAG shows the parallel regulatory check and due diligence steps before the final report compilation.

## Deploy It

```bash
kubectl apply \
  -f docs/showcases/legal-compliance/agents/ \
  -f docs/showcases/legal-compliance/workflows/
```

Trigger the workflow from the dashboard or via your intake system webhook:

```bash
curl -X POST http://localhost:8082/api/trigger/ai-agents/contract-review \
  -H 'Content-Type: application/json' \
  -d '{
    "clientName": "GlobalTech Inc",
    "contractType": "SaaS vendor agreement",
    "contractContent": "...",
    "jurisdiction": "US - Delaware"
  }'
```

## Representative Agent

The contract-analyst is the most sensitive role in the workflow:

```yaml
apiVersion: purko.io/v1alpha1
kind: Agent
metadata:
  name: contract-analyst
  namespace: ai-agents
spec:
  type: reviewer
  model:
    provider: anthropic
    name: claude-sonnet-4-6
    temperature: 0.2
  role: "Senior Contract Analyst"
  autonomyLevel: supervised
  memory:
    type: summary
  guardrails:
    maxIterations: 20
    costLimit: "$5.00"
```

Temperature 0.2 enforces precision over creativity. The system prompt instructs the agent to cite specific clause numbers and never speculate on legal interpretation. Memory type `summary` lets it accumulate institutional knowledge about common risk patterns across reviewed contracts.

## Customize It

- **Jurisdiction** — pass `jurisdiction` as a workflow parameter. The regulatory-monitor adapts its checks accordingly (Delaware corporate law, GDPR, HIPAA, CCPA, etc.).
- **Contract types** — update the contract-analyst's system prompt to reference your firm's standard templates for different categories (vendor agreements, employment contracts, NDAs, licensing).
- **Risk thresholds** — the system prompt defines Critical/Warning/Info severity levels. Adjust these thresholds to match your firm's risk appetite and engagement type.
- **Intake automation** — connect your document management system to trigger the pipeline via webhook when a new contract arrives.

!!! warning "Supervised autonomy is intentional"
    The contract-analyst is set to `supervised` and should remain there until you have substantial data on its performance for your specific contract types. Use the Shu-Ha-Ri dashboard to track its approval rate before considering a promotion to `restricted`.

!!! tip "Data stays in your cluster"
    All processing runs in your own Kubernetes cluster. No client document content is sent to third-party SaaS. If you configure Ollama for local inference, no data leaves your infrastructure.

## Cost

A typical contract-review run — initial analysis, parallel regulatory check and due diligence, final report — costs under $1 in LLM spend, depending on contract length and complexity.
