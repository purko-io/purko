package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

const mcpServerFinalizer = "purko.io/mcpserver-finalizer"

type MCPServerReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	AgentNamespace string // namespace where mcp-servers ConfigMap lives
}

func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	srv := &v1alpha1.MCPServer{}
	if err := r.Get(ctx, req.NamespacedName, srv); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !srv.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(srv, mcpServerFinalizer) {
			logger.Info("Cleaning up MCPServer", "name", srv.Name)
			r.removeFromConfigMap(ctx, srv)
			controllerutil.RemoveFinalizer(srv, mcpServerFinalizer)
			if err := r.Update(ctx, srv); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(srv, mcpServerFinalizer) {
		controllerutil.AddFinalizer(srv, mcpServerFinalizer)
		if err := r.Update(ctx, srv); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, srv); err != nil {
		r.setStatus(ctx, srv, "Error", "DeploymentFailed", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, srv); err != nil {
		r.setStatus(ctx, srv, "Error", "ServiceFailed", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Update mcp-servers ConfigMap
	if err := r.updateConfigMap(ctx, srv); err != nil {
		logger.Error(err, "Failed to update mcp-servers ConfigMap")
	}

	// Set Ready status
	r.setStatus(ctx, srv, "Ready", "Deployed",
		fmt.Sprintf("Deployment and Service created, registered in mcp-servers ConfigMap"))

	logger.Info("Reconciled MCPServer", "name", srv.Name, "image", srv.Spec.Image)
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *MCPServerReconciler) reconcileDeployment(ctx context.Context, srv *v1alpha1.MCPServer) error {
	port := srv.Spec.Port
	if port == 0 {
		port = 8000
	}
	replicas := int32(1)
	if srv.Spec.Replicas != nil {
		replicas = int32(*srv.Spec.Replicas)
	}

	deploy := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: srv.Name, Namespace: srv.Namespace}, deploy)

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      srv.Name,
			Namespace: srv.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":    srv.Name,
				"app.kubernetes.io/part-of": "purko",
				"purko.io/mcp-server":     srv.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "purko.io/v1alpha1",
					Kind:       "MCPServer",
					Name:       srv.Name,
					UID:        srv.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"purko.io/mcp-server": srv.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"purko.io/mcp-server": srv.Name,
					},
				},
				Spec: func() corev1.PodSpec {
					spec := corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:            "mcp-server",
								Image:           srv.Spec.Image,
								ImagePullPolicy: corev1.PullIfNotPresent,
								Args:            srv.Spec.Args,
								Ports: []corev1.ContainerPort{
									{ContainerPort: int32(port)},
								},
							},
						},
					}
					if srv.Spec.HostNetwork {
						spec.HostNetwork = true
						spec.DNSPolicy = corev1.DNSDefault
						// Remove containerPort to avoid scheduling conflicts
						spec.Containers[0].Ports = nil
					}
					return spec
				}(),
			},
		},
	}

	// Apply resource requirements
	if srv.Spec.Resources != nil {
		reqs := corev1.ResourceList{}
		for k, v := range srv.Spec.Resources.Requests {
			reqs[corev1.ResourceName(k)] = resource.MustParse(v)
		}
		limits := corev1.ResourceList{}
		for k, v := range srv.Spec.Resources.Limits {
			limits[corev1.ResourceName(k)] = resource.MustParse(v)
		}
		desired.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
			Requests: reqs,
			Limits:   limits,
		}
	}

	// Auth token env var
	if srv.Spec.Auth == "bearer" && srv.Spec.SecretRef != "" {
		desired.Spec.Template.Spec.Containers[0].Env = append(
			desired.Spec.Template.Spec.Containers[0].Env,
			corev1.EnvVar{
				Name: "AUTH_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: srv.Spec.SecretRef},
						Key:                  "token",
						Optional:             boolPtr(true),
					},
				},
			},
		)
	}

	// Additional env vars from spec
	for _, e := range srv.Spec.Env {
		envVar := corev1.EnvVar{Name: e.Name, Value: e.Value}
		// Support secretKeyRef for env vars (value starts with "secret:")
		if e.Value == "" && srv.Spec.SecretRef != "" {
			envVar.ValueFrom = &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: srv.Spec.SecretRef},
					Key:                  "token",
					Optional:             boolPtr(true),
				},
			}
		}
		desired.Spec.Template.Spec.Containers[0].Env = append(
			desired.Spec.Template.Spec.Containers[0].Env, envVar)
	}

	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	// Update existing
	deploy.Spec = desired.Spec
	return r.Update(ctx, deploy)
}

