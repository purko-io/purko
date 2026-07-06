package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The dashboard must show WHO is logged in (F28). /api/whoami surfaces the
// identity the auth proxy forwards; without SSO the headers are absent and
// the UI hides the chip.
func TestWhoamiReturnsForwardedIdentity(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.Header.Set("X-Forwarded-Email", "admin@purko.io")
	req.Header.Set("X-Forwarded-User", "CiQwOGE4-opaque")
	w := httptest.NewRecorder()
	s.handleWhoami(w, req)

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["user"] != "admin@purko.io" {
		t.Errorf("user = %q, want email preferred", resp["user"])
	}
}

func TestWhoamiEmptyWithoutAuth(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleWhoami(w, httptest.NewRequest(http.MethodGet, "/api/whoami", nil))
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["user"] != "" {
		t.Errorf("user = %q, want empty without auth headers", resp["user"])
	}
}
