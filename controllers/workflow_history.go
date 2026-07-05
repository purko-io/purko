package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/history"
)

// History write errors are logged but never block workflow execution —
// history is an audit concern, not a runtime dependency (Spec 24).

// historyRunID returns the workflow_runs primary key: <workflow-name>-<run-id>.
// Empty until the run-id annotation has been assigned.
func historyRunID(wf *v1alpha1.Workflow) string {
	runID := wf.Annotations["purko.io/run-id"]
	if runID == "" {
		return ""
	}
	return wf.Name + "-" + runID
}

// recordWorkflowHistory upserts the workflow run row from the current status.
// Called on init and on every phase transition (via setPhase).
func (r *WorkflowReconciler) recordWorkflowHistory(ctx context.Context, wf *v1alpha1.Workflow) {
	if r.HistoryStore == nil {
		return
	}
	id := historyRunID(wf)
	if id == "" {
		return
	}
	var params string
	if len(wf.Spec.Parameters) > 0 {
		if b, err := json.Marshal(wf.Spec.Parameters); err == nil {
			params = string(b)
		}
	}
	run := &history.WorkflowRun{
		ID:             id,
		Name:           wf.Name,
		Namespace:      wf.Namespace,
		Phase:          wf.Status.Phase,
		Parameters:     params,
		TotalSteps:     wf.Status.TotalSteps,
		CompletedSteps: wf.Status.CompletedSteps,
		FailedSteps:    wf.Status.FailedSteps,
		StartTime:      metaTimePtr(wf.Status.StartTime),
		CompletionTime: metaTimePtr(wf.Status.CompletionTime),
		Message:        wf.Status.Message,
	}
	if err := r.HistoryStore.SaveWorkflowRun(run); err != nil {
		log.FromContext(ctx).Error(err, "Failed to save workflow run to history", "run", id)
	}
}

// recordStepHistory records a completed (succeeded or terminally failed) step
// execution and its tool call audit trail.
func (r *WorkflowReconciler) recordStepHistory(ctx context.Context, wf *v1alpha1.Workflow, ss *v1alpha1.StepStatus, agentName string, output json.RawMessage) {
	if r.HistoryStore == nil {
		return
	}
	runID := historyRunID(wf)
	if runID == "" {
		return
	}
	logger := log.FromContext(ctx)

	var metrics struct {
		Metrics struct {
			TokensIn  int     `json:"tokens_in"`
			TokensOut int     `json:"tokens_out"`
			CostUSD   float64 `json:"cost_usd"`
		} `json:"_metrics"`
	}
	if len(output) > 0 {
		_ = json.Unmarshal(output, &metrics)
	}

	step := &history.StepExecution{
		ID:             fmt.Sprintf("%s-%s-%d", runID, ss.Name, ss.RetryCount),
		WorkflowRunID:  runID,
		StepName:       ss.Name,
		AgentName:      agentName,
		Phase:          ss.Phase,
		Output:         string(output),
		Error:          ss.Error,
		RetryCount:     ss.RetryCount,
		JobName:        ss.JobName,
		StartTime:      metaTimePtr(ss.StartTime),
		CompletionTime: metaTimePtr(ss.CompletionTime),
		TokensIn:       metrics.Metrics.TokensIn,
		TokensOut:      metrics.Metrics.TokensOut,
		CostUSD:        metrics.Metrics.CostUSD,
	}
	if err := r.HistoryStore.SaveStepExecution(step); err != nil {
		logger.Error(err, "Failed to save step execution to history", "step", step.ID)
		return
	}
	if toolCalls := parseToolCalls(output); len(toolCalls) > 0 {
		if err := r.HistoryStore.SaveToolCalls(step.ID, toolCalls); err != nil {
			logger.Error(err, "Failed to save tool calls to history", "step", step.ID)
		}
	}
}

// parseToolCalls extracts the executor's tool_call_log entries from step
// output. Blocked calls are skipped; previews are capped at 500 chars.
func parseToolCalls(output json.RawMessage) []history.ToolCall {
	if len(output) == 0 {
		return nil
	}
	var parsed struct {
		ToolCallLog []struct {
			Tool         string  `json:"tool"`
			Server       string  `json:"server"`
			ElapsedSec   float64 `json:"elapsed_s"`
			InputPreview string  `json:"input_preview"`
			ResultBytes  int     `json:"result_bytes"`
			Status       string  `json:"status"`
		} `json:"tool_call_log"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil
	}
	var calls []history.ToolCall
	for _, tc := range parsed.ToolCallLog {
		if tc.Status == "blocked" {
			continue
		}
		preview := tc.InputPreview
		if len(preview) > 500 {
			preview = preview[:500]
		}
		calls = append(calls, history.ToolCall{
			ToolName:     tc.Tool,
			ToolServer:   tc.Server,
			ToolType:     "mcp",
			InputPreview: preview,
			ResultBytes:  tc.ResultBytes,
			ElapsedMs:    int(tc.ElapsedSec * 1000),
		})
	}
	return calls
}

func metaTimePtr(t *metav1.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.Time.UTC()
	return &utc
}
