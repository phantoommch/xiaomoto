package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Mac signing client posts these 小moto paths. This server does not
// implement them. They must stay 404 until the users table that backs
// /v1/me gains its real password columns. Do not add a second hasher here
// and do not point the client at another handler.
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
