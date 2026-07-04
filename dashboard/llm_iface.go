package dashboard

// Shared LLM type contracts (Spec 28). This file ships to BOTH editions:
// the Server struct references LLMProvider, so the interface must exist in
// the community build even though the concrete providers, constructors, and
// the intent prompt (llm.go) are Pro-only and excluded via OSS_EXCLUDE.
// Type contracts are not IP; keep implementations out of this file.

import "context"

// LLMProvider is the interface for LLM completions used by the dashboard
// (Intent Bar, webhook routing fallback).
type LLMProvider interface {
	Complete(ctx context.Context, system string, prompt string) (string, error)
}

// LLMConfig holds provider configuration.
type LLMConfig struct {
	Provider  string // vertex-ai, anthropic, openai
	Model     string // e.g. claude-sonnet-4-6
	APIKey    string // for direct API providers
	ProjectID string // for Vertex AI
	Region    string // for Vertex AI
}
