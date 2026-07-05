# Contributing to Purko

Thank you for your interest in contributing to Purko! This document provides guidelines for contributing to the project.

## Development Setup

### Prerequisites

- Go 1.25+
- Python 3.12+ (for the executor)
- kubectl
- minikube (or any Kubernetes cluster)
- Docker or Podman

### Getting Started

```bash
# Clone the repository
git clone https://github.com/purko-io/purko.git
cd purko

# Build the operator
make build

# Run locally (development mode, connects to current kubeconfig cluster)
make run

# Run tests
make test
```

## Making Changes

### Branch Naming

Use descriptive branch names with a prefix:

- `feat/` — new features (e.g., `feat/ollama-support`)
- `fix/` — bug fixes (e.g., `fix/workflow-timeout`)
- `docs/` — documentation changes
- `chore/` — maintenance tasks

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Ollama executor client
fix: workflow controller timeout handling
docs: update CRD reference for MCPServer
chore: update Go dependencies
```

## Pull Request Process

If you're new to the project, please introduce yourself by emailing **georgievski@purko.io** before you start. We'd love to connect, help you find the right area to contribute, and make sure your work aligns with the roadmap. Once we've connected, you're welcome to submit PRs freely.

1. Fork the repository and create your branch from `main`
2. Make your changes with clear, focused commits
3. Fill out the pull request template
4. Ensure CI checks pass
5. Request a review — one approval is required to merge

## Code Style

- **Go:** Format with `gofmt`, lint with `golangci-lint` (`make lint`)
- **Python:** Lint with `ruff`
- **YAML:** 2-space indentation

## CRD Changes

If you modify `api/v1alpha1/types.go`, you must regenerate the CRD manifests:

```bash
make generate
make manifests
```

Commit the generated files along with your changes.

## Reporting Issues

Use the [issue templates](https://github.com/purko-io/purko/issues/new/choose) to report bugs or request features. Please include:

- Purko version (tag or commit)
- Kubernetes version and environment (minikube, kind, OpenShift, etc.)
- Steps to reproduce (for bugs)

## Community

- [GitHub Issues](https://github.com/purko-io/purko/issues) — bug reports and feature requests
- [GitHub Discussions](https://github.com/purko-io/purko/discussions) — questions, ideas, and general discussion
