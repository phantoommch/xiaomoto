package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"motocam/server/internal/appdist"
	"motocam/server/internal/recog"
	"motocam/server/internal/store"
)

type statsEnv struct {
	t  *testing.T
	db *store.Store
	h  http.Handler
}

func newStatsEnv(t *testing.T, opts HandlerOptions) *statsEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	apps, err := appdist.Open(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatal(err)
	}
	opts.AdminToken = "tok"
	opts.MaxImage = 1 << 20
	opts.DB = db
	opts.Apps = apps
	return &statsEnv{t: t, db: db, h: NewWithOptions(opts)}
}

func (e *statsEnv) do(method, path string, hdr map[string]string, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *statsEnv) stats() store.Stats {
	e.t.Helper()
	s, err := e.db.Stats(time.Now(), store.StatsOptions{Loc: shanghai})
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

const browserUA = "Mozilla/5.0 (Linux; Android 14; PJZ110) AppleWebKit/537.36 Chrome/126 Mobile Safari/537.36"

func TestDownloadCounting(t *testing.T) {
	e := newStatsEnv(t, HandlerOptions{})
	body, ct := appMultipart(t, map[string]string{"platform": "android", "versionCode": "34", "versionName": "1.0", "notes": "n"}, "a.apk", []byte("apk-34-payload"))
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/app", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish %d", rec.Code)
	}
	ip := func(n string) map[string]string {
		return map[string]string{"User-Agent": browserUA, "X-Forwarded-For": "203.0.113." + n}
	}

	// Page views: GET counts, HEAD does not, a quick reload does not.
	e.do(http.MethodGet, "/download", ip("1"), nil)
	e.do(http.MethodHead, "/download", ip("2"), nil)
	e.do(http.MethodGet, "/download", ip("1"), nil)
	e.do(http.MethodGet, "/download", map[string]string{"User-Agent": "Googlebot/2.1", "X-Forwarded-For": "66.249.0.1"}, nil)

	// Web download: redirect, then the file. Only the file request counts.
	r := e.do(http.MethodGet, "/download/android", ip("1"), nil)
	loc := r.Header().Get("Location")
	if r.Code != http.StatusFound || !strings.HasPrefix(loc, "/v1/app/download/android/") {
		t.Fatalf("redirect %d %s", r.Code, loc)
	}
	if f := e.do(http.MethodGet, loc, ip("1"), nil); f.Code != 200 || f.Body.String() != "apk-34-payload" {
		t.Fatalf("file %d", f.Code)
	}
	// Range continuation and HEAD are not new downloads; Range from 0 is.
	h := ip("3")
	h["Range"] = "bytes=5-"
	if f := e.do(http.MethodGet, loc, h, nil); f.Code != http.StatusPartialContent {
		t.Fatalf("range %d", f.Code)
	}
	e.do(http.MethodHead, loc, ip("4"), nil)
	h = ip("5")
	h["Range"] = "bytes=0-"
	e.do(http.MethodGet, loc, h, nil)
	// In-app updater (User-Agent XiaoMoto/1.0, X-App-Build).
	_, token, _, err := e.db.Login("rider@example.com", "v1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	app := map[string]string{"User-Agent": "XiaoMoto/1.0", "X-App-Build": "33", "X-Forwarded-For": "203.0.113.1"}
	if f := e.do(http.MethodGet, loc, app, nil); f.Code != http.StatusUnauthorized {
		t.Fatalf("build 33 without token %d", f.Code)
	}
	app["Authorization"] = "Bearer " + token
	if f := e.do(http.MethodGet, loc, app, nil); f.Code != 200 {
		t.Fatalf("app update %d", f.Code)
	}

	// TestFlight button.
	tf := e.do(http.MethodGet, "/download/ios", map[string]string{"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0)", "X-Forwarded-For": "198.51.100.9"}, nil)
	if tf.Code != http.StatusFound || tf.Header().Get("Location") != testFlightURL {
		t.Fatalf("testflight %d %s", tf.Code, tf.Header().Get("Location"))
	}
	e.do(http.MethodHead, "/download/ios", ip("6"), nil)

	s := e.stats()
	if s.PageViews.Total != 1 {
		t.Fatalf("page views %+v", s.PageViews)
	}
	if s.AndroidWeb.Total != 2 || s.AndroidWeb.Today != 2 || s.AndroidWeb.Week != 2 || s.AndroidWeb.Unique != 2 {
		t.Fatalf("web %+v", s.AndroidWeb)
	}
	if s.AndroidApp.Total != 1 {
		t.Fatalf("app %+v", s.AndroidApp)
	}
	if s.TestFlight.Total != 1 {
		t.Fatalf("testflight %+v", s.TestFlight)
	}
	// iOS package downloads are not counted as Android.
	if c := e.do(http.MethodGet, "/v1/app/download/ios/none.ipa", ip("7"), nil); c.Code != http.StatusNotFound {
		t.Fatalf("ios %d", c.Code)
	}
}

