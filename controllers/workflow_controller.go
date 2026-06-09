package controllers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

const (
	workflowFinalizer = "purko.io/workflow-finalizer"
)

// MCPServersProvider returns the current MCP servers JSON for executor pods.
type MCPServersProvider interface {
	GetServersJSON(ctx context.Context) string
}

type WorkflowReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Clientset    *kubernetes.Clientset
	MCPServers   MCPServersProvider // dynamic MCP server config
}

// +kubebuilder:rbac:groups=purko.io,resources=workflows,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=purko.io,resources=workflows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=purko.io,resources=workflows/finalizers,verbs=update
// +kubebuilder:rbac:groups=purko.io,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods;pods/log,verbs=get;list

func (r *WorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	wf := &v1alpha1.Workflow{}
	if err := r.Get(ctx, req.NamespacedName, wf); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !wf.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(wf, workflowFinalizer) {
			logger.Info("Cleaning up workflow resources", "workflow", wf.Name)
			// Delete all jobs for this workflow
			r.deleteWorkflowJobs(ctx, wf)
			controllerutil.RemoveFinalizer(wf, workflowFinalizer)
			if err := r.Update(ctx, wf); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(wf, workflowFinalizer) {
		controllerutil.AddFinalizer(wf, workflowFinalizer)
		if err := r.Update(ctx, wf); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Skip if already completed, failed, or cancelled (but NOT RollingBack — that needs monitoring)
	if wf.Status.Phase == "Succeeded" || wf.Status.Phase == "Failed" || wf.Status.Phase == "Cancelled" {
		return ctrl.Result{}, nil
	}

	// Skip template workflows — they exist as templates for trigger cloning, not for execution
	if wf.Annotations != nil && wf.Annotations["purko.io/template"] == "true" {
		return ctrl.Result{}, nil
	}

	// Handle RollingBack phase — monitor rollback jobs
	if wf.Status.Phase == "RollingBack" {
		rollbackDone := true
		for i := range wf.Status.StepStatuses {
			ss := &wf.Status.StepStatuses[i]
			if ss.Phase == "RollingBack" {
				if ss.JobName == "" {
					ss.Phase = "RolledBack"
					continue
				}
				rbJob := &batchv1.Job{}
				if err := r.Get(ctx, client.ObjectKey{Name: ss.JobName, Namespace: wf.Namespace}, rbJob); err != nil {
					ss.Phase = "RolledBack"
					continue
				}
				if isJobComplete(rbJob) || isJobFailed(rbJob) {
					ss.Phase = "RolledBack"
					logger.Info("Rollback step completed", "step", ss.Name)
				} else {
					rollbackDone = false
				}
			}
		}
		if rollbackDone {
			now := metav1.Now()
			wf.Status.CompletionTime = &now
			var rolledBack []string
			for _, ss := range wf.Status.StepStatuses {
				if ss.Phase == "RolledBack" {
					rolledBack = append(rolledBack, ss.Name)
				}
			}
			r.setPhase(ctx, wf, "Failed", "RolledBack",
				fmt.Sprintf("Rolled back %d steps: %s", len(rolledBack), strings.Join(rolledBack, ", ")))
			WorkflowsActive.WithLabelValues(wf.Namespace).Dec()
			return ctrl.Result{}, nil
		}
		// Still rolling back — update status and requeue
		if err := r.Status().Update(ctx, wf); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// 3.3: Check for cancellation via annotation
	if wf.Annotations != nil && wf.Annotations["purko.io/cancel"] == "true" {
		logger.Info("Workflow cancelled via annotation", "workflow", wf.Name)
		r.deleteWorkflowJobs(ctx, wf)
		for i := range wf.Status.StepStatuses {
			if wf.Status.StepStatuses[i].Phase == "Running" || wf.Status.StepStatuses[i].Phase == "" {
				wf.Status.StepStatuses[i].Phase = "Skipped"
			}
		}
		now := metav1.Now()
		wf.Status.CompletionTime = &now
		r.setPhase(ctx, wf, "Cancelled", "UserCancelled", "Workflow cancelled via purko.io/cancel annotation")
		return ctrl.Result{}, nil
	}

	// 3.1: Check workflow-level timeout
	if wf.Status.StartTime != nil && wf.Status.Phase == "Running" {
		// Check spec.timeout (from Timeout RawExtension) or default 30m
		workflowTimeout := 30 * time.Minute
		if wf.Spec.Timeout != nil && wf.Spec.Timeout.Raw != nil {
			var timeoutStr string
			if json.Unmarshal(wf.Spec.Timeout.Raw, &timeoutStr) == nil && timeoutStr != "" {
				if parsed, err := time.ParseDuration(timeoutStr); err == nil {
					workflowTimeout = parsed
				}
			}
		}
		elapsed := time.Since(wf.Status.StartTime.Time)
		if elapsed > workflowTimeout {
			logger.Info("Workflow timeout exceeded", "workflow", wf.Name, "elapsed", elapsed, "timeout", workflowTimeout)
			r.deleteWorkflowJobs(ctx, wf)
			now := metav1.Now()
			wf.Status.CompletionTime = &now
			r.setPhase(ctx, wf, "Failed", "WorkflowTimeout",
				fmt.Sprintf("Workflow exceeded timeout of %s (elapsed: %s)", workflowTimeout, elapsed.Round(time.Second)))
			return ctrl.Result{}, nil
		}
	}

	// Concurrency policy enforcement
	if wf.Status.Phase == "" && wf.Spec.Concurrency != nil && wf.Spec.Concurrency.Policy != "" {
		result, err := r.enforceConcurrency(ctx, wf)
		if err != nil {
			return ctrl.Result{}, err
		}
		if result != nil {
			return *result, nil
		}
	}

	// Initialize status
	if wf.Status.Phase == "" {
		wf.Status.Phase = "Pending"
		wf.Status.TotalSteps = len(wf.Spec.Steps)
		wf.Status.CompletedSteps = 0
		wf.Status.FailedSteps = 0
		now := metav1.Now()
		wf.Status.StartTime = &now
		WorkflowsActive.WithLabelValues(wf.Namespace).Inc()
	}

	// Generate run-id if not set
	runID := wf.Annotations["purko.io/run-id"]
	if runID == "" {
		runID = buildRunID(string(wf.UID))
		if wf.Annotations == nil {
			wf.Annotations = map[string]string{}
		}
		wf.Annotations["purko.io/run-id"] = runID
		if err := r.Update(ctx, wf); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate agent references
	agentCache := map[string]*v1alpha1.Agent{}
	for _, step := range wf.Spec.Steps {
		if step.AgentRef.Name == "" {
			continue
		}
		agent := &v1alpha1.Agent{}
		ns := wf.Namespace
		if step.AgentRef.Namespace != "" {
			ns = step.AgentRef.Namespace
		}
		if err := r.Get(ctx, client.ObjectKey{Name: step.AgentRef.Name, Namespace: ns}, agent); err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("Agent %q not found for step %q", step.AgentRef.Name, step.Name)
				r.setPhase(ctx, wf, "Failed", "AgentNotFound", msg)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		agentCache[step.Name] = agent
	}

	// Get existing jobs for this workflow
	jobMap, err := r.getWorkflowJobs(ctx, wf)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Build step status index
	statusMap := map[string]*v1alpha1.StepStatus{}
	for i := range wf.Status.StepStatuses {
		statusMap[wf.Status.StepStatuses[i].Name] = &wf.Status.StepStatuses[i]
	}

	// Sync status from existing jobs — create status entries for Jobs without one
	statusChanged := false
	for stepName, job := range jobMap {
		ss := statusMap[stepName]
		if ss == nil {
			// Job exists but no step status — create one
			ssNew := ensureStepStatus(wf, stepName)
			ssNew.Phase = "Running"
			ssNew.JobName = job.Name
			statusMap[stepName] = ssNew
			ss = ssNew
			statusChanged = true
		}
		if ss.Phase == "Running" {
			if isJobComplete(job) {
				// Job succeeded — read output and store in ConfigMap
				output := r.readJobOutput(ctx, job)
				now := metav1.Now()
				ss.Phase = "Succeeded"
				ss.CompletionTime = &now
				ss.Output = &runtime.RawExtension{Raw: output}
				wf.Status.CompletedSteps++
				statusChanged = true
				// Persist output to ConfigMap for downstream inputFrom resolution
				if err := r.storeStepOutput(ctx, wf, stepName, output); err != nil {
					logger.Error(err, "Failed to store step output", "step", stepName)
				}
				logger.Info("Step completed", "step", stepName, "job", job.Name)
				// Prometheus metrics
				if agentName := job.Labels[labelAgent]; agentName != "" {
					AgentInvocations.WithLabelValues(agentName, wf.Namespace, "success").Inc()
					if ss.StartTime != nil {
						dur := now.Sub(ss.StartTime.Time).Seconds()
						AgentLatency.WithLabelValues(agentName, wf.Namespace).Observe(dur)
						WorkflowStepDuration.WithLabelValues(wf.Name, stepName, agentName).Observe(dur)
					}
					// Track workflow-level cost from step output
					var outMap map[string]json.RawMessage
					if json.Unmarshal(output, &outMap) == nil {
						if metricsRaw, ok := outMap["_metrics"]; ok {
							var m struct{ CostUSD float64 `json:"cost_usd"` }
							if json.Unmarshal(metricsRaw, &m) == nil && m.CostUSD > 0 {
								WorkflowCostUSD.WithLabelValues(wf.Name, wf.Namespace).Add(m.CostUSD)
							}
						}
					}
					// Update agent metrics from step output _metrics
					r.updateAgentMetrics(ctx, wf.Namespace, agentName, output, ss.StartTime, &now)
				}
			} else if isJobFailed(job) {
				// Job failed — read error
				errMsg := r.readJobError(ctx, job)
				// Check retry with backoff
				step := getStepByName(wf, stepName)
				if ss.RetryCount < getMaxRetries(step) {
					// 3.4: Calculate backoff delay
					delay := getRetryDelay(step, ss.RetryCount)
					if ss.CompletionTime != nil && time.Since(ss.CompletionTime.Time) < delay {
						// Not yet time to retry — wait for backoff
						continue
					}
					ss.RetryCount++
					ss.Phase = "" // reset to trigger re-creation
					ss.CompletionTime = nil
					// Delete failed job
					r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
					delete(jobMap, stepName)
					logger.Info("Retrying step", "step", stepName, "retry", ss.RetryCount, "delay", delay)
				} else {
					now := metav1.Now()
					ss.Phase = "Failed"
					ss.CompletionTime = &now
					ss.Error = errMsg
					wf.Status.FailedSteps++
					// Prometheus + Shu-Ha-Ri failure tracking
					if agentName := job.Labels[labelAgent]; agentName != "" {
						AgentInvocations.WithLabelValues(agentName, wf.Namespace, "failure").Inc()
						r.recordAgentFailure(ctx, wf.Namespace, agentName)
					}
					logger.Info("Step failed", "step", stepName, "error", errMsg)
				}
				statusChanged = true
			}
		}
	}

	// Check for failFast
	if wf.Status.FailedSteps > 0 && wf.Spec.FailureStrategy == "failFast" {
		r.deleteWorkflowJobs(ctx, wf)
		for i := range wf.Status.StepStatuses {
			if wf.Status.StepStatuses[i].Phase == "Running" || wf.Status.StepStatuses[i].Phase == "" || wf.Status.StepStatuses[i].Phase == "Pending" {
				wf.Status.StepStatuses[i].Phase = "Skipped"
			}
		}
		r.setPhase(ctx, wf, "Failed", "StepFailed", "A step failed with failFast strategy")
		return ctrl.Result{}, nil
	}

	// Check for rollback strategy
	if wf.Status.FailedSteps > 0 && (wf.Spec.FailureStrategy == "rollback" || wf.Spec.FailureStrategy == "stop") {
		if wf.Spec.FailureStrategy == "rollback" {
			// Check if we're already in rollback phase
			if wf.Status.Phase == "RollingBack" {
				// Monitor rollback jobs — check if all rollback jobs are complete
				rollbackDone := true
				for i := range wf.Status.StepStatuses {
					ss := &wf.Status.StepStatuses[i]
					if ss.Phase == "RollingBack" {
						// Check if rollback job is done
						rbJobName := ss.JobName
						if rbJobName == "" {
							continue
						}
						rbJob := &batchv1.Job{}
						if err := r.Get(ctx, client.ObjectKey{Name: rbJobName, Namespace: wf.Namespace}, rbJob); err != nil {
							ss.Phase = "RolledBack"
							continue
						}
						if isJobComplete(rbJob) || isJobFailed(rbJob) {
							ss.Phase = "RolledBack"
							logger.Info("Rollback step completed", "step", ss.Name)
						} else {
							rollbackDone = false
						}
					}
				}
				if rollbackDone {
					now := metav1.Now()
					wf.Status.CompletionTime = &now
					var rolledBack []string
					for _, ss := range wf.Status.StepStatuses {
						if ss.Phase == "RolledBack" {
							rolledBack = append(rolledBack, ss.Name)
						}
					}
					r.setPhase(ctx, wf, "Failed", "RolledBack",
						fmt.Sprintf("Rolled back %d steps: %s", len(rolledBack), strings.Join(rolledBack, ", ")))
					return ctrl.Result{}, nil
				}
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}

			// First time entering rollback — stop running jobs and create rollback jobs
			logger.Info("Rollback triggered", "workflow", wf.Name)
			r.deleteWorkflowJobs(ctx, wf)

			// Get outputs for rollback input
			outputs := r.getStepOutputs(ctx, wf)

			// Collect completed steps in reverse order and create rollback jobs
			var completedSteps []string
			for i := len(wf.Status.StepStatuses) - 1; i >= 0; i-- {
				ss := &wf.Status.StepStatuses[i]
				if ss.Phase == "Succeeded" {
					completedSteps = append(completedSteps, ss.Name)

					// Create rollback job for this step
					step := getStepByName(wf, ss.Name)
					agent := agentCache[ss.Name]
					if step != nil && agent != nil {
						// Build rollback input with action=rollback and original output
						rollbackInput := map[string]interface{}{
							"action":         "rollback",
							"originalOutput": outputs[ss.Name],
							"task":           fmt.Sprintf("Rollback step %q: undo the actions taken in the original execution. Original output is provided.", ss.Name),
						}
						rollbackJSON, _ := json.Marshal(rollbackInput)

						mcpJSON := ""
						if r.MCPServers != nil {
							mcpJSON = r.MCPServers.GetServersJSON(ctx)
						}

						rbLLM, rbKey, _ := r.resolveLLMProvider(ctx, agent.Spec.Model.Provider)
						rbJob := buildStepJob(wf, *step, agent, runID+"-rb", json.RawMessage(rollbackJSON), nil, mcpJSON, rbLLM, rbKey)
						rbJob.Name = fmt.Sprintf("%s-rollback-%s", wf.Name, ss.Name)
						if err := r.Create(ctx, rbJob); err != nil {
							if !errors.IsAlreadyExists(err) {
								logger.Error(err, "Failed to create rollback job", "step", ss.Name)
							}
						} else {
							logger.Info("Created rollback job", "step", ss.Name, "job", rbJob.Name)
						}
						ss.Phase = "RollingBack"
						ss.JobName = rbJob.Name
					} else {
						// No agent — just mark as rolled back
						ss.Phase = "RolledBack"
					}
				} else if ss.Phase == "Running" || ss.Phase == "" || ss.Phase == "Pending" {
					ss.Phase = "Skipped"
				}
			}

			r.setPhase(ctx, wf, "RollingBack", "RollbackInProgress",
				fmt.Sprintf("Rolling back %d completed steps: %s",
					len(completedSteps), strings.Join(completedSteps, ", ")))
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		// "stop" is same as failFast
		r.deleteWorkflowJobs(ctx, wf)
		for i := range wf.Status.StepStatuses {
			if wf.Status.StepStatuses[i].Phase == "Running" || wf.Status.StepStatuses[i].Phase == "" || wf.Status.StepStatuses[i].Phase == "Pending" {
				wf.Status.StepStatuses[i].Phase = "Skipped"
			}
		}
		r.setPhase(ctx, wf, "Failed", "StepFailed", "A step failed with stop strategy")
		return ctrl.Result{}, nil
	}

	// Check if all done (completed + failed = total under continueOnError)
	allFinished := wf.Status.CompletedSteps + wf.Status.FailedSteps >= wf.Status.TotalSteps
	if allFinished {
		now := metav1.Now()
		wf.Status.CompletionTime = &now
		// Force all step statuses to Succeeded and capture any missing outputs
		for i := range wf.Status.StepStatuses {
			ss := &wf.Status.StepStatuses[i]
			if ss.Phase == "Running" || ss.Phase == "" {
				ss.Phase = "Succeeded"
				if ss.CompletionTime == nil {
					ss.CompletionTime = &now
				}
				// Read output if not captured yet
				if ss.Output == nil || len(ss.Output.Raw) <= 2 {
					if job, ok := jobMap[ss.Name]; ok && isJobComplete(job) {
						output := r.readJobOutput(ctx, job)
						ss.Output = &runtime.RawExtension{Raw: output}
						if err := r.storeStepOutput(ctx, wf, ss.Name, output); err != nil {
							logger.Error(err, "Failed to store late step output", "step", ss.Name)
						}
						logger.Info("Captured late output", "step", ss.Name, "bytes", len(output))
					}
				}
			}
		}
		finalPhase := "Succeeded"
		finalReason := "AllStepsCompleted"
		finalMsg := "All workflow steps completed"
		if wf.Status.FailedSteps > 0 && wf.Spec.FailureStrategy == "continueOnError" {
			finalPhase = "Succeeded"
			finalReason = "CompletedWithErrors"
			finalMsg = fmt.Sprintf("%d/%d steps succeeded, %d failed (continueOnError)", wf.Status.CompletedSteps, wf.Status.TotalSteps, wf.Status.FailedSteps)
		} else if wf.Status.FailedSteps > 0 {
			finalPhase = "Failed"
			finalReason = "StepsFailed"
			finalMsg = fmt.Sprintf("%d/%d steps succeeded, %d failed", wf.Status.CompletedSteps, wf.Status.TotalSteps, wf.Status.FailedSteps)
		}
		r.setPhase(ctx, wf, finalPhase, finalReason, finalMsg)
		// Prometheus: workflow duration
		if wf.Status.StartTime != nil && wf.Status.CompletionTime != nil {
			dur := wf.Status.CompletionTime.Sub(wf.Status.StartTime.Time).Seconds()
			WorkflowDuration.WithLabelValues(wf.Name, wf.Namespace, finalPhase).Observe(dur)
		}
		WorkflowsActive.WithLabelValues(wf.Namespace).Dec()
		logger.Info("Workflow completed", "workflow", wf.Name, "phase", finalPhase)
		return ctrl.Result{}, nil
	}

	// Load existing outputs for condition evaluation and inputFrom resolution
	outputs := r.getStepOutputs(ctx, wf)

	// Find executable steps (dependencies satisfied, conditions met)
	executable := r.findExecutableSteps(wf, jobMap)

	// Count running jobs for parallelism enforcement
	runningJobs := 0
	for _, job := range jobMap {
		if job.Status.Active > 0 {
			runningJobs++
		}
	}

	parallelism := 1
	if wf.Spec.Parallelism != nil {
		parallelism = *wf.Spec.Parallelism
	}

	// Start new steps up to parallelism limit
	for _, stepName := range executable {
		if runningJobs >= parallelism {
			break
		}

		step := getStepByName(wf, stepName)
		if step == nil {
			continue
		}

		// Evaluate condition expression if present
		if step.ConditionExpr != "" {
			result, err := evaluateCondition(step.ConditionExpr, outputs)
			if err != nil {
				logger.Info("Condition evaluation error, skipping step", "step", stepName, "error", err)
			}
			if !result {
				now := metav1.Now()
				ss := ensureStepStatus(wf, stepName)
				ss.Phase = "Skipped"
				ss.CompletionTime = &now
				wf.Status.CompletedSteps++
				logger.Info("Step skipped (condition not met)", "step", stepName, "condition", step.ConditionExpr)
				statusChanged = true
				continue
			}
		}

		// Skip non-agent steps (notification, etc.) — auto-complete them
		if step.AgentRef.Name == "" {
			now := metav1.Now()
			ss := ensureStepStatus(wf, stepName)
			ss.Phase = "Succeeded"
			ss.CompletionTime = &now
			wf.Status.CompletedSteps++
			logger.Info("Auto-completed non-agent step", "step", stepName, "type", step.Type)
			statusChanged = true
			continue
		}

		agent := agentCache[stepName]
		if agent == nil {
			continue
		}

		// 4.1: Check humanApprovalRequired guardrail
		if agent.Spec.Guardrails != nil && agent.Spec.Guardrails.Raw != nil {
			var guardrails map[string]interface{}
			if json.Unmarshal(agent.Spec.Guardrails.Raw, &guardrails) == nil {
				if approval, ok := guardrails["humanApprovalRequired"].(bool); ok && approval {
					ss := ensureStepStatus(wf, stepName)
					if ss.Phase != "Running" {
						// Check if deny annotation exists
						denyKey := fmt.Sprintf("purko.io/deny-%s", stepName)
						if wf.Annotations != nil && wf.Annotations[denyKey] == "true" {
							ss.Phase = "Failed"
							ss.Error = "Denied by user"
							logger.Info("Step denied by human", "step", stepName)
							statusChanged = true
							continue
						}
						// Check if approval annotation exists
						approvalKey := fmt.Sprintf("purko.io/approve-%s", stepName)
						if wf.Annotations == nil || wf.Annotations[approvalKey] != "true" {
							ss.Phase = "Pending"
							ss.Error = "Waiting for human approval (set annotation purko.io/approve-" + stepName + "=true)"
							logger.Info("Step waiting for approval", "step", stepName, "agent", agent.Name)
							statusChanged = true
							continue
						}
						logger.Info("Step approved by human", "step", stepName)
					}
				}
			}
		}

		// Build step input — resolve inputFrom from ConfigMap outputs
		stepInput := buildStepInput(wf, step, outputs)

		// Build inputFrom env vars (STEP_INPUT_{STEP}_{KEY})
		var inputFromEnvs []corev1.EnvVar
		for _, ref := range step.InputFrom {
			stepOutput, ok := outputs[ref.Step]
			if !ok {
				continue
			}
			var outputMap map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stepOutput), &outputMap); err != nil {
				continue
			}
			if val, ok := outputMap[ref.OutputKey]; ok {
				envName := "STEP_INPUT_" +
					strings.ToUpper(strings.ReplaceAll(ref.Step, "-", "_")) + "_" +
					strings.ToUpper(strings.ReplaceAll(ref.OutputKey, "-", "_"))
				inputFromEnvs = append(inputFromEnvs, corev1.EnvVar{
					Name:  envName,
					Value: string(val),
				})
			}
		}

		// Get MCP servers JSON from registry
		mcpJSON := ""
		if r.MCPServers != nil {
			mcpJSON = r.MCPServers.GetServersJSON(ctx)
		}

		// Create the Job
		llmProvider, llmAPIKey, llmErr := r.resolveLLMProvider(ctx, agent.Spec.Model.Provider)
		if llmErr != nil {
			logger.Error(llmErr, "Failed to resolve LLM provider", "provider", agent.Spec.Model.Provider)
		}
		if llmProvider == nil {
			logger.Info("No LLMProvider CR found, falling back to operator env vars (deprecated)", "provider", agent.Spec.Model.Provider)
		}

		job := buildStepJob(wf, *step, agent, runID, stepInput, inputFromEnvs, mcpJSON, llmProvider, llmAPIKey)
		if err := r.Create(ctx, job); err != nil {
			if errors.IsAlreadyExists(err) {
				logger.Info("Job already exists", "step", stepName)
			} else {
				logger.Error(err, "Failed to create job", "step", stepName)
				continue
			}
		} else {
			logger.Info("Created job for step", "step", stepName, "job", job.Name)
		}

		// Update step status
		ss := ensureStepStatus(wf, stepName)
		ss.Phase = "Running"
		ss.JobName = job.Name
		now := metav1.Now()
		ss.StartTime = &now
		runningJobs++
		statusChanged = true
	}

	// Update status
	if statusChanged || wf.Status.Phase == "Pending" {
		wf.Status.ObservedGeneration = wf.Generation
		r.setPhase(ctx, wf, "Running", "StepsInProgress",
			fmt.Sprintf("%d/%d steps completed, %d running",
				wf.Status.CompletedSteps, wf.Status.TotalSteps, runningJobs))
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// findExecutableSteps returns step names whose dependencies are all satisfied
// and whose conditions (if any) evaluate to true.
func (r *WorkflowReconciler) findExecutableSteps(wf *v1alpha1.Workflow, jobMap map[string]*batchv1.Job) []string {
	completed := map[string]bool{}
	finished := map[string]bool{} // Succeeded OR Failed
	active := map[string]bool{}
	for _, ss := range wf.Status.StepStatuses {
		if ss.Phase == "Succeeded" {
			completed[ss.Name] = true
			finished[ss.Name] = true
		}
		if ss.Phase == "Failed" {
			finished[ss.Name] = true
		}
		if ss.Phase == "Skipped" {
			completed[ss.Name] = true
			finished[ss.Name] = true
		}
		if ss.Phase == "Running" {
			active[ss.Name] = true
		}
	}
	// Also check job map for steps without status yet
	for stepName := range jobMap {
		active[stepName] = true
	}

	continueOnError := wf.Spec.FailureStrategy == "continueOnError"

	var executable []string
	for _, step := range wf.Spec.Steps {
		if completed[step.Name] || finished[step.Name] || active[step.Name] {
			continue
		}
		allDepsReady := true
		for _, dep := range step.DependsOn {
			if continueOnError {
				// Under continueOnError, deps are satisfied when finished (succeeded or failed)
				if !finished[dep] {
					allDepsReady = false
					break
				}
			} else {
				// Under failFast, deps must be succeeded
				if !completed[dep] {
					allDepsReady = false
					break
				}
			}
		}
		if allDepsReady {
			executable = append(executable, step.Name)
		}
	}
	return executable
}

// getWorkflowJobs returns all Jobs for a workflow, indexed by step name.
func (r *WorkflowReconciler) getWorkflowJobs(ctx context.Context, wf *v1alpha1.Workflow) (map[string]*batchv1.Job, error) {
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList,
		client.InNamespace(wf.Namespace),
		client.MatchingLabels{labelWorkflow: wf.Name}); err != nil {
		return nil, err
	}
	result := map[string]*batchv1.Job{}
	for i := range jobList.Items {
		stepName := jobList.Items[i].Labels[labelStep]
		if stepName != "" {
			result[stepName] = &jobList.Items[i]
		}
	}
	return result, nil
}

// deleteWorkflowJobs deletes all Jobs for a workflow.
func (r *WorkflowReconciler) deleteWorkflowJobs(ctx context.Context, wf *v1alpha1.Workflow) {
	logger := log.FromContext(ctx)
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList,
		client.InNamespace(wf.Namespace),
		client.MatchingLabels{labelWorkflow: wf.Name}); err != nil {
		logger.Error(err, "Failed to list workflow jobs for cleanup")
		return
	}
	for i := range jobList.Items {
		r.Delete(ctx, &jobList.Items[i],
			client.PropagationPolicy(metav1.DeletePropagationBackground))
	}
}

// readJobOutput reads the OUTPUT: line from the Job's pod logs.
func (r *WorkflowReconciler) readJobOutput(ctx context.Context, job *batchv1.Job) json.RawMessage {
	if r.Clientset == nil {
		return json.RawMessage(`{"note":"no clientset configured"}`)
	}

	pods, err := r.Clientset.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", job.Name),
	})
	if err != nil || len(pods.Items) == 0 {
		return json.RawMessage(`{}`)
	}

	// Read all logs (no tail limit) to ensure OUTPUT: line is captured
	logOpts := &corev1.PodLogOptions{}
	stream, err := r.Clientset.CoreV1().Pods(job.Namespace).
		GetLogs(pods.Items[0].Name, logOpts).Stream(ctx)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	defer stream.Close()

	var lastOutput string
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0), 1024*1024) // 1MB buffer for large LLM outputs
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "OUTPUT:") {
			lastOutput = strings.TrimPrefix(line, "OUTPUT:")
		}
	}

	if lastOutput != "" {
		// Validate it's valid JSON
		if json.Valid([]byte(lastOutput)) {
			return json.RawMessage(lastOutput)
		}
	}
	return json.RawMessage(`{}`)
}

