package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/memory"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// memoryBehavior returns the effective behavior for the NEW provider path, or ""
// when the agent should keep its legacy code path (Spec 34 §1: the new path
// activates ONLY when behavior is explicitly set; legacy type never switches path).
func memoryBehavior(agent *v1alpha1.Agent) string {
	if agent.Spec.Memory == nil {
		return "session" // memory unset == today's default (buffer/session)
	}
	if b := agent.Spec.Memory.Behavior; b != "" {
		return b
	}
	if agent.Spec.Memory.Type != "" {
		return "" // legacy type set, no behavior -> legacy path, not the new one
	}
	return "session" // memory present, nothing set -> session
}

// memoryEnvType returns the MEMORY_TYPE value (Spec 34 §1 env mapping). Behavior
// path: off->none, session->buffer, persistent->summary. Legacy path (behavior "")
// returns the raw legacy Type (default buffer), so vector/summary keep their paths.
func memoryEnvType(agent *v1alpha1.Agent) string {
	switch memoryBehavior(agent) {
	case "off":
		return "none"
	case "session":
		return "buffer"
	case "persistent":
		return "summary"
	default: // legacy path
		if agent.Spec.Memory != nil && agent.Spec.Memory.Type != "" {
			return agent.Spec.Memory.Type
		}
		return "buffer"
	}
}

// memoryScopeKey computes the recall pool key. Controller-owned, never trusted from
// the executor (Spec 34 §1/§6). group derives from app.kubernetes.io/component.
//
// When scope is "group" but the app.kubernetes.io/component label is missing or
// empty, we fall back to agent scope rather than emit "ns/group/" — that key would
// be ONE shared pool for every label-less group-scoped agent in the namespace
// (cross-agent memory bleed). The admission webhook (Task 12) will require the
// label, but the controller must be safe standalone: pre-webhook installs and
// webhook-bypassed CRs still reach this code.
func (r *WorkflowReconciler) memoryScopeKey(agent *v1alpha1.Agent, wf *v1alpha1.Workflow) string {
	ns := wf.Namespace
	scope := "agent"
	if agent.Spec.Memory != nil && agent.Spec.Memory.Scope != "" {
		scope = agent.Spec.Memory.Scope
	}
	switch scope {
	case "group":
		if group := agent.Labels["app.kubernetes.io/component"]; group != "" {
			return fmt.Sprintf("%s/group/%s", ns, group)
		}
		return fmt.Sprintf("%s/agent/%s", ns, agent.Name)
	case "namespace":
		return fmt.Sprintf("%s/%s", ns, ns)
	default:
		return fmt.Sprintf("%s/agent/%s", ns, agent.Name)
	}
}

// resolveMemoryRetention returns the per-scope cap from the platform MemoryProvider
// (default provider or providerRef), defaulting to 500 (Spec 34 §5). Providers live
// in purko-system like LLMProviders. Missing provider = built-in defaults.
func (r *WorkflowReconciler) resolveMemoryRetention(ctx context.Context, providerRef string) int {
	maxEntries := 500
	// Test-only reachability: production reconcilers always carry a client; unit
	// tests wiring only a Memory store get the documented builtin default (500).
	if r.Client == nil {
		return maxEntries
	}
	var providers v1alpha1.MemoryProviderList
	if err := r.List(ctx, &providers, client.InNamespace("purko-system")); err != nil {
		return maxEntries
	}
	var chosen *v1alpha1.MemoryProvider
	for i := range providers.Items {
		if providers.Items[i].Name == providerRef {
			chosen = &providers.Items[i]
			break
		}
		if providers.Items[i].Spec.Default {
			chosen = &providers.Items[i]
		}
	}
	if chosen != nil && chosen.Spec.Retention != nil && chosen.Spec.Retention.MaxEntriesPerScope != nil {
		maxEntries = *chosen.Spec.Retention.MaxEntriesPerScope
	}
	return maxEntries
}