func TestAdminDisabledWithoutPassword(t *testing.T) {
	e := newStatsEnv(t, HandlerOptions{})
	if r := e.do(http.MethodGet, "/admin", nil, nil); r.Code != http.StatusNotFound {
		t.Fatalf("disabled admin %d", r.Code)
	}
	if r := e.do(http.MethodPost, "/admin/login", nil, url.Values{"password": {"x"}}); r.Code != http.StatusNotFound {
		t.Fatalf("disabled login %d", r.Code)
	}
}

func TestAdminLoginAndStatsPage(t *testing.T) {
	const pw = "correct-horse-battery-staple"
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	e := newStatsEnv(t, HandlerOptions{
		AdminPasswordHash: string(hash),
		AdminHost:         "dl.xiaomoto.cloud",
		ReviewEmail:       "review@xiaomoto.app",
		StatsTestEmails:   []string{"test-account-1@example.com", "test-account-2@example.com"},
	})
	now := time.Now()
	for _, em := range []string{"review@xiaomoto.app", "test-account-1@example.com", "rider.one@qq.com", "zhang@163.com"} {
		if _, _, _, err := e.db.Login(em, "v1", now); err != nil {
			t.Fatal(err)
		}
	}
	host := func(extra map[string]string) map[string]string {
		m := map[string]string{"Host": "dl.xiaomoto.cloud", "X-Forwarded-For": "203.0.113.50"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	// Other hosts (sslip.io) never see the page.
	if r := e.do(http.MethodGet, "/admin", map[string]string{"Host": "8-216-50-228.sslip.io"}, nil); r.Code != http.StatusNotFound {
		t.Fatalf("other host %d", r.Code)
	}
	// Not logged in: login form only, no numbers.
	r := e.do(http.MethodGet, "/admin", host(nil), nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `type="password"`) || strings.Contains(r.Body.String(), "注册总人数") {
		t.Fatalf("login form %d", r.Code)
	}
	if r.Header().Get("Cache-Control") != "no-store" || !strings.Contains(r.Header().Get("X-Robots-Tag"), "noindex") {
		t.Fatal("admin headers")
	}
	// A bogus cookie is not a session.
	if r := e.do(http.MethodGet, "/admin", host(map[string]string{"Cookie": adminCookie + "=deadbeef"}), nil); strings.Contains(r.Body.String(), "注册总人数") {
		t.Fatal("bogus cookie accepted")
	}
	// Wrong password.
	r = e.do(http.MethodPost, "/admin/login", host(nil), url.Values{"password": {"wrong"}})
	if r.Code != http.StatusUnauthorized || r.Header().Get("Set-Cookie") != "" || !strings.Contains(r.Body.String(), "密码不对") {
		t.Fatalf("wrong password %d %q", r.Code, r.Header().Get("Set-Cookie"))
	}
	// Cross-site post is refused.
	if r := e.do(http.MethodPost, "/admin/login", host(map[string]string{"Origin": "https://evil.example"}), url.Values{"password": {pw}}); r.Code != http.StatusForbidden {
		t.Fatalf("cross origin %d", r.Code)
	}
	if r := e.do(http.MethodPost, "/admin/login", host(map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "null"}), url.Values{"password": {pw}}); r.Code != http.StatusForbidden {
		t.Fatalf("cross site null origin %d", r.Code)
	}
	// Correct password: cookie + redirect.
	r = e.do(http.MethodPost, "/admin/login", host(map[string]string{"Origin": "https://dl.xiaomoto.cloud", "Sec-Fetch-Site": "same-origin"}), url.Values{"password": {pw}})
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/admin" {
		t.Fatalf("login %d", r.Code)
	}
	cookies := r.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != adminCookie || !cookies[0].HttpOnly || !cookies[0].Secure ||
		cookies[0].MaxAge != 30*24*3600 || cookies[0].Path != "/admin" || len(cookies[0].Value) < 32 {
		t.Fatalf("cookie %+v", cookies)
	}
	sess := host(map[string]string{"Cookie": adminCookie + "=" + cookies[0].Value})

	e.do(http.MethodGet, "/download", host(map[string]string{"User-Agent": browserUA}), nil)
	page := e.do(http.MethodGet, "/admin", sess, nil)
	body := page.Body.String()
	if page.Code != 200 {
		t.Fatalf("stats page %d", page.Code)
	}
	for _, want := range []string{"注册总人数", "其中审核/测试账号 2 个", "审核账号", "测试账号", "r***@qq.com", "z***@163.com",
		"近 30 天每天新注册", "安卓·网页下载", "安卓·App 内更新", "下载页打开次数", "iPhone 测试版按钮点击", "开始统计", "用户上传点位", "刷新", "iPhone 测试版（TestFlight）", "未配置"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stats page missing %q", want)
		}
	}
	if strings.Contains(body, "rider.one@") || strings.Contains(body, "zhang@") || strings.Contains(body, "qdchenjian") {
		t.Fatal("unmasked email on page")
	}
	if m := regexp.MustCompile(`<span class="num">(\d+)</span> 人 <span class="lbl">注册总人数`).FindStringSubmatch(body); m == nil || m[1] != "4" {
		t.Fatalf("total users %v", m)
	}
	if !strings.Contains(body, `<tr><th>下载页打开次数</th><td>1</td><td>1</td><td>1</td></tr>`) {
		t.Fatal("page view row")
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "http://") && !strings.Contains(line, "https://") {
			continue
		}
		if strings.Contains(line, "uri.amap.com/marker") {
			continue
		}
		t.Fatalf("unexpected external link: %s", trunc(line, 120))
	}
	for _, want := range []string{"待审点", "违法点位", "导出 CSV"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing section %q", want)
		}
	}

	// Logout ends the session.
	if r := e.do(http.MethodPost, "/admin/logout", sess, nil); r.Code != http.StatusSeeOther {
		t.Fatalf("logout %d", r.Code)
	}
	if r := e.do(http.MethodGet, "/admin", sess, nil); strings.Contains(r.Body.String(), "注册总人数") {
		t.Fatal("session survived logout")
	}
}

