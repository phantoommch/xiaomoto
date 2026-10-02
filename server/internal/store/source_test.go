package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestImportDeviceIDMatchesScript(t *testing.T) {
	text := string(mustRead(t, importScriptPath(t)))
	if !strings.Contains(text, `IMPORT_DEVICE_ID = "`+ImportDeviceID+`"`) {
		t.Fatal("import_points.py device id drifted from ImportDeviceID")
	}
	if !strings.Contains(text, `POINT_SOURCE = "`+SourceImport+`"`) {
		t.Fatal("import_points.py source drifted from SourceImport")
	}
}

func TestImporterWritesImportSource(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	st, err := Open(dbPath, filepath.Join(dir, "img"), Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, "points.json")
	body := []byte(`{"points":[{"name":"导入收费站","type":"banned_toll","latitude":31.2,"longitude":121.5,"road_code":"G15"}]}`)
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("python3", importScriptPath(t), dbPath, jsonPath).CombinedOutput()
	if err != nil {
		t.Fatalf("importer: %v %s", err, out)
	}
	st, err = Open(dbPath, filepath.Join(dir, "img"), Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	list, err := st.List(nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 1 || list.Points[0].Source != SourceImport {
		t.Fatalf("imported point %+v", list.Points)
	}
	var device string
	if err := st.db.QueryRow(`SELECT device_id FROM reports`).Scan(&device); err != nil {
		t.Fatal(err)
	}
	if device != ImportDeviceID {
		t.Fatalf("report device %s", device)
	}
}

func TestSubmitImportDeviceSetsImportUntilRealReport(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	imported := ReportInput{Name: "导入点", Type: "camera", Latitude: 30.1, Longitude: 120.1, DeviceID: ImportDeviceID}
	got := mustSubmit(t, st, imported)
	if got.Status != "approved" {
		t.Fatalf("import publish %+v", got)
	}
	list, err := st.List(nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 1 || list.Points[0].Source != SourceImport {
		t.Fatalf("source %+v", list.Points)
	}
	real := imported
	real.DeviceID = deviceID(2)
	again := mustSubmit(t, st, real)
	if again.Status != "approved" || again.PublicPointID != got.PublicPointID {
		t.Fatalf("real report should merge, %+v", again)
	}
	list, err = st.List(nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if list.Points[0].Source != SourceUserUpload {
		t.Fatalf("real report should label user_upload, got %s", list.Points[0].Source)
	}
}

func TestSourceBackfillOnce(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100})
	clearBackfill(t, st)
	insertPointRow(t, st, "only-import", SourceUserUpload)
	insertReportRow(t, st, "only-import", ImportDeviceID)
	insertPointRow(t, st, "mixed", SourceUserUpload)
	insertReportRow(t, st, "mixed", ImportDeviceID)
	insertReportRow(t, st, "mixed", deviceID(4))
	insertPointRow(t, st, "kept", "official")
	insertReportRow(t, st, "kept", ImportDeviceID)
	insertPointRow(t, st, "empty-real", "")
	insertReportRow(t, st, "empty-real", deviceID(5))
	insertPointRow(t, st, "blank", "")
	if err := st.backfillPointSource(); err != nil {
		t.Fatal(err)
	}
	assertSource(t, st, "only-import", SourceImport)
	assertSource(t, st, "mixed", SourceUserUpload)
	assertSource(t, st, "kept", "official")
	assertSource(t, st, "empty-real", SourceUserUpload)
	assertSource(t, st, "blank", "")

	if _, err := st.db.Exec(`UPDATE points SET source='' WHERE id='empty-real'`); err != nil {
		t.Fatal(err)
	}
	if err := st.backfillPointSource(); err != nil {
		t.Fatal(err)
	}
	assertSource(t, st, "empty-real", "")
	assertSource(t, st, "only-import", SourceImport)
}

func TestReopenDoesNotRefillClearedSource(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	img := filepath.Join(dir, "img")
	limits := Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50}
	st, err := Open(dbPath, img, limits)
	if err != nil {
		t.Fatal(err)
	}
	clearBackfill(t, st)
	insertPointRow(t, st, "rider", "")
	insertReportRow(t, st, "rider", deviceID(3))
	if err := st.backfillPointSource(); err != nil {
		t.Fatal(err)
	}
	assertSource(t, st, "rider", SourceUserUpload)
	if _, err := st.db.Exec(`UPDATE points SET source='' WHERE id='rider'`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dbPath, img, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	assertSource(t, st, "rider", "")
}

func TestHistoricalImportDeviceMajority(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100})
	clearBackfill(t, st)
	historical := "11111111-1111-4111-8111-111111111111"
	for i := 0; i < 11; i++ {
		id := "hist-" + string(rune('a'+i))
		insertPointRow(t, st, id, SourceUserUpload)
		insertReportRow(t, st, id, historical)
	}
	insertPointRow(t, st, "real-a", SourceUserUpload)
	insertReportRow(t, st, "real-a", deviceID(7))
	insertPointRow(t, st, "real-b", SourceUserUpload)
	insertReportRow(t, st, "real-b", deviceID(8))
	if err := st.backfillPointSource(); err != nil {
		t.Fatal(err)
	}
	assertSource(t, st, "hist-a", SourceImport)
	assertSource(t, st, "real-a", SourceUserUpload)
	assertSource(t, st, "real-b", SourceUserUpload)

	got := mustSubmit(t, st, ReportInput{
		Name: "再次导入", Type: "other", Latitude: 35, Longitude: 110, DeviceID: historical,
	})
	if got.Status != "approved" {
		t.Fatalf("historical device publish %+v", got)
	}
	var source string
	if err := st.db.QueryRow(`SELECT source FROM points WHERE id=?`, got.PublicPointID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != SourceImport {
		t.Fatalf("later import from the recorded device got %s", source)
	}
}

func TestSmallUnknownDeviceIsNotImport(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	clearBackfill(t, st)
	for i := 0; i < 3; i++ {
		id := "few-" + string(rune('a'+i))
		insertPointRow(t, st, id, SourceUserUpload)
		insertReportRow(t, st, id, deviceID(9))
	}
	if err := st.backfillPointSource(); err != nil {
		t.Fatal(err)
	}
	assertSource(t, st, "few-a", SourceUserUpload)
}

func TestKnownImportDeviceBeatsLargerStranger(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 50, RatePerDay: 100})
	clearBackfill(t, st)
	insertPointRow(t, st, "script", SourceUserUpload)
	insertReportRow(t, st, "script", ImportDeviceID)
	stranger := "22222222-2222-4222-8222-222222222222"
	for i := 0; i < 11; i++ {
		id := "str-" + string(rune('a'+i))
		insertPointRow(t, st, id, SourceUserUpload)
		insertReportRow(t, st, id, stranger)
	}
	if err := st.backfillPointSource(); err != nil {
		t.Fatal(err)
	}
	assertSource(t, st, "script", SourceImport)
	assertSource(t, st, "str-a", SourceUserUpload)
}

