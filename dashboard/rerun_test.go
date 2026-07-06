package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// Edit & Re-run must preserve the full spec — above all the step input
// templates (${parameters.*}) — while applying the edited parameters.
// The old JS path rebuilt the spec by hand and dropped every step input
// (Stage 1 finding: re-run ran agents with empty tasks).
func TestRerunWorkflowPreservesSpecAndOverridesParameters(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	wf := &v1alpha1.Workflow{}
	wf.APIVersion = "purko.io/v1alpha1"
	wf.Kind = "Workflow"
	wf.Name = "research-report"
	wf.Namespace = "ai-agents"
	wf.Spec.Parameters = map[string]string{"question": "", "depth": "comprehensive"}
	wf.Spec.Steps = []v1alpha1.WorkflowStep{{
		Name:     "research",
		AgentRef: v1alpha1.AgentRef{Name: "researcher"},
		Input:    &runtime.RawExtension{Raw: []byte(`{"raw":"Research: ${parameters.question}"}`)},
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wf).Build()
	s := &Server{Client: c}

	req := httptest.NewRequest(http.MethodPost, "/api/rerun/workflow/research-report",
		strings.NewReader(`{"parameters":{"question":"sqlite vs postgres"}}`))
	w := httptest.NewRecorder()
	s.handleRerunWorkflow(w, req)

	got := &v1alpha1.Workflow{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "research-report", Namespace: "ai-agents"}, got); err != nil {
		t.Fatalf("recreated workflow not found: %v", err)
	}
	if got.Spec.Parameters["question"] != "sqlite vs postgres" {
		t.Errorf("question = %q, want override", got.Spec.Parameters["question"])
	}
	if got.Spec.Parameters["depth"] != "comprehensive" {
		t.Errorf("depth = %q, want untouched original", got.Spec.Parameters["depth"])
	}
	if len(got.Spec.Steps) != 1 || got.Spec.Steps[0].Input == nil ||
		!strings.Contains(string(got.Spec.Steps[0].Input.Raw), "${parameters.question}") {
		t.Errorf("step input template lost: %+v", got.Spec.Steps)
	}
}
