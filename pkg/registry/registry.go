package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"gopkg.in/yaml.v3"
)

// MCPServerInfo describes a registered MCP server and its discovered tools.
type MCPServerInfo struct {
	Name      string   `json:"name"      yaml:"name"`
	URL       string   `json:"url"       yaml:"url"`
	Auth      string   `json:"auth"      yaml:"auth"`
	SecretRef string   `json:"secretRef" yaml:"secretRef"`
	Icon      string   `json:"icon"      yaml:"icon"`
	Category  string   `json:"category"  yaml:"category"`
	Tools     []string `json:"tools"     yaml:"-"`
	Status    string   `json:"status"    yaml:"-"`
	ToolCount int      `json:"toolCount" yaml:"-"`
}

// ToolInfo describes a single tool from an MCP server.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Server      string `json:"server"`
	Category    string `json:"category"`
}

// MetricsCallback is called after each sync to update Prometheus metrics.
type MetricsCallback func(serverName, category, status string, toolCount int)

// MCPServerRegistry reads MCP server config from a ConfigMap and discovers tools.
type MCPServerRegistry struct {
	Client    client.Client
	Namespace string
	OnMetrics MetricsCallback

	mu       sync.RWMutex
	servers  []MCPServerInfo
	tools    []ToolInfo
	lastSync time.Time
}

const (
	mcpServersConfigMap = "mcp-servers"
	syncInterval        = 60 * time.Second
)

// Sync reads the mcp-servers ConfigMap and discovers tools from each server.
func (r *MCPServerRegistry) Sync(ctx context.Context) error {
	logger := ctrllog.FromContext(ctx).WithName("mcp-registry")

	cm := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, client.ObjectKey{
		Name:      mcpServersConfigMap,
		Namespace: r.Namespace,
	}, cm); err != nil {
		logger.Info("No mcp-servers ConfigMap found, registry empty", "error", err)
		r.mu.Lock()
		r.servers = nil
		r.tools = nil
		r.lastSync = time.Now()
		r.mu.Unlock()
		return nil
	}

	serversYAML, ok := cm.Data["servers"]
	if !ok {
		return fmt.Errorf("mcp-servers ConfigMap missing 'servers' key")
	}

	var configs []MCPServerInfo
	if err := yaml.Unmarshal([]byte(serversYAML), &configs); err != nil {
		return fmt.Errorf("parse mcp-servers: %w", err)
	}

	var allTools []ToolInfo
	for i := range configs {
		srv := &configs[i]
		token := ""
		if srv.Auth == "bearer" && srv.SecretRef != "" {
			token = r.resolveSecret(ctx, srv.SecretRef)
		}

		tools, err := discoverMCPTools(srv.URL, token)
		if err != nil {
			logger.Info("Failed to discover tools", "server", srv.Name, "error", err)
			srv.Status = "error"
			srv.Tools = nil
			srv.ToolCount = 0
			continue
		}

		srv.Status = "connected"
		srv.Tools = make([]string, len(tools))
		for j, t := range tools {
			srv.Tools[j] = t.Name
			t.Server = srv.Name
			t.Category = srv.Category
			allTools = append(allTools, t)
		}
		srv.ToolCount = len(tools)
		logger.Info("Discovered tools", "server", srv.Name, "count", len(tools))
	}

	if r.OnMetrics != nil {
		for _, srv := range configs {
			r.OnMetrics(srv.Name, srv.Category, srv.Status, srv.ToolCount)
		}
	}

	r.mu.Lock()
	r.servers = configs
	r.tools = allTools
	r.lastSync = time.Now()
	r.mu.Unlock()

	return nil
}

// GetServers returns all registered servers with their status and tool counts.
func (r *MCPServerRegistry) GetServers() []MCPServerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.servers == nil {
		return []MCPServerInfo{}
	}
	result := make([]MCPServerInfo, len(r.servers))
	copy(result, r.servers)
	return result
}

// GetAllTools returns all tools from all servers.
func (r *MCPServerRegistry) GetAllTools() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.tools == nil {
		return []ToolInfo{}
	}
	result := make([]ToolInfo, len(r.tools))
	copy(result, r.tools)
	return result
}

// GetServersJSON returns the server list as a JSON array suitable for the
// MCP_SERVERS env var passed to executor pods.
func (r *MCPServerRegistry) GetServersJSON(ctx context.Context) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type serverEntry struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Auth  string `json:"auth"`
		Token string `json:"token,omitempty"`
	}

	entries := make([]serverEntry, 0, len(r.servers))
	for _, srv := range r.servers {
		entry := serverEntry{
			Name: srv.Name,
			URL:  srv.URL,
			Auth: srv.Auth,
		}
		if srv.Auth == "bearer" && srv.SecretRef != "" {
			entry.Token = r.resolveSecret(ctx, srv.SecretRef)
		}
		entries = append(entries, entry)
	}

	data, _ := json.Marshal(entries)
	return string(data)
}

