package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

type LLMProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *LLMProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	provider := &v1alpha1.LLMProvider{}
	if err := r.Get(ctx, req.NamespacedName, provider); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Validate credentials
	credStatus := r.validateCredentials(ctx, provider)

	// Count available models
	modelCount := len(provider.Spec.Models)
	if modelCount == 0 {
		modelCount = 1 // at least the default model
	}

	// Set status
	phase := "Ready"
	message := fmt.Sprintf("Provider %s ready, %d models available", provider.Spec.Type, modelCount)

	if !credStatus {
		phase = "Error"
		message = "Credentials not found or invalid"
	}

	provider.Status.Phase = phase
	provider.Status.Message = message
	provider.Status.AvailableModels = modelCount
	now := metav1.Now()
	provider.Status.LastHealthCheck = &now

	condStatus := metav1.ConditionTrue
	condReason := "Ready"
	if phase != "Ready" {
		condStatus = metav1.ConditionFalse
		condReason = "CredentialsInvalid"
	}

	meta.SetStatusCondition(&provider.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus,
		LastTransitionTime: metav1.Now(),
		Reason:             condReason,
		Message:            message,
	})

	if credStatus {
		// Credential-less providers (ollama/custom) validate true with a nil
		// Credentials — don't dereference SecretRef for them.
		credMsg := "No credentials required"
		credReason := "NoCredentialsRequired"
		if provider.Spec.Credentials != nil {
			credMsg = fmt.Sprintf("Credentials loaded from %s", provider.Spec.Credentials.SecretRef)
			credReason = "SecretFound"
		}
		meta.SetStatusCondition(&provider.Status.Conditions, metav1.Condition{
			Type:               "CredentialsValid",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             credReason,
			Message:            credMsg,
		})
	}

	if err := r.Status().Update(ctx, provider); err != nil {
		logger.Error(err, "Failed to update LLMProvider status")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciled LLMProvider",
		"name", provider.Name,
		"type", provider.Spec.Type,
		"model", provider.Spec.Model,
		"phase", phase,
		"models", modelCount,
		"default", provider.Spec.Default,
	)

	// Requeue for health check
	interval := 5 * time.Minute
	if provider.Spec.HealthCheck != nil && provider.Spec.HealthCheck.IntervalSeconds > 0 {
		interval = time.Duration(provider.Spec.HealthCheck.IntervalSeconds) * time.Second
	}

	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *LLMProviderReconciler) validateCredentials(ctx context.Context, provider *v1alpha1.LLMProvider) bool {
	if provider.Spec.Credentials == nil || provider.Spec.Credentials.SecretRef == "" {
		// Some providers don't need credentials (ollama, local)
		if provider.Spec.Type == "ollama" || provider.Spec.Type == "custom" {
			return true
		}
		return false
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      provider.Spec.Credentials.SecretRef,
		Namespace: provider.Namespace,
	}, secret); err != nil {
		return false
	}

	// Check the key exists
	key := provider.Spec.Credentials.SecretKey
	if key == "" {
		key = "api-key"
		if provider.Spec.Type == "vertex-ai" {
			key = "credentials.json"
		}
	}

	_, exists := secret.Data[key]
	return exists
}

func (r *LLMProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.LLMProvider{}).
		Complete(r)
}
