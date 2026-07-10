package dashboard

import (
	"testing"
)

func TestMemorySpecFromRequest(t *testing.T) {
	if memorySpecFromRequest("", "", "", 0) != nil {
		t.Error("empty behavior should map to nil (CRD default session)")
	}
	m := memorySpecFromRequest("persistent", "group", "custom", 4096)
	if m == nil || m.Behavior != "persistent" || m.Scope != "group" || m.ProviderRef != "custom" || m.MaxContextTokens == nil || *m.MaxContextTokens != 4096 {
		t.Fatalf("mapping wrong: %+v", m)
	}
	m2 := memorySpecFromRequest("session", "agent", "", 0)
	if m2.Scope != "" {
		t.Error("scope=agent (default) should not be persisted")
	}
	// "Platform default" + agent scope + no ctx: every optional field omitted.
	if m2.ProviderRef != "" {
		t.Error("empty providerRef (Platform default) should not be persisted")
	}
	if m2.MaxContextTokens != nil {
		t.Error("maxCtx 0 should leave MaxContextTokens nil")
	}
}
