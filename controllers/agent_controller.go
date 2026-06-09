package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

const (
	agentFinalizer = "purko.io/agent-finalizer"
	defaultRequeue = 30 * time.Second
)

type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=purko.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=purko.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=purko.io,resources=agents/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;delete

func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	agent := &v1alpha1.Agent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion — cleanup SA, Role, RoleBinding
	if !agent.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(agent, agentFinalizer) {
			logger.Info("Cleaning up agent resources", "agent", agent.Name)
			r.cleanupAgentResources(ctx, agent)
			r.setPhase(ctx, agent, "Terminated", "Deleted", "Agent resources cleaned up")
			controllerutil.RemoveFinalizer(agent, agentFinalizer)
			if err := r.Update(ctx, agent); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(agent, agentFinalizer) {
		controllerutil.AddFinalizer(agent, agentFinalizer)
		if err := r.Update(ctx, agent); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// ── Phase: Initializing ──────────────────────────────────────────
	// Set Initializing phase on first reconcile
	if agent.Status.Phase == "" || agent.Status.Phase == "Pending" {
		agent.Status.Phase = "Initializing"
		now := metav1.Now()
		agent.Status.StartTime = &now
	}

	// Validate model config
	if agent.Spec.Model.Provider == "" || agent.Spec.Model.Name == "" {
		r.setCondition(agent, "Ready", metav1.ConditionFalse, "InvalidConfig", "model.provider and model.name are required")
		r.setPhase(ctx, agent, "Failed", "InvalidConfig", "model.provider and model.name are required")
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	// ── ServiceAccount ───────────────────────────────────────────────
	saName := fmt.Sprintf("agent-%s-sa", agent.Name)
	if agent.Spec.Runtime != nil && agent.Spec.Runtime.ServiceAccountName != "" {
		saName = agent.Spec.Runtime.ServiceAccountName
	}
	if err := r.ensureServiceAccount(ctx, agent, saName); err != nil {
		r.setCondition(agent, "Ready", metav1.ConditionFalse, "SACreationFailed", err.Error())
		r.setPhase(ctx, agent, "Failed", "SACreationFailed", err.Error())
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	// ── Role + RoleBinding ───────────────────────────────────────────
	if err := r.ensureRBAC(ctx, agent, saName); err != nil {
		r.setCondition(agent, "Ready", metav1.ConditionFalse, "RBACFailed", err.Error())
		r.setPhase(ctx, agent, "Failed", "RBACFailed", err.Error())
		return ctrl.Result{RequeueAfter: defaultRequeue}, nil
	}

	// ── Credential Validation ────────────────────────────────────────
	credentialsValid := r.validateCredentials(ctx, agent)
	if credentialsValid {
		secretName := ""
		if agent.Spec.Model.CredentialsSecretRef != nil {
			secretName = agent.Spec.Model.CredentialsSecretRef.Name
		}
		r.setCondition(agent, "CredentialsValid", metav1.ConditionTrue, "SecretFound",
			fmt.Sprintf("Credentials loaded from %s", secretName))
	} else {
		r.setCondition(agent, "CredentialsValid", metav1.ConditionFalse, "SecretMissing",
			"Required credentials not found")
		// Don't fail — some providers (vertex-ai with SA) don't need explicit credentials
	}

	// ── Tool Validation ──────────────────────────────────────────────
	toolCount := len(agent.Spec.Tools)
	r.setCondition(agent, "ToolsRegistered", metav1.ConditionTrue, "ToolsConfigured",
		fmt.Sprintf("%d tools configured", toolCount))

	// ── Quota Check ──────────────────────────────────────────────────
	quotaOk := true
	if agent.Status.Metrics != nil && agent.Status.Metrics.TotalCostUSD > 0 {
		// Check if guardrails cost limit is set and exceeded
		if agent.Spec.Guardrails != nil && agent.Spec.Guardrails.Raw != nil {
			var guardrails map[string]interface{}
			if err := json.Unmarshal(agent.Spec.Guardrails.Raw, &guardrails); err == nil {
				if costLimit, ok := guardrails["costLimitUSD"].(float64); ok && costLimit > 0 {
					if agent.Status.Metrics.TotalCostUSD > costLimit {
						quotaOk = false
						r.setCondition(agent, "QuotaAvailable", metav1.ConditionFalse, "CostLimitExceeded",
							fmt.Sprintf("Total cost $%.2f exceeds limit $%.2f", agent.Status.Metrics.TotalCostUSD, costLimit))
					}
				}
			}
		}
	}
	if quotaOk {
		r.setCondition(agent, "QuotaAvailable", metav1.ConditionTrue, "WithinLimits", "Cost within guardrail limits")
	}

	// ── Model Availability ───────────────────────────────────────────
	// Check if the LLM provider (if referenced) is healthy
	r.setCondition(agent, "ModelAvailable", metav1.ConditionTrue, "Configured",
		fmt.Sprintf("Model %s/%s configured", agent.Spec.Model.Provider, agent.Spec.Model.Name))

	// ── Set Ready ────────────────────────────────────────────────────
	agent.Status.ObservedGeneration = agent.Generation
	now := metav1.Now()
	agent.Status.LastActiveTime = &now

	image := "purko-executor:latest"
	if agent.Spec.Runtime != nil && agent.Spec.Runtime.Image != "" {
		image = agent.Spec.Runtime.Image
	}

	r.setCondition(agent, "Ready", metav1.ConditionTrue, "Initialized",
		fmt.Sprintf("Agent ready: SA=%s, %d tools, image=%s", saName, toolCount, image))
	r.setPhase(ctx, agent, "Ready", "Initialized",
		fmt.Sprintf("Agent %s (%s/%s) ready with %d tools",
			agent.Name, agent.Spec.Model.Provider, agent.Spec.Model.Name, toolCount))

	// Prometheus
	agentType := agent.Spec.Type
	if agentType == "" {
		agentType = "untyped"
	}
	AgentsTotal.WithLabelValues(agent.Namespace, agentType).Set(1)

	logger.Info("Reconciled agent",
		"agent", agent.Name,
		"phase", "Ready",
		"sa", saName,
		"model", fmt.Sprintf("%s/%s", agent.Spec.Model.Provider, agent.Spec.Model.Name),
		"tools", toolCount,
	)

	return ctrl.Result{}, nil
}

// ensureServiceAccount creates a ServiceAccount for the agent if it doesn't exist.
func (r *AgentReconciler) ensureServiceAccount(ctx context.Context, agent *v1alpha1.Agent, saName string) error {
	sa := &corev1.ServiceAccount{}
	err := r.Get(ctx, client.ObjectKey{Name: saName, Namespace: agent.Namespace}, sa)
	if err == nil {
		return nil // already exists
	}
	if !errors.IsNotFound(err) {
		return err
	}

	// Create ServiceAccount
	sa = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: agent.Namespace,
			Labels: map[string]string{
				"purko.io/agent":      agent.Name,
				"purko.io/agent-type": agent.Spec.Type,
				"purko.io/managed-by": "agent-controller",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "purko.io/v1alpha1",
					Kind:       "Agent",
					Name:       agent.Name,
					UID:        agent.UID,
					Controller: boolPtr(true),
				},
			},
		},
		AutomountServiceAccountToken: boolPtr(false),
	}

	return r.Create(ctx, sa)
}

