package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"motocam/server/internal/recog"
	"motocam/server/internal/store"
)

func TestRelocationDoesNotBlockUpload(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	started := make(chan struct{})
	release := make(chan struct{})
	handler := NewWithOptions(HandlerOptions{
		AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true, AmapKey: "test-key",
		Locate: func(ctx context.Context, in store.ReportInput) recog.Result {
			close(started)
			select {
			case <-release:
				return recog.Result{
					Lat: 31.425402, Lon: 120.220966, HasPoint: true,
					Method: "tunnel-portal", Conf: "high", Level: "exact", How: "东端 (驾车路线已核对)",
				}
			case <-ctx.Done():
				return recog.Result{}
			}
		},
	})
	body := map[string]any{
		"name": "太湖隧道东入口", "type": "camera",
		"latitude": 31.43, "longitude": 120.20,
		"confidence": "low", "ocr_text": "太湖隧道东入口,由东向西",
	}
	begin := time.Now()
	rec := postBuild(t, handler, body, "", "")
	if time.Since(begin) > time.Second {
		t.Fatalf("upload blocked for %s", time.Since(begin))
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Status != "pending" {
		t.Fatalf("body %s", rec.Body.String())
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("relocator did not start")
	}
	list, err := db.List(nil, nil, nil, nil, nil, 10)
	if err != nil || len(list.Points) != 0 {
		t.Fatalf("published before relocation %+v %v", list.Points, err)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		list, err = db.List(nil, nil, nil, nil, nil, 10)
		if err == nil && len(list.Points) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("point not published %+v %v", list.Points, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if list.Points[0].Latitude < 31.42 || list.Points[0].Latitude > 31.43 {
		t.Fatalf("published %+v", list.Points[0])
	}
	if list.Points[0].Source != "user_upload" {
		t.Fatalf("source %q", list.Points[0].Source)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/points", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("user_upload")) || !bytes.Contains(rec.Body.Bytes(), []byte("150")) {
		t.Fatalf("admin points %d %s", rec.Code, rec.Body.String())
	}
}

func TestRelocationTimeoutLeavesReportPending(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	finished := make(chan struct{})
	handler := NewWithOptions(HandlerOptions{
		AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true, AmapKey: "test-key",
		RelocDeadline: 30 * time.Millisecond,
		Locate: func(ctx context.Context, in store.ReportInput) recog.Result {
			<-ctx.Done()
			close(finished)
			return recog.Result{
				Lat: 37.57, Lon: 109.75, HasPoint: true,
				Method: "glued-stake", Conf: "medium", Level: "exact", How: "partial",
			}
		},
	})
	rec := postBuild(t, handler, map[string]any{
		"name": "待定点", "type": "camera", "latitude": 30.1, "longitude": 120.1,
		"confidence": "low", "ocr_text": "G15 1公里",
	}, "", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("deadline did not fire")
	}
	list, err := db.List(nil, nil, nil, nil, nil, 10)
	if err != nil || len(list.Points) != 0 {
		t.Fatalf("timeout should not publish %+v %v", list.Points, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/reports?status=pending", nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK && bytes.Contains(rec.Body.Bytes(), []byte("定位超时")) &&
			bytes.Contains(rec.Body.Bytes(), []byte("30.1")) && !bytes.Contains(rec.Body.Bytes(), []byte("109.75")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("review %d %s", rec.Code, rec.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExactUploadSkipsRelocator(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	called := false
	handler := NewWithOptions(HandlerOptions{
		AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true, AmapKey: "test-key",
		Locate: func(ctx context.Context, in store.ReportInput) recog.Result {
			called = true
			return recog.Result{}
		},
	})
	rec := postBuild(t, handler, map[string]any{
		"name": "已定点", "type": "camera", "latitude": 30.2, "longitude": 120.2,
		"confidence": "exact", "road_code": "G15",
	}, "", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Status != "approved" {
		t.Fatalf("body %s", rec.Body.String())
	}
	if called {
		t.Fatal("exact client result was relocated")
	}
}

func TestAdminSegmentEndpoint(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler := NewWithOptions(HandlerOptions{AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true})
	start := postBuild(t, handler, map[string]any{
		"name": "起点", "type": "camera", "latitude": 24.89, "longitude": 103.07,
		"confidence": "exact", "road_code": "G78",
	}, "", "")
	end := postBuild(t, handler, map[string]any{
		"name": "终点", "type": "camera", "latitude": 24.93, "longitude": 103.14,
		"confidence": "exact", "road_code": "G78",
	}, "", "")
	var a, b struct {
		PublicPointID string `json:"public_point_id"`
	}
	if json.Unmarshal(start.Body.Bytes(), &a) != nil || json.Unmarshal(end.Body.Bytes(), &b) != nil || a.PublicPointID == "" || b.PublicPointID == "" {
		t.Fatalf("seed %s %s", start.Body.String(), end.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/points/"+a.PublicPointID+"/segment", bytes.NewReader(mustJSON(t, map[string]any{
		"end_point_id": b.PublicPointID, "stake": "K1807+910", "direction": "上行",
	})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("segment %d %s", rec.Code, rec.Body.String())
	}
	list, err := db.List(nil, nil, nil, nil, nil, 10)
	if err != nil || len(list.Points) != 2 {
		t.Fatalf("points %+v %v", list.Points, err)
	}
}

func TestUnconfirmedTunnelStaysPending(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	done := make(chan struct{})
	handler := NewWithOptions(HandlerOptions{
		AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true, AmapKey: "test-key",
		Locate: func(ctx context.Context, in store.ReportInput) recog.Result {
			defer close(done)
			return recog.Result{
				Lat: 31.422501, Lon: 120.232943, HasPoint: true,
				Method: "tunnel-portal", Conf: "low", Level: "low", How: "太湖隧道 东端 (驾车路线未核对)",
			}
		},
	})
	rec := postBuild(t, handler, map[string]any{
		"name": "太湖隧道东入口", "type": "camera",
		"latitude": 31.43, "longitude": 120.20,
		"confidence": "exact", "ocr_text": "太湖隧道东入口,由东向西",
	}, "", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("review did not run")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/reports?status=pending", nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code == http.StatusOK && bytes.Contains(out.Body.Bytes(), []byte("隧道洞口尚未核对")) &&
			bytes.Contains(out.Body.Bytes(), []byte("31.43")) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending review %s", out.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRelocationQueueDropsExtraJob(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	handler := NewWithOptions(HandlerOptions{
		AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true, AmapKey: "test-key",
		RelocQueue: 1,
		Locate: func(ctx context.Context, in store.ReportInput) recog.Result {
			once.Do(func() { close(started) })
			<-release
			return recog.Result{}
		},
	})
	t.Cleanup(func() { close(release) })
	body := func(name string, lat float64) map[string]any {
		return map[string]any{
			"name": name, "type": "camera", "latitude": lat, "longitude": 120.2,
			"confidence": "low", "ocr_text": "G15 1公里",
		}
	}
	if postBuild(t, handler, body("第一", 30.1), "", "").Code != http.StatusCreated {
		t.Fatal("first")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start")
	}
	if postBuild(t, handler, body("第二", 30.2), "", "").Code != http.StatusCreated {
		t.Fatal("second")
	}
	third := postBuild(t, handler, body("第三", 30.3), "", "")
	if third.Code != http.StatusCreated {
		t.Fatal("third")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/reports?status=pending", nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if bytes.Contains(out.Body.Bytes(), []byte("重定位队列已满")) && bytes.Contains(out.Body.Bytes(), []byte("30.3")) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue drop %s", out.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
