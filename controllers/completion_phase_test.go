package controllers

import "testing"

// User-validated on the demo walkthrough (Stage 1 F10): a run with failed
// steps must never present as a plain green "Succeeded".
func TestCompletionPhase(t *testing.T) {
	cases := []struct {
		name      string
		completed int
		failed    int
		total     int
		strategy  string
		wantPhase string
	}{
		{"all succeeded", 4, 0, 4, "continueOnError", "Succeeded"},
		{"partial failure under continueOnError", 2, 2, 4, "continueOnError", "CompletedWithErrors"},
		{"every step failed", 0, 4, 4, "continueOnError", "Failed"},
		{"single step failed", 0, 1, 1, "continueOnError", "Failed"},
		{"partial failure other strategy", 3, 1, 4, "", "Failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			phase, reason, msg := completionPhase(c.completed, c.failed, c.total, c.strategy)
			if phase != c.wantPhase {
				t.Errorf("phase = %q, want %q", phase, c.wantPhase)
			}
			if reason == "" || msg == "" {
				t.Errorf("reason/msg must be set, got %q / %q", reason, msg)
			}
		})
	}
}
