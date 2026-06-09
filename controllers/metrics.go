package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// Agent metrics
	AgentInvocations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "purko",
		Name:      "agent_invocations_total",
		Help:      "Total agent invocations by status",
	}, []string{"agent", "namespace", "status"})

	AgentTokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "purko",
		Name:      "agent_tokens_total",
		Help:      "Total tokens consumed by agents",
	}, []string{"agent", "namespace"})

	AgentCostUSD = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "purko",
		Name:      "agent_cost_usd_total",
		Help:      "Total cost in USD by agent",
	}, []string{"agent", "namespace"})

	AgentLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "purko",
		Name:      "agent_latency_seconds",
		Help:      "Agent step execution latency",
		Buckets:   []float64{5, 15, 30, 60, 120, 300, 600},
	}, []string{"agent", "namespace"})

	// Workflow metrics
	WorkflowDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "purko",
		Name:      "workflow_duration_seconds",
		Help:      "Workflow execution duration",
		Buckets:   []float64{10, 30, 60, 120, 300, 600, 1800},
	}, []string{"workflow", "namespace", "status"})

	WorkflowCostUSD = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "purko",
		Name:      "workflow_cost_usd_total",
		Help:      "Total cost in USD per workflow execution",
	}, []string{"workflow", "namespace"})

	WorkflowStepDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "purko",
		Name:      "step_duration_seconds",
		Help:      "Workflow step execution duration",
		Buckets:   []float64{5, 15, 30, 60, 120, 300},
	}, []string{"workflow", "step", "agent"})

	WorkflowsActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "purko",
		Name:      "workflows_active",
		Help:      "Number of currently running workflows",
	}, []string{"namespace"})

	// Shu-Ha-Ri metrics
	ShuHaRiLevel = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "purko",
		Name:      "shuhari_level",
		Help:      "Agent Shu-Ha-Ri level (1=shu, 2=ha, 3=ri)",
	}, []string{"agent", "namespace"})

	// MCP metrics
	MCPServerStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "purko",
		Name:      "mcp_server_status",
		Help:      "MCP server connection status (1=connected, 0=error)",
	}, []string{"server", "category"})

	MCPServerTools = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "purko",
		Name:      "mcp_server_tools",
		Help:      "Number of tools per MCP server",
	}, []string{"server", "category"})

	// Platform totals
	AgentsTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "purko",
		Name:      "agents_total",
		Help:      "Total number of agents by type",
	}, []string{"namespace", "type"})
)

func init() {
	metrics.Registry.MustRegister(
		AgentInvocations,
		AgentTokens,
		AgentCostUSD,
		AgentLatency,
		WorkflowDuration,
		WorkflowCostUSD,
		WorkflowStepDuration,
		WorkflowsActive,
		ShuHaRiLevel,
		MCPServerStatus,
		MCPServerTools,
		AgentsTotal,
	)
}

// ShuHaRiLevelToFloat converts Shu-Ha-Ri level string to float for Prometheus.
func ShuHaRiLevelToFloat(level string) float64 {
	switch level {
	case "shu":
		return 1
	case "ha":
		return 2
	case "ri":
		return 3
	default:
		return 0
	}
}

// UpdateMCPMetrics is called by the registry to update MCP Prometheus metrics.
func UpdateMCPMetrics(serverName, category, status string, toolCount int) {
	statusVal := 0.0
	if status == "connected" {
		statusVal = 1.0
	}
	MCPServerStatus.WithLabelValues(serverName, category).Set(statusVal)
	MCPServerTools.WithLabelValues(serverName, category).Set(float64(toolCount))
}
