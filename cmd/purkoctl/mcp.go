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

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP servers",
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all MCP servers",
	RunE:  runMCPList,
}

var mcpToolsCmd = &cobra.Command{
	Use:   "tools [server]",
	Short: "List tools from MCP servers",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMCPTools,
}

var mcpTestCmd = &cobra.Command{
	Use:   "test <server>",
	Short: "Test connectivity to an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPTest,
}

var mcpGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show detailed info for an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPGet,
}

var mcpCreateFile string

var mcpCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an MCP server from a YAML file",
	RunE:  runMCPCreate,
}

var mcpDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPDelete,
}

func init() {
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpGetCmd)
	mcpCreateCmd.Flags().StringVarP(&mcpCreateFile, "file", "f", "", "Path to MCPServer YAML file (required)")
	mcpCreateCmd.MarkFlagRequired("file")
	mcpCmd.AddCommand(mcpCreateCmd)
	mcpCmd.AddCommand(mcpDeleteCmd)
	mcpCmd.AddCommand(mcpToolsCmd)
	mcpCmd.AddCommand(mcpTestCmd)
}

func listMCPServers() ([]v1alpha1.MCPServer, error) {
	var servers v1alpha1.MCPServerList
	// Try all namespaces first; fall back to the current namespace on RBAC failure.
	if err := k8sClient.List(context.TODO(), &servers, client.InNamespace("")); err != nil {
		if err2 := k8sClient.List(context.TODO(), &servers, client.InNamespace(namespace)); err2 != nil {
			return nil, fmt.Errorf("mcp: unable to list servers: %w", err2)
		}
	}
	sort.Slice(servers.Items, func(i, j int) bool {
		if servers.Items[i].Namespace != servers.Items[j].Namespace {
			return servers.Items[i].Namespace < servers.Items[j].Namespace
		}
		return servers.Items[i].Name < servers.Items[j].Name
	})
	return servers.Items, nil
}

func runMCPList(cmd *cobra.Command, args []string) error {
	items, err := listMCPServers()
	if err != nil {
		return err
	}

	if outputFmt == "json" {
		return printJSON(items)
	}

	headers := []string{"NAME", "NAMESPACE", "CATEGORY", "PHASE", "TOOLS", "AGE"}
	rows := make([][]string, 0, len(items))
	for _, s := range items {
		category := s.Spec.Category
		if category == "" {
			category = "-"
		}
		rows = append(rows, []string{
			s.Name,
			s.Namespace,
			category,
			s.Status.Phase,
			fmt.Sprintf("%d", s.Status.ToolCount),
			formatAge(s.CreationTimestamp),
		})
	}
	printTable(headers, rows)
	return nil
}

func runMCPTools(cmd *cobra.Command, args []string) error {
	filterServer := ""
	if len(args) == 1 {
		filterServer = args[0]
	}

	items, err := listMCPServers()
	if err != nil {
		return err
	}

	type toolRow struct {
		tool     string
		server   string
		category string
	}

	var rows []toolRow
	for _, s := range items {
		if filterServer != "" && s.Name != filterServer {
			continue
		}
		category := s.Spec.Category
		if category == "" {
			category = "-"
		}
		// Tools are reported as a count only in status; expose what we know.
		// If the server has tools discovered, list a synthetic entry per server.
		if s.Status.ToolCount > 0 {
			rows = append(rows, toolRow{
				tool:     fmt.Sprintf("(%d tools)", s.Status.ToolCount),
				server:   s.Name,
				category: category,
			})
		} else {
			rows = append(rows, toolRow{
				tool:     "-",
				server:   s.Name,
				category: category,
			})
		}
	}

	if filterServer != "" && len(rows) == 0 {
		return fmt.Errorf("mcp server %q not found", filterServer)
	}

	if outputFmt == "json" {
		type jsonRow struct {
			Tool     string `json:"tool"`
			Server   string `json:"server"`
			Category string `json:"category"`
		}
		out := make([]jsonRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, jsonRow{Tool: r.tool, Server: r.server, Category: r.category})
		}
		return printJSON(out)
	}

	headers := []string{"TOOL", "SERVER", "CATEGORY"}
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{r.tool, r.server, r.category})
	}
	printTable(headers, tableRows)
	return nil
}

