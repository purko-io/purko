package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

var version = "dev"

var (
	namespace  string
	outputFmt  string
	kubeconfig string

	k8sClient    client.Client
	k8sClientset *kubernetes.Clientset
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

var rootCmd = &cobra.Command{
	Use:   "purkoctl",
	Short: "CLI for managing Purko workflows and agents",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" && cmd.Parent().Use == "purkoctl" {
			return nil
		}
		return setupK8sClient()
	},
	SilenceUsage: true,
}

func setupK8sClient() error {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})

	if namespace == "" {
		ns, _, err := config.Namespace()
		if err != nil {
			return fmt.Errorf("unable to determine namespace: %w", err)
		}
		namespace = ns
		if namespace == "" {
			namespace = "default"
		}
	}

	restConfig, err := config.ClientConfig()
	if err != nil {
		return fmt.Errorf("unable to connect to cluster: %w", err)
	}

	k8sClient, err = client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("unable to create K8s client: %w", err)
	}

	k8sClientset, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("unable to create K8s clientset: %w", err)
	}

	return nil
}

func defaultKubeconfig() string {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}

func main() {
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Target namespace (defaults to kubeconfig context namespace)")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "Output format: table or json")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", defaultKubeconfig(), "Path to kubeconfig")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(workflowCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
