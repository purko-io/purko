package history

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func ts(t *testing.T, s string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return &parsed
}

func sampleRun(id, name, namespace string) *WorkflowRun {
	return &WorkflowRun{
		ID:         id,
		Name:       name,
		Namespace:  namespace,
		Phase:      "Pending",
		Parameters: `{"env":"prod"}`,
		TotalSteps: 3,
	}
}

func TestWorkflowRunCRUD(t *testing.T) {
	store := newTestStore(t)

	run := sampleRun("wf-a-run1", "wf-a", "ai-agents")
	run.StartTime = ts(t, "2026-07-04T10:00:00Z")
	if err := store.SaveWorkflowRun(run); err != nil {
		t.Fatalf("SaveWorkflowRun: %v", err)
	}

	got, err := store.GetWorkflowRun("wf-a-run1")
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if got.Name != "wf-a" || got.Namespace != "ai-agents" || got.Phase != "Pending" {
		t.Errorf("unexpected run: %+v", got)
	}
	if got.Parameters != `{"env":"prod"}` || got.TotalSteps != 3 {
		t.Errorf("unexpected parameters/steps: %+v", got)
	}
	if got.StartTime == nil || !got.StartTime.Equal(*run.StartTime) {
		t.Errorf("StartTime = %v, want %v", got.StartTime, run.StartTime)
	}
	if got.CompletionTime != nil {
		t.Errorf("CompletionTime should be nil, got %v", got.CompletionTime)
	}

	// Update to completed
	got.Phase = "Succeeded"
	got.CompletedSteps = 3
	got.CompletionTime = ts(t, "2026-07-04T10:05:00Z")
	got.Message = "all steps completed"
	if err := store.UpdateWorkflowRun(got); err != nil {
		t.Fatalf("UpdateWorkflowRun: %v", err)
	}
	updated, err := store.GetWorkflowRun("wf-a-run1")
	if err != nil {
		t.Fatalf("GetWorkflowRun after update: %v", err)
	}
	if updated.Phase != "Succeeded" || updated.CompletedSteps != 3 || updated.Message != "all steps completed" {
		t.Errorf("update not persisted: %+v", updated)
	}
	if updated.CompletionTime == nil {
		t.Error("CompletionTime not persisted")
	}
}

func TestGetWorkflowRunNotFound(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.GetWorkflowRun("nope"); err == nil {
		t.Fatal("expected error for missing run, got nil")
	}
}

func TestSaveWorkflowRunUpsertOnConflict(t *testing.T) {
	// Controller may reconcile the same workflow init twice — second save
	// must not fail on the primary key.
	store := newTestStore(t)
	run := sampleRun("wf-a-run1", "wf-a", "ai-agents")
	if err := store.SaveWorkflowRun(run); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := store.SaveWorkflowRun(run); err != nil {
		t.Fatalf("second save (upsert): %v", err)
	}
}

func TestListWorkflowRunsFilterAndPagination(t *testing.T) {
	store := newTestStore(t)
	for i, spec := range []struct{ id, name, ns string }{
		{"wf-a-run1", "wf-a", "ns1"},
		{"wf-a-run2", "wf-a", "ns1"},
		{"wf-b-run1", "wf-b", "ns1"},
		{"wf-c-run1", "wf-c", "ns2"},
	} {
		run := sampleRun(spec.id, spec.name, spec.ns)
		start := time.Date(2026, 7, 4, 10, i, 0, 0, time.UTC)
		run.StartTime = &start
		if err := store.SaveWorkflowRun(run); err != nil {
			t.Fatalf("save %s: %v", spec.id, err)
		}
	}

	all, err := store.ListWorkflowRuns(ListOptions{})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("expected 4 runs, got %d", len(all))
	}

	ns1, err := store.ListWorkflowRuns(ListOptions{Namespace: "ns1"})
	if err != nil {
		t.Fatalf("list ns1: %v", err)
	}
	if len(ns1) != 3 {
		t.Errorf("expected 3 runs in ns1, got %d", len(ns1))
	}

	byName, err := store.ListWorkflowRuns(ListOptions{Namespace: "ns1", Name: "wf-a"})
	if err != nil {
		t.Fatalf("list by name: %v", err)
	}
	if len(byName) != 2 {
		t.Errorf("expected 2 wf-a runs, got %d", len(byName))
	}

	page1, err := store.ListWorkflowRuns(ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("expected 2 runs with limit 2, got %d", len(page1))
	}
	page2, err := store.ListWorkflowRuns(ListOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("expected 2 runs on page 2, got %d", len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Error("pages overlap — offset not applied")
	}
}

