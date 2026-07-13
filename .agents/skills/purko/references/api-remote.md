# Remote-Mode API Reference (Flows H/I/J without kubectl)

> Used when the active target is `remote`/`hosted` (SaaS or ingress-exposed
> Purko). All calls go through the LOCAL server (`http://127.0.0.1:<WEB>`),
> which attaches the stored credential (dashboard token or PAT) upstream.
> Always send `-H 'X-Purko-CSRF: 1' -H 'Content-Type: application/json'` on
> mutations. Error responses are `{"error": "..."}` — surface them verbatim;
> AG-xxx / WF-xxx codes map to the same rules in `agents.md`/`workflows.md`.

## Live facts (replaces cluster.list_*)

| Fact | Endpoint | Notes |
|---|---|---|
| LLM providers | `GET /api/llm/providers` | `.providers[].metadata.name`, `.spec.type/model/default`. Workspace tenants get a REDACTED view (no endpoints/secrets) — names are all the interview needs. |
| MCP servers | `GET /api/mcp/servers` | `.servers[].metadata.name` (+ builtins `search-knowledge`, `web-search` always exist). |
| Skills | `GET /api/skills` | `.skills[].metadata.name`. |
| Agents (for WF refs) | `GET /api/agents` | list is already scoped to the caller's workspace. |
| Caller identity | `GET /api/whoami` | `registered`, `role`, `namespaces`, `workspace{name,namespace,phase}` — use `workspace.namespace` as the default namespace for creates. |

## Create agent (Flow H remote)

`POST /api/create/agent` — body (subset; omit what the interview didn't set):

```json
{
  "name": "release-notes-writer",
  "namespace": "<whoami.workspace.namespace or ''>",
  "type": "executor",
  "provider": "<REAL provider name from /api/llm/providers>",
  "model": "llama3.1:8b",
  "temperature": 0.7,
  "autonomy": "supervised",
  "role": "one-line role",
  "systemPrompt": "...",
  "memoryBehavior": "session",
  "memoryScope": "agent",
  "costLimit": 5.0,
  "maxIterations": 20,
  "tools": ["web-search"],
  "skills": [],
  "minReplicas": 1, "maxReplicas": 3, "targetCPU": 70
}
```

Success: `{"name": "...", "status": "created"}`. **There is no dry-run over
the API** — the admission webhook validates at create time; on an AG-xxx
error, explain the rule from `agents.md`, fix the field, and re-POST (same
fix-and-loop as Flow H step 5, one step later).

## Create workflow (Flow I remote)

`POST /api/create/workflow`:

```json
{
  "name": "release-notes-pipeline",
  "namespace": "<whoami.workspace.namespace or ''>",
  "description": "...",
  "parallelism": 2,
  "strategy": "continueOnError",
  "parameters": {"version": "Unreleased"},
  "steps": [
    {"name": "draft", "agent": "release-notes-writer", "dependsOn": [], "input": "Version: ${parameters.version}"},
    {"name": "review", "agent": "brand-reviewer", "dependsOn": ["draft"], "input": "Draft: ${steps.draft.output.response}"}
  ],
  "cron": ""
}
```

Note the flat shape: `agent` (not `agentRef.name`), `input` as a string (not
`input.raw`), `strategy` (not `failureStrategy`). Interpolation rules are
unchanged (`${steps.X.output.response}` — never bare `.output`). WF-005 still
applies: check `GET /api/agents` for every referenced agent first.

## Manage (Flow J remote)

| Action | Endpoint |
|---|---|
| Update agent | `POST /api/update/agent` (same body as create; name identifies) |
| Delete agent | `DELETE /api/delete/agent/{name}` |
| Delete workflow | `DELETE /api/delete/workflow/{name}` |
| Re-run workflow | `POST /api/rerun/workflow/{name}` body `{"parameters": {...}}` |
| Approve / deny a gate | `POST /api/approve/{workflow}/{step}` / `POST /api/deny/{workflow}/{step}` |
| Skill create/update | `POST /api/skill/{name}` `{"description","content","whenToUse",...}` |
| Skill delete | `DELETE /api/skill/{name}` |

Deletes keep the typed-confirmation rule from Flow J. Diffing: `GET
/api/agent/{name}` returns the live object for the before/after diff.

## SaaS specifics

- **Credentials**: prefer a **personal access token** (dashboard → Settings →
  Personal Access Tokens; `pat_…`, shown once). PATs are scoped to the user's
  role + workspace and die instantly on suspension. On SaaS instances the
  upstream URL for PATs is the API host (e.g. `https://api.<host>`), which
  accepts only `Authorization: Bearer pat_…`.
- **Workspace signup** (first-time users): `POST /api/workspace` provisions;
  poll `GET /api/workspace/status` until `phase: Ready`. 403 = not entitled —
  the user needs an invite/allowlisted domain or a subscription.
- Namespaces: omit or use `whoami.workspace.namespace`. The server refuses
  out-of-scope namespaces (403 `namespace:...`).
