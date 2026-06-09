package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents in the namespace",
	RunE:  runAgentList,
}

var agentGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show detailed info for a single agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentGet,
}

var agentCreateFile string

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an agent from a YAML file",
	RunE:  runAgentCreate,
}

var agentDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentDelete,
}

func init() {
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentGetCmd)
	agentCreateCmd.Flags().StringVarP(&agentCreateFile, "file", "f", "", "Path to agent YAML file (required)")
	agentCreateCmd.MarkFlagRequired("file")
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentDeleteCmd)
}

func runAgentCreate(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(agentCreateFile)
	if err != nil {
		return fmt.Errorf("unable to read file %q: %w", agentCreateFile, err)
	}

	var agent v1alpha1.Agent
	if err := yaml.Unmarshal(data, &agent); err != nil {
		return fmt.Errorf("unable to parse agent YAML: %w", err)
	}

	if agent.Namespace == "" {
		agent.Namespace = namespace
	}

	if err := k8sClient.Create(context.TODO(), &agent); err != nil {
		return fmt.Errorf("unable to create agent %q: %w", agent.Name, err)
	}

	fmt.Printf("Agent %s created in namespace %s\n", agent.Name, agent.Namespace)
	return nil
}

func runAgentDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	var agent v1alpha1.Agent
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &agent); err != nil {
		return fmt.Errorf("agent %q not found in namespace %q", name, namespace)
	}

	if err := k8sClient.Delete(context.TODO(), &agent); err != nil {
		return fmt.Errorf("unable to delete agent %q: %w", name, err)
	}

	fmt.Printf("Agent %s deleted from namespace %s\n", name, namespace)
	return nil
}

func runAgentList(cmd *cobra.Command, args []string) error {
	var agents v1alpha1.AgentList
	if err := k8sClient.List(context.TODO(), &agents, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("agents: unable to list in namespace %q: %w", namespace, err)
	}

	sort.Slice(agents.Items, func(i, j int) bool {
		return agents.Items[i].Name < agents.Items[j].Name
	})

	if outputFmt == "json" {
		return printJSON(agents.Items)
	}

	headers := []string{"NAME", "TYPE", "MODEL", "PHASE", "AUTONOMY", "INVOCATIONS", "COST", "AGE"}
	rows := make([][]string, 0, len(agents.Items))
	for _, a := range agents.Items {
		invocations := int64(0)
		cost := 0.0
		if a.Status.Metrics != nil {
			invocations = a.Status.Metrics.TotalInvocations
			cost = a.Status.Metrics.TotalCostUSD
		}
		model := a.Spec.Model.Provider + "/" + a.Spec.Model.Name
		rows = append(rows, []string{
			a.Name,
			a.Spec.Type,
			model,
			a.Status.Phase,
			a.Spec.AutonomyLevel,
			fmt.Sprintf("%d", invocations),
			formatCost(cost),
			formatAge(a.CreationTimestamp),
		})
	}
	printTable(headers, rows)
	return nil
}

func runAgentGet(cmd *cobra.Command, args []string) error {
	name := args[0]
	var agent v1alpha1.Agent
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &agent); err != nil {
		return fmt.Errorf("agent %q not found in namespace %q", name, namespace)
	}

	if outputFmt == "json" {
		return printJSON(agent)
	}

	fmt.Printf("Name:         %s\n", agent.Name)
	fmt.Printf("Namespace:    %s\n", agent.Namespace)
	fmt.Printf("Type:         %s\n", agent.Spec.Type)
	fmt.Printf("Phase:        %s\n", agent.Status.Phase)
	fmt.Printf("Age:          %s\n", formatAge(agent.CreationTimestamp))

	fmt.Println()
	fmt.Println("Model:")
	fmt.Printf("  Provider:     %s\n", agent.Spec.Model.Provider)
	fmt.Printf("  Name:         %s\n", agent.Spec.Model.Name)
	if agent.Spec.Model.Temperature != nil {
		fmt.Printf("  Temperature:  %.1f\n", *agent.Spec.Model.Temperature)
	}

	fmt.Println()
	fmt.Println("Autonomy:")
	fmt.Printf("  Level:        %s\n", agent.Spec.AutonomyLevel)
	if agent.Status.ShuHaRi != nil {
		fmt.Printf("  Shu-Ha-Ri:    %s\n", agent.Status.ShuHaRi.CurrentLevel)
		fmt.Printf("  Ready:        %t\n", agent.Status.ShuHaRi.ReadyForPromotion)
		if agent.Status.ShuHaRi.PromotionProgress != nil {
			p := agent.Status.ShuHaRi.PromotionProgress
			fmt.Printf("  Progress:     %d/%d actions, %.1f%% success rate, %d/%d days\n",
				p.ActionsCompleted, p.ActionsRequired,
				p.SuccessRate*100,
				p.DaysInLevel, p.DaysRequired)
		}
	}

	if len(agent.Spec.Tools) > 0 {
		fmt.Println()
		fmt.Printf("Tools (%d):\n", len(agent.Spec.Tools))
		toolHeaders := []string{"  NAME", "TYPE"}
		toolRows := make([][]string, 0, len(agent.Spec.Tools))
		for _, t := range agent.Spec.Tools {
			toolRows = append(toolRows, []string{"  " + t.Name, t.Type})
		}
		printTable(toolHeaders, toolRows)
	}

	fmt.Println()
	fmt.Println("Metrics:")
	if agent.Status.Metrics != nil {
		m := agent.Status.Metrics
		successRate := 0.0
		if m.TotalInvocations > 0 {
			successRate = float64(m.SuccessCount) / float64(m.TotalInvocations) * 100
		}
		fmt.Printf("  Invocations:  %d\n", m.TotalInvocations)
		fmt.Printf("  Tokens:       %d\n", m.TotalTokensUsed)
		fmt.Printf("  Cost:         %s\n", formatCost(m.TotalCostUSD))
		fmt.Printf("  Avg Latency:  %dms\n", m.AverageLatencyMs)
		fmt.Printf("  Success:      %d (%.1f%%)\n", m.SuccessCount, successRate)
		fmt.Printf("  Failures:     %d\n", m.FailureCount)
	} else {
		fmt.Printf("  Invocations:  0\n")
		fmt.Printf("  Cost:         $0.00\n")
	}

	if len(agent.Status.Conditions) > 0 {
		fmt.Println()
		fmt.Println("Conditions:")
		condHeaders := []string{"  TYPE", "STATUS", "REASON", "MESSAGE"}
		condRows := make([][]string, 0, len(agent.Status.Conditions))
		for _, c := range agent.Status.Conditions {
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
