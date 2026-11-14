package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"motocam/server/internal/store"
)

func TestRestoredRoutesDoNotLeakWhenLoggedOut(t *testing.T) {
	const marker = "LEAK-MARKER-9f3c"
	hash, err := bcrypt.GenerateFromPassword([]byte("restore-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	e := newStatsEnv(t, HandlerOptions{
		AdminPasswordHash: string(hash),
		AdminHost:         "dl.xiaomoto.cloud",
	})
	if _, _, _, err := e.db.Login("leak-check@example.com", "v1", time.Now()); err != nil {
		t.Fatal(err)
	}
	pending, err := e.db.Submit(store.ReportInput{
		Name: marker, Type: "camera", Latitude: 30.1, Longitude: 120.1,
		Confidence: "low", ForceReview: true, ReviewReason: "待审",
		DeviceID: "00000000-0000-4000-8000-0000000000b1",
	})
	if err != nil || pending.Status != "pending" {
		t.Fatalf("submit %+v %v", pending, err)
	}
	host := map[string]string{"Host": "dl.xiaomoto.cloud"}

	page := e.do(http.MethodGet, "/admin", host, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `type="password"`) {
		t.Fatalf("login page %d", page.Code)
	}
	assertNoLeak(t, page.Body.String())
	if page.Header().Get("Set-Cookie") != "" {
		t.Fatal("logged-out admin set a cookie")
	}

	bearer := map[string]string{"Host": "dl.xiaomoto.cloud", "Authorization": "Bearer tok"}
	if body := e.do(http.MethodGet, "/admin", bearer, nil).Body.String(); strings.Contains(body, "注册总人数") || strings.Contains(body, marker) {
		t.Fatal("bearer token opened the stats page")
	}

	for _, path := range []string{
		"/admin/reports/" + pending.ID + "/image",
		"/admin/points.csv",
	} {
		res := e.do(http.MethodGet, path, host, nil)
		if res.Code == http.StatusOK || strings.Contains(res.Body.String(), marker) || strings.Contains(res.Header().Get("Content-Type"), "text/csv") {
			t.Fatalf("unauthenticated %s -> %d %s", path, res.Code, res.Header().Get("Content-Type"))
		}
	}
	if res := e.do(http.MethodGet, "/admin/", host, nil); res.Code != http.StatusMovedPermanently || strings.Contains(res.Body.String(), marker) {
		t.Fatalf("admin slash %d", res.Code)
	}
	before, err := e.db.AdminList("pending", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/admin/reports/" + pending.ID + "/approve",
		"/admin/reports/" + pending.ID + "/reject",
	} {
		res := e.do(http.MethodPost, path, host, url.Values{"reason": {"不应写入"}})
		if res.Code == http.StatusOK || strings.Contains(res.Body.String(), marker) {
			t.Fatalf("unauthenticated %s -> %d", path, res.Code)
		}
	}
	after, err := e.db.AdminList("pending", 20)
	if err != nil || len(after) != len(before) {
		t.Fatal("logged-out review changed the queue")
	}

	login := e.do(http.MethodPost, "/admin/login", map[string]string{
		"Host": "dl.xiaomoto.cloud", "Origin": "https://dl.xiaomoto.cloud", "Sec-Fetch-Site": "same-origin",
	}, url.Values{"password": {"restore-test-password"}})
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login %d", login.Code)
	}
	sess := map[string]string{"Host": "dl.xiaomoto.cloud", "Cookie": adminCookie + "=" + login.Result().Cookies()[0].Value}
	api := e.do(http.MethodGet, "/v1/admin/reports?status=pending", sess, nil)
	if api.Code != http.StatusUnauthorized || strings.Contains(api.Body.String(), marker) {
		t.Fatalf("cookie on API %d %s", api.Code, api.Body.String())
	}
	loggedIn := e.do(http.MethodGet, "/admin", sess, nil)
	if loggedIn.Code != 200 || !strings.Contains(loggedIn.Body.String(), marker) || strings.Contains(loggedIn.Body.String(), "leak-check@example.com") {
		t.Fatal("signed-in page missing the report or showed a full email")
	}
	if !strings.Contains(loggedIn.Body.String(), "未配置") {
		t.Fatal("missing ASC should say 未配置")
	}

	if code := e.do(http.MethodGet, "/v1/me/reports", nil, nil).Code; code == http.StatusNotFound {
		t.Fatal("GET /v1/me/reports missing")
	}
	if code := e.do(http.MethodPost, "/v1/admin/reports/reprocess", nil, nil).Code; code == http.StatusNotFound {
		t.Fatal("POST /v1/admin/reports/reprocess missing")
	}
	if code := e.do(http.MethodPost, "/v1/auth/login-password", nil, nil).Code; code == http.StatusNotFound {
		t.Fatal("POST /v1/auth/login-password missing")
	}
	if code := e.do(http.MethodPost, "/v1/auth/reset-password", nil, nil).Code; code == http.StatusNotFound {
		t.Fatal("POST /v1/auth/reset-password missing")
	}
	if code := e.do(http.MethodPost, "/v1/auth/set-password", nil, nil).Code; code == http.StatusNotFound {
		t.Fatal("POST /v1/auth/set-password missing")
	}
	if e.do(http.MethodGet, "/download", nil, nil).Code != http.StatusOK {
		t.Fatal("/download not public")
	}
	if e.do(http.MethodGet, "/download/", nil, nil).Code != http.StatusMovedPermanently {
		t.Fatal("/download/ slash redirect missing")
	}
	if e.do(http.MethodGet, "/download/ios", nil, nil).Code != http.StatusFound {
		t.Fatal("/download/ios missing")
	}
	icon := e.do(http.MethodGet, "/download/icon.png", nil, nil)
	if icon.Code != http.StatusOK || icon.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("icon %d %s", icon.Code, icon.Header().Get("Content-Type"))
	}
	if e.do(http.MethodGet, "/healthz", nil, nil).Code != http.StatusOK {
		t.Fatal("health")
	}
}

func assertNoLeak(t *testing.T, body string) {
	t.Helper()
	for _, secret := range []string{"LEAK-MARKER-9f3c", "leak-check@example.com", "注册总人数", "安卓·网页下载"} {
		if strings.Contains(body, secret) {
			t.Fatalf("logged-out page contains %q", secret)
		}
	}
}
