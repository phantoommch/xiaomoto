package recog

import (
	"fmt"
	"strings"
	"testing"
)

// Sanqing is the wrapped 违法处理 ticket: the photo overlay says
// 地点:三清高速K15+350M曲靖方向 and the 违法地点 field breaks after “15”.
const sanqingFull = "地点:三清高速K15+350M曲靖方向 车道号:02\n" +
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

func TestSanqingParseKeepsBothStakes(t *testing.T) {
	body, ov := richerSources("三清高速（曲靖三宝－昆明清水）15", sanqingFull, "")
	if !strings.Contains(ov, "K15+350") {
		t.Fatalf("overlay %q", ov)
	}
	p := Parse(body)
	if p.Code != "S34" || p.NameCanon != "三清" || p.NameHow != "exact" {
		t.Fatalf("road %#v", p)
	}
	if !p.Range || len(p.Stakes) != 2 {
		t.Fatalf("stakes %#v range %v body %q", p.Stakes, p.Range, body)
	}
	if p.Stakes[0].KM != 15 || p.Stakes[0].Metres == nil || *p.Stakes[0].Metres != 350 {
		t.Fatalf("start %#v", p.Stakes[0])
	}
	if p.Stakes[1].KM != 9 || p.Stakes[1].Metres == nil || *p.Stakes[1].Metres != 810 {
		t.Fatalf("end %#v", p.Stakes[1])
	}
	if !strings.Contains(p.Direction, "西向北") {
		t.Fatalf("dir %q", p.Direction)
	}
	sig := ExtractSignals(sanqingFull, ov)
	if sig.Overlay == nil || sig.Overlay.Code != "" || sig.Overlay.KM != 15 || sig.Overlay.M != 350 {
		t.Fatalf("overlay loc %#v", sig.Overlay)
	}
	if !strings.Contains(sig.Overlay.Name, "三清") {
		t.Fatalf("overlay name %q", sig.Overlay.Name)
	}
	if !strings.Contains(sig.OverlayDir, "曲靖方向") {
		t.Fatalf("overlay dir %q", sig.OverlayDir)
	}
	// A middle dot is a section join even when the text between the piles is long
	// and contains no hyphen.
	dot := Parse("三清高速K15+350·沿三清高速往昆明清水行驶经过收费广场以外路段K9+810")
	if !dot.Range || len(dot.Stakes) != 2 || dot.Stakes[0].KM != 15 || dot.Stakes[1].KM != 9 {
		t.Fatalf("dot range %#v", dot)
	}
}

func TestSanqingLocateUsesKmMarkerNotVillage(t *testing.T) {
	m := &stakeMap{}
	r := Locate("三清高速（曲靖三宝－昆明清水）15", sanqingFull, "", m)
	if !r.HasPoint || !r.HasEnd {
		t.Fatalf("not a section method=%s how=%s conf=%s", r.Method, r.How, r.Conf)
	}
	if r.Road != "S34" || r.Level != "exact" || r.Method != "stake" {
		t.Fatalf("class method=%s level=%s road=%s conf=%s how=%s", r.Method, r.Level, r.Road, r.Conf, r.How)
	}
	if r.StakeKM != 15 || r.StakeM != 350 {
		t.Fatalf("stake %v +%d", r.StakeKM, r.StakeM)
	}
	if r.StakeLabel() != "K15+350-K9+810" {
		t.Fatalf("label %s", r.StakeLabel())
	}
	if !strings.Contains(r.Direction, "西向北") || !strings.Contains(r.Direction, "曲靖方向") {
		t.Fatalf("dir %q", r.Direction)
	}
	wantLat, wantLon := interpGCJ(25.380000, 103.560000, 25.373000, 103.553000, 0.35)
	if hav(r.Lat, r.Lon, wantLat, wantLon) > 5 {
		t.Fatalf("K15+350 %.6f,%.6f want %.6f,%.6f (%s)", r.Lat, r.Lon, wantLat, wantLon, r.How)
	}
	endLat, endLon := interpGCJ(25.430000, 103.620000, 25.423000, 103.613000, 0.81)
	if hav(r.EndLat, r.EndLon, endLat, endLon) > 5 {
		t.Fatalf("K9+810 %.6f,%.6f want %.6f,%.6f", r.EndLat, r.EndLon, endLat, endLon)
	}
	vlat, vlon := gcjToWGS(24.990710, 102.889100)
	if hav(r.Lat, r.Lon, vlat, vlon) < 50000 {
		t.Fatalf("pin is the Kunming Qingshui village %.6f,%.6f", r.Lat, r.Lon)
	}
	if len(m.addrs) == 0 {
		t.Fatal("no geocode")
	}
	for _, addr := range m.addrs {
		if !strings.HasPrefix(addr, "S34 ") || !strings.Contains(addr, "公里") {
			t.Fatalf("query %q is not an S34 kilometre marker", addr)
		}
	}
}

func TestSanqingOverlayAloneIsTheQujingStake(t *testing.T) {
	m := &stakeMap{}
	r := Locate("", "地点:三清高速K15+350M曲靖方向", "地点:三清高速K15+350M曲靖方向", m)
	if !r.HasPoint || r.HasEnd {
		t.Fatalf("overlay point method=%s end=%v how=%s", r.Method, r.HasEnd, r.How)
	}
	if r.Road != "S34" || r.StakeKM != 15 || r.StakeM != 350 || r.Level != "exact" {
		t.Fatalf("%s %s K%g+%d %s", r.Method, r.Road, r.StakeKM, r.StakeM, r.Level)
	}
	wantLat, wantLon := interpGCJ(25.380000, 103.560000, 25.373000, 103.553000, 0.35)
	if hav(r.Lat, r.Lon, wantLat, wantLon) > 5 {
		t.Fatalf("off %.6f,%.6f", r.Lat, r.Lon)
	}
	vlat, vlon := gcjToWGS(24.990710, 102.889100)
	if hav(r.Lat, r.Lon, vlat, vlon) < 50000 {
		t.Fatal("village pin")
	}
}

