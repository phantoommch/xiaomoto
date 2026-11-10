package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"motocam/server/internal/store"
)

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %d %s", rec.Code, rec.Body.String())
	}
	return out
}

func getMe(t *testing.T, handler http.Handler, token string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	return rec.Code, decode(t, rec)
}

func pwLogin(t *testing.T, handler http.Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	return postRaw(t, handler, http.MethodPost, "/v1/auth/login-password", map[string]any{
		"email": email, "password": password, "termsVersion": "1.0",
	}, "")
}

func tokenOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	tok, _ := decode(t, rec)["token"].(string)
	if tok == "" {
		t.Fatalf("no token %s", rec.Body.String())
	}
	return tok
}

func TestCodeLoginUnchangedThenSetPassword(t *testing.T) {
	handler, db := openAuth(t, true, "", "")
	handlerDB[handler] = db
	email := "rider@example.com"
	asked := requestDevCode(t, handler, email)
	if asked.Code != 200 || asked.Body.String() != "{\"dev\":true,\"ok\":true}\n" {
		t.Fatalf("request-code %d %q", asked.Code, asked.Body.String())
	}
	verify := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": email, "code": devCode(t, email), "termsVersion": "1.0",
	}, "")
	token := tokenOf(t, verify)
	// The old verify reply is unchanged: no new keys.
	body := decode(t, verify)
	if len(body) != 3 || len(body["user"].(map[string]any)) != 3 {
		t.Fatalf("verify reply changed: %s", verify.Body.String())
	}

	_, me := getMe(t, handler, token)
	if me["hasPassword"] != false {
		t.Fatalf("new user hasPassword %v", me)
	}
	if rec := pwLogin(t, handler, email, "whatever123"); rec.Code != http.StatusBadRequest {
		t.Fatalf("login without password set %d %s", rec.Code, rec.Body.String())
	}

	short := postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{"password": "1234567"}, token)
	if short.Code != http.StatusBadRequest || !bytes.Contains(short.Body.Bytes(), []byte("8")) {
		t.Fatalf("short password %d %s", short.Code, short.Body.String())
	}
	if rec := postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{"password": "hunter22"}, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("set without token %d", rec.Code)
	}
	set := postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{"password": "摩托车安全驾驶"}, "")
	if set.Code != http.StatusUnauthorized {
		t.Fatalf("set-password no token %d", set.Code)
	}
	set = postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{"password": "rider-pass-1"}, token)
	if set.Code != 200 {
		t.Fatalf("set-password %d %s", set.Code, set.Body.String())
	}
	_, me = getMe(t, handler, token)
	if me["hasPassword"] != true {
		t.Fatalf("hasPassword after set %v", me)
	}

	if rec := pwLogin(t, handler, email, "rider-pass-2"); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong password %d %s", rec.Code, rec.Body.String())
	}
	if rec := pwLogin(t, handler, "nobody@example.com", "rider-pass-1"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown email %d %s", rec.Code, rec.Body.String())
	}
	badTerms := postRaw(t, handler, http.MethodPost, "/v1/auth/login-password", map[string]any{
		"email": email, "password": "rider-pass-1", "termsVersion": "0.9",
	}, "")
	if badTerms.Code != http.StatusBadRequest {
		t.Fatalf("bad terms %d", badTerms.Code)
	}
	second := tokenOf(t, pwLogin(t, handler, " Rider@Example.com ", "rider-pass-1"))
	if u := decode(t, pwLogin(t, handler, email, "rider-pass-1"))["user"].(map[string]any); u["hasPassword"] != true || u["email"] != email {
		t.Fatalf("login user %v", u)
	}

	// Code login keeps working for users who have a password.
	if tok := loginDevAgain(t, handler, email); tok == "" {
		t.Fatal("code login after password")
	}

	// Changing needs the current password and signs out other sessions.
	noCurrent := postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{"password": "rider-pass-3"}, second)
	if noCurrent.Code != http.StatusBadRequest {
		t.Fatalf("change without current %d %s", noCurrent.Code, noCurrent.Body.String())
	}
	changed := postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{
		"password": "rider-pass-3", "currentPassword": "rider-pass-1",
	}, second)
	if changed.Code != 200 {
		t.Fatalf("change %d %s", changed.Code, changed.Body.String())
	}
	if code, _ := getMe(t, handler, token); code != http.StatusUnauthorized {
		t.Fatalf("old session after change %d", code)
	}
	if code, _ := getMe(t, handler, second); code != 200 {
		t.Fatalf("current session after change %d", code)
	}
	if rec := pwLogin(t, handler, email, "rider-pass-1"); rec.Code != http.StatusBadRequest {
		t.Fatalf("old password still works %d", rec.Code)
	}
	tokenOf(t, pwLogin(t, handler, email, "rider-pass-3"))
}

// loginDevAgain issues a code with a later clock (skipping the 60 s gap) and
// logs in with it through the unchanged verify endpoint.
func loginDevAgain(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	db := handlerDB[handler]
	issued, err := db.IssueCode(email, "", time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	rec := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": email, "code": issued.Code, "termsVersion": "1.0",
	}, "")
	return tokenOf(t, rec)
}

