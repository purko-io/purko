package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

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

var llmGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show detailed info for an LLM provider",
	Args:  cobra.ExactArgs(1),
	RunE:  runLLMGet,
}

var llmCreateFile string

var llmCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an LLM provider from a YAML file",
	RunE:  runLLMCreate,
}

var llmDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an LLM provider",
	Args:  cobra.ExactArgs(1),
	RunE:  runLLMDelete,
}

func init() {
	llmCmd.AddCommand(llmListCmd)
	llmCmd.AddCommand(llmGetCmd)
	llmCreateCmd.Flags().StringVarP(&llmCreateFile, "file", "f", "", "Path to LLMProvider YAML file (required)")
	llmCreateCmd.MarkFlagRequired("file")
	llmCmd.AddCommand(llmCreateCmd)
	llmCmd.AddCommand(llmDeleteCmd)
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

func runLLMGet(cmd *cobra.Command, args []string) error {
	name := args[0]

	var provider v1alpha1.LLMProvider
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
			return fmt.Errorf("llm provider %q not found", name)
		}
	}

	if outputFmt == "json" {
		return printJSON(provider)
	}

	fmt.Printf("Name:         %s\n", provider.Name)
	fmt.Printf("Namespace:    %s\n", provider.Namespace)
	fmt.Printf("Type:         %s\n", provider.Spec.Type)
	fmt.Printf("Model:        %s\n", provider.Spec.Model)
	fmt.Printf("Phase:        %s\n", provider.Status.Phase)
	fmt.Printf("Default:      %t\n", provider.Spec.Default)
	fmt.Printf("Age:          %s\n", formatAge(provider.CreationTimestamp))

	if provider.Spec.Endpoint != "" {
		fmt.Printf("Endpoint:     %s\n", provider.Spec.Endpoint)
	}
	if provider.Spec.APIFormat != "" {
		fmt.Printf("API Format:   %s\n", provider.Spec.APIFormat)
	}

	if len(provider.Spec.Models) > 0 {
		fmt.Println()
		fmt.Println("Models:")
		modelHeaders := []string{"  NAME", "DISPLAY NAME", "INPUT $/MT", "OUTPUT $/MT"}
		modelRows := make([][]string, 0, len(provider.Spec.Models))
		for _, m := range provider.Spec.Models {
			inputPrice := "-"
			outputPrice := "-"
			if m.Pricing != nil {
				inputPrice = fmt.Sprintf("$%.2f", m.Pricing.InputPerMToken)
				outputPrice = fmt.Sprintf("$%.2f", m.Pricing.OutputPerMToken)
			}
			modelRows = append(modelRows, []string{
				"  " + m.Name,
				m.DisplayName,
				inputPrice,
				outputPrice,
			})
		}
		printTable(modelHeaders, modelRows)
	}

	if provider.Status.LastHealthCheck != nil && !provider.Status.LastHealthCheck.IsZero() {
		fmt.Printf("\nLast Check:   %s\n", provider.Status.LastHealthCheck.UTC().Format("2006-01-02T15:04:05Z"))
	}

	if strings.EqualFold(provider.Status.Phase, "error") && provider.Status.Message != "" {
		fmt.Printf("Error:        %s\n", provider.Status.Message)
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

func runLLMCreate(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(llmCreateFile)
	if err != nil {
		return fmt.Errorf("unable to read file %q: %w", llmCreateFile, err)
	}

	var provider v1alpha1.LLMProvider
	if err := yaml.Unmarshal(data, &provider); err != nil {
		return fmt.Errorf("unable to parse YAML: %w", err)
	}

	if provider.Namespace == "" {
		provider.Namespace = namespace
	}

	if err := k8sClient.Create(context.TODO(), &provider); err != nil {
		return fmt.Errorf("unable to create LLM provider: %w", err)
	}

	fmt.Printf("LLMProvider %s created in namespace %s\n", provider.Name, provider.Namespace)
	return nil
}

func runLLMDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	var provider v1alpha1.LLMProvider
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &provider); err != nil {
		return fmt.Errorf("llm provider %q not found in namespace %q", name, namespace)
	}

	if err := k8sClient.Delete(context.TODO(), &provider); err != nil {
		return fmt.Errorf("unable to delete llm provider %q: %w", name, err)
	}

	fmt.Printf("LLMProvider %s deleted from namespace %s\n", name, namespace)
	return nil
}