func TestTruncatedRoadDoesNotBecomeVillage(t *testing.T) {
	m := &stakeMap{}
	r := Locate("三清高速（曲靖三宝－昆明清水）15", "三清高速（曲靖三宝－昆明清水）15", "", m)
	if r.HasPoint {
		t.Fatalf("truncated road geocoded method=%s how=%s lat=%.5f", r.Method, r.How, r.Lat)
	}
	if r.Method != "stake-unresolved" {
		t.Fatalf("method %s how %s", r.Method, r.How)
	}
	for _, addr := range m.addrs {
		if !strings.HasPrefix(addr, "S34 ") {
			t.Fatalf("place geocode %q", addr)
		}
	}
}

func TestChromeIsNotAPlace(t *testing.T) {
	m := &stakeMap{}
	text := "违法处理\n返回\n加入处理清单\n处理并缴款\n申请陈述和申辩\n处理清单是空的"
	r := Locate(text, text, "", m)
	if r.HasPoint || r.Method != "invalid" {
		t.Fatalf("method %s how %s point %v", r.Method, r.How, r.HasPoint)
	}
	if len(m.addrs) != 0 {
		t.Fatalf("chrome was geocoded %#v", m.addrs)
	}
}

func TestCodedOverlayStillQueriesKmMarker(t *testing.T) {
	m := &stakeMap{}
	r := Locate("G201152公里700米", "《山东省高速公路交通安全条例》G201152公里700米", "地点:G2011青新K52+700 行驶方向:上行 设备编号:370211", m)
	if !r.HasPoint || r.Road != "G2011" || r.Method != "overlay-stake" || r.Level != "exact" {
		t.Fatalf("method %s road %s level %s how %s", r.Method, r.Road, r.Level, r.How)
	}
	if r.StakeKM != 52 || r.StakeM != 700 {
		t.Fatalf("stake %v +%d", r.StakeKM, r.StakeM)
	}
	saw := false
	for _, addr := range m.addrs {
		if addr == "G2011 52公里" {
			saw = true
		}
		if strings.Contains(addr, "青新") && !strings.Contains(addr, "公里") {
			t.Fatalf("place query %q", addr)
		}
	}
	if !saw {
		t.Fatalf("queries %#v", m.addrs)
	}
}

func interpGCJ(aLat, aLon, bLat, bLon, frac float64) (float64, float64) {
	alat, alon := gcjToWGS(aLat, aLon)
	blat, blon := gcjToWGS(bLat, bLon)
	return alat + frac*(blat-alat), alon + frac*(blon-alon)
}

// stakeMap answers AMap kilometre-marker geocodes. Any other address is the
// village hit that production used for this ticket, so a test can see it was
// not chosen.
type stakeMap struct {
	addrs []string
}

func (m *stakeMap) Cities() []string { return nil }

func (m *stakeMap) Get(path string, query map[string]string) (map[string]any, error) {
	if path != "v3/geocode/geo" {
		return map[string]any{}, nil
	}
	addr := query["address"]
	m.addrs = append(m.addrs, addr)
	code, km, ok := splitStakeQuery(addr)
	if !ok {
		return villageGeocode(), nil
	}
	lon, lat, kind, ok := stakeCoord(code, km)
	if !ok {
		return map[string]any{"geocodes": []any{}}, nil
	}
	return map[string]any{"geocodes": []any{map[string]any{
		"formatted_address": fmt.Sprintf("%s%d公里", kind, km),
		"province":          provinceOfCode(code),
		"location":          fmt.Sprintf("%.6f,%.6f", lon, lat),
		"level":             "兴趣点",
	}}}, nil
}

func splitStakeQuery(addr string) (string, int, bool) {
	var code string
	var km int
	if _, err := fmt.Sscanf(addr, "%s %d公里", &code, &km); err != nil {
		return "", 0, false
	}
	if !strings.HasSuffix(addr, "公里") {
		return "", 0, false
	}
	return code, km, true
}

func stakeCoord(code string, km int) (lon, lat float64, kind string, ok bool) {
	switch code {
	case "S34":
		switch km {
		case 15:
			return 103.560000, 25.380000, "云南省曲靖市马龙区S34三清高速", true
		case 16:
			return 103.553000, 25.373000, "云南省曲靖市马龙区S34三清高速", true
		case 9:
			return 103.620000, 25.430000, "云南省曲靖市马龙区S34三清高速", true
		case 10:
			return 103.613000, 25.423000, "云南省曲靖市马龙区S34三清高速", true
		default:
			return 0, 0, "", false
		}
	case "G2011":
		switch km {
		case 52:
			return 120.100000, 36.600000, "山东省青岛市城阳区G2011青新高速", true
		case 53:
			return 120.108000, 36.606000, "山东省青岛市城阳区G2011青新高速", true
		default:
			return 0, 0, "", false
		}
	default:
		return 0, 0, "", false
	}
}

func provinceOfCode(code string) string {
	if code == "G2011" {
		return "山东省"
	}
	return "云南省"
}

func villageGeocode() map[string]any {
	return map[string]any{"geocodes": []any{map[string]any{
		"formatted_address": "云南省昆明市呈贡区清水村",
		"province":          "云南省",
		"location":          "102.889100,24.990710",
		"level":             "村庄",
	}}}
}
