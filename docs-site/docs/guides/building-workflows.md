# How to Design Workflows for Your Business

A workflow chains agents together into an automated process. If you can draw your business process on a whiteboard -- who does what, in what order, and what happens if something fails -- you can build it in Purko.

## Start from Your Existing Process

Every workflow begins with a process you already run. Take a marketing campaign as an example:

1. Strategist reads the client brief and creates a plan
2. Three copywriters produce content in parallel (social, email, blog)
3. A reviewer checks everything for brand compliance
4. If the reviewer flags issues, the writer revises
5. A coordinator packages the final deliverables

That sequence of steps, those dependencies, those conditional branches -- that is your workflow. Purko just makes it executable.

## Identify Dependencies

Dependencies determine execution order. Ask: "Can this step start before the previous one finishes?"

- The strategist must finish before writers can start (they need the strategy)
- The three writing tasks are independent of each other
- The reviewer needs all content before reviewing
- Revision only happens if the reviewer flags issues

In YAML, dependencies use `dependsOn`:

```yaml
steps:
  - name: create-strategy
    agentRef:
      name: campaign-strategist
    # No dependsOn -- runs first

  - name: write-social
    agentRef:
      name: content-writer
    dependsOn: [create-strategy]     # Waits for strategy

  - name: review-content
    agentRef:
      name: brand-reviewer
    dependsOn: [write-social, write-email, write-blog]  # Waits for ALL content
```

## Identify Parallelism

Steps that do not depend on each other run simultaneously. This is where Purko saves time.

In the campaign example, writing social, email, and blog content are three independent tasks. They all depend on the strategy but not on each other. Purko runs all three at once:

```yaml
parallelism: 3    # Up to 3 steps can run simultaneously
```

The `parallelism` field sets the maximum number of concurrent steps. Set it to match the number of parallel tasks in your widest fan-out.

## Map Steps to Agents

Each step references an agent by name:

```yaml
- name: write-social
  agentRef:
    name: content-writer
```

The same agent can appear in multiple steps. In the campaign workflow, the `content-writer` agent handles social, email, and blog steps -- each as a separate execution with different input.

## Define Parameters

Parameters are the values that change every time you run the workflow:

```yaml
parameters:
  clientName: "Acme Corp"
  briefContent: "Launch campaign for new SaaS product..."
  channels: "social,email,blog"
```

When you trigger the workflow, you supply new parameter values. The workflow template stays the same; only the inputs change.

## Wire Outputs Between Steps

Each step produces output that downstream steps can reference:

```yaml
- name: write-social
  agentRef:
    name: content-writer
  dependsOn: [create-strategy]
  input:
    raw: |
      Using the campaign strategy, write social media content.
      Strategy context: ${steps.create-strategy.output.response}
```

The syntax `${steps.<step-name>.output.response}` passes the previous step's response as input to the next step. This is how agents collaborate -- the strategist's plan feeds into the writer's instructions.

## Add Review Gates

When a `reviewer` agent is set to `supervised` autonomy, Purko automatically creates an approval gate. The workflow pauses until a human approves or rejects the output.

For automated review gates that branch based on the verdict:

```yaml
- name: revise-content
  agentRef:
    name: content-writer
  dependsOn: [review-content]
  condition: 'steps.review-content.output.verdict == REVISE'
  input:
    raw: |
      Revise content based on reviewer feedback.
      Feedback: ${steps.review-content.output.response}
```

The `condition` field makes the step conditional -- it only runs if the reviewer's verdict matches.

## Set Failure Strategy

Two options:

| Strategy | Behavior | Use When |
|----------|----------|----------|
| `failFast` | Stop the entire workflow on first failure | Every step depends on prior steps succeeding |
| `continueOnError` | Keep running other steps, collect all results | Independent steps where partial results are still useful |

```yaml
failureStrategy: continueOnError
```

