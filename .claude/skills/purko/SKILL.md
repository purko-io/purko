---
name: purko
description: Watch and manage Purko AI agent teams. Use when the user says "/purko", "open mission control", "show my purko workflows/agents", "install purko", or "load a starter team". Opens a live Mission Control webapp over the user's purko cluster; can bootstrap purko onto a bare cluster.
---

# Purko Skill (M1+M2a: watch + bootstrap + remote connect)

Paths below are relative to this skill's directory. All helper scripts are
Python 3 stdlib-only. Persisted config: `~/.config/purko-skill/config.json`.

## Flow A — open Mission Control (default)

0a. **Version check** (interim, pre-marketplace; non-blocking): run
   `python3 scripts/version_check.py`. If the JSON has `"update_available": true`,
   surface ONE line to the user — "A newer Purko skill is available
   (`<local>` → `<latest>`). What changed: `<changelog_url>` · update by
   re-running the installer: `curl -fsSL https://raw.githubusercontent.com/purko-io/purko/main/scripts/install-skill.sh | sh`" — then continue the flow normally.
   Silent-fail: on any error or `false`, say nothing and proceed. It is
   cached ~daily, so this is at most one network read per day.
0. **Stack-reuse check**: `curl -s -m 2 http://127.0.0.1:8090/local/status`.
   Reuse only if the JSON response contains **both** `"upstream": true` **and** an `"authMode"` key — this distinguishes our server from a coincidental app on port 8090.
   REUSE it — skip steps 1–2, go straight to step 3 (auto-login) and then open the browser.
   Do NOT start a second stack.
1. **Resolve the active target**: `python3 -c "import sys, json; sys.path.insert(0,'scripts'); import purko_config; print(json.dumps(purko_config.active_target(purko_config.load())))"`.
   Result is `[name, target]`, or `[null, null]` → no targets yet: run **Flow C (first-run guided tour)** first.
   If the user named a target ("open mission control on gcp"), run **Flow E** to switch first.
   `target.mode == "remote"` or `target.mode == "hosted"` (both mean a remote upstream URL) → skip port-forward; go to step 2b (remote mode). Use `target.workspace_url` as `<url>`.
   Otherwise (`target.mode == "cluster"`) use `target.context` as `<ctx>` below.
2a. **Start the stack — cluster mode** (background, one process):
   - The WEB port is always `8090` — pass `--port 8090` and let serve.py fall back to a random port by itself if 8090 is taken (its printed URL is authoritative). A stable web port keeps browser history, app-install identity, and muscle memory working.
   - `python3 scripts/serve.py --webapp webapp --pf-context <ctx> --port 8090` (run in background; it prints its URL).
     serve.py owns the port-forward lifecycle: it starts kubectl port-forward internally, self-heals on drop (no manual restart needed), and stops the tunnel on exit.
     If the deployment is missing, serve.py exits 1 — tell the user purko isn't installed and offer Flow D.
   - Legacy two-process form still works: `kubectl ... port-forward ... & python3 scripts/serve.py --webapp webapp --upstream-port <PF> --port 8090`. Use pf-mode (above) for new sessions.
2b. **Start the stack — remote mode** (background):
   - No port-forward needed.
   - Serve: `python3 scripts/serve.py --webapp webapp --upstream-url <url> --port <WEB>` (run in background; it prints its URL).
3. **Auto-login** (before opening the browser):
   - **Cluster mode**: Probe `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:<WEB>/api/whoami`.
     - `200` → open mode, nothing to do.
     - `401` → token/sso. Try `cluster.read_dashboard_token(ctx)`;
       if a token comes back, pipe it — never echo or pass it as an argument:
       `python3 -c "import sys, json; sys.path.insert(0,'scripts'); import cluster; t = cluster.read_dashboard_token('<ctx>'); sys.exit(1) if not t else print(json.dumps({'token': t}))" | curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:<WEB>/local/token -H 'Content-Type: application/json' --data-binary @-`
       (expect 204; on non-204 fall back to the webapp paste form). Never echo the token or place it in a command argument — pipe it. If the secret read is RBAC-denied, skip — the webapp shows its token-paste form; tell the user to ask their admin for the token.
   - **Remote mode**: try loading a saved token via `keychain.load_token('<name>')`. If found, pipe it to `/local/token` (same pattern as cluster mode, using the Python keychain module instead of cluster). If not found, open the browser — the webapp token-paste form handles login. See Flow G for the save-to-keychain offer.
