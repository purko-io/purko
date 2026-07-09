package v1alpha1

import "testing"

// Spec 34 §1: legacy type<->behavior equivalence is for webhook validation and
// dashboard display ONLY (never applied at runtime). none<->off, buffer<->session,
// summary<->persistent, vector->persistent (vector has no reverse — persistent maps
// to summary's env path, and vector is deprecated for new agents).
func TestBehaviorForType(t *testing.T) {
	cases := []struct{ typ, want string }{
		{"none", "off"},
		{"buffer", "session"},
		{"summary", "persistent"},
		{"vector", "persistent"},
		{"", ""},
		{"bogus", ""},
	}
	for _, c := range cases {
		if got := BehaviorForType(c.typ); got != c.want {
			t.Errorf("BehaviorForType(%q) = %q, want %q", c.typ, got, c.want)
		}
	}
}

func TestTypeForBehavior(t *testing.T) {
	cases := []struct{ behavior, want string }{
		{"off", "none"},
		{"session", "buffer"},
		{"persistent", "summary"},
		{"", ""},
		{"bogus", ""},
	}
	for _, c := range cases {
		if got := TypeForBehavior(c.behavior); got != c.want {
			t.Errorf("TypeForBehavior(%q) = %q, want %q", c.behavior, got, c.want)
		}
	}
}