// readJobError reads error context from a failed Job's pod logs.
func (r *WorkflowReconciler) readJobError(ctx context.Context, job *batchv1.Job) string {
	if r.Clientset == nil {
		return "job failed (no clientset for log reading)"
	}

	pods, err := r.Clientset.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", job.Name),
	})
	if err != nil || len(pods.Items) == 0 {
		return "job failed (no pods found)"
	}

	tailLines := int64(20)
	logOpts := &corev1.PodLogOptions{TailLines: &tailLines}
	stream, err := r.Clientset.CoreV1().Pods(job.Namespace).
		GetLogs(pods.Items[0].Name, logOpts).Stream(ctx)
	if err != nil {
		return fmt.Sprintf("job failed (log read error: %v)", err)
	}
	defer stream.Close()

	var lines []string
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.Join(lines, "\n")
}

func isJobComplete(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isJobFailed(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func getStepByName(wf *v1alpha1.Workflow, name string) *v1alpha1.WorkflowStep {
	for i := range wf.Spec.Steps {
		if wf.Spec.Steps[i].Name == name {
			return &wf.Spec.Steps[i]
		}
	}
	return nil
}

func getMaxRetries(step *v1alpha1.WorkflowStep) int {
	if step == nil || step.RetryPolicy == nil {
		return 0
	}
	return step.RetryPolicy.MaxRetries
}

// getRetryDelay calculates backoff delay for a retry attempt.
// Uses initialDelay * backoffMultiplier^retryCount.
// Defaults: initialDelay=5s, backoffMultiplier=2.0
func getRetryDelay(step *v1alpha1.WorkflowStep, retryCount int) time.Duration {
	if step == nil || step.RetryPolicy == nil {
		return 5 * time.Second
	}

	initialDelay := 5 * time.Second
	if step.RetryPolicy.Backoff != "" {
		if parsed, err := time.ParseDuration(step.RetryPolicy.Backoff); err == nil {
			initialDelay = parsed
		}
	}

	// Simple exponential backoff: delay * 2^retryCount
	delay := initialDelay
	for i := 0; i < retryCount; i++ {
		delay *= 2
	}

	// Cap at 5 minutes
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}

	return delay
}

func ensureStepStatus(wf *v1alpha1.Workflow, stepName string) *v1alpha1.StepStatus {
	for i := range wf.Status.StepStatuses {
		if wf.Status.StepStatuses[i].Name == stepName {
			return &wf.Status.StepStatuses[i]
		}
	}
	wf.Status.StepStatuses = append(wf.Status.StepStatuses, v1alpha1.StepStatus{Name: stepName})
	return &wf.Status.StepStatuses[len(wf.Status.StepStatuses)-1]
}

func buildStepInput(wf *v1alpha1.Workflow, step *v1alpha1.WorkflowStep, outputs map[string]string) json.RawMessage {
	// Start with explicit input if provided
	input := map[string]json.RawMessage{}
	if step.Input != nil && step.Input.Raw != nil {
		json.Unmarshal(step.Input.Raw, &input)
	}

	// Resolve inputFrom references from the outputs ConfigMap
	for _, ref := range step.InputFrom {
		stepOutput, ok := outputs[ref.Step]
		if !ok {
			continue
		}
		// Parse the source step's output and extract the requested key
		var outputMap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stepOutput), &outputMap); err != nil {
			// If not a JSON object, store the whole output
			input[ref.OutputKey] = json.RawMessage(stepOutput)
			continue
		}
		if val, ok := outputMap[ref.OutputKey]; ok {
			input[ref.OutputKey] = val
		} else {
			// Key not found in output — store null
			input[ref.OutputKey] = json.RawMessage(`null`)
		}
	}

	// Apply variable substitution to the serialized input
	result, _ := json.Marshal(input)
	resultStr := substituteVariables(string(result), outputs)
	// Apply parameter substitution: ${parameters.X}
	resultStr = substituteParameters(resultStr, wf.Spec.Parameters)
	return json.RawMessage(resultStr)
}

