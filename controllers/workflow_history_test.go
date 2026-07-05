package controllers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/history"
)

func newHistoryReconciler(t *testing.T) (*WorkflowReconciler, history.Store) {
	t.Helper()
	store, err := history.NewSQLiteStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &WorkflowReconciler{HistoryStore: store}, store
}

func historyTestWorkflow() *v1alpha1.Workflow {
	wf := &v1alpha1.Workflow{}
	wf.Name = "deploy-app"
	wf.Namespace = "ai-agents"
	wf.Annotations = map[string]string{"purko.io/run-id": "abc123"}
	wf.Spec.Parameters = map[string]string{"env": "prod"}
	wf.Spec.Steps = []v1alpha1.WorkflowStep{{Name: "build"}, {Name: "deploy"}}
	wf.Status.Phase = "Pending"
	wf.Status.TotalSteps = 2
	now := metav1.Now()
	wf.Status.StartTime = &now
	return wf
}

func TestParseToolCalls(t *testing.T) {
	output := json.RawMessage(`{
		"response": "done",
		"tool_call_log": [
			{"tool": "list_pods", "server": "lumino", "elapsed_s": 0.4, "input_preview": "{\"ns\":\"default\"}", "result_bytes": 2048},
			{"tool": "delete_pod", "server": "lumino", "status": "blocked", "reason": "autonomy"},
			{"tool": "get_logs", "server": "lumino", "elapsed_s": 1.5, "input_preview": "` + strings.Repeat("x", 600) + `", "result_bytes": 10}
		]
	}`)

	calls := parseToolCalls(output)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (blocked skipped), got %d", len(calls))
	}
	if calls[0].ToolName != "list_pods" || calls[0].ToolServer != "lumino" {
		t.Errorf("unexpected first call: %+v", calls[0])
	}
	if calls[0].ElapsedMs != 400 {
		t.Errorf("ElapsedMs = %d, want 400", calls[0].ElapsedMs)
	}
	if calls[0].ResultBytes != 2048 {
		t.Errorf("ResultBytes = %d, want 2048", calls[0].ResultBytes)
	}
	if len(calls[1].InputPreview) > 500 {
		t.Errorf("InputPreview not truncated: %d chars", len(calls[1].InputPreview))
	}
}

func TestParseToolCallsInvalid(t *testing.T) {
	if calls := parseToolCalls(json.RawMessage(`not json`)); calls != nil {
		t.Errorf("expected nil for invalid JSON, got %v", calls)
	}
	if calls := parseToolCalls(json.RawMessage(`{"response":"no tools"}`)); calls != nil {
		t.Errorf("expected nil when tool_call_log missing, got %v", calls)
	}
}

func TestRecordWorkflowHistory(t *testing.T) {
	r, store := newHistoryReconciler(t)
	wf := historyTestWorkflow()

	r.recordWorkflowHistory(context.Background(), wf)

	run, err := store.GetWorkflowRun("deploy-app-abc123")
	if err != nil {
		t.Fatalf("run not recorded: %v", err)
	}
	if run.Phase != "Pending" || run.Namespace != "ai-agents" || run.TotalSteps != 2 {
		t.Errorf("unexpected run: %+v", run)
	}
	if !strings.Contains(run.Parameters, `"env":"prod"`) {
		t.Errorf("parameters not recorded: %s", run.Parameters)
	}

	// Terminal transition updates the same row.
	wf.Status.Phase = "Succeeded"
	wf.Status.CompletedSteps = 2
	wf.Status.Message = "All workflow steps completed"
	done := metav1.Now()
	wf.Status.CompletionTime = &done
	r.recordWorkflowHistory(context.Background(), wf)

	run, err = store.GetWorkflowRun("deploy-app-abc123")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if run.Phase != "Succeeded" || run.CompletedSteps != 2 || run.CompletionTime == nil {
		t.Errorf("terminal update not recorded: %+v", run)
	}
}

func TestRecordWorkflowHistoryNoStore(t *testing.T) {
	r := &WorkflowReconciler{} // HistoryStore nil — must not panic
	r.recordWorkflowHistory(context.Background(), historyTestWorkflow())
}

func TestRecordWorkflowHistoryNoRunID(t *testing.T) {
	r, store := newHistoryReconciler(t)
	wf := historyTestWorkflow()
	wf.Annotations = nil // run-id not yet assigned

	r.recordWorkflowHistory(context.Background(), wf)

	runs, err := store.ListWorkflowRuns(history.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected no runs without run-id, got %d", len(runs))
	}
}

func TestRecordStepHistory(t *testing.T) {
	r, store := newHistoryReconciler(t)
	wf := historyTestWorkflow()
	r.recordWorkflowHistory(context.Background(), wf)

	output := json.RawMessage(`{
		"response": "built",
		"_metrics": {"tokens_in": 120, "tokens_out": 80, "cost_usd": 0.05},
		"tool_call_log": [{"tool": "run_build", "server": "ci", "elapsed_s": 2.0, "result_bytes": 5}]
	}`)
	start := metav1.Now()
	done := metav1.Now()
	ss := &v1alpha1.StepStatus{
		Name:           "build",
		Phase:          "Succeeded",
		JobName:        "deploy-app-build-job",
		RetryCount:     1,
		StartTime:      &start,
		CompletionTime: &done,
		Output:         &runtime.RawExtension{Raw: output},
	}

	r.recordStepHistory(context.Background(), wf, ss, "builder", output)

	step, err := store.GetStepExecution("deploy-app-abc123-build-1")
	if err != nil {
		t.Fatalf("step not recorded: %v", err)
	}
	if step.AgentName != "builder" || step.Phase != "Succeeded" || step.RetryCount != 1 {
		t.Errorf("unexpected step: %+v", step)
	}
	if step.TokensIn != 120 || step.TokensOut != 80 || step.CostUSD != 0.05 {
		t.Errorf("metrics not recorded: %+v", step)
	}
	if step.JobName != "deploy-app-build-job" {
		t.Errorf("job name not recorded: %+v", step)
	}

	tools, err := store.ListToolCalls("deploy-app-abc123-build-1")
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].ToolName != "run_build" || tools[0].ElapsedMs != 2000 {
		t.Errorf("tool calls not recorded: %+v", tools)
	}
}

func TestRecordStepHistoryFailedStep(t *testing.T) {
	r, store := newHistoryReconciler(t)
	wf := historyTestWorkflow()
	r.recordWorkflowHistory(context.Background(), wf)

	ss := &v1alpha1.StepStatus{
		Name:       "deploy",
		Phase:      "Failed",
		Error:      "exit code 1",
		RetryCount: 0,
	}
	r.recordStepHistory(context.Background(), wf, ss, "deployer", nil)

	step, err := store.GetStepExecution("deploy-app-abc123-deploy-0")
	if err != nil {
		t.Fatalf("failed step not recorded: %v", err)
	}
	if step.Phase != "Failed" || step.Error != "exit code 1" {
		t.Errorf("unexpected step: %+v", step)
	}
}
