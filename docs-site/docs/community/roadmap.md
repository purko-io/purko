# Roadmap

## Phase 1: Community Edition v1.0 (Current)

Phase 1 ships the core platform as a production-ready open source release. Items 1.1–1.3 are complete; the documentation site (this site) is in progress; CI/CD and container image publishing are planned.

| Item | Status | Description |
|------|--------|-------------|
| GitHub repo setup | Done | Apache 2.0 license, README, issue templates, PR template |
| purkoctl CLI | Done | 11 commands: `agent list/get`, `workflow list/get/trigger/logs/approve/deny/cancel/rerun`, `version` |
| Helm chart hardening | Done | Security contexts, PodDisruptionBudget, operator Service, NOTES.txt, Helm test |
| Documentation site | In Progress | This site — MkDocs Material with 31 pages across 7 sections |
| CI/CD pipeline | Planned | GitHub Actions for build, test, and release on every PR and tag push |
| Container images | Planned | Multi-arch (amd64 + arm64) images published to quay.io |

## Phase 2: SDLC Agent Library

Phase 2 expands the agent and workflow library for software development lifecycle automation.

| Item | Status | Description |
|------|--------|-------------|
| SDLC agents | Partially done | 13 agents in `examples/agents/sdlc/` covering requirements, code review, testing, security, deployment |
| SDLC workflows | Partially done | 9 workflows in `examples/workflows/sdlc/` for feature development, PR review, release, and more |
| New MCP servers | Planned | Slack (notifications), Atlassian/Jira (requirements tracking), Trivy (security scanning) |
| Ollama support | Planned | Local LLM via Ollama executor client — run agents without a cloud API key |

## Phase 3: Pro Features

Phase 3 adds production-grade features targeting teams and organizations. These are planned for after Community Edition GA.

| Item | Description |
|------|-------------|
| SSO Integration | OAuth2 Proxy sidecar for dashboard authentication (OIDC: GitHub, Google, Okta) |
| Execution history | SQLite persistence for workflow runs, step logs, and audit trail beyond pod/ConfigMap lifecycle |
| Multi-model routing | Per-agent model selection — different agents can use different LLM providers and models |
| Grafana dashboards | 4 pre-built dashboards: platform overview, agent performance, workflow execution, cost tracking |

## Version Targets

| Version | Contents | Target |
|---------|----------|--------|
| v1.3.0 | Phase 1 complete — GitHub, CLI, Helm, docs site, CI/CD, container images | Week 3 |
| v1.4.0 | Phase 2 complete — full SDLC agent library, new MCP servers, Ollama | Week 7 |
| v1.5.0 | Phase 3 complete — SSO, execution history, multi-model routing, Grafana | Week 10 |
| v2.0.0 | Community Edition GA | Week 10 |

## How to Contribute

See the [Contributing guide](contributing.md) for development setup, branch naming, commit style, and the pull request process.

Have an idea or found a bug? Open an issue on [GitHub Issues](https://github.com/geored/purko/issues) or start a discussion on [GitHub Discussions](https://github.com/geored/purko/discussions).