4. **Open the browser**: `open http://127.0.0.1:<WEB>` (macOS) / `xdg-open` (Linux).
5. Report: cluster/context or workspace URL, auth mode, URL, and that the page live-updates every ~3s.
   Also tell the user where to MANAGE their namespace (create/edit agents, workflows,
   MCPs, skills, tokens): Mission Control is watch+approve; the full dashboard is at the
   `dashboardUrl` from `/local/status` — surfaced in the masthead as "Manage in dashboard →".
   In sso mode that dashboard is scoped to the user's own workspace.

## Flow B — stop

Kill the `serve.py` process you started (track its PID when launching). In pf-mode, serve.py owns the port-forward — killing it also stops the tunnel. In the legacy two-process form, also kill the `kubectl port-forward` process. Remote mode: only serve.py — no port-forward to kill. Confirm to the user.

## Flow C — first-run guided tour

**Check for returning users first.** If targets already exist, skip directly to Flow A:
```
python3 -c "import sys, json; sys.path.insert(0,'scripts'); import purko_config as pc; c = pc.load(); print(bool(c['targets']))"
```
`True` → run **Flow A** (returning user — no interview needed).

Otherwise, scan contexts and probe each one. Ask ONE question at a time when needed.

1. `cluster.list_contexts()` — if the list is empty, jump to step 4.

2. For each context `<ctx>` in the list, call `cluster.context_reachable('<ctx>')`:

   **Unreachable context:**
   - Tell the user: "Context `<ctx>` is not reachable — the cluster may be stopped."
   - If the context name contains "minikube": offer (with explicit consent) to run `minikube start` and wait; if they agree, run it and then re-probe `context_reachable`. If they decline or it's not minikube, suggest they check their VPN / cloud console / `kubectl get nodes --context <ctx>`.
   - Never say "no purko" for an unreachable cluster — the purko status is unknown.
   - Continue to the next context.

   **Reachable context:** call `cluster.purko_ready('<ctx>')`:
   - `'ready'` → register a named target (default name = context name):
     ```
     python3 -c "import sys; sys.path.insert(0,'scripts'); import purko_config as pc; c = pc.load(); pc.upsert_target(c, '<ctx>', mode='cluster', context='<ctx>'); pc.save(c)"
     ```
     Collect all ready targets; continue scanning.
   - `'not-ready'` → show the evidence: run `kubectl --context <ctx> -n purko-system get pods` and display its output to the user. Tell them the purko-operator is installed but not ready, and offer to help diagnose (e.g. inspect pod events, check resource limits). Do NOT offer onboarding or bootstrap — purko IS present. Continue to the next context.
   - `'absent'` → offer **Flow D (bootstrap)** on this cluster. If the user declines, continue scanning.

3. After scanning all contexts:
   - If **exactly one** ready target was registered → run **Flow A**.
   - If **multiple** ready targets were registered → tell the user which clusters have purko ready, ask which should be the active one, then:
     ```
     python3 -c "import sys; sys.path.insert(0,'scripts'); import purko_config as pc; c = pc.load(); pc.set_active(c, '<chosen>'); pc.save(c)"
     ```
     Then run **Flow A**.

4. **No usable context found** (no targets registered after scanning): ask:
   "Do you have a workspace URL and token from your team admin or a Purko subscription?"
   - **Yes** → run **Flow G**.
   - **No** → start the server standalone:
     `python3 scripts/serve.py --webapp webapp --port 8090`
     Then open `http://127.0.0.1:8090/onboarding.html` — it offers the demo studio, how to get access from a team admin, and the self-hosted bootstrap path.

## Flow E — switch or list targets

- "use <name>" / "switch to <name>" / "open … on <name>":
  `python3 -c "import sys; sys.path.insert(0,'scripts'); import purko_config as pc; c = pc.load(); pc.set_active(c, '<name>'); pc.save(c)"`
  (KeyError → list the known target names and ask which they meant).
