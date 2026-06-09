package licensing

// validateLicense checks a license key and returns the tier it grants.
// Stub implementation — accepts simple tier strings for testing.
// Will be replaced with JWT/signature validation when the licensing
// service is built. The returned Tier determines which features and
// limits apply.
func validateLicense(key string) Tier {
	switch key {
	case "pro":
		return Pro
	case "enterprise":
		return Enterprise
	default:
		return Community
	}
}
