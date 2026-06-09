package licensing

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

func resetState() {
	currentTier = Community
	devMode = true
	initialized = false
	once = sync.Once{}
}

func TestDevModeByDefault(t *testing.T) {
	resetState()
	if !IsDevMode() {
		t.Error("expected dev mode when Init() not called")
	}
	if !IsProEnabled() {
		t.Error("expected IsProEnabled() true in dev mode")
	}
	if !IsEnterpriseEnabled() {
		t.Error("expected IsEnterpriseEnabled() true in dev mode")
	}
}

func TestDevModeNoLicense(t *testing.T) {
	resetState()
	os.Unsetenv("PURKO_LICENSE")
	Init(nil)
	if !IsDevMode() {
		t.Error("expected dev mode with no license")
	}
	if !IsProEnabled() {
		t.Error("expected IsProEnabled() true in dev mode")
	}
	if GetTier() != Community {
		t.Errorf("expected Community tier, got %s", GetTier())
	}
}

func TestDevModeLicenseString(t *testing.T) {
	resetState()
	os.Setenv("PURKO_LICENSE", "dev")
	defer os.Unsetenv("PURKO_LICENSE")
	Init(nil)
	if !IsDevMode() {
		t.Error("expected dev mode with license=dev")
	}
}

func TestProLicense(t *testing.T) {
	resetState()
	os.Setenv("PURKO_LICENSE", "pro")
	defer os.Unsetenv("PURKO_LICENSE")
	Init(nil)
	if IsDevMode() {
		t.Error("expected non-dev mode with pro license")
	}
	if GetTier() != Pro {
		t.Errorf("expected Pro tier, got %s", GetTier())
	}
	if !IsProEnabled() {
		t.Error("expected IsProEnabled() true for Pro tier")
	}
	if IsEnterpriseEnabled() {
		t.Error("expected IsEnterpriseEnabled() false for Pro tier")
	}
}

func TestEnterpriseLicense(t *testing.T) {
	resetState()
	os.Setenv("PURKO_LICENSE", "enterprise")
	defer os.Unsetenv("PURKO_LICENSE")
	Init(nil)
	if GetTier() != Enterprise {
		t.Errorf("expected Enterprise tier, got %s", GetTier())
	}
	if !IsEnterpriseEnabled() {
		t.Error("expected IsEnterpriseEnabled() true for Enterprise tier")
	}
}

func TestUnknownLicense(t *testing.T) {
	resetState()
	os.Setenv("PURKO_LICENSE", "invalid-key-xyz")
	defer os.Unsetenv("PURKO_LICENSE")
	Init(nil)
	if IsDevMode() {
		t.Error("expected non-dev mode with unknown license")
	}
	if GetTier() != Community {
		t.Errorf("expected Community tier for unknown key, got %s", GetTier())
	}
}

func TestSecretReaderFallback(t *testing.T) {
	resetState()
	os.Unsetenv("PURKO_LICENSE")
	Init(func() (string, error) {
		return "pro", nil
	})
	if GetTier() != Pro {
		t.Errorf("expected Pro from secret reader, got %s", GetTier())
	}
}

func TestSecretReaderError(t *testing.T) {
	resetState()
	os.Unsetenv("PURKO_LICENSE")
	Init(func() (string, error) {
		return "", fmt.Errorf("secret not found")
	})
	if !IsDevMode() {
		t.Error("expected dev mode when secret reader fails")
	}
}

func TestGetLimitsDevMode(t *testing.T) {
	resetState()
	os.Unsetenv("PURKO_LICENSE")
	Init(nil)
	limits := GetLimits()
	if limits.MaxAgents != 0 {
		t.Errorf("expected unlimited agents in dev mode, got %d", limits.MaxAgents)
	}
}

func TestGetLimitsCommunity(t *testing.T) {
	resetState()
	os.Setenv("PURKO_LICENSE", "invalid")
	defer os.Unsetenv("PURKO_LICENSE")
	Init(nil)
	limits := GetLimits()
	if limits.MaxAgents != 10 {
		t.Errorf("expected 10 agents for Community, got %d", limits.MaxAgents)
	}
	if limits.DAGWorkflows {
		t.Error("expected DAGWorkflows false for Community")
	}
}

func TestTierString(t *testing.T) {
	if Community.String() != "Community" {
		t.Errorf("expected Community, got %s", Community.String())
	}
	if Pro.String() != "Pro" {
		t.Errorf("expected Pro, got %s", Pro.String())
	}
	if Enterprise.String() != "Enterprise" {
		t.Errorf("expected Enterprise, got %s", Enterprise.String())
	}
}