func clearBackfill(t *testing.T, st *Store) {
	t.Helper()
	if _, err := st.db.Exec(`DELETE FROM schema_meta WHERE key=?`, sourceBackfillKey); err != nil {
		t.Fatal(err)
	}
}

func insertPointRow(t *testing.T, st *Store, id, source string) {
	t.Helper()
	_, err := st.db.Exec(`INSERT INTO points (
  id, name, type, latitude, longitude, source_count, created_at, updated_at, source
) VALUES (?, '点', 'other', 30, 120, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', ?)`, id, source)
	if err != nil {
		t.Fatal(err)
	}
}

func insertReportRow(t *testing.T, st *Store, pointID, device string) {
	t.Helper()
	_, err := st.db.Exec(`INSERT INTO reports (
  id, device_id, name, type, latitude, longitude, status, public_point_id, created_at
) VALUES (?, ?, '点', 'other', 30, 120, 'merged', ?, '2026-01-01T00:00:00Z')`, pointID+"-"+device, device, pointID)
	if err != nil {
		t.Fatal(err)
	}
}

func assertSource(t *testing.T, st *Store, id, want string) {
	t.Helper()
	var got string
	if err := st.db.QueryRow(`SELECT source FROM points WHERE id=?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("point %s source %q, want %q", id, got, want)
	}
}

func importScriptPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "import_points.py")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
