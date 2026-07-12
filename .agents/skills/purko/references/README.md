# References — Judgment Layer

This directory contains **generated authoring guides** that give the purko skill
*judgment* — archetypes, model-tier guidance, field semantics, and when to apply
guardrails. These guides do NOT contain cluster-specific facts.

---

## Two-Layer Architecture

The purko skill operates with two distinct knowledge layers:

| Layer | Source | What it provides |
|-------|--------|-----------------|
| **Judgment (this directory)** | Generated at release from purko source + showcases | CRD field semantics, agent archetypes, workflow patterns, guardrail guidance, validation rules |
| **Facts (live cluster queries)** | Runtime cluster queries via `kubectl` | Real LLMProvider names, registered MCPServer CRs, existing Agent/Workflow names, current namespace |

The guides here tell the skill **how to author well**. The live layer tells the
skill **what is available**. Neither is sufficient alone.

---

## Files in This Directory

| File | Contents |
|------|----------|
| `agents.md` | Agent CRD shape; required vs optional fields; archetype table (planner/executor/reviewer/researcher/monitor/router); model-tier guidance; autonomy levels (Shu/Ha/Ri); when to gate with `humanApprovalRequired`; webhook validation rules |
| `workflows.md` | Workflow CRD shape; step fields; `${parameters.X}` and `${steps.Y.output.response}` interpolation; parallel fan-out patterns; human approval gates; triggers (webhook + schedule); failure strategy; WF-005 and other validation rules |
| `tools-mcp.md` | MCPServer CR fields; RunAsNonRoot caveat; how agents reference tools (mcp vs builtin); dashboard catalog; tool discovery flow; URL/connect mode |
| `templates/` | Showcase YAML sets, one directory per industry, byte-copied from the purko repo at the SHA recorded in `PURKO_REF_VERSION` |
| `PURKO_REF_VERSION` | Stamp file: purko git short-SHA and generation date for version-skew detection |

---

## Template Industries

```
templates/
  data-analytics/       anomaly-detector, data-quality-analyst, insight-synthesizer, report-generator + client-analytics workflow
  digital-agency/       brand-reviewer, campaign-reporter, campaign-strategist, content-writer + campaign-delivery workflow
  legal-compliance/     compliance-reporter, contract-analyst, due-diligence-researcher, regulatory-monitor + contract-review workflow
  real-estate/          client-matcher, listing-writer, market-analyst, transaction-coordinator + listing-to-close workflow
  video-production/     distribution-strategist, post-production-coordinator, script-writer, shot-planner + video-production workflow
```

Each industry directory contains agent YAMLs and the industry workflow YAML.
Apply a set with: `kubectl apply -f references/templates/<industry>/ -n <namespace>`

---

## Version Skew Detection

`PURKO_REF_VERSION` records the purko git SHA these guides were generated from:

```
purko_sha=abc1234
generated=2026-07-12
```

The guided-create flow (T3) should compare this SHA against the purko version
on the cluster (e.g. from the operator image tag or a well-known ConfigMap)
and warn if they diverge significantly.

---

## Regenerating References

Run from the repo root:

```bash
./scripts/sync-references.sh [/path/to/purko/checkout]
```

Point it at a local checkout of `purko-io/purko`. The script:
1. Validates the purko path has `docs/showcases/`.
2. Copies all 5 industry showcase sets into `references/templates/`.
3. Writes `references/PURKO_REF_VERSION` with the purko git short-SHA and date.
4. Is idempotent — safe to re-run; only modifies files under `references/`.

The `.md` guide files (`agents.md`, `workflows.md`, `tools-mcp.md`, `README.md`)
are **hand-authored judgment** and are NOT overwritten by the sync script. They
should be updated manually when the purko CRD schema or showcase patterns change
in ways that affect authoring guidance.
