package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Mac signing build posts these three 小moto paths. This server does not
// implement them. Do not alias them to /v1/hajimi/auth/login: that handler is
// not this user store, and a non-account 400 does not prove its token works
// for /v1/me.
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
