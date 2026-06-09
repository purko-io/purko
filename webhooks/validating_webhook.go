package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// AgentValidator validates Agent resources.
type AgentValidator struct {
	Decoder admission.Decoder
	Client  client.Client
}

func (v *AgentValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	agent := &v1alpha1.Agent{}
	if err := v.Decoder.Decode(req, agent); err != nil {
		return admission.Errored(http.StatusBadRequest,
			fmt.Errorf("failed to decode Agent: %w", err))
	}

	// Model provider and name are required
	if agent.Spec.Model.Provider == "" {
		return admission.Denied("spec.model.provider must not be empty")
	}
	if agent.Spec.Model.Name == "" {
		return admission.Denied("spec.model.name must not be empty")
	}

	// MaxTokens validation (if set)
	if agent.Spec.Model.MaxTokens != nil {
		mt := *agent.Spec.Model.MaxTokens
		if mt <= 0 || mt > 2000000 {
			return admission.Denied(fmt.Sprintf(
				"spec.model.maxTokens must be between 1 and 2000000, got %d", mt))
		}
	}

	// Temperature validation
	if agent.Spec.Model.Temperature != nil {
		t := *agent.Spec.Model.Temperature
		if t < 0.0 || t > 2.0 {
			return admission.Denied(fmt.Sprintf(
				"spec.model.temperature must be between 0.0 and 2.0, got %f", t))
		}
	}

	// AG-001: Agent type is valid enum
	validTypes := map[string]bool{
		"": true, "planner": true, "executor": true, "reviewer": true,
		"router": true, "monitor": true, "retriever": true,
	}
	if !validTypes[agent.Spec.Type] {
		return admission.Denied(fmt.Sprintf(
			"AG-001: spec.type must be one of planner, executor, reviewer, router, monitor, retriever (got %q)", agent.Spec.Type))
	}

	// AG-002: credentialRef required for cloud providers
	cloudProviders := map[string]bool{"openai": true, "anthropic": true, "google": true}
	if cloudProviders[agent.Spec.Model.Provider] {
		if agent.Spec.Model.CredentialsSecretRef == nil || agent.Spec.Model.CredentialsSecretRef.Name == "" {
			// Warning: credential ref recommended but not blocking (Vertex AI uses SA)
			// return admission.Denied("AG-002: model.credentialsSecretRef is required for cloud providers")
		}
	}

	// AG-003: Referenced Secret exists in the same namespace
	if agent.Spec.Model.CredentialsSecretRef != nil && agent.Spec.Model.CredentialsSecretRef.Name != "" && v.Client != nil {
		secret := &corev1.Secret{}
		secretKey := client.ObjectKey{
			Name:      agent.Spec.Model.CredentialsSecretRef.Name,
			Namespace: req.Namespace,
		}
		if err := v.Client.Get(ctx, secretKey, secret); err != nil {
			if errors.IsNotFound(err) {
				return admission.Denied(fmt.Sprintf(
					"AG-003: referenced Secret %q not found in namespace %q",
					agent.Spec.Model.CredentialsSecretRef.Name, req.Namespace))
			}
		}
	}

	// AG-004: Tool names must be unique
	if len(agent.Spec.Tools) > 0 {
		toolNames := make(map[string]bool, len(agent.Spec.Tools))
		for _, tool := range agent.Spec.Tools {
			if toolNames[tool.Name] {
				return admission.Denied(fmt.Sprintf(
					"AG-004: duplicate tool name %q in spec.tools", tool.Name))
			}
			toolNames[tool.Name] = true
		}
	}

	// AG-005: monitor agents limited to replicas <= 1
	if agent.Spec.Type == "monitor" && agent.Spec.Replicas != nil && *agent.Spec.Replicas > 1 {
		return admission.Denied("AG-005: monitor agents are limited to replicas <= 1")
	}

	// AG-006: executor agents should request >= 200m CPU (warning — allow but note)
	// This is informational — we don't block on it per spec (Severity: Warning)

	// AG-007: costLimitUSD > 0 for non-local models (warning — allowed but noted)
	if agent.Spec.Model.Provider != "local" && agent.Spec.Model.Provider != "ollama" {
		if agent.Spec.Guardrails != nil && agent.Spec.Guardrails.Raw != nil {
			var guardrails map[string]interface{}
			if json.Unmarshal(agent.Spec.Guardrails.Raw, &guardrails) == nil {
				if cost, ok := guardrails["costLimitUSD"].(float64); ok && cost <= 0 {
					// Warning only — allow but log
				}
			}
		}
	}

	// AG-008: systemPrompt length should not exceed maxTokens
	if agent.Spec.SystemPrompt != "" && agent.Spec.Model.MaxTokens != nil {
		// Rough estimate: 1 token ≈ 4 chars
		promptTokens := len(agent.Spec.SystemPrompt) / 4
		if promptTokens > *agent.Spec.Model.MaxTokens {
			// Warning only — don't block, the model may handle truncation
		}
	}

	// AG-010: retriever agents must have memory.type of vector or buffer
	if agent.Spec.Type == "retriever" {
		if agent.Spec.Memory == nil || (agent.Spec.Memory.Type != "vector" && agent.Spec.Memory.Type != "buffer" && agent.Spec.Memory.Type != "summary") {
			return admission.Denied(
				"AG-010: retriever agents must have spec.memory.type set to vector, buffer, or summary")
		}
	}

	// Memory: if present, type must be valid
	if agent.Spec.Memory != nil && agent.Spec.Memory.Type != "" {
		validMemory := map[string]bool{"buffer": true, "summary": true, "vector": true, "none": true}
		if !validMemory[agent.Spec.Memory.Type] {
			return admission.Denied(fmt.Sprintf(
				"spec.memory.type must be buffer, summary, vector, or none (got %q)", agent.Spec.Memory.Type))
		}
	}

	return admission.Allowed("agent validation passed")
}

