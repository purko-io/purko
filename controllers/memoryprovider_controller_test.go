package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/memory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestMemoryProviderStatusWritten(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	mp := &v1alpha1.MemoryProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "builtin", Namespace: "purko-system"},
		Spec:       v1alpha1.MemoryProviderSpec{Type: "builtin", Default: true},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mp).WithStatusSubresource(mp).Build()
	// statsNS starts as a sentinel so we can assert the reconciler asked for
	// provider-GLOBAL stats (ns "") — memories live under agent namespaces,
	// not the CR's purko-system (spec 34 T11 review).
	fms := &fakeMemStore{stats: memory.Stats{TotalEntries: 7}, statsNS: "sentinel"}
	r := &MemoryProviderReconciler{Client: cl, Scheme: scheme, Memory: fms}

	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKey{Name: "builtin", Namespace: "purko-system"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := &v1alpha1.MemoryProvider{}
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "builtin", Namespace: "purko-system"}, got)
	if !got.Status.Healthy {
		t.Errorf("expected healthy=true after reconcile against a healthy fake store")
	}
	if got.Status.LastChecked.IsZero() {
		t.Errorf("lastChecked not stamped")
	}
	if got.Status.EntryCount != 7 {
		t.Errorf("entryCount = %d, want 7 (from store Stats)", got.Status.EntryCount)
	}
	if fms.statsNS != "" {
		t.Errorf("Stats called with ns %q, want \"\" (provider-global)", fms.statsNS)
	}
}

func TestMemoryProviderBuiltinUnhealthy(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	mp := &v1alpha1.MemoryProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "builtin", Namespace: "purko-system"},
		Spec:       v1alpha1.MemoryProviderSpec{Type: "builtin", Default: true},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mp).WithStatusSubresource(mp).Build()
	fms := &fakeMemStore{healthyErr: errors.New("db locked")}
	r := &MemoryProviderReconciler{Client: cl, Scheme: scheme, Memory: fms}

	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKey{Name: "builtin", Namespace: "purko-system"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := &v1alpha1.MemoryProvider{}
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "builtin", Namespace: "purko-system"}, got)
	if got.Status.Healthy {
		t.Errorf("expected healthy=false when Healthy() errors")
	}
	if got.Status.LastError != "db locked" {
		t.Errorf("lastError = %q, want the Healthy() error propagated", got.Status.LastError)
	}
	if got.Status.LastChecked.IsZero() {
		t.Errorf("lastChecked not stamped")
	}
}

func TestMemoryProviderReservedTypeUnhealthy(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	mp := &v1alpha1.MemoryProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "redis-future", Namespace: "purko-system"},
		Spec:       v1alpha1.MemoryProviderSpec{Type: "redis"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mp).WithStatusSubresource(mp).Build()
	r := &MemoryProviderReconciler{Client: cl, Scheme: scheme, Memory: &fakeMemStore{}}

	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKey{Name: "redis-future", Namespace: "purko-system"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := &v1alpha1.MemoryProvider{}
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "redis-future", Namespace: "purko-system"}, got)
	if got.Status.Healthy {
		t.Errorf("expected healthy=false for reserved type redis")
	}
	if got.Status.LastError == "" {
		t.Errorf("expected lastError to be set for reserved type")
	}
	if got.Status.LastChecked.IsZero() {
		t.Errorf("lastChecked not stamped")
	}
}
