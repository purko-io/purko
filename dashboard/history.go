package dashboard

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/purko-io/purko/pkg/history"
)

// History API (Spec 24) — read-only endpoints backed by the SQLite archive.
// Separate from /api/workflow/* which reads live state from the K8s API.

const defaultHistoryLimit = 50

type historyRunJSON struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Namespace      string     `json:"namespace"`
	Phase          string     `json:"phase"`
	Parameters     string     `json:"parameters,omitempty"`
	TotalSteps     int        `json:"totalSteps"`
	CompletedSteps int        `json:"completedSteps"`
	FailedSteps    int        `json:"failedSteps"`
	StartTime      *time.Time `json:"startTime,omitempty"`
	CompletionTime *time.Time `json:"completionTime,omitempty"`
	Message        string     `json:"message,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type historyStepJSON struct {
	ID             string     `json:"id"`
	WorkflowRunID  string     `json:"workflowRunId"`
	StepName       string     `json:"stepName"`
	AgentName      string     `json:"agentName,omitempty"`
	Phase          string     `json:"phase"`
	Input          string     `json:"input,omitempty"`
	Output         string     `json:"output,omitempty"`
	Error          string     `json:"error,omitempty"`
	RetryCount     int        `json:"retryCount"`
	JobName        string     `json:"jobName,omitempty"`
	StartTime      *time.Time `json:"startTime,omitempty"`
	CompletionTime *time.Time `json:"completionTime,omitempty"`
	TokensIn       int        `json:"tokensIn"`
	TokensOut      int        `json:"tokensOut"`
	CostUSD        float64    `json:"costUsd"`
}

type historyToolCallJSON struct {
	ID              int64     `json:"id"`
	StepExecutionID string    `json:"stepExecutionId"`
	ToolName        string    `json:"toolName"`
	ToolType        string    `json:"toolType,omitempty"`
	ToolServer      string    `json:"toolServer,omitempty"`
	InputPreview    string    `json:"inputPreview,omitempty"`
	ResultBytes     int       `json:"resultBytes"`
	ElapsedMs       int       `json:"elapsedMs"`
	AutonomyLevel   string    `json:"autonomyLevel,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

func runToJSON(run *history.WorkflowRun) historyRunJSON {
	return historyRunJSON{
		ID: run.ID, Name: run.Name, Namespace: run.Namespace, Phase: run.Phase,
		Parameters: run.Parameters, TotalSteps: run.TotalSteps,
		CompletedSteps: run.CompletedSteps, FailedSteps: run.FailedSteps,
		StartTime: run.StartTime, CompletionTime: run.CompletionTime,
		Message: run.Message, CreatedAt: run.CreatedAt,
	}
}

func stepToJSON(step *history.StepExecution) historyStepJSON {
	return historyStepJSON{
		ID: step.ID, WorkflowRunID: step.WorkflowRunID, StepName: step.StepName,
		AgentName: step.AgentName, Phase: step.Phase, Input: step.Input,
		Output: step.Output, Error: step.Error, RetryCount: step.RetryCount,
		JobName: step.JobName, StartTime: step.StartTime,
		CompletionTime: step.CompletionTime, TokensIn: step.TokensIn,
		TokensOut: step.TokensOut, CostUSD: step.CostUSD,
	}
}

func toolCallToJSON(call *history.ToolCall) historyToolCallJSON {
	return historyToolCallJSON{
		ID: call.ID, StepExecutionID: call.StepExecutionID, ToolName: call.ToolName,
		ToolType: call.ToolType, ToolServer: call.ToolServer,
		InputPreview: call.InputPreview, ResultBytes: call.ResultBytes,
		ElapsedMs: call.ElapsedMs, AutonomyLevel: call.AutonomyLevel,
		Timestamp: call.Timestamp,
	}
}

// historyEnabled writes a 503 and returns false when no history store is configured.
func (s *Server) historyEnabled(w http.ResponseWriter) bool {
	if s.History == nil {
		http.Error(w, "execution history is not enabled", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// handleHistoryRuns serves GET /api/history/runs?namespace=X&name=X&limit=N&offset=N
func (s *Server) handleHistoryRuns(w http.ResponseWriter, r *http.Request) {
	if !s.historyEnabled(w) {
		return
	}
	q := r.URL.Query()
	opts := history.ListOptions{
		Namespace: q.Get("namespace"),
		Name:      q.Get("name"),
		Limit:     defaultHistoryLimit,
	}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		opts.Limit = v
	}
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v > 0 {
		opts.Offset = v
	}
	runs, err := s.History.ListWorkflowRuns(opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]historyRunJSON, 0, len(runs))
	for i := range runs {
		out = append(out, runToJSON(&runs[i]))
	}
	writeJSON(w, out)
}

// handleHistoryRunDetail serves:
//
//	GET /api/history/run/{id}        — run with step details
//	GET /api/history/run/{id}/steps  — step executions only
func (s *Server) handleHistoryRunDetail(w http.ResponseWriter, r *http.Request) {
	if !s.historyEnabled(w) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/history/run/")
	stepsOnly := false
	if strings.HasSuffix(path, "/steps") {
		stepsOnly = true
		path = strings.TrimSuffix(path, "/steps")
	}
	if path == "" {
		http.Error(w, "run id required", http.StatusBadRequest)
		return
	}

	steps, err := s.History.ListStepExecutions(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stepsJSON := make([]historyStepJSON, 0, len(steps))
	for i := range steps {
		stepsJSON = append(stepsJSON, stepToJSON(&steps[i]))
	}

	if stepsOnly {
		writeJSON(w, stepsJSON)
		return
	}

	run, err := s.History.GetWorkflowRun(path)
	if err != nil {
		http.Error(w, "workflow run not found: "+path, http.StatusNotFound)
		return
	}
	writeJSON(w, struct {
		historyRunJSON
		Steps []historyStepJSON `json:"steps"`
	}{runToJSON(run), stepsJSON})
}

// handleHistoryStepTools serves GET /api/history/step/{id}/tools
func (s *Server) handleHistoryStepTools(w http.ResponseWriter, r *http.Request) {
	if !s.historyEnabled(w) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/history/step/")
	id := strings.TrimSuffix(path, "/tools")
	if id == "" || id == path {
		http.Error(w, "expected /api/history/step/{id}/tools", http.StatusBadRequest)
		return
	}
	calls, err := s.History.ListToolCalls(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]historyToolCallJSON, 0, len(calls))
	for i := range calls {
		out = append(out, toolCallToJSON(&calls[i]))
	}
	writeJSON(w, out)
}