- "list targets" / "which cluster am I on": print each target's name, mode, context or workspace_url, marking the active one.
- Adding a workspace target explicitly ("add my workspace <url>") → run **Flow G** (upsert mode:'remote' with the URL, connect, offer remember).

## Flow D — bootstrap purko onto a bare cluster (§5.4 of the spec)

NEVER install without explicit consent. Steps:
1. **Preflight**: `helm version --short` (missing → print install instructions, e.g. `brew install helm`, and stop); `kubectl --context <ctx> get nodes` reachable.
2. **Consent**: show exactly:
   `helm install purko oci://ghcr.io/purko-io/purko/purko --kube-context <ctx> --namespace purko-system --create-namespace` (add `--version <V>` if the user pins one) and the target context. Wait for an explicit yes.
3. **Install & verify**: run it, then wait:
   `kubectl --context <ctx> -n purko-system rollout status deploy/purko-operator --timeout=180s`.
   Report the REAL result — on failure show `kubectl -n purko-system get pods` output; do not claim success.
4. **Starter team (optional)**: offer `kubectl --context <ctx> apply -f references/templates/legal-compliance/ -n ai-agents` so Mission Control isn't empty. Mention: resources appear immediately; RUNNING a workflow needs an LLM provider + API key — offer that as a separate explicit step and skip it unless asked.
5. Continue to Flow A.

## Flow F — demo studio (offline, no cluster needed)

"show the demo", "purko demo": the demo page is a self-contained client-side
replay (legal-compliance showcase, interactive approval gate) — serve it with
the normal stack and open `http://127.0.0.1:<WEB>/demo.html`. Works with no
cluster at all: `python3 scripts/serve.py --webapp webapp --port 8090` alone
is enough. The hosted variants of this page (plus the onboarding page in
`proposals/O-atelier-v2-onboarding.html`) ship with M3 on the GCP cluster.

## Flow G — connect by URL + token (no kubectl)

For team members who access Purko via an ingress-exposed dashboard URL, or Purko
SaaS workspace holders — no local kubectl required.

1. Ask for the workspace URL (e.g. `https://acme.purko.cloud` or a self-hosted ingress
   URL like `https://purko.internal.example.com`).
   **SaaS/multi-user instances:** the right credential is a **personal access
   token** (`pat_…`) the user mints in the dashboard (Settings → Personal
   Access Tokens; shown exactly once) — and the URL to connect is the API
   host (e.g. `https://api.acme.purko.cloud`), which accepts only PAT bearer
   auth. PATs are scoped to the user's own workspace and die on suspension.
   First-time users with no workspace: the dashboard's welcome page
   self-provisions one (or `POST /api/workspace` — see references/api-remote.md).
   - If the URL starts with `http://` (not `https://`), warn clearly:
     "⚠ Cleartext HTTP — credentials will be transmitted unencrypted. Proceed only on a trusted local network or for local dev use."

2. Derive a short target name from the URL hostname (e.g. `acme.purko.cloud`).
   Upsert a remote target:
   ```
   python3 -c "import sys; sys.path.insert(0,'scripts'); import purko_config as pc; c = pc.load(); pc.upsert_target(c, '<name>', mode='remote', workspace_url='<url>'); pc.save(c)"
   ```
   The first target added becomes active automatically.

3. Start the local server with the remote upstream — **no port-forward** needed:
   `python3 scripts/serve.py --webapp webapp --upstream-url <url> --port <WEB>` (background; it prints its URL).

4. Auto-login: check the OS keychain for a previously saved token. If one exists, pipe it to `/local/token` (never pass as a shell argument):
     ```
     python3 -c "import sys, json; sys.path.insert(0,'scripts'); import keychain; t = keychain.load_token('<name>'); sys.exit(1) if not t else print(json.dumps({'token': t}))" \
       | curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:<WEB>/local/token \
           -H 'Content-Type: application/json' --data-binary @-
     ```
     204 → signed in. On non-204, fall back to the webapp paste form.
   - If no saved token, open the browser to the webapp — the token-paste form handles login.

