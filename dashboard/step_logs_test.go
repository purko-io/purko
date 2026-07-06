package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// When the step's pod/job is gone (TTL, failFast cleanup), the logs panel
// must fall back to the error and output persisted in workflow status —
// not "No logs available" seconds after a failure (F26).
func TestStepLogsFallsBackToStatusWhenPodGone(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{v1alpha1.AddToScheme, corev1.AddToScheme, batchv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatalf("scheme: %v", err)
		}
	}

	wf := &v1alpha1.Workflow{}
	wf.Name = "dead-endpoint-test"
	wf.Namespace = "ai-agents"
	wf.Status.StepStatuses = []v1alpha1.StepStatus{{
		Name:  "doomed",
		Phase: "Failed",
		Error: "ERROR Model API not reachable: connection refused port=11499",
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wf).Build()
	s := &Server{Client: c}

	req := httptest.NewRequest(http.MethodGet, "/api/logs/dead-endpoint-test/doomed", nil)
	w := httptest.NewRecorder()
	s.handleStepLogs(w, req)

	var resp struct {
		Lines  []string `json:"lines"`
		Status string   `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "failed" {
		t.Errorf("status = %q, want failed", resp.Status)
	}
	joined := strings.Join(resp.Lines, "\n")
	if !strings.Contains(joined, "Model API not reachable") {
		t.Errorf("persisted error not served, lines: %v", resp.Lines)
	}
}
