package registry

import "testing"

// Users paste full MCP endpoints (…/mcp) while Helm examples use base URLs;
// both must work (Stage 1 finding F21 — …/mcp/mcp 404s).
func TestStreamableEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://demo-mcp.ai-agents:8000":      "http://demo-mcp.ai-agents:8000/mcp",
		"http://demo-mcp.ai-agents:8000/":     "http://demo-mcp.ai-agents:8000/mcp",
		"http://demo-mcp.ai-agents:8000/mcp":  "http://demo-mcp.ai-agents:8000/mcp",
		"http://demo-mcp.ai-agents:8000/mcp/": "http://demo-mcp.ai-agents:8000/mcp",
	}
	for in, want := range cases {
		if got := streamableEndpoint(in); got != want {
			t.Errorf("streamableEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSSEEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://x:8000":     "http://x:8000/sse",
		"http://x:8000/mcp": "http://x:8000/sse",
		"http://x:8000/sse": "http://x:8000/sse",
	}
	for in, want := range cases {
		if got := sseEndpoint(in); got != want {
			t.Errorf("sseEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}