func TestResetPassword(t *testing.T) {
	handler, db := openAuth(t, true, "", "")
	handlerDB[handler] = db
	email := "forgot@example.com"
	old := tokenOf(t, func() *httptest.ResponseRecorder {
		requestDevCode(t, handler, email)
		return postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
			"email": email, "code": devCode(t, email), "termsVersion": "1.0",
		}, "")
	}())
	if rec := postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{"password": "first-pass"}, old); rec.Code != 200 {
		t.Fatalf("set %d", rec.Code)
	}

	issued, err := db.IssueCode(email, "", time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// A too-short password is refused before the code is used.
	short := postRaw(t, handler, http.MethodPost, "/v1/auth/reset-password", map[string]any{
		"email": email, "code": issued.Code, "password": "short", "termsVersion": "1.0",
	}, "")
	if short.Code != http.StatusBadRequest {
		t.Fatalf("short reset %d", short.Code)
	}
	wrong := postRaw(t, handler, http.MethodPost, "/v1/auth/reset-password", map[string]any{
		"email": email, "code": "000000x", "password": "second-pass", "termsVersion": "1.0",
	}, "")
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong code reset %d", wrong.Code)
	}
	reset := postRaw(t, handler, http.MethodPost, "/v1/auth/reset-password", map[string]any{
		"email": email, "code": issued.Code, "password": "second-pass", "termsVersion": "1.0",
	}, "")
	fresh := tokenOf(t, reset)
	if code, _ := getMe(t, handler, old); code != http.StatusUnauthorized {
		t.Fatalf("old session after reset %d", code)
	}
	if _, me := getMe(t, handler, fresh); me["hasPassword"] != true {
		t.Fatalf("me after reset %v", me)
	}
	if rec := pwLogin(t, handler, email, "first-pass"); rec.Code != http.StatusBadRequest {
		t.Fatalf("old password after reset %d", rec.Code)
	}
	tokenOf(t, pwLogin(t, handler, email, "second-pass"))
	// The code cannot be used twice.
	again := postRaw(t, handler, http.MethodPost, "/v1/auth/reset-password", map[string]any{
		"email": email, "code": issued.Code, "password": "third-pass", "termsVersion": "1.0",
	}, "")
	if again.Code != http.StatusBadRequest {
		t.Fatalf("reused code %d", again.Code)
	}

	// A new email can sign up straight into a password.
	newbie := "newbie@example.com"
	code2, err := db.IssueCode(newbie, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tokenOf(t, postRaw(t, handler, http.MethodPost, "/v1/auth/reset-password", map[string]any{
		"email": newbie, "code": code2.Code, "password": "newbie-pass", "termsVersion": "1.0",
	}, ""))
	tokenOf(t, pwLogin(t, handler, newbie, "newbie-pass"))
}

func TestPasswordLockout(t *testing.T) {
	handler, db := openAuth(t, true, "", "")
	user, _, _, err := db.Login("lock@example.com", "1.0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPassword(user.ID, "right-pass", "", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if rec := pwLogin(t, handler, "lock@example.com", "wrong-pass"); rec.Code != http.StatusBadRequest {
			t.Fatalf("try %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	locked := pwLogin(t, handler, "lock@example.com", "right-pass")
	if locked.Code != http.StatusTooManyRequests || locked.Header().Get("Retry-After") == "" {
		t.Fatalf("locked %d %s", locked.Code, locked.Body.String())
	}
}

func TestDisabledPasswordLogin(t *testing.T) {
	handler, db := openAuth(t, true, "", "")
	user, _, _, err := db.Login("off@example.com", "1.0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPassword(user.ID, "right-pass", "", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDisabled("off@example.com", true); err != nil {
		t.Fatal(err)
	}
	if rec := pwLogin(t, handler, "off@example.com", "right-pass"); rec.Code != http.StatusForbidden {
		t.Fatalf("disabled %d %s", rec.Code, rec.Body.String())
	}
}

func TestReviewAccountPassword(t *testing.T) {
	handler, _ := openAuth(t, false, "review@example.com", "246810")
	// Old code login for review still works.
	verify := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": "review@example.com", "code": "246810", "termsVersion": "1.0",
	}, "")
	tok := tokenOf(t, verify)
	if _, me := getMe(t, handler, tok); me["hasPassword"] != true {
		t.Fatalf("review hasPassword %v", me)
	}
	// Password login with the review code as password.
	tokenOf(t, pwLogin(t, handler, "Review@Example.com", "246810"))
	if rec := pwLogin(t, handler, "review@example.com", "000000"); rec.Code != http.StatusBadRequest {
		t.Fatalf("review wrong password %d", rec.Code)
	}
	if rec := postRaw(t, handler, http.MethodPost, "/v1/auth/set-password", map[string]any{
		"password": "changed-pass", "currentPassword": "246810",
	}, tok); rec.Code != http.StatusForbidden {
		t.Fatalf("review set-password %d", rec.Code)
	}
	if rec := postRaw(t, handler, http.MethodPost, "/v1/auth/reset-password", map[string]any{
		"email": "review@example.com", "code": "246810", "password": "changed-pass", "termsVersion": "1.0",
	}, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("review reset %d", rec.Code)
	}
	tokenOf(t, pwLogin(t, handler, "review@example.com", "246810"))

	withPw := NewWithOptions(HandlerOptions{DB: handlerDBAny(t), ReviewEmail: "review@example.com", ReviewCode: "246810", ReviewPassword: "review-pass-9"})
	tokenOf(t, pwLogin(t, withPw, "review@example.com", "review-pass-9"))
	if rec := pwLogin(t, withPw, "review@example.com", "246810"); rec.Code != http.StatusBadRequest {
		t.Fatalf("review code as password when REVIEW_PASSWORD set %d", rec.Code)
	}
}

var handlerDB = map[http.Handler]*store.Store{}

func handlerDBAny(t *testing.T) *store.Store {
	_, db := openAuth(t, false, "", "")
	return db
}
