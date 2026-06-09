# SDLC Automation

Purko ships with a complete software development lifecycle (SDLC) pipeline that takes a GitHub issue from creation to merged pull request -- fully automated, with human approval gates at critical points.

## The Pipeline

The SDLC pipeline mirrors how a development team works:

```
  Read Issue --> Plan --> Implement --> Create PR --> Test --> Review --> Merge
                                                                |
                                                          (request changes)
                                                                |
                                                          Fix --> Re-Review --> Merge
```

A parallel security scan runs alongside the review. If the review requests changes, a fix-and-rereview loop handles revisions. After merge, documentation is updated automatically.

## The 13 SDLC Agents

Each agent handles one phase of the development process:

| # | Agent | Type | Phase | Role |
|---|-------|------|-------|------|
| 1 | `sdlc-router` | router | All | Classifies incoming work items and routes to the right specialist |
| 2 | `requirements-analyst` | planner | Requirements | Reads issues and produces structured requirements with acceptance criteria |
| 3 | `architecture-designer` | planner | Design | Designs implementation plans with component diagrams and ADRs |
| 4 | `code-generator` | executor | Development | Implements features by creating branches, writing code, and pushing files |
| 5 | `sdlc-code-reviewer` | reviewer | Review | Reviews code for quality, security, and adherence to conventions |
| 6 | `test-engineer` | executor | Testing | Writes tests, executes them in a sandbox, and reports coverage |
| 7 | `security-scanner` | monitor | Security | Scans code for vulnerabilities, anti-patterns, and CVEs |
| 8 | `cicd-engineer` | monitor | CI/CD | Monitors build pipelines and analyzes failures |
| 9 | `container-builder` | executor | Build | Analyzes Dockerfiles and validates container images |
| 10 | `deployment-manager` | executor | Deployment | Manages rollouts, monitors health, and handles rollbacks |
| 11 | `documentation-writer` | planner | Documentation | Generates and updates technical documentation |
| 12 | `release-manager` | planner | Release | Manages versioning, changelogs, and GitHub releases |
| 13 | `pr-manager` | executor | PR Management | Creates and merges pull requests |

!!! tip "You do not need all 13"
    The SDLC agents are a complete library. Most teams start with the issue-to-merge workflow (agents 2-5, 7, 13) and add others as needed.

## The Issue-to-Merge Workflow

The core workflow reads a GitHub issue, plans the implementation, writes the code, reviews it, and merges the result. Here is the full DAG:

```
  read-issue
      |
  plan-implementation
      |
  implement-code
      |
  create-pr ---+
      |        |
  validate-tests   security-check
      |        |
    review ----+
      |
  +---+---+
  |       |
(approve) (request_changes)
  |       |
merge   fix-if-rejected
  |       |
  |    re-review
  |       |
  |    merge-after-rereview
  |       |
  +---+---+
      |
  update-docs
```

### Step-by-Step Walk-Through

**1. Read Issue** (`requirements-analyst`)

The requirements analyst reads the GitHub issue and the existing codebase. It produces a requirements brief with acceptance criteria in Given/When/Then format, dependencies, complexity estimate, and risk flags.

```yaml
- name: read-issue
  agentRef:
    name: requirements-analyst
  input:
    raw: '{"task": "Read GitHub issue #${parameters.issueNumber} in ${parameters.repository}..."}'
  timeout:
    timeoutSeconds: 600
```

**2. Plan Implementation** (`architecture-designer`)

The architect reads the requirements brief and designs the solution -- which files to modify, new types needed, and how to integrate with existing code. This step has a condition: it only runs if the requirements analyst did not reject the issue as infeasible.

```yaml
- name: plan-implementation
  agentRef:
    name: architecture-designer
  dependsOn: [read-issue]
  condition: 'steps.read-issue.output.feasibility != rejected'
```

**3. Implement Code** (`code-generator`)

The code generator creates a feature branch, reads the existing codebase, writes all modified source files and tests, and pushes. It runs in a sandboxed executor with code execution enabled for syntax validation.

**4. Create PR** (`pr-manager`)

A focused agent that does one thing: create the pull request with a descriptive title and body. Intentionally simple -- 10 max iterations, $1 cost limit.

**5. Validate Tests** (`test-engineer`)

The test engineer reads the pushed code, verifies test correctness, checks for missing edge cases, and can execute tests in a sandbox to validate logic.

**6. Review** (`sdlc-code-reviewer`)

The code reviewer evaluates: code quality, test coverage, language idioms, error handling, and whether acceptance criteria are met. It produces a structured verdict: `approve` or `request_changes`.

**7. Security Check** (`security-scanner`)

Runs in parallel with the review. Scans for security anti-patterns, injection vulnerabilities, secrets in code, and known CVEs in dependencies.

**8. Merge or Fix**

If the review approves and security passes, the PR manager merges with a squash merge. If the review requests changes, the code generator fixes the issues and a re-review cycle runs.

**9. Update Docs** (`documentation-writer`)

After merge, the documentation writer checks whether README or other docs need updating to reflect the new functionality.

### Workflow Parameters

The workflow takes three parameters:

```yaml
parameters:
  repository: ""           # e.g., "myorg/myrepo"
  branch: "main"           # Target branch
  issueNumber: ""          # GitHub issue number
```

Trigger it:

```bash
purko workflow trigger sdlc-issue-to-merge \
  --param repository="myorg/myapp" \
  --param branch="main" \
  --param issueNumber="42"
```

## How Agents Collaborate

The key design principle is that each agent has a narrow scope and limited blast radius:

- **Read-only agents** (requirements analyst, architecture designer, code reviewer, security scanner) cannot modify the repository. They analyze and report.
- **Write agents** (code generator, pr-manager) can modify the repository but require human approval (`supervised` autonomy).
- **The code generator** has `blastRadiusLimit: repository` -- it can only affect files within the target repository, not cluster resources.
- **The deployment manager** has `blastRadiusLimit: namespace` -- it can only affect resources within its namespace.

Outputs flow forward through the DAG via `${steps.<name>.output.response}`. The requirements brief feeds the architecture design. The architecture design feeds the code generator. The code feeds the reviewer. Each agent builds on the work of the previous one.

## Customizing for Your Team

### Change the Language or Framework

The SDLC agents are language-agnostic by default. To specialize for your stack, update the system prompts:

```yaml
systemPrompt: |
  You are a Code Reviewer for a Go microservices project.
  Check for: proper error wrapping, context propagation,
  interface compliance, and table-driven tests.
```

### Add or Remove Steps

Not every team needs a security scan or documentation update. Remove steps by deleting them from the workflow. Add steps by defining new agents and inserting them into the DAG with appropriate `dependsOn`.

### Adjust Autonomy

For a mature, well-tested codebase, you might set the code reviewer to `full` autonomy -- letting it approve and merge without human intervention. For a new project, keep everything `supervised` until the agents prove reliable.

### Connect to CI/CD

The `cicd-engineer` agent can monitor your existing CI/CD pipelines and report failures. Connect it to your Jenkins, GitHub Actions, or Tekton instance via MCP servers.

## Next Steps

- [How to Design Agents](building-agents.md) -- customize SDLC agents or build new ones
- [How to Design Workflows](building-workflows.md) -- modify the pipeline to match your team
- [Agent CRD Reference](../reference/crd-agent.md) -- full specification for agent fields
- [Workflow CRD Reference](../reference/crd-workflow.md) -- full specification for workflow fields
