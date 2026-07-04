package controllers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

const (
	defaultExecutorImage = "localhost/purko-executor:latest"
	defaultTimeoutSec    = 1800
	jobTTLSeconds        = 3600
	labelWorkflow        = "purko.io/workflow"
	labelStep            = "purko.io/step"
	labelAgent           = "purko.io/agent"
)

// buildRunID creates a deterministic-ish run ID from workflow UID + random suffix.
func buildRunID(wfUID string) string {
	uid := wfUID
	if len(uid) > 8 {
		uid = uid[:8]
	}
	suffix := fmt.Sprintf("%04x", rand.Intn(0xFFFF))
	return uid + "-" + suffix
}

// buildJobName creates a Job name that fits within 63 chars.
func buildJobName(workflowName, stepName, runID string) string {
	base := workflowName + "-" + stepName + "-" + runID
	if len(base) <= 63 {
		return base
	}
	// Hash the workflow+step to 12 chars, append run-id
	h := sha256.Sum256([]byte(workflowName + "/" + stepName))
	return fmt.Sprintf("%.12x-%s", h[:6], runID)
}

// buildStepJob creates a Kubernetes Job spec for a workflow step.
// mcpServersJSON is a JSON array of MCP server configs (from the registry).
func buildStepJob(wf *v1alpha1.Workflow, step v1alpha1.WorkflowStep, agent *v1alpha1.Agent, runID string, stepInput json.RawMessage, inputFromEnvs []corev1.EnvVar, mcpServersJSON string, llmProvider *v1alpha1.LLMProvider, llmAPIKey string) *batchv1.Job {
	jobName := buildJobName(wf.Name, step.Name, runID)

	// Determine image — use agent's specified image, fall back to default executor
	image := defaultExecutorImage
	if agent.Spec.Runtime != nil && agent.Spec.Runtime.Image != "" {
		image = agent.Spec.Runtime.Image
	}

	// Determine timeout — step timeout > guardrails maxExecutionTime > default
	timeout := int64(defaultTimeoutSec)
	// 4.2: Read maxExecutionTime from agent guardrails
	if agent.Spec.Guardrails != nil && agent.Spec.Guardrails.Raw != nil {
		var guardrails map[string]interface{}
		if json.Unmarshal(agent.Spec.Guardrails.Raw, &guardrails) == nil {
			if maxExec, ok := guardrails["maxExecutionTime"].(string); ok && maxExec != "" {
				if parsed, err := time.ParseDuration(maxExec); err == nil {
					timeout = int64(parsed.Seconds())
				}
			}
		}
	}
	// Step-level timeout overrides guardrails
	if step.StepTimeout != nil && step.StepTimeout.TimeoutSeconds > 0 {
		timeout = int64(step.StepTimeout.TimeoutSeconds)
	}

	// Build env vars
	env := []corev1.EnvVar{
		{Name: "STEP_NAME", Value: step.Name},
		{Name: "WORKFLOW_NAME", Value: wf.Name},
		{Name: "MODEL_PROVIDER", Value: agent.Spec.Model.Provider},
		{Name: "MODEL_NAME", Value: agent.Spec.Model.Name},
	}
	if agent.Spec.Model.MaxTokens != nil {
		env = append(env, corev1.EnvVar{Name: "MODEL_MAX_TOKENS", Value: fmt.Sprintf("%d", *agent.Spec.Model.MaxTokens)})
	}

	// LLM provider configuration from CRD
	if llmProvider != nil {
		apiFormat := llmProvider.Spec.APIFormat
		if apiFormat == "" {
			switch llmProvider.Spec.Type {
			case "anthropic", "vertex-ai":
				apiFormat = "anthropic"
			default:
				apiFormat = "openai"
			}
		}
		env = append(env, corev1.EnvVar{Name: "MODEL_API_FORMAT", Value: apiFormat})

		if llmProvider.Spec.Endpoint != "" {
			env = append(env, corev1.EnvVar{Name: "MODEL_ENDPOINT", Value: llmProvider.Spec.Endpoint})
		}

		if llmAPIKey != "" {
			env = append(env, corev1.EnvVar{Name: "MODEL_API_KEY", Value: llmAPIKey})
		}

		if llmProvider.Spec.Type == "vertex-ai" {
			if projectID, ok := llmProvider.Spec.Config["projectId"]; ok {
				env = append(env, corev1.EnvVar{Name: "ANTHROPIC_VERTEX_PROJECT_ID", Value: projectID})
			}
			if region, ok := llmProvider.Spec.Config["region"]; ok {
				env = append(env, corev1.EnvVar{Name: "CLOUD_ML_REGION", Value: region})
			}
			env = append(env, corev1.EnvVar{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/var/run/secrets/gcp/credentials.json"})
		}
	}

	if agent.Spec.AutonomyLevel != "" {
		env = append(env, corev1.EnvVar{Name: "AUTONOMY_LEVEL", Value: agent.Spec.AutonomyLevel})
	}

	if agent.Spec.SystemPrompt != "" {
		env = append(env, corev1.EnvVar{Name: "AGENT_SYSTEM_PROMPT", Value: agent.Spec.SystemPrompt})
	}

	if len(stepInput) > 0 {
		env = append(env, corev1.EnvVar{Name: "STEP_INPUT", Value: string(stepInput)})
	}

	// MCP servers — passed as JSON array from the registry
	if mcpServersJSON != "" {
		env = append(env, corev1.EnvVar{
			Name:  "MCP_SERVERS",
			Value: mcpServersJSON,
		})
	}

	// Legacy fallback: if MCP_SERVERS is empty, check env vars for backward compat
	if mcpServersJSON == "" {
		if mcpURL := os.Getenv("LUMINO_MCP_URL"); mcpURL != "" {
			env = append(env, corev1.EnvVar{Name: "MCP_SERVER_URL", Value: mcpURL})
		}
		if ghURL := os.Getenv("GITHUB_MCP_URL"); ghURL != "" {
			env = append(env, corev1.EnvVar{Name: "GITHUB_MCP_URL", Value: ghURL})
			env = append(env, corev1.EnvVar{
				Name: "GITHUB_MCP_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "github-mcp-token"},
						Key:                  "token",
						Optional:             boolPtr(true),
					},
				},
			})
		}
	}

	// Vertex AI credentials — fallback from operator env (deprecated, use LLMProvider CR)
	useVertex := llmProvider != nil && llmProvider.Spec.Type == "vertex-ai"
	if llmProvider == nil || llmProvider.Spec.Type != "vertex-ai" {
		if vertexProject := os.Getenv("ANTHROPIC_VERTEX_PROJECT_ID"); vertexProject != "" {
			useVertex = true
			env = append(env,
				corev1.EnvVar{Name: "ANTHROPIC_VERTEX_PROJECT_ID", Value: vertexProject},
				corev1.EnvVar{Name: "CLOUD_ML_REGION", Value: os.Getenv("CLOUD_ML_REGION")},
				corev1.EnvVar{Name: "GOOGLE_APPLICATION_CREDENTIALS", Value: "/var/run/secrets/gcp/credentials.json"},
			)
		}
	}

	// Add model temperature if set
	if agent.Spec.Model.Temperature != nil {
		env = append(env, corev1.EnvVar{
			Name:  "MODEL_TEMPERATURE",
			Value: fmt.Sprintf("%.2f", *agent.Spec.Model.Temperature),
		})
	}

	// Parse guardrails and pass to executor
	if agent.Spec.Guardrails != nil && agent.Spec.Guardrails.Raw != nil {
		var guardrails map[string]interface{}
		if err := json.Unmarshal(agent.Spec.Guardrails.Raw, &guardrails); err == nil {
			if v, ok := guardrails["maxIterations"]; ok {
				env = append(env, corev1.EnvVar{Name: "MAX_TOOL_CALLS", Value: fmt.Sprintf("%v", v)})
			}
			if v, ok := guardrails["costLimitUSD"]; ok {
				env = append(env, corev1.EnvVar{Name: "COST_LIMIT_USD", Value: fmt.Sprintf("%v", v)})
			}
			if v, ok := guardrails["contentFilters"]; ok {
				if filters, err := json.Marshal(v); err == nil {
					env = append(env, corev1.EnvVar{Name: "CONTENT_FILTERS", Value: string(filters)})
				}
			}
			if v, ok := guardrails["maxExecutionTime"]; ok {
				env = append(env, corev1.EnvVar{Name: "MAX_EXECUTION_TIME", Value: fmt.Sprintf("%v", v)})
			}
		}
	}

	// Memory configuration
	memoryType := "buffer"
	if agent.Spec.Memory != nil && agent.Spec.Memory.Type != "" {
		memoryType = agent.Spec.Memory.Type
	}
	env = append(env, corev1.EnvVar{Name: "MEMORY_TYPE", Value: memoryType})
	if memoryType == "summary" {
		env = append(env, corev1.EnvVar{Name: "MEMORY_CM_NAME", Value: agent.Name + "-memory"})
	}
	if agent.Spec.Memory != nil && agent.Spec.Memory.MaxContextTokens != nil {
		env = append(env, corev1.EnvVar{Name: "MAX_CONTEXT_TOKENS", Value: fmt.Sprintf("%d", *agent.Spec.Memory.MaxContextTokens)})
	}

	// Code execution config
	if agent.Spec.Runtime != nil && agent.Spec.Runtime.CodeExecution != nil && agent.Spec.Runtime.CodeExecution.Enabled {
		ce := agent.Spec.Runtime.CodeExecution
		env = append(env, corev1.EnvVar{Name: "CODE_EXECUTION_ENABLED", Value: "true"})
		if len(ce.Languages) > 0 {
			env = append(env, corev1.EnvVar{Name: "CODE_LANGUAGES", Value: strings.Join(ce.Languages, ",")})
		}
		if ce.Sandbox != nil {
			sandboxJSON, _ := json.Marshal(ce.Sandbox)
			env = append(env, corev1.EnvVar{Name: "CODE_SANDBOX", Value: string(sandboxJSON)})
		}
	}

	// Serialize agent tools for the executor
	if len(agent.Spec.Tools) > 0 {
		toolsJSON, _ := json.Marshal(agent.Spec.Tools)
		env = append(env, corev1.EnvVar{Name: "AGENT_TOOLS", Value: string(toolsJSON)})
	}

	// Inject credentials from agent secret if available (skipped when LLMProvider already provided a key)
	if agent.Spec.Model.CredentialsSecretRef != nil && llmAPIKey == "" {
		key := agent.Spec.Model.CredentialsSecretRef.Key
		if key == "" {
			key = "api-key"
		}
		env = append(env, corev1.EnvVar{
			Name: "MODEL_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: agent.Spec.Model.CredentialsSecretRef.Name,
					},
					Key:      key,
					Optional: boolPtr(true),
				},
			},
		})
	}

	// Add inputFrom resolved env vars (STEP_INPUT_{STEP}_{KEY})
	env = append(env, inputFromEnvs...)

	// Add agent's env vars and config
	if agent.Spec.Runtime != nil {
		for _, e := range agent.Spec.Runtime.Env {
			env = append(env, corev1.EnvVar{Name: e.Name, Value: e.Value})
		}
		// Pass runtime.config as EXECUTOR_* env vars
		for k, v := range agent.Spec.Runtime.Config {
			envName := "EXECUTOR_" + strings.ToUpper(strings.ReplaceAll(k, ".", "_"))
			env = append(env, corev1.EnvVar{Name: envName, Value: v})
		}
	}

	// Service account — only use if explicitly set and the SA exists
	// For minikube/dev, agent examples reference non-existent SAs, so default to ""
	sa := ""

	// Build job
	backoffLimit := int32(0)
	ttl := int32(jobTTLSeconds)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: wf.Namespace,
			Labels: map[string]string{
				labelWorkflow: wf.Name,
				labelStep:     step.Name,
				labelAgent:    agent.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "purko.io/v1alpha1",
					Kind:       "Workflow",
					Name:       wf.Name,
					UID:        wf.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &timeout,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelWorkflow: wf.Name,
						labelStep:     step.Name,
						labelAgent:    agent.Name,
					},
				},
				Spec: func() corev1.PodSpec {
					// Normal pod networking by default so executors reach
					// in-cluster services (LLM endpoints, MCP servers) via
					// cluster DNS. PURKO_EXECUTOR_HOST_NETWORK=true opts into
					// host networking for local dev where MCP servers run on
					// the node (e.g. minikube with localhost MCP servers).
					hostNet := os.Getenv("PURKO_EXECUTOR_HOST_NETWORK") == "true"
					dnsPolicy := corev1.DNSClusterFirst
					if hostNet {
						dnsPolicy = corev1.DNSDefault
					}
					spec := corev1.PodSpec{
						RestartPolicy:      corev1.RestartPolicyNever,
						ServiceAccountName: sa,
						HostNetwork:        hostNet,
						DNSPolicy:          dnsPolicy,
						Containers: []corev1.Container{
							{
								Name:            "step-executor",
								Image:           image,
								ImagePullPolicy: corev1.PullIfNotPresent,
								Env:             env,
							},
						},
					}
					// Mount tool configRef ConfigMaps
					for _, tool := range agent.Spec.Tools {
						if tool.Config != nil && tool.Config.Raw != nil {
							var configRef struct {
								ConfigMapName string `json:"configMapName"`
								Key           string `json:"key"`
							}
							if json.Unmarshal(tool.Config.Raw, &configRef) == nil && configRef.ConfigMapName != "" {
								volName := "tool-cfg-" + strings.ReplaceAll(tool.Name, "_", "-")
								if len(volName) > 63 {
									volName = volName[:63]
								}
								spec.Volumes = append(spec.Volumes, corev1.Volume{
									Name: volName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{Name: configRef.ConfigMapName},
											Optional:             boolPtr(true),
										},
									},
								})
								spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{
									Name:      volName,
									MountPath: "/etc/tool-config/" + tool.Name,
									ReadOnly:  true,
								})
							}
						}
					}
					// Mount vector memory PVC if configured
					if memoryType == "vector" && agent.Spec.Memory != nil &&
						agent.Spec.Memory.PersistentStorage != nil &&
						agent.Spec.Memory.PersistentStorage.Enabled {
						pvcName := agent.Spec.Memory.PersistentStorage.VolumeClaimRef
						if pvcName == "" {
							pvcName = agent.Name + "-memory"
						}
						spec.Volumes = append(spec.Volumes, corev1.Volume{
							Name: "agent-memory",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						})
						spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{
							Name:      "agent-memory",
							MountPath: "/var/run/agent-memory",
						})
					}
					// Mount GCP credentials if using Vertex AI
					if useVertex {
						gcpSecretName := "gcp-credentials"
						if llmProvider != nil && llmProvider.Spec.Type == "vertex-ai" && llmProvider.Spec.Credentials != nil && llmProvider.Spec.Credentials.SecretRef != "" {
							gcpSecretName = llmProvider.Spec.Credentials.SecretRef
						}
						spec.Volumes = append(spec.Volumes, corev1.Volume{
							Name: "gcp-creds",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: gcpSecretName,
									Optional:   boolPtr(true),
								},
							},
						})
						spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{
							Name:      "gcp-creds",
							MountPath: "/var/run/secrets/gcp",
							ReadOnly:  true,
						})
					}
					return spec
				}(),
			},
		},
	}

	return job
}

func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}
