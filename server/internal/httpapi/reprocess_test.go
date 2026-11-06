package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"motocam/server/internal/recog"
	"motocam/server/internal/store"
)

// The rejected card kept the Kunming Qingshui village pin. These are the WGS
// values stored for the GCJ point the admin UI showed. Reject deletes the
// photo; the stored OCR still has the wrapped 违法地点 and the overlay.
const (
	reprocessOCR = "地点:三清高速K15+350M曲靖方向 车道号:02\n" +
		"违法行为：驾驶校车、中型以上载客载货汽车、危险物品运输车辆以外的机动车行驶超过规定时速百分之十以上未达到百分之二十的\n" +
		"罚款100元 记0分\n" +
		"加入处理清单\n" +
		"违法车辆 云A8351Y\n" +
		"违法地点\n" +
		"三清高速（曲靖三宝－昆明清水）15\n" +
		"公里350米·三清高速（曲靖三宝－昆明清水）9公里810米（西向北方向）\n" +
		"违法时间\n" +
		"2026-08-20 16:13\n" +
		"申请陈述和申辩\n" +
		"处理并缴款\n"
	villageLat = 24.99384
	villageLon = 102.88773
)

func TestReprocessRejectedSanqingAndLeaveTheOther(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), store.Limits{
		PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	markers := &reprocessMap{}
	handler := NewWithOptions(HandlerOptions{
		AdminToken: "admin-token", MaxImage: 1 << 20, DB: db, DevMode: true,
		Locate: func(_ context.Context, in store.ReportInput) recog.Result {
			return recog.Locate(in.OcrLocation, in.OcrText, "", markers)
		},
	})
	sanqing := postJSON(t, handler, device(1), map[string]any{
		"name": "三清 · 三清高速(曲靖三宝－昆明清水)15", "type": "other",
		"latitude": villageLat, "longitude": villageLon,
		"confidence": "low", "road_name": "三清",
		"ocr_location": "三清高速（曲靖三宝－昆明清水）15",
		"ocr_text":     reprocessOCR,
	})
	other := postJSON(t, handler, device(2), map[string]any{
		"name": "违法处理", "type": "other",
		"latitude": villageLat, "longitude": villageLon,
		"confidence":   "low",
		"ocr_location": "违法处理",
		"ocr_text":     "违法处理\n返回\n加入处理清单\n处理并缴款\n申请陈述和申辩",
	})
	reject := httptest.NewRequest(http.MethodPost, "/v1/admin/reports/"+sanqing["id"].(string)+"/reject", nil)
	reject.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, reject)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject %d %s", rec.Code, rec.Body.String())
	}
	approve := httptest.NewRequest(http.MethodPost, "/v1/admin/reports/"+sanqing["id"].(string)+"/approve", nil)
	approve.Header.Set("Authorization", "Bearer admin-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, approve)
	if rec.Code != http.StatusConflict {
		t.Fatalf("approve rejected report %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/reports/reprocess", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reprocess %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Approved []struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			RoadCode string `json:"road_code"`
			Stake    string `json:"stake"`
		} `json:"approved"`
		Skipped []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Approved) != 1 || out.Approved[0].ID != sanqing["id"].(string) {
		t.Fatalf("approved %#v", out.Approved)
	}
	if out.Approved[0].Status != "merged" || out.Approved[0].RoadCode != "S34" || out.Approved[0].Stake != "K15+350-K9+810" {
		t.Fatalf("approved row %#v", out.Approved[0])
	}
	if len(out.Skipped) != 1 || out.Skipped[0].ID != other["id"].(string) {
		t.Fatalf("skipped %#v", out.Skipped)
	}

	merged := adminReports(t, handler, "merged")
	if len(merged) != 1 {
		t.Fatalf("merged %#v", merged)
	}
	got := merged[0]
	if got.RoadCode != "S34" || got.RoadName != "三清" || got.Stake != "K15+350-K9+810" || got.Confidence != "exact" {
		t.Fatalf("report %#v", got)
	}
	if !strings.Contains(got.Direction, "西向北") || !strings.Contains(got.Direction, "曲靖方向") {
		t.Fatalf("direction %q", got.Direction)
	}
	if !strings.Contains(got.OcrLocation, "15公里350米") || !strings.Contains(got.OcrLocation, "9公里810米") {
		t.Fatalf("location %q", got.OcrLocation)
	}
	if nearVillage(got.Latitude, got.Longitude) || got.Latitude < 25.2 || got.Longitude < 103.2 {
		t.Fatalf("still the village pin %.5f,%.5f", got.Latitude, got.Longitude)
	}
	if got.EndLatitude == nil || got.EndLongitude == nil || nearVillage(*got.EndLatitude, *got.EndLongitude) || *got.EndLatitude < 25.2 {
		t.Fatalf("missing section end %#v", got)
	}
	pending := adminReports(t, handler, "pending")
	if len(pending) != 1 || pending[0].ID != other["id"].(string) || !nearVillage(pending[0].Latitude, pending[0].Longitude) {
		t.Fatalf("other report changed %#v", pending)
	}
	rejected := adminReports(t, handler, "rejected")
	if len(rejected) != 0 {
		t.Fatalf("rejected left %#v", rejected)
	}

	mine := httptest.NewRequest(http.MethodGet, "/v1/me/reports", nil)
	applyUser(t, handler, mine)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, mine)
	if rec.Code != http.StatusOK {
		t.Fatalf("mine %d %s", rec.Code, rec.Body.String())
	}
	var mineBody struct {
		Reports []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Label  string `json:"label"`
			Stake  string `json:"stake"`
		} `json:"reports"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &mineBody); err != nil {
		t.Fatal(err)
	}
	sawPass := false
	for _, row := range mineBody.Reports {
		if row.ID == sanqing["id"].(string) {
			sawPass = row.Status == "merged" && row.Label == "已通过" && row.Stake == "K15+350-K9+810"
		}
		if row.ID == other["id"].(string) && row.Label == "已通过" {
			t.Fatal("chrome upload was shown as approved")
		}
	}
	if !sawPass {
		t.Fatalf("uploader cannot see 已通过: %#v", mineBody.Reports)
	}
	sawStakeQuery := false
	for _, addr := range markers.addrs {
		if addr == "S34 15公里" || addr == "S34 9公里" {
			sawStakeQuery = true
		}
		if strings.Contains(addr, "三清高速（") || strings.Contains(addr, "清水村") {
			t.Fatalf("place-name geocode %q", addr)
		}
	}
	if !sawStakeQuery {
		t.Fatalf("queries %#v", markers.addrs)
	}
	if store.SubmissionLabel("merged") != "已通过" || store.SubmissionLabel("rejected") != "已驳回" {
		t.Fatal("label")
	}
}

func adminReports(t *testing.T, handler http.Handler, status string) []store.AdminReport {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/reports?status="+status, nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %s %d %s", status, rec.Code, rec.Body.String())
	}
	var body struct {
		Reports []store.AdminReport `json:"reports"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Reports
}