// substituteVariables replaces ${steps.<step>.output.<key>} with actual values.
var varPattern = regexp.MustCompile(`\$\{steps\.(\w[\w-]*)\.output\.([\w.]+)\}`)

func substituteVariables(template string, outputs map[string]string) string {
	return varPattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := varPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		stepName, outputKey := parts[1], parts[2]
		stepOutput, ok := outputs[stepName]
		if !ok {
			return match // leave unresolved
		}
		var outputMap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stepOutput), &outputMap); err != nil {
			return match
		}
		// Support dot notation for nested keys (e.g., "metrics.count")
		keys := strings.Split(outputKey, ".")
		current := json.RawMessage(stepOutput)
		for _, k := range keys {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(current, &m); err != nil {
				return match
			}
			val, ok := m[k]
			if !ok {
				return match
			}
			current = val
		}
		// Unquote strings for inline substitution
		var s string
		if err := json.Unmarshal(current, &s); err == nil {
			return s
		}
		return string(current)
	})
}

// substituteParameters replaces ${parameters.X} with values from spec.parameters.
var paramPattern = regexp.MustCompile(`\$\{parameters\.(\w+)\}`)

func substituteParameters(template string, params map[string]string) string {
	if len(params) == 0 {
		return template
	}
	return paramPattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := paramPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		if val, ok := params[parts[1]]; ok {
			return val
		}
		return match // leave unresolved
	})
}

