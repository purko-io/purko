# Adapting Purko to Any Industry

Purko is not tied to a single industry. The same pattern that automates a marketing campaign at a creative agency works for contract review at a law firm, property listings at a brokerage, and data pipelines at a consultancy. This guide shows you the universal pattern and how to apply it to your business.

## The Universal Pattern

Every Purko deployment follows five steps:

```
  Roles --> Agents --> Process --> Workflow --> Trust Levels
```

1. **List the roles** in your business process (the people who do the work today)
2. **Create agents** that mirror those roles (planner, executor, reviewer, router, monitor, retriever)
3. **Map the process** -- draw the sequence, dependencies, and decision points
4. **Build the workflow** -- translate the process into steps with `dependsOn` and `condition`
5. **Assign trust levels** -- supervised for client-facing output, restricted for internal work, full for monitoring

This pattern works for every industry because every industry has people who plan, people who produce, and people who check quality.

## Business Functions Mapped to Agent Types

No matter your industry, your business runs common functions. Here is how they map to Purko agents:

| Function | Typical Agents | Agent Types | Example Tools |
|----------|---------------|-------------|---------------|
| **Sales** | Lead qualifier, proposal writer, deal reviewer | router, executor, reviewer | CRM API, email API |
| **Marketing** | Campaign strategist, content writer, brand reviewer | planner, executor, reviewer | CMS, social media APIs |
| **Operations** | Process monitor, report generator, capacity planner | monitor, executor, planner | Monitoring APIs, dashboards |
| **Legal** | Contract analyst, due diligence researcher, compliance reporter | reviewer, retriever, planner | Document APIs, legal databases |
| **Creative** | Script writer, content producer, creative director | executor, executor, reviewer | CMS, DAM, social APIs |
| **Engineering** | Code generator, code reviewer, architect, deployment manager | executor, reviewer, planner, executor | GitHub MCP, Kubernetes |
| **Support** | Ticket classifier, KB retriever, escalation monitor | router, retriever, monitor | Ticketing API, knowledge base |
| **Finance** | Anomaly detector, report generator, auditor | monitor, executor, reviewer | Accounting API, ERP |
| **HR** | Resume screener, onboarding coordinator, policy checker | router, planner, reviewer | HRIS API, calendar API |
| **Data & Analytics** | Data quality analyst, anomaly detector, insight synthesizer | reviewer, monitor, planner | Database APIs, BI tools |

!!! tip "Start with functions you already have"
    You do not need to build agents for every function at once. Pick the one where your team spends the most time on repetitive work.

## How to Start Small

The biggest mistake is trying to automate everything at once. Instead:

**Pick ONE workflow.** Choose the process that is:

- **High volume** -- runs daily or weekly, not quarterly
- **Low risk** -- internal reports, not client contracts
- **Well defined** -- your team already follows a clear process
- **Measurable** -- you can tell whether the output is good

Good first workflows:

| Industry | Good First Workflow | Why |
|----------|-------------------|-----|
| Marketing | Weekly performance report | Internal, high volume, structured output |
| Legal | Regulatory change monitoring | Automated scanning, low risk, high value |
| Real Estate | Comparable market analysis | Structured data, clear output format |
| Engineering | Pull request review | Well-defined criteria, immediate feedback |
| Support | Ticket classification and routing | High volume, low risk, measurable accuracy |
| Finance | Expense report validation | Rule-based, high volume, clear pass/fail |

**Build 2-4 agents** for that workflow. Do not build 20 agents and hope they work together. Start with the minimum viable team.

**Set everything to supervised.** Every agent starts with human oversight. Watch the output for a week. Adjust system prompts based on what you see. Then gradually loosen the reins.

## How to Scale

Once your first workflow runs reliably:

### Add More Agents

Build agents for adjacent roles. If your campaign workflow has a strategist, writer, and reviewer, add a reporter agent that generates weekly performance summaries.

### Connect More Tools

Each MCP server you connect gives every agent new capabilities. Add a Slack MCP server and your agents can post updates. Add a CRM MCP server and your sales agents can look up leads. See [Connect MCP Servers](../getting-started/connect-mcp.md).

### Build an Internal Agent Library

As you build agents, you create an internal library of reusable components. The same `brand-reviewer` agent can work in your campaign workflow, your social media calendar, and your client reporting pipeline. Define once, reuse everywhere.

