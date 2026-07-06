package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/purko-io/purko/api/v1alpha1"
)

func newMCPServer(t *testing.T, objs ...client.Object) (*Server, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1 AddToScheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &Server{Client: c}, c
}

func mcpConfigEntries(t *testing.T, c client.Client) []map[string]interface{} {
	t.Helper()
	cm := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: "mcp-servers", Namespace: "ai-agents"}, cm); err != nil {
		t.Fatalf("get mcp-servers ConfigMap: %v", err)
	}
	var servers []map[string]interface{}
	if err := json.Unmarshal([]byte(cm.Data["servers"]), &servers); err != nil {
		t.Fatalf("parse servers: %v", err)
	}
	return servers
}

// The MCP form could only DEPLOY a server from a container image; there was
// no way to CONNECT an already-running server by URL (Stage 1 finding F20),
// even though the registry reads URL entries from the mcp-servers ConfigMap.
func TestMCPServerConnectByURL(t *testing.T) {
	s, c := newMCPServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/server", strings.NewReader(`{
		"name": "demo-tools", "url": "http://demo-mcp.ai-agents:8000/mcp",
		"auth": "none", "category": "demo", "icon": "T"
	}`))
	w := httptest.NewRecorder()
	s.handleMCPServerCreate(w, req)

	servers := mcpConfigEntries(t, c)
	if len(servers) != 1 || servers[0]["name"] != "demo-tools" ||
		servers[0]["url"] != "http://demo-mcp.ai-agents:8000/mcp" {
		t.Errorf("unexpected servers: %+v", servers)
	}

	// No MCPServer CR should be created for a URL connection
	list := &v1alpha1.MCPServerList{}
	if err := c.List(context.Background(), list); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("URL connect must not create an MCPServer CR, got %d", len(list.Items))
	}
}

func TestMCPServerConnectUpsertsByName(t *testing.T) {
	cm := &corev1.ConfigMap{}
	cm.Name = "mcp-servers"
	cm.Namespace = "ai-agents"
	cm.Data = map[string]string{"servers": `[{"name":"demo-tools","url":"http://old:1"},{"name":"other","url":"http://keep:2"}]`}
	s, c := newMCPServer(t, cm)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/server", strings.NewReader(`{
		"name": "demo-tools", "url": "http://new:9"
	}`))
	s.handleMCPServerCreate(httptest.NewRecorder(), req)

	servers := mcpConfigEntries(t, c)
	if len(servers) != 2 {
		t.Fatalf("want 2 servers, got %+v", servers)
	}
	for _, srv := range servers {
		if srv["name"] == "demo-tools" && srv["url"] != "http://new:9" {
			t.Errorf("demo-tools not updated: %+v", srv)
		}
		if srv["name"] == "other" && srv["url"] != "http://keep:2" {
			t.Errorf("other entry damaged: %+v", srv)
		}
	}
}

// Deleting a URL-connected server (no CR) must remove its ConfigMap entry
// instead of returning 404.
func TestMCPServerDeleteRemovesConfigMapEntry(t *testing.T) {
	cm := &corev1.ConfigMap{}
	cm.Name = "mcp-servers"
	cm.Namespace = "ai-agents"
	cm.Data = map[string]string{"servers": `[{"name":"demo-tools","url":"http://x:1"},{"name":"other","url":"http://keep:2"}]`}
	s, c := newMCPServer(t, cm)

	req := httptest.NewRequest(http.MethodDelete, "/api/mcp/server/demo-tools", nil)
	w := httptest.NewRecorder()
	s.handleMCPServerCRUD(w, req)
	if w.Code == 404 {
		t.Fatalf("delete returned 404 for ConfigMap-registered server")
	}

	servers := mcpConfigEntries(t, c)
	if len(servers) != 1 || servers[0]["name"] != "other" {
		t.Errorf("want only 'other' left, got %+v", servers)
	}
}
