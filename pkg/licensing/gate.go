// Package licensing provides tier-based feature gating for Purko.
//
// Purko supports three tiers: Community (free), Pro, and Enterprise.
// The tier is determined by a license key read from the PURKO_LICENSE
// environment variable or a purko-license Kubernetes Secret.
//
// When no license key is configured, the system runs in "dev mode"
// where all features are enabled. This ensures existing installations
// and development environments work without a license.
//
// Call Init() once at operator startup before using any gate functions.
package licensing

import (
	"log"
	"os"
	"sync"
)

// Tier represents a Purko license tier.
type Tier int

const (
	Community  Tier = 0
	Pro        Tier = 1
	Enterprise Tier = 2
)

func (t Tier) String() string {
	switch t {
	case Pro:
		return "Pro"
	case Enterprise:
		return "Enterprise"
	default:
		return "Community"
	}
}

var (
	currentTier Tier
	devMode     = true
	initialized bool
	once        sync.Once
)

// Init initializes the licensing system. Called once at operator startup.
// Reads license from PURKO_LICENSE env var, falls back to purko-license Secret
// via the provided secretReader callback. If no license found, enters dev mode
// (all features enabled).
//
// The secretReader should return the raw license key string from the
// purko-license Secret's "license" key. Pass nil if Secret reading is
// not available.
func Init(secretReader func() (string, error)) {
	once.Do(func() {
		initialized = true
		license := os.Getenv("PURKO_LICENSE")

		if license == "" && secretReader != nil {
			val, err := secretReader()
			if err != nil {
				log.Printf("licensing: warning: failed to read license secret: %v", err)
			} else {
				license = val
			}
		}

		if license == "" || license == "dev" {
			devMode = true
			currentTier = Community
			return
		}

		devMode = false
		currentTier = validateLicense(license)
	})
}

// GetTier returns the current license tier.
func GetTier() Tier {
	return currentTier
}

// IsDevMode returns true when no license is configured (all features enabled).
func IsDevMode() bool {
	return devMode
}

// IsProEnabled returns true if the current tier is Pro or higher, or if in dev mode.
func IsProEnabled() bool {
	return devMode || currentTier >= Pro
}

// IsEnterpriseEnabled returns true if the current tier is Enterprise, or if in dev mode.
func IsEnterpriseEnabled() bool {
	return devMode || currentTier >= Enterprise
}
