package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// Spec 29 #6: /api/features carries historyRetentionDays as a parallel
// field. The Spec 28 contract is preserved: every other key is a bool,
// and the handler works on a Server with no history store (nil guard).
func TestFeaturesIncludesRetentionDays(t *testing.T) {
	s := &Server{} // deliberately bare: handler must not touch history/client
	rec := httptest.NewRecorder()
	s.handleFeatures(rec, httptest.NewRequest("GET", "/api/features", nil))

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	days, ok := resp["historyRetentionDays"].(float64)
	if !ok {
		t.Fatalf("historyRetentionDays missing or not a number: %#v", resp["historyRetentionDays"])
	}
	if days < 0 {
		t.Errorf("negative retention: %v", days)
	}
	for k, v := range resp {
		if k == "historyRetentionDays" {
			continue
		}
		if _, isBool := v.(bool); !isBool {
			t.Errorf("feature %q is not a bool (%#v) — Spec 28 bool-map contract broken", k, v)
		}
	}
}