func TestAdminLoginRateLimit(t *testing.T) {
	e := newStatsEnv(t, HandlerOptions{AdminPassword: "TEST_PLACEHOLDER_ADMIN_PASSWORD"})
	from := map[string]string{"X-Forwarded-For": "198.51.100.77"}
	for i := 0; i < 5; i++ {
		if r := e.do(http.MethodPost, "/admin/login", from, url.Values{"password": {"guess"}}); r.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, r.Code)
		}
	}
	// Locked now, even with the right password.
	if r := e.do(http.MethodPost, "/admin/login", from, url.Values{"password": {"TEST_PLACEHOLDER_ADMIN_PASSWORD"}}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("not rate limited: %d", r.Code)
	}
	// Another network still gets in (plain ADMIN_PASSWORD, constant-time compare).
	r := e.do(http.MethodPost, "/admin/login", map[string]string{"X-Forwarded-For": "192.0.2.1"}, url.Values{"password": {"TEST_PLACEHOLDER_ADMIN_PASSWORD"}})
	if r.Code != http.StatusSeeOther {
		t.Fatalf("other ip login %d", r.Code)
	}
}

func TestAdminPendingReviewAndPointsExport(t *testing.T) {
	const pw = "review-board-password"
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	e := newStatsEnv(t, HandlerOptions{AdminPasswordHash: string(hash), AdminHost: "dl.xiaomoto.cloud"})
	host := func(extra map[string]string) map[string]string {
		m := map[string]string{"Host": "dl.xiaomoto.cloud", "Origin": "https://dl.xiaomoto.cloud", "Sec-Fetch-Site": "same-origin"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	r := e.do(http.MethodPost, "/admin/login", host(nil), url.Values{"password": {pw}})
	if r.Code != http.StatusSeeOther {
		t.Fatalf("login %d", r.Code)
	}
	sess := host(map[string]string{"Cookie": adminCookie + "=" + r.Result().Cookies()[0].Value})

	pending, err := e.db.Submit(store.ReportInput{
		Name: "赤石收费站", Type: "banned_toll", RoadCode: "G25", RoadName: "长深高速",
		Latitude: 28.2, Longitude: 119.9, Confidence: "low", ForceReview: true,
		ReviewReason: "定位结果不一致", DeviceID: "00000000-0000-4000-8000-0000000000a1",
		Image: tinyJPEG(t),
	})
	if err != nil || pending.Status != "pending" {
		t.Fatalf("submit %+v %v", pending, err)
	}
	pub, err := e.db.Submit(store.ReportInput{
		Name: "公开点", Type: "camera", RoadCode: "G15", Latitude: 31.2, Longitude: 121.5,
		Violation: "闯红灯", FineYuan: intPtr(200), DeviceID: "00000000-0000-4000-8000-0000000000a2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pub.Status == "pending" {
		if err := e.db.Approve(pub.ID); err != nil {
			t.Fatal(err)
		}
	}

	page := e.do(http.MethodGet, "/admin", sess, nil)
	body := page.Body.String()
	if page.Code != 200 || !strings.Contains(body, "赤石收费站") || !strings.Contains(body, "定位结果不一致") {
		t.Fatalf("pending missing %d", page.Code)
	}
	if !strings.Contains(body, `/admin/reports/`+pending.ID+`/image`) {
		t.Fatal("image thumb missing")
	}
	if !strings.Contains(body, "公开点") || !strings.Contains(body, "抓拍摄像头") {
		t.Fatal("published point missing")
	}
	if !strings.Contains(body, "高德坐标") {
		t.Fatal("GCJ label missing")
	}
	if !strings.Contains(body, "uri.amap.com/marker") {
		t.Fatal("amap map link missing")
	}

	// Stake with no AMap km marker — pending UX must force manual 高德 pin.
	stakePend, err := e.db.Submit(store.ReportInput{
		Name: "S6桩号待定点", Type: "camera", RoadCode: "S6", Stake: "K29+550",
		Confidence: "none", ForceReview: true, Unlocated: true,
		ReviewReason: "高德无此桩号，请在地图上人工定点",
		DeviceID:     "00000000-0000-4000-8000-0000000000a4",
	})
	if err != nil || stakePend.Status != "pending" {
		t.Fatalf("stake pending %+v %v", stakePend, err)
	}
	page2 := e.do(http.MethodGet, "/admin", sess, nil)
	body2 := page2.Body.String()
	if !strings.Contains(body2, "高德无此桩号，请在地图上人工定点") || !strings.Contains(body2, "need-pin") {
		t.Fatalf("stake manual-pin UX missing")
	}
	if !strings.Contains(body2, "高德纬度") || !strings.Contains(body2, "高德经度") {
		t.Fatal("高德 lat/lng labels missing")
	}

	// Stake only in name/OCR (Stake field empty) still gets manual-pin UX.
	nameStake, err := e.db.Submit(store.ReportInput{
		Name: "S6渝泸高速江津区段29公里550米下行", Type: "other", RoadCode: "S6", RoadName: "渝泸高速",
		Confidence: "none", ForceReview: true, Unlocated: true,
		ReviewReason: "定位结果不一致或置信度不足，已保留上报坐标",
		DeviceID:     "00000000-0000-4000-8000-0000000000a5",
	})
	if err != nil || nameStake.Status != "pending" {
		t.Fatalf("name-stake pending %+v %v", nameStake, err)
	}
	page3 := e.do(http.MethodGet, "/admin", sess, nil)
	if !strings.Contains(page3.Body.String(), "高德无此桩号，请在地图上人工定点") {
		t.Fatal("name-only stake manual-pin UX missing")
	}

	// Image via cookie session (no Bearer).
	img := e.do(http.MethodGet, "/admin/reports/"+pending.ID+"/image", sess, nil)
	if img.Code != 200 || !strings.HasPrefix(img.Header().Get("Content-Type"), "image/") {
		t.Fatalf("session image %d %s", img.Code, img.Header().Get("Content-Type"))
	}
	if e.do(http.MethodGet, "/admin/reports/"+pending.ID+"/image", host(nil), nil).Code == 200 {
		t.Fatal("image without session")
	}

	// Approve with edited 高德 GCJ coords (stored as WGS).
	gLat, gLon := recog.WGSToGCJ(28.201, 119.901)
	ap := e.do(http.MethodPost, "/admin/reports/"+pending.ID+"/approve", sess, url.Values{
		"latitude": {strconv.FormatFloat(gLat, 'f', 6, 64)}, "longitude": {strconv.FormatFloat(gLon, 'f', 6, 64)},
	})
	if ap.Code != http.StatusSeeOther || !strings.Contains(ap.Header().Get("Location"), "ok=") {
		t.Fatalf("approve %d %s", ap.Code, ap.Header().Get("Location"))
	}
	list, _ := e.db.AdminList("pending", 20)
	for _, item := range list {
		if item.ID == pending.ID {
			t.Fatal("still pending after approve")
		}
	}

	// Reject another pending.
	other, err := e.db.Submit(store.ReportInput{
		Name: "待驳回", Type: "other", Latitude: 30, Longitude: 120,
		Confidence: "none", ForceReview: true, DeviceID: "00000000-0000-4000-8000-0000000000a3",
	})
	if err != nil {
		t.Fatal(err)
	}
	rj := e.do(http.MethodPost, "/admin/reports/"+other.ID+"/reject", sess, url.Values{"reason": {"坐标不对"}})
	if rj.Code != http.StatusSeeOther || !strings.Contains(rj.Header().Get("Location"), "ok=") {
		t.Fatalf("reject %d %s", rj.Code, rj.Header().Get("Location"))
	}

	csv := e.do(http.MethodGet, "/admin/points.csv", sess, nil)
	if csv.Code != 200 || !strings.Contains(csv.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv %d %s", csv.Code, csv.Header().Get("Content-Type"))
	}
	out := csv.Body.String()
	if !strings.Contains(out, "名称") || !strings.Contains(out, "纬度(高德)") || !strings.Contains(out, "公开点") || !strings.Contains(out, "抓拍摄像头") {
		t.Fatalf("csv body %q", trunc(out, 200))
	}
	filtered := e.do(http.MethodGet, "/admin?q="+url.QueryEscape("公开")+"&type=camera", sess, nil)
	if !strings.Contains(filtered.Body.String(), "公开点") {
		t.Fatal("filter missed point")
	}
}

func intPtr(v int) *int { return &v }

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 80, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
