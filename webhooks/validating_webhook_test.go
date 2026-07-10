package webhooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/purko-io/purko/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func decodeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(s)
	return s
}

func reviewAgent(t *testing.T, v *AgentValidator, agent *v1alpha1.Agent) admission.Response {
	t.Helper()
	raw, _ := json.Marshal(agent)
	return v.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: agent.Namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	})
}

func baseAgent() *v1alpha1.Agent {
	return &v1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns1"},
		Spec: v1alpha1.AgentSpec{
			Type:  "executor",
			Model: v1alpha1.ModelSpec{Provider: "anthropic", Name: "claude"},
		},
	}
}

func TestWebhookBehaviorEnum(t *testing.T) {
	scheme := decodeScheme(t)
	v := &AgentValidator{Decoder: admission.NewDecoder(scheme)}
	a := baseAgent()
	a.Spec.Memory = &v1alpha1.MemorySpec{Behavior: "bogus"}
	if reviewAgent(t, v, a).Allowed {
		t.Error("bogus behavior should be denied")
	}
	a.Spec.Memory.Behavior = "persistent"
	if !reviewAgent(t, v, a).Allowed {
		t.Error("persistent behavior should be allowed")
	}
}

func TestWebhookScopeGroupRequiresLabel(t *testing.T) {
	scheme := decodeScheme(t)
	v := &AgentValidator{Decoder: admission.NewDecoder(scheme)}
	a := baseAgent()
	a.Spec.Memory = &v1alpha1.MemorySpec{Behavior: "persistent", Scope: "group"}
	if reviewAgent(t, v, a).Allowed {
		t.Error("scope=group without app.kubernetes.io/component label must be denied")
	}
	a.Labels = map[string]string{"app.kubernetes.io/component": "triage"}
	if !reviewAgent(t, v, a).Allowed {
		t.Error("scope=group with label should be allowed")
	}
}

func TestWebhookAG010AcceptsBehavior(t *testing.T) {
	scheme := decodeScheme(t)
	v := &AgentValidator{Decoder: admission.NewDecoder(scheme)}
	a := baseAgent()
	a.Spec.Type = "retriever"
	// Dashboard default: behavior=persistent, NO legacy type. Must pass AG-010.
	a.Spec.Memory = &v1alpha1.MemorySpec{Behavior: "persistent"}
	if !reviewAgent(t, v, a).Allowed {
		t.Error("AG-010: retriever with behavior=persistent (no legacy type) must be allowed")
	}
	a.Spec.Memory = nil
	if reviewAgent(t, v, a).Allowed {
		t.Error("AG-010: retriever with no memory at all must still be denied")
	}
}

func TestWebhookProviderRefExistence(t *testing.T) {
	scheme := decodeScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha1.MemoryProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "custom", Namespace: "purko-system"},
		Spec:       v1alpha1.MemoryProviderSpec{Type: "builtin"},
	}).Build()
	v := &AgentValidator{Decoder: admission.NewDecoder(scheme), Client: cl}
	a := baseAgent()
	a.Spec.Memory = &v1alpha1.MemorySpec{Behavior: "persistent", ProviderRef: "nope"}
	if reviewAgent(t, v, a).Allowed {
		t.Error("providerRef to a non-existent MemoryProvider must be denied")
	}
	a.Spec.Memory.ProviderRef = "custom"
	if !reviewAgent(t, v, a).Allowed {
		t.Error("providerRef to an existing MemoryProvider must be allowed")
	}
}

func TestWebhookWarnings(t *testing.T) {
	scheme := decodeScheme(t)
	v := &AgentValidator{Decoder: admission.NewDecoder(scheme)}

	// All three warning branches at once: both-set, vector-override, namespace+full.
	a := baseAgent()
	a.Spec.AutonomyLevel = "full"
	a.Spec.Memory = &v1alpha1.MemorySpec{Type: "vector", Behavior: "persistent", Scope: "namespace"}
	resp := reviewAgent(t, v, a)
	if !resp.Allowed {
		t.Fatalf("agent should be allowed, got denied: %v", resp.Result)
	}
	wantSubstrings := []string{
		"both type and behavior set",
		"behavior overrides the vector PVC path",
		"scope=namespace with autonomyLevel=full",
	}
	for _, want := range wantSubstrings {
		found := false
		for _, w := range resp.Warnings {
			if strings.Contains(w, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a warning containing %q, got warnings: %v", want, resp.Warnings)
		}
	}

	// Clean agent: zero warnings.
	clean := baseAgent()
	clean.Spec.Memory = &v1alpha1.MemorySpec{Behavior: "persistent"}
	resp = reviewAgent(t, v, clean)
	if !resp.Allowed {
		t.Fatalf("clean agent should be allowed, got denied: %v", resp.Result)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("clean agent should produce zero warnings, got: %v", resp.Warnings)
	}
}

func TestWebhookBounds(t *testing.T) {
	scheme := decodeScheme(t)
	v := &AgentValidator{Decoder: admission.NewDecoder(scheme)}
	a := baseAgent()
	big := 40000
	a.Spec.Memory = &v1alpha1.MemorySpec{Behavior: "persistent", MaxContextTokens: &big}
	if reviewAgent(t, v, a).Allowed {
		t.Error("maxContextTokens > 32768 must be denied")
	}
}