// storeStepOutput writes a step's output to the workflow's ConfigMap.
func (r *WorkflowReconciler) storeStepOutput(ctx context.Context, wf *v1alpha1.Workflow, stepName string, output json.RawMessage) error {
	cmName := wf.Name + "-outputs"
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: wf.Namespace}, cm)

	if errors.IsNotFound(err) {
		// Create new ConfigMap
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: wf.Namespace,
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
			Data: map[string]string{
				stepName: string(output),
			},
		}
		return r.Create(ctx, cm)
	}
	if err != nil {
		return err
	}

	// Update existing ConfigMap
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[stepName] = string(output)
	return r.Update(ctx, cm)
}

// getStepOutputs reads all step outputs from the workflow's ConfigMap.
func (r *WorkflowReconciler) getStepOutputs(ctx context.Context, wf *v1alpha1.Workflow) map[string]string {
	cmName := wf.Name + "-outputs"
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: wf.Namespace}, cm)
	if err != nil {
		return map[string]string{}
	}
	if cm.Data == nil {
		return map[string]string{}
	}
	return cm.Data
}

func (r *WorkflowReconciler) setPhase(ctx context.Context, wf *v1alpha1.Workflow, phase, reason, message string) {
	wf.Status.Phase = phase
	wf.Status.Message = message

	status := metav1.ConditionFalse
	if phase == "Succeeded" {
		status = metav1.ConditionTrue
	}

	meta.SetStatusCondition(&wf.Status.Conditions, metav1.Condition{
		Type:               "Complete",
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	if err := r.Status().Update(ctx, wf); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update workflow status")
	}
}

// updateAgentMetrics reads _metrics from step output and aggregates into the agent's status.
func (r *WorkflowReconciler) updateAgentMetrics(ctx context.Context, namespace, agentName string, output json.RawMessage, startTime, endTime *metav1.Time) {
	logger := log.FromContext(ctx)

	// Parse _metrics from output
	var outputMap map[string]json.RawMessage
	if err := json.Unmarshal(output, &outputMap); err != nil {
		return
	}
	metricsRaw, ok := outputMap["_metrics"]
	if !ok {
		return
	}
	var stepMetrics struct {
		TokensIn  int64   `json:"tokens_in"`
		TokensOut int64   `json:"tokens_out"`
		CostUSD   float64 `json:"cost_usd"`
	}
	if err := json.Unmarshal(metricsRaw, &stepMetrics); err != nil {
		return
	}

	// Handle memory_update — store summary in agent memory ConfigMap
	if memoryUpdate, ok := outputMap["_memory_update"]; ok {
		var summary string
		json.Unmarshal(memoryUpdate, &summary)
		if summary != "" {
			r.storeAgentMemory(ctx, namespace, agentName, summary)
		}
	}

	// Calculate latency
	var latencyMs int64
	if startTime != nil && endTime != nil {
		latencyMs = endTime.Sub(startTime.Time).Milliseconds()
	}

	// Read agent and update metrics
	agent := &v1alpha1.Agent{}
	if err := r.Get(ctx, client.ObjectKey{Name: agentName, Namespace: namespace}, agent); err != nil {
		return
	}

	if agent.Status.Metrics == nil {
		agent.Status.Metrics = &v1alpha1.AgentMetrics{}
	}
	m := agent.Status.Metrics
	m.TotalInvocations++
	m.TotalTokensUsed += stepMetrics.TokensIn + stepMetrics.TokensOut
	m.TotalCostUSD += stepMetrics.CostUSD
	// Running average: new_avg = old_avg + (new_val - old_avg) / count
	if m.TotalInvocations == 1 {
		m.AverageLatencyMs = latencyMs
	} else {
		m.AverageLatencyMs = m.AverageLatencyMs + (latencyMs-m.AverageLatencyMs)/m.TotalInvocations
	}
	m.SuccessCount++
	m.ConsecutiveFailures = 0 // reset on success
	// Prometheus token/cost metrics
	AgentTokens.WithLabelValues(agentName, namespace).Add(float64(stepMetrics.TokensIn + stepMetrics.TokensOut))
	AgentCostUSD.WithLabelValues(agentName, namespace).Add(stepMetrics.CostUSD)
	now := metav1.Now()
	m.LastInvocationTime = &now

	if err := r.Status().Update(ctx, agent); err != nil {
		logger.Error(err, "Failed to update agent metrics", "agent", agentName)
	} else {
		logger.Info("Updated agent metrics", "agent", agentName,
			"invocations", m.TotalInvocations, "tokens", m.TotalTokensUsed,
			"cost", fmt.Sprintf("$%.4f", m.TotalCostUSD))
	}
}

// storeAgentMemory writes a summary to the agent's memory ConfigMap.
func (r *WorkflowReconciler) storeAgentMemory(ctx context.Context, namespace, agentName, summary string) {
	logger := log.FromContext(ctx)
	cmName := agentName + "-memory"
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, cm)

	if errors.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: namespace,
				Labels: map[string]string{
					"purko.io/agent":        agentName,
					"purko.io/memory-type":  "summary",
				},
			},
			Data: map[string]string{
				"summary": summary,
			},
		}
		if err := r.Create(ctx, cm); err != nil {
			logger.Error(err, "Failed to create agent memory ConfigMap", "agent", agentName)
		} else {
			logger.Info("Created agent memory", "agent", agentName)
		}
		return
	}
	if err != nil {
		return
	}

	// Append to existing summary (keep last N entries)
	existing := cm.Data["summary"]
	if existing != "" {
		// Keep combined summary under 8KB
		combined := existing + "\n---\n" + summary
		if len(combined) > 8192 {
			combined = combined[len(combined)-8192:]
		}
		cm.Data["summary"] = combined
	} else {
		cm.Data["summary"] = summary
	}
	if err := r.Update(ctx, cm); err != nil {
		logger.Error(err, "Failed to update agent memory", "agent", agentName)
	}
}

