package controllers

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

// A rejected status write (e.g. stale CRD missing the CompletedWithErrors
// enum) must surface as an error so the reconciler requeues — not be
// swallowed, stranding the workflow as Running forever (PR#21 review F1).
func TestSetPhasePropagatesStatusUpdateError(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	wf := &v1alpha1.Workflow{}
	wf.Name = "wf"
	wf.Namespace = "ai-agents"

	rejected := errors.New("Workflow.purko.io \"wf\" is invalid: status.phase: Unsupported value: \"CompletedWithErrors\"")
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wf).
		WithStatusSubresource(wf).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return rejected
			},
		}).Build()

	r := &WorkflowReconciler{Client: c}
	if err := r.setPhase(context.Background(), wf, "CompletedWithErrors", "CompletedWithErrors", "2/4 steps"); !errors.Is(err, rejected) {
		t.Errorf("setPhase error = %v, want the rejected status write surfaced", err)
	}

	c2 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wf).WithStatusSubresource(wf).Build()
	r2 := &WorkflowReconciler{Client: c2}
	if err := r2.setPhase(context.Background(), wf, "Succeeded", "AllStepsCompleted", "ok"); err != nil {
		t.Errorf("setPhase on healthy client = %v, want nil", err)
	}
}
