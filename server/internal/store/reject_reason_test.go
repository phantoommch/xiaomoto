package store

import "testing"

func TestRejectStoresReasonWithoutErasingEmpty(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	kept := mustSubmit(t, st, withDevice(ReportInput{
		Name: "保留原因", Type: "camera", Latitude: 30, Longitude: 120,
		Confidence: "low", ForceReview: true, ReviewReason: "原来的原因",
	}, 1))
	if err := st.Reject(kept.ID, ""); err != nil {
		t.Fatal(err)
	}
	changed := mustSubmit(t, st, withDevice(ReportInput{
		Name: "新原因", Type: "other", Latitude: 31, Longitude: 121,
		Confidence: "none", ForceReview: true, ReviewReason: "旧原因",
	}, 2))
	if err := st.Reject(changed.ID, "  坐标不对  "); err != nil {
		t.Fatal(err)
	}
	rows, err := st.AdminList("rejected", 20)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.ID] = row.ReviewReason
	}
	if got[kept.ID] != "原来的原因" || got[changed.ID] != "坐标不对" {
		t.Fatalf("reasons %#v", got)
	}
}
