package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

var llmCmd = &cobra.Command{
	Use:   "llm",
	Short: "Manage LLM providers",
}

var llmListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all LLM providers",
	RunE:  runLLMList,
}

var llmTestCmd = &cobra.Command{
	Use:   "test <name>",
	Short: "Test an LLM provider",
	Args:  cobra.ExactArgs(1),
	RunE:  runLLMTest,
}

func init() {
	llmCmd.AddCommand(llmListCmd)
	llmCmd.AddCommand(llmTestCmd)
}

func listLLMProviders() ([]v1alpha1.LLMProvider, error) {
	var providers v1alpha1.LLMProviderList
	// Try all namespaces first; fall back to the current namespace on RBAC failure.
	if err := k8sClient.List(context.TODO(), &providers, client.InNamespace("")); err != nil {
		if err2 := k8sClient.List(context.TODO(), &providers, client.InNamespace(namespace)); err2 != nil {
			return nil, fmt.Errorf("llm: unable to list providers: %w", err2)
		}
	}
	sort.Slice(providers.Items, func(i, j int) bool {
		return providers.Items[i].Name < providers.Items[j].Name
	})
	return providers.Items, nil
}

func runLLMList(cmd *cobra.Command, args []string) error {
	items, err := listLLMProviders()
	if err != nil {
		return err
	}

	if outputFmt == "json" {
		return printJSON(items)
	}

	headers := []string{"NAME", "TYPE", "MODEL", "PHASE", "MODELS", "DEFAULT", "AGE"}
	rows := make([][]string, 0, len(items))
	for _, p := range items {
		isDefault := ""
		if p.Spec.Default {
			isDefault = "true"
		}
		rows = append(rows, []string{
			p.Name,
			p.Spec.Type,
			p.Spec.Model,
			p.Status.Phase,
			fmt.Sprintf("%d", p.Status.AvailableModels),
			isDefault,
			formatAge(p.CreationTimestamp),
		})
	}
	printTable(headers, rows)
	return nil
}

func runLLMTest(cmd *cobra.Command, args []string) error {
	name := args[0]

	var provider v1alpha1.LLMProvider
	// Try current namespace, then scan all namespaces.
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &provider); err != nil {
		items, listErr := listLLMProviders()
		if listErr != nil {
			return fmt.Errorf("llm provider %q not found in namespace %q", name, namespace)
		}
		found := false
		for _, p := range items {
			if p.Name == name {
				provider = p
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("llm provider %q not found in namespace %q", name, namespace)
		}
	}

	phase := provider.Status.Phase
	if phase == "" {
		phase = "Unknown"
	}

	fmt.Printf("Provider:      %s\n", provider.Name)
	fmt.Printf("Type:          %s\n", provider.Spec.Type)
	fmt.Printf("Model:         %s\n", provider.Spec.Model)
	fmt.Printf("Phase:         %s\n", phase)

	if provider.Status.LastHealthCheck != nil && !provider.Status.LastHealthCheck.IsZero() {
		fmt.Printf("Last Check:    %s\n", provider.Status.LastHealthCheck.UTC().Format("2006-01-02T15:04:05Z"))
	}

	if strings.EqualFold(phase, "error") && provider.Status.Message != "" {
		fmt.Printf("Error:         %s\n", provider.Status.Message)
	}

	if len(provider.Status.Conditions) > 0 {
		fmt.Println()
		fmt.Println("Conditions:")
		condHeaders := []string{"  TYPE", "STATUS", "REASON", "MESSAGE"}
		condRows := make([][]string, 0, len(provider.Status.Conditions))
		for _, c := range provider.Status.Conditions {
			condRows = append(condRows, []string{
				"  " + c.Type,
				string(c.Status),
				c.Reason,
				c.Message,
			})
		}
		printTable(condHeaders, condRows)
	}

	return nil
}
