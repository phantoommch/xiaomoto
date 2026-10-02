package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"motocam/server/internal/store"
)

func TestPublicPagesNeedNoLogin(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := New("test-admin-token", 1<<20, db)

	pages := []struct {
		path string
		want []string
	}{
		{"/privacy", []string{"隐私政策", "English", "contact@example.com", "注销账号", "X-Device-Id"}},
		{"/terms", []string{"用户协议与免责声明", "提醒判断在手机上进行", "contact@example.com"}},
		{"/support", []string{"常见问题", "怎么登录", "contact@example.com"}},
	}
	for _, page := range pages {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, page.path, nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status %d", page.path, rec.Code)
		}
		if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s content-type %q", page.path, rec.Header().Get("Content-Type"))
		}
		body := rec.Body.String()
		for _, phrase := range page.want {
			if !strings.Contains(body, phrase) {
				t.Fatalf("%s missing %q", page.path, phrase)
			}
		}
		if strings.Contains(body, "http://") || strings.Contains(body, "https://") {
			t.Fatalf("%s should not load an external asset", page.path)
		}
	}
}
