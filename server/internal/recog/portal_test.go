package recog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPortalOnRoutePicksNamedTunnelEnd(t *testing.T) {
	steps := []routeStep{
		{road: "沪渝高速", instruction: "沿沪渝高速行驶", pts: [][2]float64{{120.100, 31.400}, {120.150, 31.410}}},
		{road: "太湖隧道", instruction: "进入太湖隧道", pts: [][2]float64{{120.196, 31.430}, {120.210, 31.425}}},
		{road: "太湖隧道", instruction: "沿太湖隧道行驶", pts: [][2]float64{{120.210, 31.425}, {120.230, 31.420}}},
		{road: "沪渝高速", instruction: "驶出隧道", pts: [][2]float64{{120.230, 31.420}, {120.400, 31.300}}},
	}
	lon, lat, confirmed, ok := portalOnRoute(steps, "太湖隧道", "东", nil)
	if !ok || confirmed || lon != 120.230 || lat != 31.420 {
		t.Fatalf("east portal got %v %v confirmed=%v ok=%v", lon, lat, confirmed, ok)
	}
	lon, lat, confirmed, ok = portalOnRoute(steps, "太湖隧道", "西", nil)
	if !ok || confirmed || lon != 120.196 || lat != 31.430 {
		t.Fatalf("west portal got %v %v confirmed=%v ok=%v", lon, lat, confirmed, ok)
	}
	if _, _, _, ok := portalOnRoute(steps[:1], "太湖隧道", "东", nil); ok {
		t.Fatal("expressway polyline must not be used as the tunnel")
	}
}

func TestPortalOnRouteIgnoresNameOvershoot(t *testing.T) {
	const eastRealLon, eastRealLat = 120.220966, 31.425402
	const eastWrongLon, eastWrongLat = 120.232943, 31.422501
	steps := []routeStep{
		{road: "太湖隧道", instruction: "进入太湖隧道", pts: [][2]float64{{120.196000, 31.436000}, {120.210000, 31.430000}}},
		{road: "太湖隧道", instruction: "沿太湖隧道行驶", pts: [][2]float64{{120.210000, 31.430000}, {eastRealLon, eastRealLat}}},
		{road: "太湖隧道", instruction: "驶出隧道", action: "驶出隧道", pts: [][2]float64{{eastRealLon, eastRealLat}, {eastWrongLon, eastWrongLat}}},
	}
	osm := [][2]float64{{eastRealLon, eastRealLat}}
	lon, lat, confirmed, ok := portalOnRoute(steps, "太湖隧道", "东", osm)
	if !ok || !confirmed {
		t.Fatalf("expected a confirmed portal, got %v %v %v", lon, lat, ok)
	}
	if hav(lat, lon, eastRealLat, eastRealLon) > portalAgreeM {
		t.Fatalf("portal %.6f,%.6f is not the bore mouth", lat, lon)
	}
	if hav(lat, lon, eastWrongLat, eastWrongLon) < 1000 {
		t.Fatalf("portal stayed on the name-span end %.6f,%.6f", lat, lon)
	}
	plain := []routeStep{{
		road: "太湖隧道", instruction: "沿太湖隧道行驶",
		pts: [][2]float64{{120.196000, 31.436000}, {eastRealLon, eastRealLat}, {eastWrongLon, eastWrongLat}},
	}}
	lon, lat, confirmed, ok = portalOnRoute(plain, "太湖隧道", "东", nil)
	if !ok || confirmed || hav(lat, lon, eastWrongLat, eastWrongLon) > 1 {
		t.Fatalf("name span alone must stay unconfirmed at the name end: %.6f %.6f %v", lat, lon, confirmed)
	}
}

func TestPortalOnRouteNorthEnd(t *testing.T) {
	steps := []routeStep{
		{road: "某隧道", instruction: "进入某隧道", pts: [][2]float64{{118.10, 30.00}, {118.11, 30.05}}},
		{road: "某隧道", pts: [][2]float64{{118.11, 30.05}, {118.12, 30.09}}},
	}
	lon, lat, _, ok := portalOnRoute(steps, "某隧道", "北", nil)
	if !ok || lat != 30.09 || lon != 118.12 {
		t.Fatalf("north portal %v %v %v", lon, lat, ok)
	}
	lon, lat, _, ok = portalOnRoute(steps, "某隧道", "南", nil)
	if !ok || lat != 30.00 || lon != 118.10 {
		t.Fatalf("south portal %v %v %v", lon, lat, ok)
	}
}

func TestEntranceSide(t *testing.T) {
	if entranceSide("东入口", "由西向东") != "东" {
		t.Fatal("explicit portal wins")
	}
	if entranceSide("", "由东向西") != "东" || entranceSide("", "北向南") != "北" {
		t.Fatal("direction text")
	}
	if entranceSide("西口", "") != "西" || entranceSide("", "上行") != "" {
		t.Fatal("side parse")
	}
}