func runMCPTest(cmd *cobra.Command, args []string) error {
	name := args[0]

	var server v1alpha1.MCPServer
	// Try in current namespace first, then scan all namespaces.
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &server); err != nil {
		items, listErr := listMCPServers()
		if listErr != nil {
			return fmt.Errorf("mcp server %q not found in namespace %q", name, namespace)
		}
		found := false
		for _, s := range items {
			if s.Name == name {
				server = s
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("mcp server %q not found in namespace %q", name, namespace)
		}
	}

	phase := server.Status.Phase
	if phase == "" {
		phase = "Unknown"
	}
	fmt.Printf("Server %s: %s (%d tools discovered)\n", server.Name, phase, server.Status.ToolCount)

	if strings.EqualFold(phase, "error") && server.Status.Message != "" {
		fmt.Printf("Error: %s\n", server.Status.Message)
	}
	return nil
}

func runMCPGet(cmd *cobra.Command, args []string) error {
	name := args[0]

	var server v1alpha1.MCPServer
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &server); err != nil {
		items, listErr := listMCPServers()
		if listErr != nil {
			return fmt.Errorf("mcp server %q not found in namespace %q", name, namespace)
		}
		found := false
		for _, s := range items {
			if s.Name == name {
				server = s
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("mcp server %q not found", name)
		}
	}

	if outputFmt == "json" {
		return printJSON(server)
	}

	fmt.Printf("Name:         %s\n", server.Name)
	fmt.Printf("Namespace:    %s\n", server.Namespace)
	fmt.Printf("Image:        %s\n", server.Spec.Image)
	fmt.Printf("Port:         %d\n", server.Spec.Port)
	fmt.Printf("Auth:         %s\n", server.Spec.Auth)
	fmt.Printf("Category:     %s\n", server.Spec.Category)
	fmt.Printf("Phase:        %s\n", server.Status.Phase)
	fmt.Printf("Tools:        %d\n", server.Status.ToolCount)
	fmt.Printf("Age:          %s\n", formatAge(server.CreationTimestamp))

	if server.Status.Message != "" {
		fmt.Printf("Message:      %s\n", server.Status.Message)
	}

	if len(server.Status.Conditions) > 0 {
		fmt.Println()
		fmt.Println("Conditions:")
		condHeaders := []string{"  TYPE", "STATUS", "REASON", "MESSAGE"}
		condRows := make([][]string, 0, len(server.Status.Conditions))
		for _, c := range server.Status.Conditions {
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

func runMCPCreate(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(mcpCreateFile)
	if err != nil {
		return fmt.Errorf("unable to read file %q: %w", mcpCreateFile, err)
	}

	var server v1alpha1.MCPServer
	if err := yaml.Unmarshal(data, &server); err != nil {
		return fmt.Errorf("unable to parse YAML: %w", err)
	}

	if server.Namespace == "" {
		server.Namespace = namespace
	}

	if err := k8sClient.Create(context.TODO(), &server); err != nil {
		return fmt.Errorf("unable to create MCP server: %w", err)
	}

	fmt.Printf("MCPServer %s created in namespace %s\n", server.Name, server.Namespace)
	return nil
}

func runMCPDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	var server v1alpha1.MCPServer
	if err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, &server); err != nil {
		return fmt.Errorf("mcp server %q not found in namespace %q", name, namespace)
	}

	if err := k8sClient.Delete(context.TODO(), &server); err != nil {
		return fmt.Errorf("unable to delete mcp server %q: %w", name, err)
	}

	fmt.Printf("MCPServer %s deleted from namespace %s\n", name, namespace)
	return nil
}
