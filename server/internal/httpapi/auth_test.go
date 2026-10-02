package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"motocam/server/internal/store"
)

func TestAuthLoginTermsAndBuildGate(t *testing.T) {
	handler, db := openAuth(t, true, "", "")

	// Builds before 30 (and clients without X-App-Build) still upload without
	// an account; build 30+ must log in.
	noToken := postRaw(t, handler, http.MethodPost, "/v1/reports", map[string]any{
		"name": "摄像头", "type": "camera", "latitude": 31, "longitude": 121,
	}, "")
	if noToken.Code != http.StatusCreated {
		t.Fatalf("legacy report without token %d %s", noToken.Code, noToken.Body.String())
	}
	build30 := postBuild(t, handler, map[string]any{
		"name": "摄像头", "type": "camera", "latitude": 31.5, "longitude": 121.5,
	}, "30", "")
	if build30.Code != http.StatusUnauthorized {
		t.Fatalf("build 30 report without token %d %s", build30.Code, build30.Body.String())
	}

	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/v1/points", nil))
	if legacy.Code != http.StatusOK {
		t.Fatalf("legacy points %d %s", legacy.Code, legacy.Body.String())
	}

	oldBuild := httptest.NewRequest(http.MethodGet, "/v1/points", nil)
	oldBuild.Header.Set("X-App-Build", "29")
	oldRec := httptest.NewRecorder()
	handler.ServeHTTP(oldRec, oldBuild)
	if oldRec.Code != http.StatusOK {
		t.Fatalf("build 29 points %d %s", oldRec.Code, oldRec.Body.String())
	}

	blocked := httptest.NewRequest(http.MethodGet, "/v1/points", nil)
	blocked.Header.Set("X-App-Build", "30")
	blockedRec := httptest.NewRecorder()
	handler.ServeHTTP(blockedRec, blocked)
	if blockedRec.Code != http.StatusUnauthorized {
		t.Fatalf("build 30 points %d %s", blockedRec.Code, blockedRec.Body.String())
	}

	bogus := httptest.NewRequest(http.MethodGet, "/v1/points", nil)
	bogus.Header.Set("Authorization", "Bearer not-a-token")
	bogusRec := httptest.NewRecorder()
	handler.ServeHTTP(bogusRec, bogus)
	if bogusRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token %d", bogusRec.Code)
	}

	latest := httptest.NewRequest(http.MethodGet, "/v1/app/latest?platform=android", nil)
	latest.Header.Set("X-App-Build", "30")
	latestRec := httptest.NewRecorder()
	handler.ServeHTTP(latestRec, latest)
	if latestRec.Code != http.StatusUnauthorized {
		t.Fatalf("build 30 latest %d %s", latestRec.Code, latestRec.Body.String())
	}

	termsRec := httptest.NewRecorder()
	handler.ServeHTTP(termsRec, httptest.NewRequest(http.MethodGet, "/v1/legal/terms", nil))
	if termsRec.Code != 200 || !bytes.Contains(termsRec.Body.Bytes(), []byte(`"version":"1.0"`)) {
		t.Fatalf("terms %d %s", termsRec.Code, termsRec.Body.String())
	}

	badTerms := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": "rider@example.com", "code": "000000", "termsVersion": "0.9",
	}, "")
	if badTerms.Code != http.StatusBadRequest || !bytes.Contains(badTerms.Body.Bytes(), []byte("1.0")) {
		t.Fatalf("terms mismatch %d %s", badTerms.Code, badTerms.Body.String())
	}

	token := loginDev(t, handler, "rider@example.com")
	meReq := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	me := httptest.NewRecorder()
	handler.ServeHTTP(me, meReq)
	if me.Code != 200 || !bytes.Contains(me.Body.Bytes(), []byte("rider@example.com")) || !bytes.Contains(me.Body.Bytes(), []byte(`"termsAccepted":true`)) {
		t.Fatalf("me %d %s", me.Code, me.Body.String())
	}

	open := httptest.NewRequest(http.MethodGet, "/v1/points", nil)
	open.Header.Set("X-App-Build", "30")
	open.Header.Set("Authorization", "Bearer "+token)
	openRec := httptest.NewRecorder()
	handler.ServeHTTP(openRec, open)
	if openRec.Code != http.StatusOK {
		t.Fatalf("authed points %d %s", openRec.Code, openRec.Body.String())
	}

	created := postRaw(t, handler, http.MethodPost, "/v1/reports", map[string]any{
		"name": "赤石收费站", "type": "banned_toll", "latitude": 28.2, "longitude": 119.9,
	}, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("authed report %d %s", created.Code, created.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+token)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutReq)
	if logout.Code != 200 {
		t.Fatalf("logout %d %s", logout.Code, logout.Body.String())
	}
	again := httptest.NewRecorder()
	handler.ServeHTTP(again, meReq)
	if again.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout %d", again.Code)
	}

	token = loginDev(t, handler, "gone@example.com")
	posted := postRaw(t, handler, http.MethodPost, "/v1/reports", map[string]any{
		"name": "待删", "type": "other", "latitude": 30, "longitude": 120,
	}, token)
	if posted.Code != http.StatusCreated {
		t.Fatalf("before delete %d %s", posted.Code, posted.Body.String())
	}
	delReq := httptest.NewRequest(http.MethodDelete, "/v1/me", nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	del := httptest.NewRecorder()
	handler.ServeHTTP(del, delReq)
	if del.Code != 200 {
		t.Fatalf("delete %d %s", del.Code, del.Body.String())
	}
	admin := httptest.NewRequest(http.MethodGet, "/v1/admin/reports?status=merged", nil)
	admin.Header.Set("Authorization", "Bearer admin-token")
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, admin)
	if !bytes.Contains(adminRec.Body.Bytes(), []byte(`"device_id":"deleted"`)) {
		t.Fatalf("anonymized %s", adminRec.Body.String())
	}
	kept := httptest.NewRecorder()
	handler.ServeHTTP(kept, httptest.NewRequest(http.MethodGet, "/v1/points", nil))
	if !bytes.Contains(kept.Body.Bytes(), []byte("待删")) {
		t.Fatalf("published point removed %s", kept.Body.String())
	}
	dead := httptest.NewRecorder()
	handler.ServeHTTP(dead, delReq)
	if dead.Code != http.StatusUnauthorized {
		t.Fatalf("token after delete %d", dead.Code)
	}

	if err := db.SetDisabled("rider@example.com", true); err != nil {
		t.Fatal(err)
	}
	relogin := loginDev(t, handler, "still-open@example.com")
	if relogin == "" {
		t.Fatal("expected a different account to still log in")
	}
	disabledCode := requestDevCode(t, handler, "locked@example.com")
	if disabledCode.Code != http.StatusOK {
		t.Fatalf("code for locked %d %s", disabledCode.Code, disabledCode.Body.String())
	}
	if err := seedDisabled(db, "locked@example.com"); err != nil {
		t.Fatal(err)
	}
	locked := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": "locked@example.com", "code": devCode(t, "locked@example.com"), "termsVersion": "1.0",
	}, "")
	if locked.Code != http.StatusForbidden || !bytes.Contains(locked.Body.Bytes(), []byte("账号已停用")) {
		t.Fatalf("disabled %d %s", locked.Code, locked.Body.String())
	}
}

