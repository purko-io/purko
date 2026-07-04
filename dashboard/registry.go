package dashboard

import "github.com/purko-io/purko/pkg/registry"

// Re-export types from pkg/registry for backward compatibility.
// The dashboard server uses these types directly.
type MCPServerRegistry = registry.MCPServerRegistry
type MCPServerInfo = registry.MCPServerInfo
type ToolInfo = registry.ToolInfo
type MetricsCallback = registry.MetricsCallback
