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

	"github.com/purko-io/purko/api/v1alpha1"
)

func newLLMServer(t *testing.T) (*Server, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	return &Server{Client: c}, c
}

func createProvider(t *testing.T, s *Server, body string) *v1alpha1.LLMProvider {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/llm/provider", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleLLMProviderCreate(w, req)

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "" {
		t.Fatalf("create returned error: %s", resp["error"])
	}

	// The workflow controller resolves providers by listing purko-system
	// only (resolveLLMProvider), so created providers must land there —
	// anywhere else and every workflow silently runs in demo mode.
	provider := &v1alpha1.LLMProvider{}
	if err := s.Client.Get(context.Background(), client.ObjectKey{Name: "ollama", Namespace: "purko-system"}, provider); err != nil {
		t.Fatalf("get created provider in purko-system: %v", err)
	}
	return provider
}

// The webhook requires spec.endpoint for type ollama (LP-005), so the
// create API must accept an explicit endpoint field.
func TestLLMProviderCreateSetsEndpoint(t *testing.T) {
	s, _ := newLLMServer(t)
	provider := createProvider(t, s, `{
		"name": "ollama", "type": "ollama", "model": "qwen3.5:4b",
		"endpoint": "http://ollama.ai-agents:11434/v1"
	}`)
	if provider.Spec.Endpoint != "http://ollama.ai-agents:11434/v1" {
		t.Errorf("spec.endpoint = %q, want %q", provider.Spec.Endpoint, "http://ollama.ai-agents:11434/v1")
	}
}

// The form's config textarea documented `endpoint=...` as the way to set
// the endpoint; promote that key to spec.endpoint so existing guidance
// (and scripted callers) keep working.
func TestLLMProviderCreatePromotesConfigEndpoint(t *testing.T) {
	s, _ := newLLMServer(t)
	provider := createProvider(t, s, `{
		"name": "ollama", "type": "ollama", "model": "qwen3.5:4b",
		"config": {"endpoint": "http://ollama.ai-agents:11434/v1", "other": "kept"}
	}`)
	if provider.Spec.Endpoint != "http://ollama.ai-agents:11434/v1" {
		t.Errorf("spec.endpoint = %q, want promoted config endpoint", provider.Spec.Endpoint)
	}
	if _, ok := provider.Spec.Config["endpoint"]; ok {
		t.Errorf("config[endpoint] should be removed after promotion, got %q", provider.Spec.Config["endpoint"])
	}
	if provider.Spec.Config["other"] != "kept" {
		t.Errorf("config[other] = %q, want %q", provider.Spec.Config["other"], "kept")
	}
}

// An explicit endpoint field wins over a config endpoint key.
func TestLLMProviderCreateEndpointFieldWins(t *testing.T) {
	s, _ := newLLMServer(t)
	provider := createProvider(t, s, `{
		"name": "ollama", "type": "ollama", "model": "qwen3.5:4b",
		"endpoint": "http://explicit:11434/v1",
		"config": {"endpoint": "http://from-config:11434/v1"}
	}`)
	if provider.Spec.Endpoint != "http://explicit:11434/v1" {
		t.Errorf("spec.endpoint = %q, want explicit field value", provider.Spec.Endpoint)
	}
}
