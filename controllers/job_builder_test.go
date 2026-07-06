package controllers

import (
	"encoding/json"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

func buildTestJob(t *testing.T) *v1alpha1.Workflow {
	t.Helper()
	wf := &v1alpha1.Workflow{}
	wf.Name = "wf"
	wf.Namespace = "ai-agents"
	return wf
}

func TestBuildStepJobPodNetwork(t *testing.T) {
	wf := buildTestJob(t)
	step := v1alpha1.WorkflowStep{Name: "s1"}
	agent := &v1alpha1.Agent{}
	agent.Name = "a1"
	agent.Spec.Model.Provider = "local-ollama"
	agent.Spec.Model.Name = "smollm2:135m"
	input := json.RawMessage(`{"task":"x"}`)

	t.Run("default: normal pod networking with cluster DNS", func(t *testing.T) {
		t.Setenv("PURKO_EXECUTOR_HOST_NETWORK", "")
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", nil, "")
		spec := job.Spec.Template.Spec
		if spec.HostNetwork {
			t.Error("HostNetwork should default to false — it breaks cluster-DNS access to in-cluster services")
		}
		if spec.DNSPolicy != corev1.DNSClusterFirst {
			t.Errorf("DNSPolicy = %q, want ClusterFirst", spec.DNSPolicy)
		}
	})

	t.Run("agent maxTokens is passed to the executor", func(t *testing.T) {
		maxTokens := 200
		a := &v1alpha1.Agent{}
		a.Name = "a1"
		a.Spec.Model.Provider = "local-ollama"
		a.Spec.Model.Name = "smollm2:135m"
		a.Spec.Model.MaxTokens = &maxTokens
		job := buildStepJob(wf, step, a, "run1", input, nil, "", nil, "")
		found := ""
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "MODEL_MAX_TOKENS" {
				found = e.Value
			}
		}
		if found != "200" {
			t.Errorf("MODEL_MAX_TOKENS = %q, want 200", found)
		}
	})

	t.Run("no maxTokens env when agent does not set it", func(t *testing.T) {
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", nil, "")
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "MODEL_MAX_TOKENS" {
				t.Errorf("unexpected MODEL_MAX_TOKENS = %q", e.Value)
			}
		}
	})

	t.Run("opt-in host networking for local dev", func(t *testing.T) {
		t.Setenv("PURKO_EXECUTOR_HOST_NETWORK", "true")
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", nil, "")
		spec := job.Spec.Template.Spec
		if !spec.HostNetwork {
			t.Error("HostNetwork should be true when PURKO_EXECUTOR_HOST_NETWORK=true")
		}
		if spec.DNSPolicy != corev1.DNSDefault {
			t.Errorf("DNSPolicy = %q, want Default with host networking", spec.DNSPolicy)
		}
	})
}

// When the agent's named provider doesn't exist and resolution fell back
// to the default provider, the agent's model name is meaningless for that
// provider (e.g. claude-sonnet-4-6 sent to ollama → 404 model not found).
// The provider's own default model must win. A name match keeps the
// agent's model so users can still pick specific models per agent.
func TestBuildStepJobModelNameFollowsFallbackProvider(t *testing.T) {
	wf := buildTestJob(t)
	step := v1alpha1.WorkflowStep{Name: "s1"}
	input := json.RawMessage(`{"task":"x"}`)

	agent := &v1alpha1.Agent{}
	agent.Name = "a1"
	agent.Spec.Model.Provider = "anthropic"
	agent.Spec.Model.Name = "claude-sonnet-4-6"

	modelName := func(job *batchv1.Job) string {
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "MODEL_NAME" {
				return e.Value
			}
		}
		return ""
	}

	t.Run("fallback provider overrides agent model name", func(t *testing.T) {
		fallback := &v1alpha1.LLMProvider{}
		fallback.Name = "ollama" // != agent.Spec.Model.Provider → fallback
		fallback.Spec.Type = "ollama"
		fallback.Spec.Model = "qwen3.5:4b"
		fallback.Spec.Endpoint = "http://ollama.ai-agents:11434/v1"
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", fallback, "")
		if got := modelName(job); got != "qwen3.5:4b" {
			t.Errorf("MODEL_NAME = %q, want fallback provider model qwen3.5:4b", got)
		}
	})

	t.Run("name-matched provider keeps agent model name", func(t *testing.T) {
		matched := &v1alpha1.LLMProvider{}
		matched.Name = "anthropic" // == agent.Spec.Model.Provider
		matched.Spec.Type = "anthropic"
		matched.Spec.Model = "claude-haiku-4-5"
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", matched, "")
		if got := modelName(job); got != "claude-sonnet-4-6" {
			t.Errorf("MODEL_NAME = %q, want agent's own claude-sonnet-4-6", got)
		}
	})

	t.Run("fallback with empty provider model keeps agent model name", func(t *testing.T) {
		fallback := &v1alpha1.LLMProvider{}
		fallback.Name = "ollama"
		fallback.Spec.Type = "ollama"
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", fallback, "")
		if got := modelName(job); got != "claude-sonnet-4-6" {
			t.Errorf("MODEL_NAME = %q, want agent model kept when provider has none", got)
		}
	})
}