// enforceConcurrency checks if a new workflow run is allowed based on the concurrency policy.
// Returns a non-nil Result if the workflow should be requeued or cancelled, nil to proceed.
func (r *WorkflowReconciler) enforceConcurrency(ctx context.Context, wf *v1alpha1.Workflow) (*ctrl.Result, error) {
	logger := log.FromContext(ctx)
	policy := wf.Spec.Concurrency.Policy
	maxParallel := wf.Spec.Concurrency.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 1
	}

	// Find active runs of the same workflow template or name pattern
	templateName := ""
	if wf.Annotations != nil {
		templateName = wf.Annotations["purko.io/workflow-template"]
	}

	// List all Running workflows in the namespace
	allWFs := &v1alpha1.WorkflowList{}
	if err := r.List(ctx, allWFs, client.InNamespace(wf.Namespace)); err != nil {
		return nil, err
	}

	activeCount := 0
	var activeRuns []*v1alpha1.Workflow
	for i := range allWFs.Items {
		other := &allWFs.Items[i]
		if other.Name == wf.Name {
			continue // skip self
		}
		if other.Status.Phase != "Running" && other.Status.Phase != "Pending" {
			continue
		}
		// Match by template name or by name prefix
		otherTemplate := ""
		if other.Annotations != nil {
			otherTemplate = other.Annotations["purko.io/workflow-template"]
		}
		if templateName != "" && otherTemplate == templateName {
			activeCount++
			activeRuns = append(activeRuns, other)
		}
	}

	switch policy {
	case "forbid":
		if activeCount > 0 {
			logger.Info("Concurrency policy forbid: workflow blocked",
				"workflow", wf.Name, "activeRuns", activeCount)
			requeue := ctrl.Result{RequeueAfter: 30 * time.Second}
			return &requeue, nil
		}
	case "replace":
		if activeCount > 0 {
			logger.Info("Concurrency policy replace: cancelling active runs",
				"workflow", wf.Name, "cancelling", activeCount)
			for _, active := range activeRuns {
				active.Status.Phase = "Cancelled"
				active.Status.Message = "Cancelled by concurrency policy (replace)"
				now := metav1.Now()
				active.Status.CompletionTime = &now
				r.Status().Update(ctx, active)
				r.deleteWorkflowJobs(ctx, active)
			}
		}
	case "allow":
		if activeCount >= maxParallel {
			logger.Info("Concurrency policy allow: max parallel reached",
				"workflow", wf.Name, "active", activeCount, "max", maxParallel)
			requeue := ctrl.Result{RequeueAfter: 30 * time.Second}
			return &requeue, nil
		}
	}

	return nil, nil // proceed
}