### Promote Agents Through Trust Levels

As agents prove themselves, the [Shu-Ha-Ri system](../concepts/shu-ha-ri.md) promotes them automatically:

- **Shu** (learning) -- every output reviewed by a human
- **Ha** (trusted) -- output goes through, human notified but approval optional
- **Ri** (autonomous) -- fully independent operation

This progression happens per agent based on success rate, execution count, and time in service. You do not flip a switch; the agent earns trust.

## Industry Examples

Purko ships with five complete showcases that demonstrate this pattern in practice. Each showcase includes agents, workflows, and a walk-through presentation.

### Digital Agency

Four agents (campaign strategist, content writer, brand reviewer, campaign reporter) form a campaign delivery pipeline. Strategy feeds content creation, content gets reviewed for brand compliance, and deliverables are packaged automatically.

- Agents: planner, executor, reviewer, monitor
- Key pattern: fan-out (3 content types in parallel) with review gate

### Legal and Compliance

Four agents (contract analyst, due diligence researcher, regulatory monitor, compliance reporter) handle contract review and regulatory monitoring. Contracts are analyzed for risks, entities are investigated, and findings are compiled into defensible reports.

- Agents: reviewer, planner, monitor, planner
- Key pattern: linear pipeline with severity-based escalation

### Real Estate

Four agents (market analyst, client matcher, listing writer, transaction coordinator) automate property analysis and listing creation. Market data drives pricing recommendations, listings are written and optimized, and transactions are coordinated.

- Agents: monitor, router, executor, planner
- Key pattern: market analysis feeds listing creation

### Data Analytics

Four agents (data quality analyst, anomaly detector, report generator, insight synthesizer) form an analytics pipeline. Data is validated, anomalies are detected, reports are generated, and strategic insights are synthesized for executive audiences.

- Agents: reviewer, monitor, executor, planner
- Key pattern: quality-first pipeline (validate data before analyzing it)

### Video Production

Four agents (script writer, shot planner, post-production coordinator, distribution strategist) handle content production workflows. Scripts are written, shot plans are created, post-production is coordinated, and distribution strategies are developed.

- Agents: executor, planner, planner, planner
- Key pattern: creative pipeline with high-temperature generation

## Quick-Start Checklist

Use this checklist when starting a Purko deployment in any industry:

- [ ] **Identify the process** -- pick one high-volume, low-risk workflow to automate first
- [ ] **List the roles** -- write down every person involved and what they do
- [ ] **Map roles to agent types** -- planner, executor, reviewer, router, monitor, or retriever
- [ ] **Write system prompts** -- specific instructions, output format, and constraints for each agent
- [ ] **Set temperature** -- low for analytical work, high for creative work
- [ ] **Set autonomy** -- supervised for client-facing output, full for internal monitoring
- [ ] **Connect tools** -- deploy MCP servers for the APIs your agents need (CRM, CMS, ticketing, etc.)
- [ ] **Build the workflow** -- define steps, dependencies, conditions, and failure strategy
- [ ] **Run supervised for one week** -- watch outputs, tune prompts, adjust guardrails
- [ ] **Gradually increase autonomy** -- let Shu-Ha-Ri promote agents based on their track record

!!! tip "The 80/20 rule"
    Most businesses get 80% of the value from automating just 2-3 workflows. Start there. Do not try to build an enterprise-wide agent platform on day one.

## Putting It Together

The pattern is always the same:

1. **Your people** -- list the roles in your process
2. **Your agents** -- mirror those roles with the right type, prompt, and temperature
3. **Your process** -- draw the dependencies and decision points
4. **Your workflow** -- encode it in YAML with steps, conditions, and failure handling
5. **Your trust model** -- start supervised, let agents earn autonomy

Whether you run a law firm or a recording studio, the building blocks are identical. The system prompts, tools, and temperature settings make each agent specific to your domain. The workflow structure makes your process repeatable.

## Next Steps

- [How to Design Agents](building-agents.md) -- step-by-step agent design
- [How to Design Workflows](building-workflows.md) -- step-by-step workflow design
- [SDLC Automation](sdlc-workflow.md) -- a complete engineering workflow example
- [Your First Agent](../getting-started/first-agent.md) -- hands-on quickstart