// A provider knows its latency profile (local ollama queues requests and
// can exceed the executor's 120s default read timeout); config.timeoutSeconds
// must reach the executor as MODEL_TIMEOUT.
func TestBuildStepJobModelTimeoutFromProviderConfig(t *testing.T) {
	wf := buildTestJob(t)
	step := v1alpha1.WorkflowStep{Name: "s1"}
	input := json.RawMessage(`{"task":"x"}`)
	agent := &v1alpha1.Agent{}
	agent.Name = "a1"
	agent.Spec.Model.Provider = "ollama"
	agent.Spec.Model.Name = "qwen3.5:4b"

	timeoutEnv := func(job *batchv1.Job) string {
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "MODEL_TIMEOUT" {
				return e.Value
			}
		}
		return ""
	}

	t.Run("provider config timeoutSeconds becomes MODEL_TIMEOUT", func(t *testing.T) {
		p := &v1alpha1.LLMProvider{}
		p.Name = "ollama"
		p.Spec.Type = "ollama"
		p.Spec.Config = map[string]string{"timeoutSeconds": "600"}
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", p, "")
		if got := timeoutEnv(job); got != "600" {
			t.Errorf("MODEL_TIMEOUT = %q, want 600", got)
		}
	})

	t.Run("no MODEL_TIMEOUT env without provider config", func(t *testing.T) {
		p := &v1alpha1.LLMProvider{}
		p.Name = "ollama"
		p.Spec.Type = "ollama"
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", p, "")
		if got := timeoutEnv(job); got != "" {
			t.Errorf("unexpected MODEL_TIMEOUT = %q", got)
		}
	})

	t.Run("non-numeric timeoutSeconds is ignored", func(t *testing.T) {
		p := &v1alpha1.LLMProvider{}
		p.Name = "ollama"
		p.Spec.Type = "ollama"
		p.Spec.Config = map[string]string{"timeoutSeconds": "10m"}
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", p, "")
		if got := timeoutEnv(job); got != "" {
			t.Errorf("MODEL_TIMEOUT = %q, want empty for invalid value", got)
		}
	})
}

// Provider-declared model pricing must reach the executor so cost tracking
// reflects reality (unknown models otherwise cost $0 by design).
func TestBuildStepJobModelPricingFromProvider(t *testing.T) {
	wf := buildTestJob(t)
	step := v1alpha1.WorkflowStep{Name: "s1"}
	input := json.RawMessage(`{"task":"x"}`)
	agent := &v1alpha1.Agent{}
	agent.Name = "a1"
	agent.Spec.Model.Provider = "ollama"
	agent.Spec.Model.Name = "qwen3.5:4b"

	envOf := func(job *batchv1.Job, name string) string {
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			if e.Name == name {
				return e.Value
			}
		}
		return ""
	}

	t.Run("pricing for the resolved model becomes env", func(t *testing.T) {
		p := &v1alpha1.LLMProvider{}
		p.Name = "ollama"
		p.Spec.Type = "ollama"
		p.Spec.Models = []v1alpha1.ModelDefinition{
			{Name: "qwen3.5:4b", Pricing: &v1alpha1.ModelPricing{InputPerMToken: 1.5, OutputPerMToken: 6}},
		}
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", p, "")
		if got := envOf(job, "MODEL_PRICE_IN"); got != "1.5" {
			t.Errorf("MODEL_PRICE_IN = %q, want 1.5", got)
		}
		if got := envOf(job, "MODEL_PRICE_OUT"); got != "6" {
			t.Errorf("MODEL_PRICE_OUT = %q, want 6", got)
		}
	})

	t.Run("no pricing env without a declared price", func(t *testing.T) {
		p := &v1alpha1.LLMProvider{}
		p.Name = "ollama"
		p.Spec.Type = "ollama"
		job := buildStepJob(wf, step, agent, "run1", input, nil, "", p, "")
		if got := envOf(job, "MODEL_PRICE_IN"); got != "" {
			t.Errorf("unexpected MODEL_PRICE_IN = %q", got)
		}
	})
}
