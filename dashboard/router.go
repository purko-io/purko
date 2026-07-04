package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TriggerRule maps a webhook source + match conditions to a workflow template.
type TriggerRule struct {
	Name     string            `json:"name"`
	Source   string            `json:"source"`   // pagerduty, github, slack, or * for any
	Match    map[string]string `json:"match"`    // key-value pairs to match in normalized payload
	Workflow string            `json:"workflow"` // workflow template name, or "_intent" for LLM fallback
}

// NormalizedPayload extracts key fields from different webhook sources.
type NormalizedPayload struct {
	Source      string `json:"source"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Service     string `json:"service"`
	Action      string `json:"action"`
	Type        string `json:"type"`
	Repository  string `json:"repository"`
	User        string `json:"user"`
	Description string `json:"description"`
}

// normalizePayload extracts standard fields from different webhook formats.
func normalizePayload(source string, raw map[string]interface{}) NormalizedPayload {
	n := NormalizedPayload{Source: source}

	switch strings.ToLower(source) {
	case "pagerduty":
		if event, ok := raw["event"].(map[string]interface{}); ok {
			if data, ok := event["data"].(map[string]interface{}); ok {
				n.Title = getString(data, "title")
				n.Severity = getString(data, "urgency")
				if svc, ok := data["service"].(map[string]interface{}); ok {
					n.Service = getString(svc, "name")
				}
			}
			n.Type = getString(event, "event_type")
			n.Action = getString(event, "event_type")
		}
		// Also try flat format
		if n.Title == "" {
			n.Title = getString(raw, "title")
		}
		if n.Severity == "" {
			n.Severity = getString(raw, "severity")
		}
		if n.Service == "" {
			n.Service = getString(raw, "service")
		}

	case "github":
		n.Action = getString(raw, "action")
		if repo, ok := raw["repository"].(map[string]interface{}); ok {
			n.Repository = getString(repo, "full_name")
		}
		if pr, ok := raw["pull_request"].(map[string]interface{}); ok {
			n.Title = getString(pr, "title")
			n.Type = "pull_request"
		}
		if issue, ok := raw["issue"].(map[string]interface{}); ok {
			n.Title = getString(issue, "title")
			n.Type = "issue"
		}
		if n.Type == "" {
			// Detect from known GitHub event fields
			if _, ok := raw["push"]; ok {
				n.Type = "push"
			}
			if _, ok := raw["ref"]; ok {
				n.Type = "push"
			}
		}

	case "slack":
		n.Action = getString(raw, "command")
		n.Title = getString(raw, "text")
		n.User = getString(raw, "user_name")
		n.Type = "slash_command"

	default:
		// Generic extraction
		n.Title = getString(raw, "title")
		n.Severity = getString(raw, "severity")
		n.Service = getString(raw, "service")
		n.Action = getString(raw, "action")
		n.Type = getString(raw, "type")
	}

	// Build description for LLM fallback
	parts := []string{}
	if n.Title != "" {
		parts = append(parts, n.Title)
	}
	if n.Service != "" {
		parts = append(parts, "service: "+n.Service)
	}
	if n.Severity != "" {
		parts = append(parts, "severity: "+n.Severity)
	}
	if n.Repository != "" {
		parts = append(parts, "repo: "+n.Repository)
	}
	if n.Action != "" {
		parts = append(parts, "action: "+n.Action)
	}
	n.Description = strings.Join(parts, " | ")

	return n
}

// loadRules reads routing rules from a ConfigMap.
func loadRules(ctx context.Context, k8s client.Client, namespace string) []TriggerRule {
	cm := &corev1.ConfigMap{}
	err := k8s.Get(ctx, client.ObjectKey{Name: "trigger-rules", Namespace: namespace}, cm)
	if errors.IsNotFound(err) {
		return defaultRules()
	}
	if err != nil {
		return defaultRules()
	}

	rulesJSON, ok := cm.Data["rules"]
	if !ok {
		return defaultRules()
	}

	var rules []TriggerRule
	if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
		return defaultRules()
	}
	return rules
}

// defaultRules provides built-in routing when no ConfigMap exists.
// The catch-all "_intent" fallback requires the Pro intent handler; it is
// only included when the intent capability is compiled in (Spec 28) —
// community builds fail unmatched webhooks loudly instead of silently.
func defaultRules() []TriggerRule {
	rules := []TriggerRule{
		{
			Name: "pagerduty-critical", Source: "pagerduty",
			Match:    map[string]string{"severity": "critical"},
			Workflow: "incident-response",
		},
		{
			Name: "pagerduty-high", Source: "pagerduty",
			Match:    map[string]string{"severity": "high"},
			Workflow: "cluster-investigation",
		},
		{
			Name: "github-pr", Source: "github",
			Match:    map[string]string{"type": "pull_request"},
			Workflow: "cluster-sdlc-analysis",
		},
		{
			Name: "github-push", Source: "github",
			Match:    map[string]string{"type": "push"},
			Workflow: "cluster-sdlc-analysis",
		},
	}
	if proFeatures()["intent"] {
		rules = append(rules, TriggerRule{
			Name: "fallback", Source: "*",
			Match:    map[string]string{},
			Workflow: "_intent",
		})
	}
	return rules
}

// matchRule checks if a normalized payload matches a rule.
func matchRule(rule TriggerRule, payload NormalizedPayload) bool {
	// Check source
	if rule.Source != "*" && !strings.EqualFold(rule.Source, payload.Source) {
		return false
	}

	// Check all match conditions
	for key, expected := range rule.Match {
		var actual string
		switch key {
		case "severity":
			actual = payload.Severity
		case "service":
			actual = payload.Service
		case "action":
			actual = payload.Action
		case "type":
			actual = payload.Type
		case "repository":
			actual = payload.Repository
		}
		if !strings.EqualFold(actual, expected) {
			return false
		}
	}
	return true
}

// routeWebhook finds the best matching rule for a webhook payload.
func routeWebhook(rules []TriggerRule, payload NormalizedPayload) (TriggerRule, bool) {
	for _, rule := range rules {
		if matchRule(rule, payload) {
			return rule, true
		}
	}
	return TriggerRule{}, false
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
