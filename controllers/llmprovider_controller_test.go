package controllers

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// A credential-less provider (ollama) validates true with a nil Credentials.
// The status writer must not dereference Spec.Credentials.SecretRef for it —
// previously this panicked (nil pointer) on every reconcile.
func TestLLMProviderReconcileNilCredentials(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	prov := &v1alpha1.LLMProvider{}
	prov.Name = "ollama"
	prov.Namespace = "purko-system"
	prov.Spec.Type = "ollama"
	prov.Spec.Model = "qwen3:8b"
	// Credentials intentionally nil.

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(prov).WithStatusSubresource(prov).Build()
	r := &LLMProviderReconciler{Client: cl, Scheme: scheme}

	// Must not panic.
	key := types.NamespacedName{Namespace: "purko-system", Name: "ollama"}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile errored: %v", err)
	}

	got := &v1alpha1.LLMProvider{}
	if err := cl.Get(context.Background(), key, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	var credMsg *string
	for i := range got.Status.Conditions {
		if got.Status.Conditions[i].Type == "CredentialsValid" {
			m := got.Status.Conditions[i].Message
			credMsg = &m
		}
	}
	if credMsg == nil {
		t.Fatal("CredentialsValid condition not set for credential-less provider")
	}
	if *credMsg != "No credentials required" {
		t.Errorf("credential-less message = %q, want %q", *credMsg, "No credentials required")
	}
}