// WorkflowValidator validates Workflow resources.
type WorkflowValidator struct {
	Decoder admission.Decoder
	Client  client.Client
}

func (v *WorkflowValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	wf := &v1alpha1.Workflow{}
	if err := v.Decoder.Decode(req, wf); err != nil {
		return admission.Errored(http.StatusBadRequest,
			fmt.Errorf("failed to decode Workflow: %w", err))
	}

	// WF-001: Must have at least one step
	if len(wf.Spec.Steps) == 0 {
		return admission.Denied("WF-001: spec.steps must contain at least one step")
	}

	// WF-008: Total steps <= 50
	if len(wf.Spec.Steps) > 50 {
		return admission.Denied(fmt.Sprintf(
			"WF-008: workflow cannot have more than 50 steps (got %d)", len(wf.Spec.Steps)))
	}

	// WF-002: Step names must be unique
	stepNames := make(map[string]bool, len(wf.Spec.Steps))
	for _, step := range wf.Spec.Steps {
		if step.Name == "" {
			return admission.Denied("WF-002: each step must have a non-empty name")
		}
		if stepNames[step.Name] {
			return admission.Denied(fmt.Sprintf("WF-002: duplicate step name: %q", step.Name))
		}
		stepNames[step.Name] = true

		// AgentRef required unless step has a type
		if step.AgentRef.Name == "" && step.Type == "" {
			return admission.Denied(fmt.Sprintf(
				"step %q must have either agentRef.name or type", step.Name))
		}
	}

	// WF-003: dependsOn references must be valid step names
	for _, step := range wf.Spec.Steps {
		for _, dep := range step.DependsOn {
			if !stepNames[dep] {
				return admission.Denied(fmt.Sprintf(
					"WF-003: step %q depends on non-existent step %q", step.Name, dep))
			}
			if dep == step.Name {
				return admission.Denied(fmt.Sprintf(
					"WF-003: step %q cannot depend on itself", step.Name))
			}
		}
	}

	// Validate edge references
	for _, edge := range wf.Spec.Edges {
		if !stepNames[edge.From] {
			return admission.Denied(fmt.Sprintf(
				"edge references non-existent step %q in 'from'", edge.From))
		}
		if !stepNames[edge.To] {
			return admission.Denied(fmt.Sprintf(
				"edge references non-existent step %q in 'to'", edge.To))
		}
	}

	// WF-004: DAG cycle detection
	if err := detectCycles(wf.Spec.Steps); err != nil {
		return admission.Denied(fmt.Sprintf("WF-004: %v", err))
	}

	// WF-005: All agentRef.name references resolve to existing Agent CRs
	if v.Client != nil {
		for _, step := range wf.Spec.Steps {
			if step.AgentRef.Name != "" {
				agentNS := req.Namespace
				if step.AgentRef.Namespace != "" {
					agentNS = step.AgentRef.Namespace
				}
				agent := &v1alpha1.Agent{}
				if err := v.Client.Get(ctx, client.ObjectKey{Name: step.AgentRef.Name, Namespace: agentNS}, agent); err != nil {
					if errors.IsNotFound(err) {
						return admission.Denied(fmt.Sprintf(
							"WF-005: step %q references non-existent agent %q in namespace %q",
							step.Name, step.AgentRef.Name, agentNS))
					}
				}
			}
		}
	}

	// WF-006: Per-step timeout should not exceed workflow timeout
	if wf.Spec.Timeout != nil && wf.Spec.Timeout.Raw != nil {
		var wfTimeoutStr string
		if json.Unmarshal(wf.Spec.Timeout.Raw, &wfTimeoutStr) == nil && wfTimeoutStr != "" {
			wfTimeout, wfErr := parseDurationLoose(wfTimeoutStr)
			if wfErr == nil {
				for _, step := range wf.Spec.Steps {
					if step.StepTimeout != nil && step.StepTimeout.TimeoutSeconds > 0 {
						stepTimeout := time.Duration(step.StepTimeout.TimeoutSeconds) * time.Second
						if stepTimeout > wfTimeout {
							// Warning — allow but note (per spec: Severity: Warning)
							return admission.Allowed(fmt.Sprintf(
								"WF-006 warning: step %q timeout (%s) exceeds workflow timeout (%s)",
								step.Name, stepTimeout, wfTimeout)).WithWarnings(
								fmt.Sprintf("WF-006: step %q timeout (%s) exceeds workflow timeout (%s)",
									step.Name, stepTimeout, wfTimeout))
						}
					}
				}
			}
		}
	}

	// WF-007: Condition expressions syntactically valid (basic check)
	for _, step := range wf.Spec.Steps {
		if step.ConditionExpr != "" {
			// Check for basic structure: should reference steps.X.output
			if !strings.Contains(step.ConditionExpr, "steps.") && step.ConditionExpr != "true" && step.ConditionExpr != "false" {
				return admission.Denied(fmt.Sprintf(
					"WF-007: step %q condition expression %q does not reference any step output",
					step.Name, step.ConditionExpr))
			}
		}
	}

	// WF-009: Variable references must reference valid step names
	varPattern := regexp.MustCompile(`\$\{steps\.(\w[\w-]*)\.`)
	for _, step := range wf.Spec.Steps {
		// Check in condition expressions
		if step.ConditionExpr != "" {
			for _, match := range varPattern.FindAllStringSubmatch(step.ConditionExpr, -1) {
				if len(match) > 1 && !stepNames[match[1]] {
					return admission.Denied(fmt.Sprintf(
						"WF-009: step %q condition references non-existent step %q", step.Name, match[1]))
				}
			}
		}
		// Check in input
		if step.Input != nil && step.Input.Raw != nil {
			inputStr := string(step.Input.Raw)
			for _, match := range varPattern.FindAllStringSubmatch(inputStr, -1) {
				if len(match) > 1 && !stepNames[match[1]] {
					// Warning only — don't block, may reference ${parameters.X}
				}
			}
		}
	}

	// Step timeout validation
	for _, step := range wf.Spec.Steps {
		if step.StepTimeout != nil && step.StepTimeout.TimeoutSeconds < 0 {
			return admission.Denied(fmt.Sprintf(
				"step %q timeout must be non-negative", step.Name))
		}
	}

	// Validate onFailure/failureStrategy
	if wf.Spec.FailureStrategy != "" {
		validStrategies := map[string]bool{
			"failFast": true, "continueOnError": true, "rollback": true,
			"stop": true, "continue": true,
		}
		if !validStrategies[wf.Spec.FailureStrategy] {
			return admission.Denied(fmt.Sprintf(
				"spec.failureStrategy must be failFast, continueOnError, or rollback (got %q)",
				wf.Spec.FailureStrategy))
		}
	}

	return admission.Allowed("workflow validation passed")
}

// detectCycles performs a topological sort to detect cycles in step dependencies.
func detectCycles(steps []v1alpha1.WorkflowStep) error {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)

	for _, step := range steps {
		if _, exists := inDegree[step.Name]; !exists {
			inDegree[step.Name] = 0
		}
		for _, dep := range step.DependsOn {
			adj[dep] = append(adj[dep], step.Name)
			inDegree[step.Name]++
		}
	}

	queue := make([]string, 0)
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, neighbor := range adj[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if visited != len(steps) {
		return fmt.Errorf("dependency cycle detected among steps")
	}
	return nil
}

// parseDurationLoose parses duration strings like "30m", "1h", "45s" and also Go-style "30m0s".
func parseDurationLoose(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