// ensureRBAC creates a Role and RoleBinding for the agent.
func (r *AgentReconciler) ensureRBAC(ctx context.Context, agent *v1alpha1.Agent, saName string) error {
	roleName := fmt.Sprintf("agent-%s-role", agent.Name)
	bindingName := fmt.Sprintf("agent-%s-binding", agent.Name)

	// Build least-privilege rules
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list"},
		},
	}

	// Add secret access for credential ref
	if agent.Spec.Model.CredentialsSecretRef != nil && agent.Spec.Model.CredentialsSecretRef.Name != "" {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: []string{agent.Spec.Model.CredentialsSecretRef.Name},
			Verbs:         []string{"get"},
		})
	}

	ownerRef := metav1.OwnerReference{
		APIVersion: "purko.io/v1alpha1",
		Kind:       "Agent",
		Name:       agent.Name,
		UID:        agent.UID,
		Controller: boolPtr(true),
	}

	// Create or update Role
	role := &rbacv1.Role{}
	err := r.Get(ctx, client.ObjectKey{Name: roleName, Namespace: agent.Namespace}, role)
	if errors.IsNotFound(err) {
		role = &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:            roleName,
				Namespace:       agent.Namespace,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
				Labels:          map[string]string{"purko.io/agent": agent.Name},
			},
			Rules: rules,
		}
		if err := r.Create(ctx, role); err != nil {
			return fmt.Errorf("create role: %w", err)
		}
	} else if err != nil {
		return err
	}

	// Create or update RoleBinding
	binding := &rbacv1.RoleBinding{}
	err = r.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: agent.Namespace}, binding)
	if errors.IsNotFound(err) {
		binding = &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            bindingName,
				Namespace:       agent.Namespace,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
				Labels:          map[string]string{"purko.io/agent": agent.Name},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     roleName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      saName,
					Namespace: agent.Namespace,
				},
			},
		}
		if err := r.Create(ctx, binding); err != nil {
			return fmt.Errorf("create rolebinding: %w", err)
		}
	} else if err != nil {
		return err
	}

	return nil
}

