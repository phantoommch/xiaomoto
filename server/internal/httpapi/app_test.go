package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"motocam/server/internal/appdist"
	"motocam/server/internal/store"
)

func TestAppPublishDownloadAndManifest(t *testing.T) {
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
	handler := NewWithApps("tok", 1<<20, db, apps, "https://example.invalid")

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/v1/app/latest?platform=android", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("empty latest %d %s", missing.Code, missing.Body.String())
	}

	noToken := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/app", strings.NewReader("x"))
	handler.ServeHTTP(noToken, req)
	if noToken.Code != http.StatusUnauthorized {
		t.Fatalf("auth %d", noToken.Code)
	}

	body, contentType := appMultipart(t, map[string]string{
		"platform":    "android",
		"versionCode": "21",
		"versionName": "1.0",
		"notes":       "修复了几处提醒",
		"force":       "false",
	}, "app-debug.apk", []byte("android-apk-bytes"))
	rec := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/admin/app", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer tok")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish %d %s", rec.Code, rec.Body.String())
	}
	var published map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published["url"] != "https://example.invalid/v1/app/download/android/app-debug.apk" {
		t.Fatalf("url %#v", published["url"])
	}
	if published["force"] != false || published["notes"] != "修复了几处提醒" {
		t.Fatalf("body %#v", published)
	}

	latest := httptest.NewRecorder()
	handler.ServeHTTP(latest, httptest.NewRequest(http.MethodGet, "/v1/app/latest?platform=android", nil))
	if latest.Code != 200 || !bytes.Contains(latest.Body.Bytes(), []byte(`"versionCode":21`)) {
		t.Fatalf("latest %d %s", latest.Code, latest.Body.String())
	}

	full := httptest.NewRecorder()
	handler.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/v1/app/download/android/app-debug.apk", nil))
	if full.Code != 200 {
		t.Fatalf("download %d", full.Code)
	}
	if full.Header().Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Fatalf("type %s", full.Header().Get("Content-Type"))
	}
	if full.Body.String() != "android-apk-bytes" {
		t.Fatalf("body %q", full.Body.String())
	}

	part := httptest.NewRecorder()
	rangeReq := httptest.NewRequest(http.MethodGet, "/v1/app/download/android/app-debug.apk", nil)
	rangeReq.Header.Set("Range", "bytes=0-6")
	handler.ServeHTTP(part, rangeReq)
	if part.Code != http.StatusPartialContent {
		t.Fatalf("range %d %s", part.Code, part.Body.String())
	}
	if part.Body.String() != "android" {
		t.Fatalf("range body %q", part.Body.String())
	}
	if part.Header().Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Fatalf("range type %s", part.Header().Get("Content-Type"))
	}

	ipaBody, ipaType := appMultipart(t, map[string]string{
		"platform":    "ios",
		"versionCode": "21",
		"versionName": "1.0",
		"notes":       "同样的更新",
		"force":       "yes",
	}, "Motocam.ipa", []byte("ipa-bytes"))
	ipaRec := httptest.NewRecorder()
	ipaReq := httptest.NewRequest(http.MethodPost, "/v1/admin/app", ipaBody)
	ipaReq.Header.Set("Content-Type", ipaType)
	ipaReq.Header.Set("Authorization", "Bearer tok")
	handler.ServeHTTP(ipaRec, ipaReq)
	if ipaRec.Code != http.StatusCreated {
		t.Fatalf("ipa %d %s", ipaRec.Code, ipaRec.Body.String())
	}

	ipaGet := httptest.NewRecorder()
	handler.ServeHTTP(ipaGet, httptest.NewRequest(http.MethodGet, "/v1/app/download/ios/Motocam.ipa", nil))
	if ipaGet.Header().Get("Content-Type") != "application/octet-stream" || ipaGet.Body.String() != "ipa-bytes" {
		t.Fatalf("ipa download %d %s %q", ipaGet.Code, ipaGet.Header().Get("Content-Type"), ipaGet.Body.String())
	}

	manifest := httptest.NewRecorder()
	handler.ServeHTTP(manifest, httptest.NewRequest(http.MethodGet, "/v1/app/ios/manifest.plist", nil))
	if manifest.Code != 200 {
		t.Fatalf("manifest %d %s", manifest.Code, manifest.Body.String())
	}
	text := manifest.Body.String()
	for _, want := range []string{
		"com.example.xiaomoto",
		"小moto",
		"https://example.invalid/v1/app/download/ios/Motocam.ipa",
		"<string>1.0</string>",
		"software-package",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manifest missing %s\n%s", want, text)
		}
	}

	points := httptest.NewRecorder()
	handler.ServeHTTP(points, httptest.NewRequest(http.MethodGet, "/v1/points", nil))
	if points.Code != 200 || !bytes.Contains(points.Body.Bytes(), []byte(`"points"`)) {
		t.Fatalf("points still work %d %s", points.Code, points.Body.String())
	}
}

func TestLatestWithoutCatalogIsNotFound(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := New("tok", 1<<20, db)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/app/latest?platform=ios", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
}

func appMultipart(t *testing.T, fields map[string]string, filename string, payload []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}
