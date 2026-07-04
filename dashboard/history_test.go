package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/purko-io/purko/pkg/history"
)

func newHistoryServer(t *testing.T) (*Server, history.Store) {
	t.Helper()
	store, err := history.NewSQLiteStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &Server{History: store}, store
}

func seedHistory(t *testing.T, store history.Store) {
	t.Helper()
	for _, run := range []*history.WorkflowRun{
		{ID: "wf-a-run1", Name: "wf-a", Namespace: "ns1", Phase: "Succeeded", TotalSteps: 1, CompletedSteps: 1},
		{ID: "wf-b-run1", Name: "wf-b", Namespace: "ns2", Phase: "Failed", TotalSteps: 1, FailedSteps: 1},
	} {
		if err := store.SaveWorkflowRun(run); err != nil {
			t.Fatalf("seed run: %v", err)
		}
	}
	if err := store.SaveStepExecution(&history.StepExecution{
		ID: "wf-a-run1-build-0", WorkflowRunID: "wf-a-run1", StepName: "build",
		AgentName: "builder", Phase: "Succeeded", TokensIn: 10, TokensOut: 5, CostUSD: 0.01,
	}); err != nil {
		t.Fatalf("seed step: %v", err)
	}
	if err := store.SaveToolCalls("wf-a-run1-build-0", []history.ToolCall{
		{ToolName: "list_pods", ToolServer: "lumino", ElapsedMs: 200},
	}); err != nil {
		t.Fatalf("seed tools: %v", err)
	}
}

func get(t *testing.T, handler http.HandlerFunc, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestHistoryRunsList(t *testing.T) {
	s, store := newHistoryServer(t)
	seedHistory(t, store)

	rec := get(t, s.handleHistoryRuns, "/api/history/runs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body)
	}
	var runs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}

	rec = get(t, s.handleHistoryRuns, "/api/history/runs?namespace=ns1")
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	if len(runs) != 1 || runs[0]["name"] != "wf-a" {
		t.Errorf("namespace filter failed: %v", runs)
	}

	rec = get(t, s.handleHistoryRuns, "/api/history/runs?limit=1")
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("unmarshal limited: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("limit failed, got %d runs", len(runs))
	}
}

func TestHistoryRunDetail(t *testing.T) {
	s, store := newHistoryServer(t)
	seedHistory(t, store)

	rec := get(t, s.handleHistoryRunDetail, "/api/history/run/wf-a-run1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body)
	}
	var detail struct {
		ID    string           `json:"id"`
		Phase string           `json:"phase"`
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.ID != "wf-a-run1" || detail.Phase != "Succeeded" {
		t.Errorf("unexpected detail: %+v", detail)
	}
	if len(detail.Steps) != 1 || detail.Steps[0]["stepName"] != "build" {
		t.Errorf("steps not included: %v", detail.Steps)
	}

	rec = get(t, s.handleHistoryRunDetail, "/api/history/run/missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing run, got %d", rec.Code)
	}
}

func TestHistoryRunSteps(t *testing.T) {
	s, store := newHistoryServer(t)
	seedHistory(t, store)

	rec := get(t, s.handleHistoryRunDetail, "/api/history/run/wf-a-run1/steps")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body)
	}
	var steps []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(steps) != 1 || steps[0]["agentName"] != "builder" {
		t.Errorf("unexpected steps: %v", steps)
	}
}

func TestHistoryStepTools(t *testing.T) {
	s, store := newHistoryServer(t)
	seedHistory(t, store)

	rec := get(t, s.handleHistoryStepTools, "/api/history/step/wf-a-run1-build-0/tools")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body)
	}
	var tools []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tools); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tools) != 1 || tools[0]["toolName"] != "list_pods" {
		t.Errorf("unexpected tools: %v", tools)
	}
}

func TestHistoryDisabled(t *testing.T) {
	s := &Server{} // History nil
	rec := get(t, s.handleHistoryRuns, "/api/history/runs")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when history disabled, got %d", rec.Code)
	}
}
