package store

import "testing"

func TestMakeSegmentKeepsEndPoint(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	start := mustSubmit(t, st, withDevice(ReportInput{
		Name: "汕昆起点", Type: "camera", RoadCode: "G78", Confidence: "exact",
		Latitude: 24.892194, Longitude: 103.076795,
	}, 1))
	end := mustSubmit(t, st, withDevice(ReportInput{
		Name: "汕昆终点", Type: "camera", RoadCode: "G78", Confidence: "exact",
		Latitude: 24.933079, Longitude: 103.144746,
	}, 2))
	if start.Status != "approved" || end.Status != "approved" {
		t.Fatalf("seed %s %s", start.Status, end.Status)
	}
	err := st.MakeSegment(start.PublicPointID, SegmentInput{
		EndPointID: end.PublicPointID, Stake: "K1807+910", Direction: "上行",
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.List(nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Points) != 2 {
		t.Fatalf("end point should stay published, got %d", len(list.Points))
	}
	var seg PublicPoint
	for _, p := range list.Points {
		if p.ID == start.PublicPointID {
			seg = p
		}
	}
	if seg.EndLatitude == nil || seg.EndLongitude == nil || seg.Stake != "K1807+910" || seg.Direction != "上行" {
		t.Fatalf("segment %+v", seg)
	}
	if *seg.EndLatitude < 24.93 || *seg.EndLongitude < 103.14 {
		t.Fatalf("end coords %+v %+v", seg.EndLatitude, seg.EndLongitude)
	}
	if seg.Latitude != 24.892194 {
		t.Fatalf("start moved to %v", seg.Latitude)
	}

	err = st.MakeSegment(start.PublicPointID, SegmentInput{
		HasCoord: true, EndLat: 25, EndLon: 104, Direction: "北向南",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MakeSegment(start.PublicPointID, SegmentInput{}); err == nil {
		t.Fatal("missing end should fail")
	}
	if err := st.MakeSegment(start.PublicPointID, SegmentInput{EndPointID: start.PublicPointID}); err == nil {
		t.Fatal("same point should fail")
	}
	if err := st.MakeSegment("missing", SegmentInput{HasCoord: true, EndLat: 1, EndLon: 2}); err != ErrNotFound {
		t.Fatalf("missing start %v", err)
	}
}

func TestApplyRelocationPublishesAndSkipsClosed(t *testing.T) {
	st := openTest(t, Limits{PromoteMin: 1, PromoteRadiusM: 200, RatePerHour: 20, RatePerDay: 50})
	low := mustSubmit(t, st, withDevice(ReportInput{
		Name: "未定位违法点", Type: "camera", Confidence: "low", ForceReview: true,
		Latitude: 30, Longitude: 120, OcrText: "G56 73公里",
	}, 1))
	got, err := st.ApplyRelocation(low.ID, ReportInput{
		Name: "杭瑞高速", Type: "camera", RoadCode: "G56", Confidence: "exact",
		Latitude: 30.184227, Longitude: 119.325654, Stake: "K73+300",
	})
	if err != nil || got.Status != "approved" {
		t.Fatalf("apply %+v %v", got, err)
	}
	rejected := mustSubmit(t, st, withDevice(ReportInput{
		Name: "另一条", Type: "camera", Confidence: "low", ForceReview: true,
		Latitude: 31, Longitude: 121,
	}, 2))
	if err := st.Reject(rejected.ID); err != nil {
		t.Fatal(err)
	}
	again, err := st.ApplyRelocation(rejected.ID, ReportInput{
		Name: "另一条", Type: "camera", Confidence: "exact", Latitude: 31.1, Longitude: 121.1,
	})
	if err != nil || again.Status != "rejected" || again.PublicPointID != "" {
		t.Fatalf("closed report changed %+v %v", again, err)
	}
}