func (r *MCPServerReconciler) reconcileService(ctx context.Context, srv *v1alpha1.MCPServer) error {
	port := srv.Spec.Port
	if port == 0 {
		port = 8000
	}

	svc := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: srv.Name, Namespace: srv.Namespace}, svc)

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      srv.Name,
			Namespace: srv.Namespace,
			Labels: map[string]string{
				"purko.io/mcp-server": srv.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "purko.io/v1alpha1",
					Kind:       "MCPServer",
					Name:       srv.Name,
					UID:        srv.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"purko.io/mcp-server": srv.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       int32(port),
					TargetPort: intstr.FromInt(port),
				},
			},
		},
	}

	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	// Update existing
	svc.Spec.Selector = desired.Spec.Selector
	svc.Spec.Ports = desired.Spec.Ports
	return r.Update(ctx, svc)
}

// updateConfigMap adds this server to the mcp-servers ConfigMap in the agent namespace.
func (r *MCPServerReconciler) updateConfigMap(ctx context.Context, srv *v1alpha1.MCPServer) error {
	port := srv.Spec.Port
	if port == 0 {
		port = 8000
	}
	var serverURL string
	if srv.Spec.HostNetwork {
		// With hostNetwork, the server binds to localhost on the node
		serverURL = fmt.Sprintf("http://localhost:%d", port)
	} else {
		serverURL = fmt.Sprintf("http://%s.%s.svc:%d", srv.Name, srv.Namespace, port)
	}

	cm := &corev1.ConfigMap{}
	cmKey := client.ObjectKey{Name: "mcp-servers", Namespace: r.AgentNamespace}
	if err := r.Get(ctx, cmKey, cm); err != nil {
		if errors.IsNotFound(err) {
			// Create ConfigMap
			cm = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mcp-servers",
					Namespace: r.AgentNamespace,
				},
			}
		} else {
			return err
		}
	}

	// Parse existing servers
	var servers []map[string]interface{}
	if serversYAML, ok := cm.Data["servers"]; ok && serversYAML != "" {
		json.Unmarshal([]byte(serversYAML), &servers)
	}

	// Update or add this server
	found := false
	entry := map[string]interface{}{
		"name":     srv.Name,
		"url":      serverURL,
		"auth":     srv.Spec.Auth,
		"icon":     srv.Spec.Icon,
		"category": srv.Spec.Category,
	}
	if srv.Spec.SecretRef != "" {
		entry["secretRef"] = srv.Spec.SecretRef
	}

	for i, s := range servers {
		if s["name"] == srv.Name {
			servers[i] = entry
			found = true
			break
		}
	}
	if !found {
		servers = append(servers, entry)
	}

	serversJSON, _ := json.MarshalIndent(servers, "", "  ")
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["servers"] = string(serversJSON)

	if cm.ResourceVersion == "" {
		return r.Create(ctx, cm)
	}
	return r.Update(ctx, cm)
}

// removeFromConfigMap removes this server from the mcp-servers ConfigMap.
func (r *MCPServerReconciler) removeFromConfigMap(ctx context.Context, srv *v1alpha1.MCPServer) {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: "mcp-servers", Namespace: r.AgentNamespace}, cm); err != nil {
		return
	}

	var servers []map[string]interface{}
	if serversYAML, ok := cm.Data["servers"]; ok {
		json.Unmarshal([]byte(serversYAML), &servers)
	}

	filtered := make([]map[string]interface{}, 0, len(servers))
	for _, s := range servers {
		if s["name"] != srv.Name {
			filtered = append(filtered, s)
		}
	}

	serversJSON, _ := json.MarshalIndent(filtered, "", "  ")
	cm.Data["servers"] = string(serversJSON)
	r.Update(ctx, cm)
}

func (r *MCPServerReconciler) setStatus(ctx context.Context, srv *v1alpha1.MCPServer, phase, reason, message string) {
	srv.Status.Phase = phase
	srv.Status.Message = message

	status := metav1.ConditionFalse
	if phase == "Ready" {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&srv.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	r.Status().Update(ctx, srv)
}

func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.MCPServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