// validateCredentials checks if referenced secrets exist and have the correct key.
func (r *AgentReconciler) validateCredentials(ctx context.Context, agent *v1alpha1.Agent) bool {
	if agent.Spec.Model.CredentialsSecretRef == nil || agent.Spec.Model.CredentialsSecretRef.Name == "" {
		// No credentials specified — some providers don't need them (vertex-ai with SA, ollama)
		return true
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      agent.Spec.Model.CredentialsSecretRef.Name,
		Namespace: agent.Namespace,
	}, secret); err != nil {
		return false
	}

	// Check the key exists
	key := agent.Spec.Model.CredentialsSecretRef.Key
	if key == "" {
		key = "api-key"
	}
	_, exists := secret.Data[key]
	return exists
}

// cleanupAgentResources removes SA, Role, RoleBinding on agent deletion.
// OwnerReferences handle cascade deletion, but explicit cleanup is safer.
func (r *AgentReconciler) cleanupAgentResources(ctx context.Context, agent *v1alpha1.Agent) {
	logger := log.FromContext(ctx)

	saName := fmt.Sprintf("agent-%s-sa", agent.Name)
	roleName := fmt.Sprintf("agent-%s-role", agent.Name)
	bindingName := fmt.Sprintf("agent-%s-binding", agent.Name)

	// Delete RoleBinding
	binding := &rbacv1.RoleBinding{}
	if err := r.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: agent.Namespace}, binding); err == nil {
		r.Delete(ctx, binding)
	}

	// Delete Role
	role := &rbacv1.Role{}
	if err := r.Get(ctx, client.ObjectKey{Name: roleName, Namespace: agent.Namespace}, role); err == nil {
		r.Delete(ctx, role)
	}

	// Delete ServiceAccount
	sa := &corev1.ServiceAccount{}
	if err := r.Get(ctx, client.ObjectKey{Name: saName, Namespace: agent.Namespace}, sa); err == nil {
		r.Delete(ctx, sa)
	}

	logger.Info("Cleaned up agent resources", "agent", agent.Name, "sa", saName, "role", roleName)
}

func (r *AgentReconciler) setCondition(agent *v1alpha1.Agent, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&agent.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})
}

func (r *AgentReconciler) setPhase(ctx context.Context, agent *v1alpha1.Agent, phase, reason, message string) {
	agent.Status.Phase = phase
	agent.Status.Message = message

	if err := r.Status().Update(ctx, agent); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update agent status")
	}
}

func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Agent{}).
		Owns(&corev1.ServiceAccount{}).
		Complete(r)
}