func nearVillage(lat, lon float64) bool {
	return abs(lat-villageLat) < 0.05 && abs(lon-villageLon) < 0.05
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// reprocessMap is the recorded kilometre-marker geocode. Any other address is
// the village hit, so a place-name lookup cannot be mistaken for success.
type reprocessMap struct {
	addrs []string
}

func (m *reprocessMap) Cities() []string { return nil }

func (m *reprocessMap) Get(path string, query map[string]string) (map[string]any, error) {
	if path != "v3/geocode/geo" {
		return map[string]any{}, nil
	}
	addr := query["address"]
	m.addrs = append(m.addrs, addr)
	var code string
	var km int
	if _, err := fmt.Sscanf(addr, "%s %d公里", &code, &km); err != nil || code != "S34" {
		return map[string]any{"geocodes": []any{map[string]any{
			"formatted_address": "云南省昆明市呈贡区清水村",
			"province":          "云南省",
			"location":          "102.889100,24.990710",
			"level":             "村庄",
		}}}, nil
	}
	lon, lat, ok := reprocessStake(km)
	if !ok {
		return map[string]any{"geocodes": []any{}}, nil
	}
	return map[string]any{"geocodes": []any{map[string]any{
		"formatted_address": fmt.Sprintf("云南省曲靖市马龙区S34三清高速%d公里", km),
		"province":          "云南省",
		"location":          fmt.Sprintf("%.6f,%.6f", lon, lat),
		"level":             "兴趣点",
	}}}, nil
}

func reprocessStake(km int) (lon, lat float64, ok bool) {
	switch km {
	case 15:
		return 103.560000, 25.380000, true
	case 16:
		return 103.553000, 25.373000, true
	case 9:
		return 103.620000, 25.430000, true
	case 10:
		return 103.613000, 25.423000, true
	default:
		return 0, 0, false
	}
}