5. **Offer to remember the token** (after a successful manual paste — NEVER store without asking):
   "Would you like me to save this token in your OS keychain so you don't need to paste it next time?"
   - If yes, instruct the local server to persist the token it already holds in memory — the token
     never appears on any command line, in shell history, or in a process argument list:
     ```
     curl -s -X POST http://127.0.0.1:<WEB>/local/token/remember \
       -H 'Content-Type: application/json' \
       -d '{"target": "<name>"}'
     ```
     A `204` response confirms the token was saved to the OS keychain (or file backend). A `409`
     means the server has no token yet — the user must paste the token first (step 3 of auto-login).
   - Note: the server holds the token in memory only. The `/local/token/remember` endpoint calls
     `keychain.save_token` in-process — no token on argv, no shell expansion, no history entry.
     The endpoint is POST, so the existing origin gate in `_origin_allowed` applies.
   - Never write the token to a file, env variable, or shell history.

6. Open the browser: `open http://127.0.0.1:<WEB>` (macOS) / `xdg-open` (Linux).
   Report: workspace URL, local server address, auth state, and the "Manage in
   dashboard →" link (the workspace URL serves the scoped per-user dashboard).

## Notes

- The local server binds 127.0.0.1 only. The dashboard token is held in server
  memory (never written to disk by the skill).
- Approve/Decline in the webapp works for operator/admin roles; in token mode
  the bearer token maps to admin.
- The legacy two-process form is DEPRECATED: its kubectl port-forward dies on
  every operator rollout and needs a manual restart (bit us repeatedly in live
  testing). If you find a running legacy stack (serve.py with --upstream-port),
  offer to restart it as pf-mode (`--pf-context <ctx>`), which self-heals.
  Legacy fallback: restart the tunnel manually; the webapp reconnects its
  event stream automatically.
- `mode='remote'` and `mode='hosted'` are aliases — both mean a remote HTTPS upstream
  accessed via `--upstream-url`. Use `'remote'` for new targets; `'hosted'` is kept for
  backwards compatibility with configs written before this change.

## Flow H — create an agent (guided)

> **Two transports.** Cluster mode (target.mode == "cluster") uses kubectl as
> written below. Remote/hosted targets use the dashboard REST API through the
> local server instead — load `references/api-remote.md` and substitute: live
> facts from the GET endpoints, create via `POST /api/create/agent`, and
> validation happens AT create (no dry-run over the API — on an AG-xxx error,
> explain, fix, re-POST). The interview (step 3) is identical in both modes.
> Default the namespace from `GET /api/whoami` `.workspace.namespace` when
> present.

**References (load before interviewing):** `references/agents.md` (archetypes,
fields, webhook rules), `references/PURKO_REF_VERSION` (reference SHA for skew
check).

### Steps

1. **Check target mode.** Resolve the active target with `purko_config.active_target`.
   If `target.mode` is `"remote"` or `"hosted"`, emit the message above and stop.

2. **Version skew check (non-gating).** Call
   `python3 -c "import sys; sys.path.insert(0,'scripts'); import cluster; print(cluster.crd_version('<ctx>'))"`.
   Compare with the `purko_sha=` line in `references/PURKO_REF_VERSION`. If they
   differ meaningfully, warn the user once:
   > "Note: bundled authoring guidance was generated from purko SHA `<ref_sha>` but
   > the cluster CRD reports `<live>`. Guidance may be slightly stale — live facts
   > (LLM providers, MCP servers) govern."
   Proceed regardless; the warning is informational only.