For a campaign workflow, `continueOnError` makes sense -- if the blog post fails, you still want the social and email content. For a deployment pipeline, `failFast` is safer.

## Common DAG Patterns

Most business processes follow one of these patterns:

### Linear Pipeline

```
  A --> B --> C --> D
```

Each step depends on the previous one. Simple and predictable.

```yaml
steps:
  - name: step-a
    agentRef: { name: agent-a }
  - name: step-b
    agentRef: { name: agent-b }
    dependsOn: [step-a]
  - name: step-c
    agentRef: { name: agent-c }
    dependsOn: [step-b]
```

### Fan-Out / Fan-In

```
          +--> B --+
          |        |
  A ------+--> C --+----> E
          |        |
          +--> D --+
```

One step fans out to multiple parallel steps, then a final step collects all results. The three parallel steps all have `dependsOn: [plan]` and the aggregate step has `dependsOn: [task-b, task-c, task-d]`.

### Conditional Branch

```
                 +--(pass)--> B
  A --> Review --|
                 +--(fail)--> C
```

A reviewer step produces a verdict, and subsequent steps use `condition` to run or skip based on the result. Two steps depend on the same review step with different conditions -- only the matching branch executes.

### Review Loop

```
  Write --> Review --+--(APPROVED)--> Done
                     |
                     +--(REVISE)--> Rewrite --> Re-Review
```

A reviewer checks work, and if it needs changes, the writer revises and the reviewer checks again. This is common in content, legal, and code review workflows.

## Worked Example: Employee Onboarding

Your HR team onboards new employees. The process involves document preparation, system access setup, orientation scheduling, and a welcome package. Let us automate it.

**Step 1: List the roles.**

- HR coordinator (plans the onboarding)
- IT provisioner (sets up accounts and access)
- Document preparer (generates offer letter, NDA, handbook)
- Orientation scheduler (books meetings and training)

**Step 2: Map to agents and identify the DAG.** The plan step fans out to three parallel tasks, then a final step collects results:

```
  Plan --> (Documents, IT Access, Orientation) --> Welcome Package
```

**Step 3: Write the workflow.** Key sections (full YAML omitted for brevity):

```yaml
apiVersion: purko.io/v1alpha1
kind: Workflow
metadata:
  name: employee-onboarding
spec:
  parallelism: 3
  failureStrategy: continueOnError
  parameters:
    employeeName: ""
    role: ""
    startDate: ""
    department: ""
  steps:
    - name: create-onboarding-plan
      agentRef: { name: hr-coordinator }
      input:
        raw: "Create an onboarding plan for ${parameters.employeeName}..."

    - name: prepare-documents
      agentRef: { name: document-preparer }
      dependsOn: [create-onboarding-plan]
      input:
        raw: "Prepare documents. Plan: ${steps.create-onboarding-plan.output.response}"

    - name: provision-access
      agentRef: { name: it-provisioner }
      dependsOn: [create-onboarding-plan]

    - name: schedule-orientation
      agentRef: { name: orientation-scheduler }
      dependsOn: [create-onboarding-plan]

    - name: send-welcome-package
      agentRef: { name: hr-coordinator }
      dependsOn: [prepare-documents, provision-access, schedule-orientation]
```

Apply and trigger:

```bash
kubectl apply -f employee-onboarding.yaml
purko workflow trigger employee-onboarding \
  --param employeeName="Jane Smith" \
  --param role="Senior Engineer" \
  --param department="Platform" \
  --param startDate="2026-05-01"
```

The three middle steps run in parallel. The welcome package step waits for all three to complete, then assembles everything into a single deliverable.

## Next Steps

- [How to Design Agents](building-agents.md) -- design the agents that power your workflow steps
- [Adapting Purko to Any Industry](industry-templates.md) -- workflow patterns for different industries
- [Workflow CRD Reference](../reference/crd-workflow.md) -- full specification for all workflow fields
- [Intent Bar](intent-bar.md) -- generate workflows from plain English descriptions
