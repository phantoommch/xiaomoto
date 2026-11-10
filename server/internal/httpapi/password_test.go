package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordRoutesAgreeAndDoNotSwallow404(t *testing.T) {
	handler, _ := openAuth(t, true, "review@example.com", "246810")
	const email = "rider-pw@example.com"
	const password = "correct-horse"
	asked := requestDevCode(t, handler, email)
	if asked.Code != http.StatusOK {
		t.Fatalf("request-code %d %s", asked.Code, asked.Body.String())
	}
	registered := postRaw(t, handler, http.MethodPost, "/v1/hajimi/auth/register", map[string]any{
		"email": email, "code": devCode(t, email), "password": password, "termsVersion": "1.0",
	}, "")
	if registered.Code != http.StatusOK || !strings.Contains(registered.Body.String(), `"token"`) {
		t.Fatalf("register %d %s", registered.Code, registered.Body.String())
	}

	for _, path := range []string{"/v1/hajimi/auth/login", "/v1/auth/login"} {
		badJSON := postBytes(t, handler, path, []byte("x"))
		assertJSONError(t, badJSON, http.StatusBadRequest, "请求格式不对")
		unknown := postRaw(t, handler, http.MethodPost, path, map[string]any{
			"email": email, "password": password, "termsVersion": "1.0",
		}, "")
		assertJSONError(t, unknown, http.StatusBadRequest, "请求格式不对")
		wrong := postRaw(t, handler, http.MethodPost, path, map[string]any{
			"email": email, "password": "wrong-password",
		}, "")
		assertJSONError(t, wrong, http.StatusBadRequest, "邮箱或密码不对")
		ok := postRaw(t, handler, http.MethodPost, path, map[string]any{
			"email": email, "password": password,
		}, "")
		if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"token"`) {
			t.Fatalf("%s login %d %s", path, ok.Code, ok.Body.String())
		}
		if !strings.Contains(ok.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s content-type %s", path, ok.Header().Get("Content-Type"))
		}
	}

	review := postRaw(t, handler, http.MethodPost, "/v1/auth/login", map[string]any{
		"email": "review@example.com", "password": "246810",
	}, "")
	assertJSONError(t, review, http.StatusBadRequest, "邮箱或密码不对")

	for _, path := range []string{"/v1/v1/auth/login", "/v1/v1/hajimi/auth/login", "/v1/hajimi/v1/auth/login"} {
		miss := postRaw(t, handler, http.MethodPost, path, map[string]any{
			"email": email, "password": password,
		}, "")
		if miss.Code != http.StatusNotFound {
			t.Fatalf("%s status %d %s", path, miss.Code, miss.Body.String())
		}
		if strings.Contains(miss.Header().Get("Content-Type"), "json") || strings.Contains(miss.Body.String(), "token") {
			t.Fatalf("%s swallowed 404 %s", path, miss.Body.String())
		}
	}

	missing := postRaw(t, handler, http.MethodPost, "/v1/hajimi/auth/login", map[string]any{
		"email": "nobody@example.com", "password": "correct-horse",
	}, "")
	assertJSONError(t, missing, http.StatusBadRequest, "邮箱或密码不对")

	for _, path := range []string{
		"/v1/hajimi/auth/email/code",
		"/v1/auth/email/code",
		"/v1/hajimi/auth/email/verify",
		"/v1/auth/email/verify",
		"/v1/hajimi/auth/password/reset",
		"/v1/auth/password/reset",
		"/v1/auth/request-code",
		"/v1/auth/verify",
	} {
		got := postBytes(t, handler, path, []byte("x"))
		if got.Code == http.StatusNotFound || !strings.Contains(got.Header().Get("Content-Type"), "json") {
			t.Fatalf("%s still a plain 404 %d %s %s", path, got.Code, got.Header().Get("Content-Type"), got.Body.String())
		}
	}
	short := postRaw(t, handler, http.MethodPost, "/v1/auth/password/reset", map[string]any{"password": "short"}, "")
	assertJSONError(t, short, http.StatusBadRequest, "密码至少 8 位")
	unauth := postRaw(t, handler, http.MethodPost, "/v1/hajimi/auth/password/reset", map[string]any{"password": "long-enough"}, "")
	assertJSONError(t, unauth, http.StatusUnauthorized, "未登录")
}

func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d want %d body %s", rec.Code, status, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("content-type %s body %s", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), message) {
		t.Fatalf("body %s want %s", rec.Body.String(), message)
	}
}

func postBytes(t *testing.T, handler http.Handler, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