3. **Interview — ONE question at a time:**

   a. **Purpose / role.** "What should this agent do? (one sentence)"
      From the answer, suggest the nearest archetype from `references/agents.md`
      (Archetype Table), propose the matching `spec.type`, and name the closest
      template directory from `references/templates/<industry>/`. Wait for
      confirmation or redirection.

   b. **Model.** "Which model should this agent use?"
      Run `cluster.list_llmproviders(ctx)` — show **only** the returned names. Mark
      the one with `default: true` as "(default)". Never invent a provider name.
      If the list is empty: "No LLMProvider CRs found in `purko-system` — ask your
      admin to register a provider, then retry." and stop.
      Wait for the user to select a provider name and supply the model identifier
      (e.g. `claude-sonnet-4-6`).

   c. **Tools.** "Does this agent need external tools (MCP servers)?"
      Run `cluster.list_mcpservers(ctx)` — list returned names. Note that builtin
      tools (`search-knowledge`, `web-search`) are always available without an MCP
      server. Let the user choose zero or more from the live list plus builtins.

   d. **Memory.** "Does this agent need persistent memory across sessions?"
      - Yes → set `memory.behavior: persistent, scope: agent`. If the archetype is
        `retriever`, this also satisfies AG-010 (retriever requires memory).
      - No → omit memory block or set `memory.behavior: session` for within-session
        context retention.

   e. **Skills.** "Should this agent use any registered Skills?"
      Run `cluster.list_skills(ctx)` — list returned names. The user picks zero or
      more. Remind that AG-013 rejects refs to non-existent Skills (max 8 per AG-012).

   f. **Namespace.** "Which namespace? (default: `ai-agents`)"

4. **Draft the Agent YAML** using the field spec from `references/agents.md`:

   ```yaml
   apiVersion: purko.io/v1alpha1
   kind: Agent
   metadata:
     name: <name>
     namespace: <namespace>
   spec:
     type: <archetype-type>           # planner|executor|reviewer|router|monitor|retriever
     model:
       provider: <REAL-llmprovider-cr-name>   # from list_llmproviders — never invented
       name: <model-name>                      # e.g. claude-sonnet-4-6
     role: "<one-line role description>"
     systemPrompt: |
       <standing instructions for every invocation>
     autonomyLevel: <restricted|supervised|full>
     guardrails:
       costLimitUSD: 5.00             # number (float), never "$5.00" string
       humanApprovalRequired: false   # set true if step triggers irreversible action
     memory:
       behavior: session              # off | session | persistent
       scope: agent
     tools:
       - name: <mcp-server-name>
         type: mcp
       - name: search-knowledge
         type: builtin
     skills:
       - name: <skill-cr-name>
   ```

   Show the YAML along with a plain-language explanation for each choice:
   archetype rationale, why this autonomy level, whether a human-approval gate
   is recommended (see `references/agents.md` §humanApprovalRequired).

   **Hard rules from `references/agents.md`:**
   - `costLimitUSD` is a number (e.g. `5.00`), never a string like `"$5.00"` (string fails CRD type validation — use costLimitUSD number).
   - `model.provider` must be a real CR name from `list_llmproviders()`.
   - `retriever` type requires memory enabled (AG-010).
   - `monitor` type is limited to `replicas ≤ 1` (AG-005) — note this to the user.
   - Omit `spec.skills` array entirely if empty.

5. **Validate.** Call `cluster.dry_run_apply(ctx, yaml_text, namespace)`.
   - `ok=True` → proceed to step 6.
   - `ok=False` → parse the webhook error code (AG-001, AG-007, AG-010, AG-013…),
     explain the specific rule from `references/agents.md`, fix the field, re-draft,
     and **loop back to re-validate**. Never apply invalid YAML.

6. **Consent and apply.** Show the final YAML. Ask: "Apply this agent to the cluster?"
   On yes: call `cluster.apply_manifest(ctx, yaml_text, namespace)`.
   On failure: show the full error and stop.

7. **Wait for ready.** Call `cluster.wait_ready(ctx, "agent", name, namespace, timeout=60)`.
   - `ready=True` → "Agent `<name>` is Running in `<namespace>`."
   - `ready=False` → show the raw output of:
     ```
     kubectl --context <ctx> -n <namespace> get agent <name>
     kubectl --context <ctx> -n <namespace> describe agent <name>
     ```
     and tell the user to inspect events for startup errors.

8. **Offer Mission Control.** Say: "Your new agent `<name>` is visible in Mission
   Control under Agents. Open: `open http://127.0.0.1:8090#agent=<name>`" (or the
   current WEB URL if the stack is already running — serve.py may fall back to a
   random port — append `#agent=<name>` to deep-link directly to the new agent).

---

## Flow I — create a workflow (guided)

> **Two transports** — same as Flow H: cluster mode below; remote/hosted via
> `references/api-remote.md` (`POST /api/create/workflow`, flat step shape:
> `agent`/`input` strings; agent existence via `GET /api/agents`; validation
> at create).

