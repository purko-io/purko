package controllers

import (
	"encoding/json"
	"testing"

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