func TestStepExecutionCRUDAndRetryIDs(t *testing.T) {
	store := newTestStore(t)
	run := sampleRun("wf-a-run1", "wf-a", "ns1")
	if err := store.SaveWorkflowRun(run); err != nil {
		t.Fatalf("save run: %v", err)
	}

	// Two retries of the same step must have distinct IDs (retry discriminator).
	for retry := 0; retry < 2; retry++ {
		step := &StepExecution{
			ID:            "wf-a-run1-build-" + string(rune('0'+retry)),
			WorkflowRunID: "wf-a-run1",
			StepName:      "build",
			AgentName:     "builder",
			Phase:         "Failed",
			Input:         `{"target":"all"}`,
			Output:        `{"response":"boom"}`,
			Error:         "exit 1",
			RetryCount:    retry,
			JobName:       "wf-a-build-job",
			TokensIn:      100,
			TokensOut:     50,
			CostUSD:       0.0123,
		}
		if err := store.SaveStepExecution(step); err != nil {
			t.Fatalf("save step retry %d: %v", retry, err)
		}
	}

	steps, err := store.ListStepExecutions("wf-a-run1")
	if err != nil {
		t.Fatalf("ListStepExecutions: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 step executions, got %d", len(steps))
	}

	got, err := store.GetStepExecution("wf-a-run1-build-1")
	if err != nil {
		t.Fatalf("GetStepExecution: %v", err)
	}
	if got.RetryCount != 1 || got.CostUSD != 0.0123 || got.TokensIn != 100 {
		t.Errorf("unexpected step: %+v", got)
	}

	got.Phase = "Succeeded"
	got.Error = ""
	if err := store.UpdateStepExecution(got); err != nil {
		t.Fatalf("UpdateStepExecution: %v", err)
	}
	updated, _ := store.GetStepExecution("wf-a-run1-build-1")
	if updated.Phase != "Succeeded" || updated.Error != "" {
		t.Errorf("update not persisted: %+v", updated)
	}
}

func TestToolCalls(t *testing.T) {
	store := newTestStore(t)
	if err := store.SaveWorkflowRun(sampleRun("wf-a-run1", "wf-a", "ns1")); err != nil {
		t.Fatalf("save run: %v", err)
	}
	step := &StepExecution{ID: "wf-a-run1-scan-0", WorkflowRunID: "wf-a-run1", StepName: "scan", Phase: "Succeeded"}
	if err := store.SaveStepExecution(step); err != nil {
		t.Fatalf("save step: %v", err)
	}

	calls := []ToolCall{
		{ToolName: "list_pods", ToolType: "mcp", ToolServer: "lumino", InputPreview: `{"namespace":"default"}`, ResultBytes: 2048, ElapsedMs: 350, AutonomyLevel: "shu"},
		{ToolName: "get_logs", ToolType: "mcp", ToolServer: "lumino", ResultBytes: 4096, ElapsedMs: 800},
	}
	if err := store.SaveToolCalls("wf-a-run1-scan-0", calls); err != nil {
		t.Fatalf("SaveToolCalls: %v", err)
	}

	got, err := store.ListToolCalls("wf-a-run1-scan-0")
	if err != nil {
		t.Fatalf("ListToolCalls: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(got))
	}
	if got[0].ToolName != "list_pods" || got[0].ToolServer != "lumino" || got[0].ElapsedMs != 350 {
		t.Errorf("unexpected tool call: %+v", got[0])
	}
	if got[0].ID == 0 || got[1].ID == 0 {
		t.Error("tool call IDs not assigned")
	}

	// Empty save is a no-op, not an error.
	if err := store.SaveToolCalls("wf-a-run1-scan-0", nil); err != nil {
		t.Fatalf("SaveToolCalls(nil): %v", err)
	}
}

func TestDeleteOlderThanCascades(t *testing.T) {
	store := newTestStore(t)

	old := sampleRun("wf-old-run1", "wf-old", "ns1")
	if err := store.SaveWorkflowRun(old); err != nil {
		t.Fatalf("save old: %v", err)
	}
	// Backdate created_at beyond retention.
	if err := store.(*SQLiteStore).setCreatedAtForTest("wf-old-run1", time.Now().UTC().AddDate(0, 0, -30)); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	step := &StepExecution{ID: "wf-old-run1-s1-0", WorkflowRunID: "wf-old-run1", StepName: "s1", Phase: "Succeeded"}
	if err := store.SaveStepExecution(step); err != nil {
		t.Fatalf("save step: %v", err)
	}
	if err := store.SaveToolCalls("wf-old-run1-s1-0", []ToolCall{{ToolName: "t"}}); err != nil {
		t.Fatalf("save tools: %v", err)
	}

	fresh := sampleRun("wf-new-run1", "wf-new", "ns1")
	if err := store.SaveWorkflowRun(fresh); err != nil {
		t.Fatalf("save fresh: %v", err)
	}

	deleted, err := store.DeleteOlderThan(7)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted run, got %d", deleted)
	}

	if _, err := store.GetWorkflowRun("wf-old-run1"); err == nil {
		t.Error("old run should be deleted")
	}
	if _, err := store.GetWorkflowRun("wf-new-run1"); err != nil {
		t.Errorf("fresh run should survive: %v", err)
	}
	steps, err := store.ListStepExecutions("wf-old-run1")
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("step executions should cascade-delete, got %d", len(steps))
	}
	tools, err := store.ListToolCalls("wf-old-run1-s1-0")
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("tool calls should cascade-delete, got %d", len(tools))
	}
}

func TestWALModeAndForeignKeysEnabled(t *testing.T) {
	store := newTestStore(t).(*SQLiteStore)
	var mode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var fk int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.SaveWorkflowRun(sampleRun("wf-a-run1", "wf-a", "ns1")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	got, err := reopened.GetWorkflowRun("wf-a-run1")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got.Name != "wf-a" {
		t.Errorf("unexpected run after reopen: %+v", got)
	}
}
