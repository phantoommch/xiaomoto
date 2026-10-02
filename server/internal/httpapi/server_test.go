package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"motocam/server/internal/store"
)

func TestAdminAuthListAndImageEXIF(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := New("test-admin-token", 1<<20, db)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != 200 {
		t.Fatalf("health %d", health.Code)
	}

	pending := postJSON(t, handler, device(1), map[string]any{
		"name": "赤石收费站", "type": "banned_toll", "road_code": "G25",
		"latitude": 28.2, "longitude": 119.9, "fine_yuan": 100, "penalty_points": 1,
	})
	if pending["status"] != "pending" {
		t.Fatalf("status %#v", pending)
	}

	noToken := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/reports", nil)
	handler.ServeHTTP(noToken, req)
	if noToken.Code != http.StatusUnauthorized {
		t.Fatalf("missing token %d", noToken.Code)
	}

	body, contentType := exifMultipart(t)
	rec := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/reports", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Device-Id", device(2))
	applyUser(t, handler, req)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("multipart %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	imageReq := httptest.NewRequest(http.MethodGet, "/v1/admin/reports/"+created["id"].(string)+"/image", nil)
	imageReq.Header.Set("Authorization", "Bearer test-admin-token")
	imageRec := httptest.NewRecorder()
	handler.ServeHTTP(imageRec, imageReq)
	if imageRec.Code != 200 {
		t.Fatalf("image %d %s", imageRec.Code, imageRec.Body.String())
	}
	if bytes.Contains(imageRec.Body.Bytes(), []byte("GPS-SECRET-PLATE")) {
		t.Fatal("exif payload was stored")
	}

	approve := httptest.NewRequest(http.MethodPost, "/v1/admin/reports/"+pending["id"].(string)+"/approve", nil)
	approve.Header.Set("Authorization", "Bearer test-admin-token")
	approveRec := httptest.NewRecorder()
	handler.ServeHTTP(approveRec, approve)
	if approveRec.Code != 200 {
		t.Fatalf("approve %d %s", approveRec.Code, approveRec.Body.String())
	}

	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/v1/points", nil))
	var list struct {
		Points []struct {
			Name     string `json:"name"`
			RoadCode string `json:"road_code"`
			Coord    string `json:"coord"`
		} `json:"points"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 1 || list.Points[0].RoadCode != "G25" || list.Points[0].Coord != "wgs84" {
		t.Fatalf("points %+v", list.Points)
	}

	bbox := httptest.NewRecorder()
	handler.ServeHTTP(bbox, httptest.NewRequest(http.MethodGet, "/v1/points?min_lat=1&min_lon=1&max_lat=2&max_lon=2", nil))
	var empty struct {
		Points []any `json:"points"`
	}
	_ = json.Unmarshal(bbox.Body.Bytes(), &empty)
	if len(empty.Points) != 0 {
		t.Fatalf("bbox should miss the point: %+v", empty.Points)
	}

	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/v1/points?min_lat=1", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("partial bbox %d", bad.Code)
	}
}

func TestUnlocatedReportStaysPending(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := New("tok", 1<<20, db)

	missing := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(mustJSON(t, map[string]any{
		"name": "吴楼隧道", "type": "other",
	})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", device(1))
	applyUser(t, handler, req)
	handler.ServeHTTP(missing, req)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing coords %d %s", missing.Code, missing.Body.String())
	}

	ok := postJSON(t, handler, device(1), map[string]any{
		"name": "吴楼隧道", "type": "other", "located": false,
		"ocr_location": "福建省宁德市福安市G15沈海高速吴楼隧道",
		"violation":    "违反禁令标志指示",
	})
	if ok["status"] != "pending" {
		t.Fatalf("status %#v", ok)
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/points", nil))
	if bytes.Contains(list.Body.Bytes(), []byte("吴楼隧道")) {
		t.Fatal("unlocated report became public")
	}
	admin := httptest.NewRequest(http.MethodGet, "/v1/admin/reports?status=pending", nil)
	admin.Header.Set("Authorization", "Bearer tok")
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, admin)
	if !bytes.Contains(adminRec.Body.Bytes(), []byte("福建省宁德市福安市G15沈海高速吴楼隧道")) {
		t.Fatalf("admin list %s", adminRec.Body.String())
	}
	approve := httptest.NewRequest(http.MethodPost, "/v1/admin/reports/"+ok["id"].(string)+"/approve", nil)
	approve.Header.Set("Authorization", "Bearer tok")
	approveRec := httptest.NewRecorder()
	handler.ServeHTTP(approveRec, approve)
	if approveRec.Code != http.StatusBadRequest {
		t.Fatalf("approve unlocated %d %s", approveRec.Code, approveRec.Body.String())
	}
}

func TestRejectAndRateLimitHTTP(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 1, RatePerDay: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := New("tok", 1<<20, db)
	first := postJSON(t, handler, device(1), map[string]any{
		"name": "摄像头", "type": "camera", "latitude": 31, "longitude": 121,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(mustJSON(t, map[string]any{
		"name": "摄像头", "type": "camera", "latitude": 31, "longitude": 121,
	})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", device(1))
	applyUser(t, handler, req)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate %d %s", rec.Code, rec.Body.String())
	}

	reject := httptest.NewRequest(http.MethodPost, "/v1/admin/reports/"+first["id"].(string)+"/reject", nil)
	reject.Header.Set("Authorization", "Bearer wrong")
	wrong := httptest.NewRecorder()
	handler.ServeHTTP(wrong, reject)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token %d", wrong.Code)
	}
	reject = httptest.NewRequest(http.MethodPost, "/v1/admin/reports/"+first["id"].(string)+"/reject", nil)
	reject.Header.Set("Authorization", "Bearer tok")
	ok := httptest.NewRecorder()
	handler.ServeHTTP(ok, reject)
	if ok.Code != 200 {
		t.Fatalf("reject %d %s", ok.Code, ok.Body.String())
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/points", nil))
	if bytes.Contains(list.Body.Bytes(), []byte("摄像头")) {
		t.Fatal("rejected report became public")
	}
}

var userTokens sync.Map

func applyUser(t *testing.T, handler http.Handler, req *http.Request) {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+userToken(t, handler))
}

func userToken(t *testing.T, handler http.Handler) string {
	t.Helper()
	if cached, ok := userTokens.Load(handler); ok {
		return cached.(string)
	}
	email := fmt.Sprintf("rider-%s@example.com", stringsTrimPointer(fmt.Sprintf("%p", handler)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/request-code", bytes.NewReader(mustJSON(t, map[string]any{"email": email})))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("request-code %d %s", rec.Code, rec.Body.String())
	}
	code, ok := devCodes.Load(email)
	if !ok {
		t.Fatalf("dev code missing for %s", email)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/verify", bytes.NewReader(mustJSON(t, map[string]any{
		"email": email, "code": code, "termsVersion": "1.0",
	})))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("token %s", rec.Body.String())
	}
	userTokens.Store(handler, body.Token)
	return body.Token
}

func stringsTrimPointer(raw string) string {
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "0"
	}
	return string(out)
}

func postJSON(t *testing.T, handler http.Handler, deviceID string, payload map[string]any) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(mustJSON(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", deviceID)
	applyUser(t, handler, req)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("post %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func device(n int) string {
	const hex = "0123456789abcdef"
	suffix := bytes.Repeat([]byte("0"), 12)
	v := n
	for i := 11; i >= 0 && v > 0; i-- {
		suffix[i] = hex[v%16]
		v /= 16
	}
	return "00000000-0000-4000-8000-" + string(suffix)
}

func exifMultipart(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	raw := jpegBuf.Bytes()
	payload := []byte("Exif\x00\x00GPS-SECRET-PLATE")
	segLen := len(payload) + 2
	marked := append([]byte{}, raw[:2]...)
	marked = append(marked, 0xFF, 0xE1, byte(segLen>>8), byte(segLen))
	marked = append(marked, payload...)
	marked = append(marked, raw[2:]...)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("name", "赤石收费站")
	_ = w.WriteField("type", "banned_toll")
	_ = w.WriteField("road_code", "G25")
	_ = w.WriteField("latitude", "28.2")
	_ = w.WriteField("longitude", "119.9")
	part, err := w.CreateFormFile("image", "ticket.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(marked)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, w.FormDataContentType()
}