// SyncIfStale re-syncs the registry if the last sync was more than syncInterval ago.
func (r *MCPServerRegistry) SyncIfStale(ctx context.Context) {
	r.mu.RLock()
	stale := time.Since(r.lastSync) > syncInterval
	r.mu.RUnlock()
	if stale {
		r.Sync(ctx)
	}
}

// StartBackgroundSync runs periodic sync in a goroutine.
func (r *MCPServerRegistry) StartBackgroundSync(ctx context.Context) {
	r.Sync(ctx)

	go func() {
		ticker := time.NewTicker(syncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.Sync(ctx)
			}
		}
	}()
}

func (r *MCPServerRegistry) resolveSecret(ctx context.Context, secretName string) string {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{
		Name:      secretName,
		Namespace: r.Namespace,
	}, secret); err != nil {
		return ""
	}
	if token, ok := secret.Data["token"]; ok {
		return string(token)
	}
	return ""
}

func discoverMCPTools(serverURL, authToken string) ([]ToolInfo, error) {
	serverURL = replaceIP(serverURL)

	tools, err := discoverViaStreamableHTTP(serverURL, authToken)
	if err == nil {
		return tools, nil
	}

	tools, sseErr := discoverViaSSE(serverURL, authToken)
	if sseErr == nil {
		return tools, nil
	}

	return nil, fmt.Errorf("streamable-http: %v; sse: %v", err, sseErr)
}

func discoverViaStreamableHTTP(serverURL, authToken string) ([]ToolInfo, error) {
	endpoint := serverURL + "/mcp"

	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json, text/event-stream",
	}
	if authToken != "" {
		headers["Authorization"] = "Bearer " + authToken
	}

	initPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo":      map[string]string{"name": "agentic-registry", "version": "1.0"},
		},
		"id": 1,
	}

	sessionID, _, err := mcpRequest(endpoint, initPayload, headers)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	if sessionID != "" {
		headers["mcp-session-id"] = sessionID
	}

	listPayload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"params":  map[string]interface{}{},
		"id":      2,
	}

	_, respData, err := mcpRequest(endpoint, listPayload, headers)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var result struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}

	tools := make([]ToolInfo, 0, len(result.Result.Tools))
	for _, t := range result.Result.Tools {
		tools = append(tools, ToolInfo{
			Name:        t.Name,
			Description: t.Description,
		})
	}
	return tools, nil
}

func mcpRequest(endpoint string, payload map[string]interface{}, headers map[string]string) (string, json.RawMessage, error) {
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	sessionID := resp.Header.Get("mcp-session-id")

	if resp.StatusCode != 200 {
		return sessionID, nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")

	if strings.Contains(ct, "application/json") {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return sessionID, nil, err
		}
		return sessionID, data, nil
	}

	buf, _ := io.ReadAll(resp.Body)
	for _, line := range splitLines(string(buf)) {
		if len(line) > 6 && line[:6] == "data: " {
			return sessionID, json.RawMessage(line[6:]), nil
		}
	}

	return sessionID, nil, fmt.Errorf("no data in SSE response")
}

func discoverViaSSE(serverURL, authToken string) ([]ToolInfo, error) {
	sseURL := serverURL + "/sse"

	headers := map[string]string{
		"Accept": "text/event-stream",
	}
	if authToken != "" {
		headers["Authorization"] = "Bearer " + authToken
	}

	req, err := http.NewRequest("GET", sseURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("SSE status %d", resp.StatusCode)
	}

	buf, _ := io.ReadAll(resp.Body)
	sseData := string(buf)

	var messagesURL string
	for _, line := range splitLines(sseData) {
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if strings.Contains(data, "/messages") {
				messagesURL = data
				break
			}
		}
	}

	if messagesURL == "" {
		return nil, fmt.Errorf("no messages URL in SSE stream")
	}

	if strings.HasPrefix(messagesURL, "/") {
		messagesURL = serverURL + messagesURL
	}

	postHeaders := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json, text/event-stream",
	}
	if authToken != "" {
		postHeaders["Authorization"] = "Bearer " + authToken
	}

	_, _, err = mcpRequest(messagesURL, map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]interface{}{},
			"clientInfo":      map[string]string{"name": "purko-registry", "version": "1.0"},
		},
		"id": 1,
	}, postHeaders)
	if err != nil {
		return nil, fmt.Errorf("SSE initialize: %w", err)
	}

	_, respData, err := mcpRequest(messagesURL, map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"params":  map[string]interface{}{},
		"id":      2,
	}, postHeaders)
	if err != nil {
		return nil, fmt.Errorf("SSE tools/list: %w", err)
	}

	var result struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("parse SSE tools: %w", err)
	}

	tools := make([]ToolInfo, 0, len(result.Result.Tools))
	for _, t := range result.Result.Tools {
		tools = append(tools, ToolInfo{Name: t.Name, Description: t.Description})
	}
	return tools, nil
}

func replaceIP(url string) string {
	return strings.ReplaceAll(url, "127.0.0.1", "localhost")
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
