package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
	"github.com/purko-io/purko/controllers"
	"github.com/purko-io/purko/dashboard"
	"github.com/purko-io/purko/pkg/history"
	"github.com/purko-io/purko/pkg/licensing"
	"github.com/purko-io/purko/pkg/registry"
	"github.com/purko-io/purko/webhooks"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var webhookPort int
	var dashboardPort int
	var enableLeaderElection bool
	var enableWebhooks bool
	var enableDashboard bool
	var agentNamespace string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "Webhook server port.")
	flag.IntVar(&dashboardPort, "dashboard-port", 8082, "Dashboard web UI port.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for HA.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false, "Enable admission webhooks.")
	flag.BoolVar(&enableDashboard, "dashboard-enabled", true, "Enable embedded dashboard.")
	flag.StringVar(&agentNamespace, "agent-namespace", "ai-agents", "Namespace for agents and workflows.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	logger := ctrl.Log.WithName("setup")

	cfg := ctrl.GetConfigOrDie()

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "unable to create kubernetes clientset")
		os.Exit(1)
	}

	// Initialize licensing system
	licensing.Init(func() (string, error) {
		secret, err := clientset.CoreV1().Secrets("purko-system").Get(
			context.Background(), "purko-license", metav1.GetOptions{},
		)
		if err != nil {
			return "", err
		}
		if val, ok := secret.Data["license"]; ok {
			return string(val), nil
		}
		return "", nil
	})

	if licensing.IsDevMode() {
		logger.Info("Licensing: dev mode (all features enabled)")
	} else {
		logger.Info("Licensing: active", "tier", licensing.GetTier().String())
	}

	mgrOpts := ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "purko-operator-lock",
	}

	if enableWebhooks {
		mgrOpts.WebhookServer = webhook.NewServer(webhook.Options{Port: webhookPort})
	}

	mgr, err := ctrl.NewManager(cfg, mgrOpts)
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Create MCP server registry
	mcpRegistry := &registry.MCPServerRegistry{
		Client:    mgr.GetClient(),
		Namespace: agentNamespace,
		OnMetrics: controllers.UpdateMCPMetrics,
	}

	// Set up Agent controller
	if err := (&controllers.AgentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "Agent")
		os.Exit(1)
	}

	// Open execution history archive (Spec 24/28 Phase 2). Enabled via
	// PURKO_HISTORY_ENABLED (set by Helm); failures are logged but never
	// block the operator — history is an audit concern, not a runtime
	// dependency. Community retention is governed by the licensing tier.
	var historyStore *history.SQLiteStore
	if os.Getenv("PURKO_HISTORY_ENABLED") == "true" {
		historyPath := os.Getenv("PURKO_HISTORY_PATH")
		if historyPath == "" {
			historyPath = "/var/lib/purko/history.db"
		}
		historyStore, err = history.NewSQLiteStore(historyPath)
		if err != nil {
			logger.Error(err, "Failed to open execution history database — continuing WITHOUT history archival", "path", historyPath)
			historyStore = nil
		} else {
			defer historyStore.Close()
			logger.Info("Execution history enabled", "path", historyPath, "retention_days", licensing.GetLimits().HistoryRetention)
		}
	}

	// Set up Workflow controller
	wfReconciler := &controllers.WorkflowReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Clientset:  clientset,
		MCPServers: mcpRegistry,
	}
	if historyStore != nil {
		wfReconciler.HistoryStore = historyStore
	}
	if err := wfReconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "Workflow")
		os.Exit(1)
	}

	// Set up MCPServer controller
	if err := (&controllers.MCPServerReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		AgentNamespace: agentNamespace,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "MCPServer")
		os.Exit(1)
	}

	// Set up LLMProvider controller
	if err := (&controllers.LLMProviderReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "LLMProvider")
		os.Exit(1)
	}

	// Set up admission webhooks if enabled
	if enableWebhooks {
		decoder := admission.NewDecoder(mgr.GetScheme())
		mgr.GetWebhookServer().Register(
			"/validate-purko-io-v1alpha1-agent",
			&webhook.Admission{Handler: &webhooks.AgentValidator{Decoder: decoder, Client: mgr.GetClient()}},
		)
		mgr.GetWebhookServer().Register(
			"/validate-purko-io-v1alpha1-workflow",
			&webhook.Admission{Handler: &webhooks.WorkflowValidator{Decoder: decoder, Client: mgr.GetClient()}},
		)
		logger.Info("Admission webhooks enabled", "port", webhookPort)
	}

	// Health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Start MCP server registry background sync
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mcpRegistry.StartBackgroundSync(ctx)

	// History retention cleanup — runs immediately at startup, then every
	// 24h. Retention days come from the license tier (community: 7 days;
	// 0 = unlimited, no cleanup).
	if historyStore != nil {
		go func() {
			cleanup := func() {
				retention := licensing.GetLimits().HistoryRetention
				if retention <= 0 {
					return
				}
				deleted, err := historyStore.DeleteOlderThan(retention)
				if err != nil {
					logger.Error(err, "Failed to cleanup history")
				} else if deleted > 0 {
					logger.Info("History cleanup", "deleted", deleted, "retention_days", retention)
				}
			}

			cleanup()

			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					cleanup()
				}
			}
		}()
	}

	// Start the embedded community dashboard (Spec 28).
	// No LLM wiring: LLMConfig/NewLLMProvider/NewIntentLLMProvider are
	// Pro-only and not compiled into this edition; the LLM/IntentLLM fields
	// stay nil (the interface is shared via llm_iface.go).
	if enableDashboard {
		sched := &dashboard.Scheduler{
			Client:    mgr.GetClient(),
			Namespace: agentNamespace,
		}
		go sched.Start(ctx)

		dash := &dashboard.Server{
			Client:    mgr.GetClient(),
			Clientset: clientset,
			Port:      dashboardPort,
			Scheduler: sched,
			Registry:  mcpRegistry,
			Namespace: agentNamespace,
		}
		if historyStore != nil {
			dash.History = historyStore
		}
		go func() {
			if err := dash.Start(ctx); err != nil && err != http.ErrServerClosed {
				logger.Error(err, "dashboard server failed")
			}
		}()
		logger.Info("Embedded dashboard enabled (community)", "port", dashboardPort)
	} else {
		logger.Info("Embedded dashboard disabled")
	}

	logger.Info("Starting purko-operator (Community Edition)",
		"metricsAddr", metricsAddr,
		"probeAddr", probeAddr,
		"leaderElection", enableLeaderElection,
		"webhooks", enableWebhooks,
	)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		os.Exit(1)
	}
}
