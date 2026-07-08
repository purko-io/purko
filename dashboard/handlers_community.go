package dashboard

// Community-edition replacement for handlers_pro.go (Spec 28).
//
// The license boundary is handler presence: this file registers nothing, so
// /api/intent and /api/autonomy/policy do not exist in the community binary
// (404) — there is no gate to flip, the Pro code is simply not compiled in.
// This file lives under _community_overrides/ in the private repo (Go
// tooling ignores the underscore-prefixed dir) and is moved into dashboard/
// by the OSS sync script, replacing the excluded handlers_pro.go.

import (
	"context"
	"net/http"
)

// registerProHandlers is a no-op in the community edition: no Pro routes.
func (s *Server) registerProHandlers(mux *http.ServeMux) {}

// proFeatures reports which Pro capabilities are compiled into this build.
// HARD-CODED all-false by design — never derived from the license tier, so
// it can never advertise a handler that is not compiled in.
func proFeatures() map[string]bool {
	return map[string]bool{
		"intent":   false,
		"autonomy": false,
		"sso":      false,
		"history":  true,
	}
}

// createIntentWorkflow is a compile stub: the shared handleWebhookTrigger
// calls it in its "_intent" branch. Returning "" makes the caller treat the
// webhook as not-routed and respond with the community-specific error
// ("the _intent fallback requires Purko Pro").
func (s *Server) createIntentWorkflow(ctx context.Context, ns string, payload NormalizedPayload) string {
	return ""
}

// historyRetentionDays reports the community retention window. HARD-CODED
// like proFeatures: the community binary carries no licensing package, and
// the value must match the community pruner's 7-day window.
func historyRetentionDays() int { return 7 }
