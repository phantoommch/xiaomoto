package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"motocam/server/internal/store"
)

// Old builds (no X-App-Build, or < 30) upload without a token like build 28;
// build 30+ needs a token; a valid token from any build records user_id.
func TestUploadBuildGateAndUserID(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	db, err := store.Open(dbPath, filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := NewWithOptions(HandlerOptions{AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true})
	token := loginDev(t, handler, "uploader@example.com")
	meReq := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	var me struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(meRec.Body.Bytes(), &me); err != nil || me.ID == "" {
		t.Fatalf("me %d %s", meRec.Code, meRec.Body.String())
	}

	cases := []struct {
		name     string
		build    string
		token    string
		wantCode int
		wantUser string
	}{
		{"no header, no token", "", "", http.StatusCreated, ""},
		{"build 28, no token", "28", "", http.StatusCreated, ""},
		{"build 29, no token", "29", "", http.StatusCreated, ""},
		{"garbage build, no token", "abc", "", http.StatusCreated, ""},
		{"build 30, no token", "30", "", http.StatusUnauthorized, ""},
		{"build 31, no token", "31", "", http.StatusUnauthorized, ""},
		{"build 28, bad token", "28", "not-a-token", http.StatusUnauthorized, ""},
		{"build 30, bad token", "30", "not-a-token", http.StatusUnauthorized, ""},
		{"build 30, token", "30", token, http.StatusCreated, me.ID},
		{"build 28, token", "28", token, http.StatusCreated, me.ID},
		{"no header, token", "", token, http.StatusCreated, me.ID},
	}
	raw, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for i, tc := range cases {
		rec := postBuild(t, handler, map[string]any{
			"name": "测试点", "type": "camera", "latitude": 30 + float64(i), "longitude": 120,
		}, tc.build, tc.token)
		if rec.Code != tc.wantCode {
			t.Fatalf("%s: code %d %s", tc.name, rec.Code, rec.Body.String())
		}
		if rec.Code != http.StatusCreated {
			continue
		}
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.ID == "" {
			t.Fatalf("%s: body %s", tc.name, rec.Body.String())
		}
		var userID string
		if err := raw.QueryRow(`SELECT user_id FROM reports WHERE id=?`, out.ID).Scan(&userID); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if userID != tc.wantUser {
			t.Fatalf("%s: user_id %q want %q", tc.name, userID, tc.wantUser)
		}
	}

	// Legacy upload that fails validation is rejected after the auth check, without creating data.
	invalid := postBuild(t, handler, map[string]any{"name": "测试点", "type": "camera"}, "28", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid legacy upload %d %s", invalid.Code, invalid.Body.String())
	}
}
