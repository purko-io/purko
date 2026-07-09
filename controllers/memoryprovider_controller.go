package controllers

import (
	"context"
	"time"

	"github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/pkg/memory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MemoryProviderReconciler owns MemoryProvider.status (Spec 34 §4). The built-in
// provider is the operator's own store; external types report unhealthy until
// implemented. Resyncs every 5 minutes.
type MemoryProviderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Memory memory.Store
}

func (r *MemoryProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var mp v1alpha1.MemoryProvider
	if err := r.Get(ctx, req.NamespacedName, &mp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	mp.Status.LastChecked = metav1.Now()
	switch {
	case mp.Spec.Type != "builtin":
		mp.Status.Healthy = false
		mp.Status.LastError = "provider type not implemented (only builtin ships in Spec 34)"
	case r.Memory == nil:
		// Defensive only: main.go registers this reconciler solely when the store
		// is real (memoryStore != nil guard), so this branch is unreachable in
		// production wiring — kept so a typed-nil Memory can never rename builtin
		// to "type not implemented".
		mp.Status.Healthy = false
		mp.Status.LastError = "memory store not initialized"
	default:
		if err := r.Memory.Healthy(ctx); err != nil {
			mp.Status.Healthy = false
			mp.Status.LastError = err.Error()
		} else {
			mp.Status.Healthy = true
			mp.Status.LastError = ""
			// ns "" = provider-global stats: memories live under agent namespaces,
			// not the CR's purko-system, and entryCount is the provider-wide count
			// (spec 34 §1 "kubectl get memoryproviders shows health and entry counts").
			if st, err := r.Memory.Stats(ctx, ""); err == nil {
				mp.Status.EntryCount = st.TotalEntries
			}
		}
	}

	if err := r.Status().Update(ctx, &mp); err != nil {
		logger.Error(err, "failed to update MemoryProvider status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *MemoryProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.MemoryProvider{}).
		Complete(r)
}