// recallMemory runs recall for a persistent-behavior step: queries the provider,
// records recall_log (keyed per run/step for the transparency panel), and returns
// the formatted AGENT_MEMORY block. Advisory — any error logs + emits an event and
// returns "" so the run proceeds (Spec 34 §3/§4).
//
// Gating on behavior=="persistent" is the CALLER's job (the job builder, Task 8);
// this helper does not re-check behavior and will recall for whoever calls it.
func (r *WorkflowReconciler) recallMemory(ctx context.Context, agent *v1alpha1.Agent, wf *v1alpha1.Workflow, runID, step, stepInput string) string {
	if r.Memory == nil {
		return ""
	}
	logger := log.FromContext(ctx)
	scopeKey := r.memoryScopeKey(agent, wf)
	maxTokens := 2048
	if agent.Spec.Memory != nil && agent.Spec.Memory.MaxContextTokens != nil {
		maxTokens = *agent.Spec.Memory.MaxContextTokens
	}
	entries, err := r.Memory.Recall(ctx, wf.Namespace, scopeKey, stepInput, maxTokens)
	if err != nil {
		logger.Error(err, "memory recall failed — proceeding without recalled context", "agent", agent.Name)
		r.memoryEvent(wf, corev1.EventTypeWarning, "MemoryRecallFailed", fmt.Sprintf("recall for step %s failed: %v", step, err))
		return ""
	}
	if len(entries) == 0 {
		return ""
	}
	ids := make([]string, len(entries))
	for i := range entries {
		ids[i] = entries[i].ID
	}
	if err := r.Memory.WriteRecallLog(ctx, wf.Namespace, runID, step, ids); err != nil {
		logger.Error(err, "recall_log write failed (transparency panel will miss this step)")
	}
	return formatRecallBlock(entries)
}

// formatRecallBlock renders recalled entries with provenance headers (Spec 34 §3).
func formatRecallBlock(entries []memory.Entry) string {
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		date := e.CreatedAt.Format("2006-01-02")
		fmt.Fprintf(&b, "[%s %s/%s] %s", date, e.Workflow, e.Step, e.Content)
	}
	return b.String()
}

// memoryEvent emits a K8s event on the Workflow. Guarded so tests with a nil
// recorder don't panic.
func (r *WorkflowReconciler) memoryEvent(wf *v1alpha1.Workflow, eventType, reason, msg string) {
	if r.Recorder != nil {
		r.Recorder.Event(wf, eventType, reason, msg)
	}
}

// persistMemory stores one memory entry for a persistent-behavior step (Spec 34
// §3). Uses the executor's _memory_update when present; otherwise auto-summarizes
// from input+response so memory works even with executors that never heard of it.
// Advisory: Learn errors log + event, step still succeeds. Evicts over the cap.
func (r *WorkflowReconciler) persistMemory(ctx context.Context, agent *v1alpha1.Agent, wf *v1alpha1.Workflow, step, memoryUpdate, stepInput, response string) {
	if r.Memory == nil {
		return
	}
	logger := log.FromContext(ctx)
	content := memoryUpdate
	// Defensive: older executors prefix _memory_update with [workflow/step];
	// strip it so it isn't double-labeled by formatRecallBlock at recall
	// (the controller stores workflow/step structurally). New executors emit
	// no prefix.
	content = strings.TrimPrefix(content, fmt.Sprintf("[%s/%s] ", wf.Name, step))
	if content == "" {
		// Auto-summary fallback. The completion path does not retain the resolved
		// step input (StepStatus has no Input field), so callers there pass ""
		// and we label the task with the step name; callers that do have the input
		// (or tests) pass it and it is used verbatim.
		task := stepInput
		if task == "" {
			task = step
		}
		content = fmt.Sprintf("Task: %s | Result: %s", truncate(task, 200), truncate(response, 500))
	}
	scopeKey := r.memoryScopeKey(agent, wf)
	entry := memory.Entry{
		Namespace: wf.Namespace,
		ScopeKey:  scopeKey,
		Agent:     agent.Name,
		Workflow:  wf.Name,
		Step:      step,
		Content:   content,
	}
	if err := r.Memory.Learn(ctx, entry); err != nil {
		logger.Error(err, "memory learn failed — step still succeeded", "agent", agent.Name)
		r.memoryEvent(wf, corev1.EventTypeWarning, "MemoryLearnFailed", fmt.Sprintf("persist for step %s failed: %v", step, err))
		return
	}
	providerRef := ""
	if agent.Spec.Memory != nil {
		providerRef = agent.Spec.Memory.ProviderRef
	}
	if _, err := r.Memory.Retain(ctx, scopeKey, r.resolveMemoryRetention(ctx, providerRef)); err != nil {
		logger.Error(err, "memory retention eviction failed (non-fatal)")
	}
}