func TestLocateTunnelUsesDrivingPolyline(t *testing.T) {
	saved := overpassServers
	overpassServers = []string{"http://127.0.0.1:1/overpass"}
	t.Cleanup(func() { overpassServers = saved })

	m := &scriptMap{}
	r := Locate("太湖隧道东入口,由东向西", "太湖隧道东入口,由东向西", "", m)
	if r.Method != "tunnel-portal" || r.Level != "low" || !strings.Contains(r.How, "未核对") {
		t.Fatalf("method %s level %s how %s", r.Method, r.Level, r.How)
	}
	wantLat, wantLon := gcjToWGS(31.420, 120.230)
	if hav(r.Lat, r.Lon, wantLat, wantLon) > 1 {
		t.Fatalf("got %.6f %.6f want %.6f %.6f (%s)", r.Lat, r.Lon, wantLat, wantLon, r.How)
	}
	if len(m.paths) == 0 || m.paths[0] != "v3/place/text" {
		t.Fatalf("calls %v", m.paths)
	}
	sawRoute := false
	for _, p := range m.paths {
		if p == "v3/direction/driving" {
			sawRoute = true
		}
	}
	if !sawRoute {
		t.Fatal("expected a driving route")
	}
}

type scriptMap struct {
	paths []string
}

func (m *scriptMap) Cities() []string { return nil }

func (m *scriptMap) Get(path string, _ map[string]string) (map[string]any, error) {
	m.paths = append(m.paths, path)
	switch path {
	case "v3/place/text":
		return map[string]any{"pois": []any{map[string]any{
			"name": "太湖隧道", "type": "交通设施服务;道路附属设施;隧道",
			"location": "120.210000,31.425000",
			"pname":    "江苏省", "cityname": "无锡市", "adname": "滨湖区",
		}}}, nil
	case "v3/direction/driving":
		return map[string]any{"route": map[string]any{"paths": []any{map[string]any{"steps": []any{
			map[string]any{"road": "沪渝高速", "instruction": "沿沪渝高速行驶", "polyline": "120.100000,31.400000;120.150000,31.410000"},
			map[string]any{"road": "太湖隧道", "instruction": "进入太湖隧道", "polyline": "120.196000,31.430000;120.210000,31.425000"},
			map[string]any{"road": "太湖隧道", "instruction": "沿太湖隧道行驶", "polyline": "120.210000,31.425000;120.230000,31.420000"},
			map[string]any{"road": "沪渝高速", "instruction": "驶出隧道", "polyline": "120.230000,31.420000;120.400000,31.300000"},
		}}}}}, nil
	default:
		return map[string]any{}, nil
	}
}

func TestLocateTunnelConfirmsWhenOSMAgrees(t *testing.T) {
	prev := testOSMEnds
	testOSMEnds = func(lat, lon float64) [][2]float64 {
		return [][2]float64{{120.230, 31.420}}
	}
	t.Cleanup(func() { testOSMEnds = prev })
	m := &scriptMap{}
	r := Locate("太湖隧道东入口,由东向西", "太湖隧道东入口,由东向西", "", m)
	if r.Method != "tunnel-portal" || r.Level != "exact" || !strings.Contains(r.How, "已核对") {
		t.Fatalf("method %s level %s how %s", r.Method, r.Level, r.How)
	}
	wantLat, wantLon := gcjToWGS(31.420, 120.230)
	if hav(r.Lat, r.Lon, wantLat, wantLon) > 1 {
		t.Fatalf("got %.6f %.6f", r.Lat, r.Lon)
	}
}

func TestGetRetriesTimeout(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(200 * time.Millisecond)
		}
		_, _ = w.Write([]byte(`{"status":"1","info":"OK"}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := &Client{Key: "test", Base: srv.URL + "/", Ctx: ctx, HTTP: &http.Client{Timeout: 40 * time.Millisecond}}
	out, err := c.Get("v3/geocode/geo", map[string]string{"address": "retry"})
	if err != nil || str(out["status"]) != "1" || calls.Load() < 2 {
		t.Fatalf("retry err=%v calls=%d out=%v", err, calls.Load(), out)
	}
}

func TestAMapCallTimeout(t *testing.T) {
	if (&Client{}).client().Timeout != AMapTimeout || AMapTimeout != 5*time.Second {
		t.Fatalf("amap timeout %s", AMapTimeout)
	}
	if OverpassTimeout != 8*time.Second {
		t.Fatalf("overpass timeout %s", OverpassTimeout)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"1"}`))
	}))
	defer srv.Close()
	c := &Client{Key: "test", Base: srv.URL + "/", HTTP: &http.Client{Timeout: 50 * time.Millisecond}}
	if _, err := c.Get("v3/geocode/geo", map[string]string{"address": "x"}); err == nil {
		t.Fatal("slow call should time out")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.HTTP = srv.Client()
	c.Ctx = ctx
	if _, err := c.Get("v3/geocode/geo", nil); err == nil {
		t.Fatal("cancelled context should not call")
	}
}
