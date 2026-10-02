package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"motocam/server/internal/geo"
)

func TestPromotionNeedsIndependentNearbyReports(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	base := ReportInput{Name: "赤石收费站", Type: "banned_toll", RoadCode: "G25", RoadName: "长深高速", Latitude: 28.2, Longitude: 119.9, Violation: "违反禁令标志"}

	first := mustSubmit(t, st, withDevice(base, 1))
	if first.Status != "pending" {
		t.Fatalf("first status %s", first.Status)
	}
	second := mustSubmit(t, st, withDevice(base, 2))
	if second.Status != "pending" {
		t.Fatalf("two reports should stay pending, got %s", second.Status)
	}
	same := mustSubmit(t, st, withDevice(base, 1))
	if same.Status != "pending" {
		t.Fatalf("same device must not count twice, got %s", same.Status)
	}

	far := base
	far.Latitude, far.Longitude = geo.Offset(base.Latitude, base.Longitude, 350, 0)
	if geo.Distance(base.Latitude, base.Longitude, far.Latitude, far.Longitude) <= 200 {
		t.Fatal("fixture is not far enough")
	}
	outside := mustSubmit(t, st, withDevice(far, 3))
	if outside.Status != "pending" {
		t.Fatalf("350m away should not join, got %s", outside.Status)
	}

	third := mustSubmit(t, st, withDevice(base, 4))
	if third.Status != "approved" || third.PublicPointID == "" {
		t.Fatalf("third independent report should publish, %+v", third)
	}
	list, err := st.List(nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 1 || list.Points[0].SourceCount != 3 || list.Points[0].RoadCode != "G25" {
		t.Fatalf("public point %+v", list.Points)
	}
	if list.Points[0].Coord != "wgs84" {
		t.Fatalf("coord %s", list.Points[0].Coord)
	}

	again := mustSubmit(t, st, withDevice(base, 5))
	if again.Status != "approved" || again.PublicPointID != third.PublicPointID {
		t.Fatalf("later report should join %+v", again)
	}
	list, _ = st.List(nil, nil, nil, nil, nil, 10)
	if list.Points[0].SourceCount != 4 {
		t.Fatalf("source count %d", list.Points[0].SourceCount)
	}
	duplicate := mustSubmit(t, st, withDevice(base, 5))
	_ = duplicate
	list, _ = st.List(nil, nil, nil, nil, nil, 10)
	if list.Points[0].SourceCount != 4 {
		t.Fatalf("same device should not inflate source count, got %d", list.Points[0].SourceCount)
	}
}

func TestConflictingRoadOrTypeDoesNotAgree(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 2, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	base := ReportInput{Name: "赤石收费站", Type: "banned_toll", RoadCode: "g25", Latitude: 28.2, Longitude: 119.9}
	mustSubmit(t, st, withDevice(base, 1))
	otherRoad := base
	otherRoad.RoadCode = "G15"
	got := mustSubmit(t, st, withDevice(otherRoad, 2))
	if got.Status != "pending" {
		t.Fatal("different road code should not auto-publish")
	}
	otherType := base
	otherType.Type = "camera"
	otherType.RoadCode = "G25"
	got = mustSubmit(t, st, withDevice(otherType, 3))
	if got.Status != "pending" {
		t.Fatal("different type should not auto-publish")
	}
	blank := ReportInput{Name: "赤石收费站", Type: "banned_toll", Latitude: 29.4, Longitude: 120.2}
	mustSubmit(t, st, withDevice(ReportInput{Name: "赤石收费站", Type: "banned_toll", RoadCode: "G25", Latitude: 29.4, Longitude: 120.2}, 5))
	got = mustSubmit(t, st, withDevice(blank, 6))
	if got.Status != "approved" {
		t.Fatalf("empty road code still agrees, got %s", got.Status)
	}
	list, err := st.List(nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 1 || list.Points[0].RoadCode != "G25" {
		t.Fatalf("point %+v", list.Points)
	}
}

func TestRateLimitAndValidation(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 2, RatePerDay: 5})
	base := ReportInput{Name: "点", Type: "other", Latitude: 30, Longitude: 120}
	mustSubmit(t, st, withDevice(base, 1))
	mustSubmit(t, st, withDevice(base, 1))
	if _, err := st.Submit(withDevice(base, 1)); err != ErrRateLimited {
		t.Fatalf("expected rate limit, got %v", err)
	}
	bad := withDevice(base, 2)
	bad.Name = "  "
	if _, err := st.Submit(bad); err == nil {
		t.Fatal("empty name should fail")
	}
	bad = withDevice(base, 2)
	bad.DeviceID = "nope"
	if _, err := st.Submit(bad); err == nil {
		t.Fatal("bad device id should fail")
	}
}

