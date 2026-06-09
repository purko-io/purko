package licensing

// TierLimits defines the resource and feature limits for each tier.
// A value of 0 for integer limits means unlimited.
// Callers should check: if limit > 0 && current > limit { deny }
type TierLimits struct {
	MaxAgents          int  // 0 = unlimited
	MaxExecutionsMonth int  // 0 = unlimited
	MaxClusters        int  // 0 = unlimited
	MaxUsers           int  // 0 = unlimited
	DAGWorkflows       bool
	WebDashboard       bool
	IntentBar          bool
	VisualBuilder      bool
	WebhookEvents      int  // per month, 0 = unlimited
	HistoryRetention   int  // days, 0 = unlimited
}

var Limits = map[Tier]TierLimits{
	Community: {
		MaxAgents:          10,
		MaxExecutionsMonth: 1000,
		MaxClusters:        1,
		MaxUsers:           0,
		DAGWorkflows:       false,
		WebDashboard:       false,
		IntentBar:          false,
		VisualBuilder:      false,
		WebhookEvents:      0,
		HistoryRetention:   7,
	},
	Pro: {
		MaxAgents:          50,
		MaxExecutionsMonth: 25000,
		MaxClusters:        3,
		MaxUsers:           25,
		DAGWorkflows:       true,
		WebDashboard:       true,
		IntentBar:          true,
		VisualBuilder:      true,
		WebhookEvents:      10000,
		HistoryRetention:   90,
	},
	Enterprise: {
		MaxAgents:          0,
		MaxExecutionsMonth: 0,
		MaxClusters:        0,
		MaxUsers:           0,
		DAGWorkflows:       true,
		WebDashboard:       true,
		IntentBar:          true,
		VisualBuilder:      true,
		WebhookEvents:      0,
		HistoryRetention:   0,
	},
}

// GetLimits returns the limits for the current tier.
// In dev mode, returns Enterprise limits (no restrictions).
func GetLimits() TierLimits {
	if IsDevMode() {
		return Limits[Enterprise]
	}
	return Limits[GetTier()]
}