// extractResponseText returns the first non-empty of response/result/output from a
// step output map (Spec 34 §3 auto-summary). Values are JSON-encoded; unquote
// strings, fall back to the raw JSON for non-string shapes.
func extractResponseText(outputMap map[string]json.RawMessage) string {
	for _, k := range []string{"response", "result", "output"} {
		raw, ok := outputMap[k]
		if !ok || len(raw) == 0 {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if s != "" {
				return s
			}
			continue
		}
		return string(raw)
	}
	return ""
}

// importLegacyMemory one-shot-migrates an agent's legacy <agent>-memory ConfigMap
// (key summary) into the provider on its first persistent run, then annotates the
// Agent so it never re-imports (Spec 34 §9). The ConfigMap is left in place.
func (r *WorkflowReconciler) importLegacyMemory(ctx context.Context, agent *v1alpha1.Agent, wf *v1alpha1.Workflow) {
	if r.Memory == nil || agent.Annotations["purko.io/memory-imported"] == "true" {
		return
	}
	logger := log.FromContext(ctx)
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: agent.Name + "-memory", Namespace: wf.Namespace}, cm)
	if apierrors.IsNotFound(err) {
		// Nothing to import; still mark so we don't re-check every run.
		r.markMemoryImported(ctx, agent, wf)
		return
	}
	if err != nil {
		return // transient; retry next run
	}
	if summary := cm.Data["summary"]; summary != "" {
		entry := memory.Entry{
			Namespace: wf.Namespace,
			// Same scope key as recall on purpose: the imported entry must land
			// in the pool this agent recalls from (agent/group/namespace).
			ScopeKey: r.memoryScopeKey(agent, wf),
			Agent:    agent.Name,
			Workflow: wf.Name,
			Step:     "imported",
			Content:  summary,
		}
		if err := r.Memory.Learn(ctx, entry); err != nil {
			logger.Error(err, "legacy memory import failed (will retry next run)", "agent", agent.Name)
			r.memoryEvent(wf, corev1.EventTypeWarning, "MemoryImportFailed", fmt.Sprintf("legacy import for agent %s failed: %v", agent.Name, err))
			return
		}
		logger.Info("Imported legacy summary memory", "agent", agent.Name)
	}
	r.markMemoryImported(ctx, agent, wf)
}

// markMemoryImported sets the idempotency annotation via an annotation-only
// merge patch: unlike r.Update, MergeFrom carries no resourceVersion
// precondition, so an earlier Status().Update on the same cached agent in this
// reconcile cannot 409 the marker write (an unmarked import would re-run next
// reconcile and store a duplicate entry). Patch failure is advisory (log +
// Warning event). The in-memory annotation deliberately stays set on failure so
// the rest of THIS reconcile skips re-import; the next reconcile re-fetches the
// un-annotated agent and retries — one duplicate entry in that failure window
// is the accepted residual.
func (r *WorkflowReconciler) markMemoryImported(ctx context.Context, agent *v1alpha1.Agent, wf *v1alpha1.Workflow) {
	// Agent has no generated DeepCopy() *Agent (bare .DeepCopy() would promote
	// ObjectMeta's); go through DeepCopyObject.
	orig := agent.DeepCopyObject().(*v1alpha1.Agent)
	if agent.Annotations == nil {
		agent.Annotations = map[string]string{}
	}
	agent.Annotations["purko.io/memory-imported"] = "true"
	if err := r.Patch(ctx, agent, client.MergeFrom(orig)); err != nil {
		log.FromContext(ctx).Error(err, "failed to set memory-imported annotation", "agent", agent.Name)
		r.memoryEvent(wf, corev1.EventTypeWarning, "MemoryImportFailed", fmt.Sprintf("memory-imported annotation patch for agent %s failed: %v", agent.Name, err))
	}
}

// truncate cuts s to at most n bytes without splitting a UTF-8 rune: the cut
// backs up to the previous rune boundary, so the budgets (200/500) remain byte
// ceilings while the output stays valid UTF-8.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
