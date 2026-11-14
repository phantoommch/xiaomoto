package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"motocam/server/internal/appdist"
	"motocam/server/internal/store"
)

func TestDownloadPageAndAndroidRedirect(t *testing.T) {
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
	h := NewWithApps("tok", 1<<20, db, apps, "")
	get := func(path, ua string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := get("/download/android", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("empty redirect %d", rec.Code)
	}
	if rec := get("/download", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "安卓版即将发布") {
		t.Fatalf("empty page %d", rec.Code)
	}

	for _, v := range []string{"33", "34"} {
		body, ct := appMultipart(t, map[string]string{"platform": "android", "versionCode": v, "versionName": "1.0", "notes": "n"}, "a.apk", []byte("apk-"+v))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/admin/app", body)
		req.Header.Set("Content-Type", ct)
		req.Header.Set("Authorization", "Bearer tok")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("publish %s %d %s", v, rec.Code, rec.Body.String())
		}
	}
	rec := get("/download/android", "")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/v1/app/download/android/") {
		t.Fatalf("redirect %d %q", rec.Code, rec.Header().Get("Location"))
	}
	loc := rec.Header().Get("Location")
	file := get(loc, "")
	if file.Code != 200 || file.Body.String() != "apk-34" {
		t.Fatalf("followed %s: %d %q", loc, file.Code, file.Body.String())
	}

	page := get("/download", "Mozilla/5.0 (Linux; Android 14)").Body.String()
	for _, want := range []string{"小moto安全驾驶指南", "第 34 版", `href="/download/android"`, `href="/download/ios"`, "继续安装", "App Store 正式版即将上线"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page missing %q", want)
		}
	}
	if strings.Index(page, `id="android"`) > strings.Index(page, `id="iphone"`) {
		t.Fatal("android first for android UA")
	}
	if strings.Contains(page, `<html lang="zh-CN" class="wx">`) {
		t.Fatal("wx class for plain browser")
	}
	wx := get("/download", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0) MicroMessenger/8.0.50").Body.String()
	if !strings.Contains(wx, `class="wx"`) || strings.Index(wx, `id="iphone"`) > strings.Index(wx, `id="android"`) {
		t.Fatal("wechat iPhone page")
	}
	icon := get("/download/icon.png", "")
	if icon.Code != 200 || icon.Header().Get("Content-Type") != "image/png" || icon.Body.Len() < 1000 {
		t.Fatalf("icon %d", icon.Code)
	}
	if rec := get("/download/", ""); rec.Code != http.StatusMovedPermanently {
		t.Fatalf("slash %d", rec.Code)
	}
}