**References:** `references/workflows.md` (CRD shape, interpolation, gates, triggers,
validation rules). `references/PURKO_REF_VERSION` for skew check (same non-gating
warning as Flow H step 2).

### Steps

1. **Check target mode.** Same as Flow H step 1.

2. **Version skew check** — same non-gating warning as Flow H step 2.

3. **Interview — ONE question at a time:**

   a. **Purpose.** "What should this workflow accomplish? (one sentence)"
      Suggest the closest showcase template from `references/templates/<industry>/`.

   b. **Agents.** "Which agents should the workflow use?"
      Run `kubectl --context <ctx> -n <namespace> get agents -o name` to list
      existing Agent CRs. **Every `agentRef.name` must exist in the namespace before
      applying the workflow (WF-005)** — if any named agent does not exist, alert
      the user and offer to create it first via Flow H.

   c. **Step structure.** Walk through each step one at a time: what it does, which
      agent runs it, what it depends on. Identify parallel fan-out (multiple steps
      sharing the same `dependsOn` entry run concurrently up to `spec.parallelism`).

   d. **Parameters.** "Which values should callers be able to vary per run?"
      These become `spec.parameters` entries. Reference them in `input.raw` as
      `${parameters.KEY}`.

   e. **Gates.** "Should any step pause for human approval before it runs?"
      If yes: the gate is set on the **agent** (`guardrails.humanApprovalRequired: true`),
      not on the step. If the agent already exists without a gate, offer to patch it
      via Flow J.

   f. **Trigger.** "Should this workflow run on a schedule, a webhook, or on-demand?"
      - Webhook → add `spec.trigger.type: webhook` with `webhook.secret.name`.
      - Schedule → add `spec.trigger.type: schedule` with `schedule.cron` (standard
        5-field UTC cron, e.g. `"0 8 * * 1"` = every Monday 08:00 UTC).
      - On-demand → omit `spec.trigger` entirely.

   g. **Namespace.** Default: `ai-agents`.

4. **Draft the Workflow YAML** using `references/workflows.md`:

   ```yaml
   apiVersion: purko.io/v1alpha1
   kind: Workflow
   metadata:
     name: <name>
     namespace: <namespace>
   spec:
     description: "<human summary>"
     parameters:
       param1: "default value"
     parallelism: 2
     failureStrategy: continueOnError
     steps:
       - name: step-one
         agentRef:
           name: <existing-agent-name>
         input:
           raw: |
             Task: ${parameters.param1}

       - name: step-two
         agentRef:
           name: <another-existing-agent>
         dependsOn: [step-one]
         input:
           raw: |
             Previous output: ${steps.step-one.output.response}

       - name: step-three
         agentRef:
           name: <another-existing-agent>
         dependsOn: [step-one]           # parallel to step-two (fan-out)

       - name: step-join
         agentRef:
           name: <final-agent>
         dependsOn: [step-two, step-three]   # fan-in
         input:
           raw: |
             Two: ${steps.step-two.output.response}
             Three: ${steps.step-three.output.response}
   ```

   Show the YAML with a plain-language explanation: dependency order, which paths
   run in parallel, where gates fire, how the workflow is triggered.

   **Hard rules from `references/workflows.md`:**
   - Use `${steps.X.output.response}` (with `.response`) — never bare `${steps.X.output}`
     (the controller does not substitute the bare form).
   - Every `agentRef.name` must exist as an Agent CR in the namespace (WF-005).
   - Step names: `[a-zA-Z0-9_.-]{1,63}`, unique within the workflow (WF-002).
   - `dependsOn` entries must reference existing steps; no self-dependency; no cycles
     (WF-003, WF-004).
   - Maximum 50 steps (WF-008).

5. **Validate.** Call `cluster.dry_run_apply(ctx, yaml_text, namespace)`.
   - `ok=True` → proceed.
   - `ok=False` → parse the WF-xxx error, explain from `references/workflows.md`,
     fix the field, re-draft, and **loop**. Never apply invalid YAML.

6. **Consent and apply.** Ask: "Apply this workflow to the cluster?"
   On yes: `cluster.apply_manifest(ctx, yaml_text, namespace)`.
   On failure: show the error and stop.