func TestDeleteLeavesTombstoneAndDoesNotImmediatelyRepublish(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 3, PromoteRadiusM: 200, RatePerHour: 30, RatePerDay: 50})
	base := ReportInput{Name: "赤石收费站", Type: "banned_toll", RoadCode: "G25", Latitude: 28.2, Longitude: 119.9}
	var published string
	for i := 1; i <= 3; i++ {
		res := mustSubmit(t, st, withDevice(base, i))
		published = res.PublicPointID
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := st.DeletePoint(published); err != nil {
		t.Fatal(err)
	}
	list, err := st.List(nil, nil, nil, nil, &before, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 0 || len(list.RemovedIDs) != 1 || list.RemovedIDs[0] != published {
		t.Fatalf("list %+v", list)
	}
	again := mustSubmit(t, st, withDevice(base, 9))
	if again.Status != "pending" {
		t.Fatalf("archived reports must not count, got %s", again.Status)
	}
}

func TestListReturnsNationwideSetAboveFormerCap(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 10, RatePerDay: 10})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	const total = 5001
	for i := 0; i < total; i++ {
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		_, err := tx.Exec(`INSERT INTO points (id, name, type, latitude, longitude, source_count, created_at, updated_at)
VALUES (?, ?, 'other', ?, 120, 1, ?, ?)`, fmt.Sprintf("p%05d", i), "点", 20+float64(i)*0.001, ts, ts)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	all, err := st.List(nil, nil, nil, nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Points) != total {
		t.Fatalf("omitted limit returned %d, want %d", len(all.Points), total)
	}
	page, err := st.List(nil, nil, nil, nil, nil, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Points) != 5000 {
		t.Fatalf("first page %d", len(page.Points))
	}
	cursor, err := time.Parse(time.RFC3339, page.Points[len(page.Points)-1].UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	next, err := st.List(nil, nil, nil, nil, &cursor, 5000)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, point := range page.Points {
		seen[point.ID] = true
	}
	extra := 0
	for _, point := range next.Points {
		if !seen[point.ID] {
			extra++
		}
	}
	if extra != 1 {
		t.Fatalf("since page added %d new ids, want 1 (got %d rows)", extra, len(next.Points))
	}
}

func openTest(t *testing.T, limits Limits) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "img"), limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func withDevice(in ReportInput, n int) ReportInput {
	in.DeviceID = deviceID(n)
	return in
}

func deviceID(n int) string {
	const hex = "0123456789abcdef"
	suffix := make([]byte, 12)
	for i := range suffix {
		suffix[i] = '0'
	}
	v := n
	for i := 11; i >= 0 && v > 0; i-- {
		suffix[i] = hex[v%16]
		v /= 16
	}
	return "00000000-0000-4000-8000-" + string(suffix)
}

func TestLowConfidenceStaysInReviewAndStakeMerges(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	low := ReportInput{
		Name: "G2011 K52", Type: "camera", RoadCode: "G2011", Latitude: 36.60141, Longitude: 120.10708,
		Confidence: "low", ForceReview: true, Stake: "K52+700",
	}
	got := mustSubmit(t, st, withDevice(low, 1))
	if got.Status != "pending" || got.PublicPointID != "" {
		t.Fatalf("low confidence must stay pending, %+v", got)
	}
	exact := ReportInput{
		Name: "G78 K1807", Type: "camera", RoadCode: "G78", Latitude: 24.892194, Longitude: 103.076795,
		Confidence: "exact", Stake: "K1807+910", HasEnd: true, EndLatitude: 24.933079, EndLongitude: 103.144746,
		Direction: "上行",
	}
	pub := mustSubmit(t, st, withDevice(exact, 2))
	if pub.Status != "approved" {
		t.Fatalf("exact should publish, %+v", pub)
	}
	list, err := st.List(nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 1 || list.Points[0].Stake != "K1807+910" || list.Points[0].EndLatitude == nil {
		t.Fatalf("segment %+v", list.Points)
	}
	near := exact
	near.Latitude += 0.001
	near.Name = "同一桩号"
	again := mustSubmit(t, st, withDevice(near, 3))
	if again.PublicPointID != pub.PublicPointID {
		t.Fatalf("stake within 300m should merge, %+v", again)
	}
	if err := st.EditReport(got.ID, ReportEdit{Confidence: strPtr("exact")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Approve(got.ID); err != nil {
		t.Fatal(err)
	}
}

func strPtr(s string) *string { return &s }

func mustSubmit(t *testing.T, st *Store, in ReportInput) SubmitResult {
	t.Helper()
	res, err := st.Submit(in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