// recordAgentFailure increments failure counters in agent metrics for Shu-Ha-Ri tracking.
func (r *WorkflowReconciler) recordAgentFailure(ctx context.Context, namespace, agentName string) {
	agent := &v1alpha1.Agent{}
	if err := r.Get(ctx, client.ObjectKey{Name: agentName, Namespace: namespace}, agent); err != nil {
		return
	}
	if agent.Status.Metrics == nil {
		agent.Status.Metrics = &v1alpha1.AgentMetrics{}
	}
	agent.Status.Metrics.FailureCount++
	agent.Status.Metrics.ConsecutiveFailures++
	agent.Status.Metrics.TotalInvocations++
	now := metav1.Now()
	agent.Status.Metrics.LastInvocationTime = &now

	if err := r.Status().Update(ctx, agent); err != nil {
		log.FromContext(ctx).Error(err, "Failed to record agent failure", "agent", agentName)
	}
}

func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Workflow{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func (r *WorkflowReconciler) resolveLLMProvider(ctx context.Context, providerName string) (*v1alpha1.LLMProvider, string, error) {
	var providers v1alpha1.LLMProviderList
	if err := r.List(ctx, &providers, client.InNamespace("purko-system")); err != nil {
		return nil, "", err
	}

	var matched *v1alpha1.LLMProvider
	var defaultProvider *v1alpha1.LLMProvider
	for i := range providers.Items {
		if providers.Items[i].Name == providerName {
			matched = &providers.Items[i]
			break
		}
		if providers.Items[i].Spec.Default {
			defaultProvider = &providers.Items[i]
		}
	}

	if matched == nil {
		matched = defaultProvider
	}
	if matched == nil {
		return nil, "", nil
	}

	apiKey := ""
	if matched.Spec.Credentials != nil && matched.Spec.Credentials.SecretRef != "" {
		secret := &corev1.Secret{}
		secretKey := client.ObjectKey{
			Name:      matched.Spec.Credentials.SecretRef,
			Namespace: matched.Namespace,
		}
		if err := r.Get(ctx, secretKey, secret); err != nil {
			logger := log.FromContext(ctx)
			logger.Info("Warning: LLMProvider credentials Secret not found", "secret", matched.Spec.Credentials.SecretRef, "provider", matched.Name)
		} else {
			key := matched.Spec.Credentials.SecretKey
			if key == "" {
				key = "api-key"
			}
			if val, ok := secret.Data[key]; ok {
				apiKey = string(val)
			}
		}
	}

	return matched, apiKey, nil
}