7. **Wait for ready.** Call `cluster.wait_ready(ctx, "workflow", name, namespace, timeout=60)`.
   Report real status. If not-ready:
   ```
   kubectl --context <ctx> -n <namespace> get workflow <name>
   kubectl --context <ctx> -n <namespace> describe workflow <name>
   ```

8. **Mission Control.** "Workflow `<name>` is now visible in Mission Control under
   Workflows. Open: `open http://127.0.0.1:8090#workflow=<name>`" (or the current
   WEB URL if the stack is already running — serve.py may fall back to a random port
   — append `#workflow=<name>` to deep-link directly to the new workflow).
   The workflow starts running as soon as it's applied — watch it in Mission Control.
   (A manual webhook trigger needs a configured trigger secret; see references/workflows.md.)

---

## Flow J — manage (edit / delete)

> **Two transports** — same as Flow H: cluster mode below; remote/hosted via
> `references/api-remote.md` (update/delete/rerun/approve endpoints; fetch the
> live object with `GET /api/agent/{name}` for the diff). The typed-confirmation
> rule for deletes applies in BOTH modes.

Use this flow for requests like "add tool X to agent Y", "put a gate before publish",
"raise the cost cap on agent Z", "delete agent foo", "remove workflow bar".

### Edit

1. **Identify the resource.** Parse the user's request for kind (Agent or Workflow),
   name, and namespace (default `ai-agents`).

2. **Fetch live YAML.**
   ```
   kubectl --context <ctx> -n <namespace> get <kind> <name> -o yaml
   ```

3. **Compute the minimal patch.** Change only the fields the user asked about.
   Show a unified diff between the original and the proposed YAML:
   ```diff
   --- original
   +++ proposed
   @@ ... @@
    unchanged context
   -  old: value
   +  new: value
    unchanged context
   ```

4. **Validate.** Call `cluster.dry_run_apply(ctx, patched_yaml, namespace)`.
   - `ok=False` → explain the error from the relevant reference file, fix, re-diff,
     and loop.

5. **Consent and apply.** "Apply this change?"
   On yes: `cluster.apply_manifest(ctx, patched_yaml, namespace)`.

6. Report the result. If it changed agent memory or skills, note any webhook rules
   (AG-010, AG-012, AG-013) that might be triggered.

### Delete

1. **Identify the resource** (kind, name, namespace).

2. **Show what will be deleted.**
   ```
   kubectl --context <ctx> -n <namespace> get <kind> <name>
   ```
   For Workflows: note that deleting a running workflow aborts all in-progress steps.
   For Agents: note any workflows that reference this agent — they will fail WF-005
   validation on their next apply.

3. **Require typed confirmation.** Ask the user to type the exact resource name:
   > "To confirm deletion, type the resource name exactly: `<name>`"
   Do **not** proceed until the user types it verbatim. Accept no abbreviation.

4. **Delete.**
   ```
   kubectl --context <ctx> -n <namespace> delete <kind> <name>
   ```
   Report the real output.

---

## Knowledge & version note

The guided flows use a **three-layer knowledge model**:

| Layer | Source | Role |
|-------|--------|------|
| **Judgment** | `references/agents.md`, `references/workflows.md`, `references/tools-mcp.md` | Archetypes, field semantics, webhook rules — generated from the purko SHA in `references/PURKO_REF_VERSION` |
| **Facts** | Live cluster (`cluster.list_llmproviders`, `cluster.list_mcpservers`, `cluster.list_skills`, `kubectl get`) | What actually exists: real LLMProvider names, registered MCP servers, Skill CRs, live Agent names |
| **Validation** | `cluster.dry_run_apply` | Purko's admission webhook — the authoritative gate before any apply |

**The skill never trusts bundled docs for live facts.** Provider names, MCP server
names, and Skill CR names must always come from live cluster queries. The `references/`
files give the *why*; the cluster gives the *what*.

**Version skew.** If `cluster.crd_version(ctx)` returns a value that does not match
the SHA in `references/PURKO_REF_VERSION`, warn the user once that the bundled guidance
may be stale, but proceed — live facts and the webhook's actual validation govern.
The mismatch is informational only and never blocks a flow.
