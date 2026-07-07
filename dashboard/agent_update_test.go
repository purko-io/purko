package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// Guardrails are safety-critical (cost cap, iteration cap) and must be
// editable from the UI (Stage 1 finding F19). The update must MERGE into
// existing guardrails — keys the form doesn't send survive.
func TestUpdateAgentMergesGuardrails(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	agent := &v1alpha1.Agent{}
	agent.Name = "knowledge-retriever"
	agent.Namespace = "ai-agents"
	agent.Spec.Model.Provider = "anthropic"
	agent.Spec.Model.Name = "claude-sonnet-4-6"
	agent.Spec.Guardrails = &runtime.RawExtension{Raw: []byte(`{"maxIterations":15,"customKey":"keep-me"}`)}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
	s := &Server{Client: c}

	req := httptest.NewRequest(http.MethodPost, "/api/update/agent", strings.NewReader(`{
		"name": "knowledge-retriever", "namespace": "ai-agents",
		"provider": "anthropic", "model": "claude-sonnet-4-6",
		"autonomy": "restricted", "role": "r", "systemPrompt": "p",
		"costLimit": 8.5, "maxExecutionTime": "5m", "rollbackOnFailure": true
	}`))
	w := httptest.NewRecorder()
	s.handleUpdateAgent(w, req)

	got := &v1alpha1.Agent{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "knowledge-retriever", Namespace: "ai-agents"}, got); err != nil {
		t.Fatalf("get updated agent: %v", err)
	}
	if got.Spec.Guardrails == nil {
		t.Fatal("guardrails removed by update")
	}
	var g map[string]interface{}
	if err := json.Unmarshal(got.Spec.Guardrails.Raw, &g); err != nil {
		t.Fatalf("unmarshal guardrails: %v", err)
	}
	if g["costLimitUSD"] != 8.5 {
		t.Errorf("costLimitUSD = %v, want 8.5", g["costLimitUSD"])
	}
	if g["maxExecutionTime"] != "5m" {
		t.Errorf("maxExecutionTime = %v, want 5m", g["maxExecutionTime"])
	}
	if g["rollbackOnFailure"] != true {
		t.Errorf("rollbackOnFailure = %v, want true", g["rollbackOnFailure"])
	}
	if g["maxIterations"] != float64(15) {
		t.Errorf("maxIterations = %v, want preserved 15", g["maxIterations"])
	}
	if g["customKey"] != "keep-me" {
		t.Errorf("customKey = %v, want preserved", g["customKey"])
	}
}

// The agent forms' image dropdown hardcoded dev-only localhost images and
// silently pinned spec.runtime.image on every edit — breaking all runs on
// real clusters (Stage 2 F41). Empty image now CLEARS the pin (operator
// default applies); omitted image leaves it untouched.
func TestUpdateAgentImageSemantics(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	newAgent := func(image string) *v1alpha1.Agent {
		a := &v1alpha1.Agent{}
		a.Name = "a1"
		a.Namespace = "ai-agents"
		a.Spec.Model.Provider = "ollama"
		a.Spec.Model.Name = "m"
		if image != "" {
			a.Spec.Runtime = &v1alpha1.RuntimeSpec{Image: image}
		}
		return a
	}
	update := func(t *testing.T, a *v1alpha1.Agent, body string) *v1alpha1.Agent {
		t.Helper()
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a).Build()
		s := &Server{Client: c}
		req := httptest.NewRequest(http.MethodPost, "/api/update/agent", strings.NewReader(body))
		s.handleUpdateAgent(httptest.NewRecorder(), req)
		got := &v1alpha1.Agent{}
		if err := c.Get(context.Background(), client.ObjectKey{Name: "a1", Namespace: "ai-agents"}, got); err != nil {
			t.Fatalf("get: %v", err)
		}
		return got
	}

	t.Run("empty image clears the pin", func(t *testing.T) {
		got := update(t, newAgent("localhost/purko-executor:latest"),
			`{"name":"a1","namespace":"ai-agents","provider":"ollama","model":"m","image":""}`)
		if got.Spec.Runtime != nil && got.Spec.Runtime.Image != "" {
			t.Errorf("runtime image = %q, want cleared", got.Spec.Runtime.Image)
		}
	})

	t.Run("omitted image leaves pin untouched", func(t *testing.T) {
		got := update(t, newAgent("custom/executor:v1"),
			`{"name":"a1","namespace":"ai-agents","provider":"ollama","model":"m"}`)
		if got.Spec.Runtime == nil || got.Spec.Runtime.Image != "custom/executor:v1" {
			t.Errorf("runtime = %+v, want untouched custom/executor:v1", got.Spec.Runtime)
		}
	})

	t.Run("explicit image sets the pin", func(t *testing.T) {
		got := update(t, newAgent(""),
			`{"name":"a1","namespace":"ai-agents","provider":"ollama","model":"m","image":"custom/executor:v2"}`)
		if got.Spec.Runtime == nil || got.Spec.Runtime.Image != "custom/executor:v2" {
			t.Errorf("runtime = %+v, want custom/executor:v2", got.Spec.Runtime)
		}
	})
}
