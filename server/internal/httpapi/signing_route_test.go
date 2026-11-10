package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Mac signing build posts /v1/auth/login-password. This open-source server
// does not implement that route and must not grow a second password hasher for
// it. Production already has a different hasher behind /v1/hajimi/auth/login.
func TestSigningLoginPasswordStaysUnimplementedHere(t *testing.T) {
	handler, _ := openAuth(t, true, "review@example.com", "246810")
	for _, path := range []string{"/v1/auth/login-password", "/v1/auth/reset-password", "/v1/auth/set-password", "/v1/auth/login"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"probe@example.com","password":"short","termsVersion":"1.0"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status %d body %s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Header().Get("Content-Type"), "json") || strings.Contains(rec.Body.String(), "token") {
			t.Fatalf("%s must stay a plain 404, got %s", path, rec.Body.String())
		}
	}
}
