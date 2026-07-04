package dashboard

// Community-edition replacement for auth_log.go (Spec 28).
//
// The Pro auth_log.go imports pkg/pro/sso (BSL) to identify the
// authenticated user behind the OAuth2 Proxy sidecar. The community edition
// ships no SSO, so logUser is a no-op. This file lives under
// _community_overrides/ in the private repo (Go tooling ignores the
// underscore-prefixed dir) and is moved into dashboard/ by the OSS sync
// script, replacing the excluded auth_log.go.

import "net/http"

// logUser is a no-op in the community edition (no SSO user identification).
func (s *Server) logUser(r *http.Request) {}