func TestAuthWrongCodeRateLimitReviewAndSMTP(t *testing.T) {
	handler, db := openAuth(t, true, "review@example.com", "246810")

	email := "tries@example.com"
	if rec := requestDevCode(t, handler, email); rec.Code != http.StatusOK {
		t.Fatalf("request %d %s", rec.Code, rec.Body.String())
	}
	for i := 1; i <= 4; i++ {
		rec := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
			"email": email, "code": "111111", "termsVersion": "1.0",
		}, "")
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("激活码不正确")) {
			t.Fatalf("attempt %d %d %s", i, rec.Code, rec.Body.String())
		}
	}
	fifth := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": email, "code": "111111", "termsVersion": "1.0",
	}, "")
	if fifth.Code != http.StatusBadRequest || !bytes.Contains(fifth.Body.Bytes(), []byte("激活码已失效")) {
		t.Fatalf("fifth %d %s", fifth.Code, fifth.Body.String())
	}

	again := requestDevCode(t, handler, email)
	if again.Code != http.StatusTooManyRequests {
		t.Fatalf("email gap %d %s", again.Code, again.Body.String())
	}

	reviewReq := postRaw(t, handler, http.MethodPost, "/v1/auth/request-code", map[string]any{"email": "Review@example.com"}, "")
	if reviewReq.Code != 200 || bytes.Contains(reviewReq.Body.Bytes(), []byte("246810")) {
		t.Fatalf("review request %d %s", reviewReq.Code, reviewReq.Body.String())
	}
	review := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": "review@example.com", "code": "246810", "termsVersion": "1.0",
	}, "")
	if review.Code != 200 || !bytes.Contains(review.Body.Bytes(), []byte(`"token"`)) {
		t.Fatalf("review login %d %s", review.Code, review.Body.String())
	}
	if err := db.SetDisabled("review@example.com", true); err != nil {
		t.Fatal(err)
	}
	blocked := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": "review@example.com", "code": "246810", "termsVersion": "1.0",
	}, "")
	if blocked.Code != http.StatusForbidden || !bytes.Contains(blocked.Body.Bytes(), []byte("账号已停用")) {
		t.Fatalf("disabled review %d %s", blocked.Code, blocked.Body.String())
	}

	silent, _ := openAuth(t, false, "", "")
	missing := postRaw(t, silent, http.MethodPost, "/v1/auth/request-code", map[string]any{"email": "nomail@example.com"}, "")
	if missing.Code != http.StatusServiceUnavailable || !bytes.Contains(missing.Body.Bytes(), []byte(`"smtpConfigured":false`)) {
		t.Fatalf("smtp %d %s", missing.Code, missing.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(missing.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, leaked := payload["code"]; leaked {
		t.Fatalf("code leaked %s", missing.Body.String())
	}
}

func openAuth(t *testing.T, dev bool, reviewEmail, reviewCode string) (http.Handler, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := NewWithOptions(HandlerOptions{
		AdminToken:  "admin-token",
		MaxImage:    1 << 20,
		DB:          db,
		DevMode:     dev,
		ReviewEmail: reviewEmail,
		ReviewCode:  reviewCode,
	})
	return handler, db
}

func requestDevCode(t *testing.T, handler http.Handler, email string) *httptest.ResponseRecorder {
	t.Helper()
	return postRaw(t, handler, http.MethodPost, "/v1/auth/request-code", map[string]any{"email": email}, "")
}

func devCode(t *testing.T, email string) string {
	t.Helper()
	code, ok := devCodes.Load(email)
	if !ok {
		t.Fatalf("dev code missing for %s", email)
	}
	return code.(string)
}

func loginDev(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	asked := requestDevCode(t, handler, email)
	if asked.Code != http.StatusOK {
		t.Fatalf("request-code %s %d %s", email, asked.Code, asked.Body.String())
	}
	rec := postRaw(t, handler, http.MethodPost, "/v1/auth/verify", map[string]any{
		"email": email, "code": devCode(t, email), "termsVersion": "1.0",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify %s %d %s", email, rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("token %s", rec.Body.String())
	}
	return body.Token
}

func postBuild(t *testing.T, handler http.Handler, payload map[string]any, build, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", device(4))
	if build != "" {
		req.Header.Set("X-App-Build", build)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func seedDisabled(db *store.Store, email string) error {
	_, _, _, err := db.Login(email, "1.0", time.Now())
	if err != nil {
		return err
	}
	return db.SetDisabled(email, true)
}

func postRaw(t *testing.T, handler http.Handler, method, path string, payload map[string]any, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", device(3))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
