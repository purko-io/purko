package v1alpha1

// Legacy memory.type <-> behavior mapping. Spec 34 §1: this equivalence is used
// ONLY for webhook validation and dashboard display — it is NEVER applied at
// runtime (see MemorySpec.Type doc). Returns "" for unknown/empty input.

// BehaviorForType maps a legacy memory.type to its display behavior.
func BehaviorForType(t string) string {
	switch t {
	case "none":
		return "off"
	case "buffer":
		return "session"
	case "summary", "vector":
		return "persistent"
	default:
		return ""
	}
}

// TypeForBehavior maps a behavior to the legacy MEMORY_TYPE env value. Note this
// is the ENV mapping (persistent->summary keeps legacy executors loading
// AGENT_MEMORY); it is not a reverse of BehaviorForType (vector never round-trips).
func TypeForBehavior(b string) string {
	switch b {
	case "off":
		return "none"
	case "session":
		return "buffer"
	case "persistent":
		return "summary"
	default:
		return ""
	}
}
