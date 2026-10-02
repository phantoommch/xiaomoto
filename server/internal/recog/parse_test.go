package recog

import "testing"

func metres(st []Stake) []int {
	out := make([]int, len(st))
	for i, s := range st {
		if s.Metres != nil {
			out[i] = *s.Metres
		} else {
			out[i] = -1
		}
	}
	return out
}

func TestHardParse(t *testing.T) {
	sms := "05:03\n904\n云南交警\n121235300\n短信/彩信\n今天\n云南交警\n您的普通摩托车 32于2026\n年9月22日13:58在汕昆高速1807\n公里910米-汕昆高速1798公里76\n0米，被交通技术监控设备记录了"
	cases := []struct {
		name      string
		text      string
		full      string
		overlay   string
		code      string
		canon     string
		how       string
		stakes    int
		km0       float64
		m0        int
		rangeOn   bool
		dir       string
		facility  string
		portal    string
		glued     int
		wantProv  string
		overlayKm float64
	}{
		{name: "T", text: "太湖隧道东入口,由东向西", facility: "太湖隧道", portal: "东入口", dir: "由东向西"},
		{name: "A", text: "汕昆高速1807公里910米-汕昆高速1798公里760米", code: "G78", canon: "汕昆", how: "exact", stakes: 2, km0: 1807, m0: 910, rangeOn: true},
		{name: "B", text: "G201152公里700米", full: "《山东省高速公路交通安全条例》G201152公里700米", glued: 3, wantProv: "山东"},
		{name: "D", text: "G15沈海高速1709公里910米（北向南", code: "G15", canon: "沈海", how: "exact", stakes: 1, km0: 1709, m0: 910, dir: "北向南"},
		{name: "E", text: "杭瑞高速K2501+600M至杭瑞高速K2514+500M大理方向", code: "G56", canon: "杭瑞", how: "exact", stakes: 2, km0: 2501, m0: 600, rangeOn: true, dir: "大理方向"},
		{name: "ocr", text: "G56抗瑞高速安徽方向73公里300米", code: "G56", canon: "杭瑞", how: "ocrfix", stakes: 1, km0: 73, m0: 300, dir: "安徽方向"},
		{name: "C", text: sms, code: "G78", canon: "汕昆", how: "suffix", stakes: 2, km0: 1807, m0: 910, rangeOn: true, wantProv: "云南"},
		{name: "list", text: "江南大道隧道东向西入口", facility: "江南大道隧道"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Parse(tc.text)
			if tc.code != "" && p.Code != tc.code {
				t.Fatalf("code %q want %q norm %q", p.Code, tc.code, p.Norm)
			}
			if tc.canon != "" && p.NameCanon != tc.canon {
				t.Fatalf("canon %q want %q how %q", p.NameCanon, tc.canon, p.NameHow)
			}
			if tc.how != "" && p.NameHow != tc.how {
				t.Fatalf("how %q want %q", p.NameHow, tc.how)
			}
			if len(p.Stakes) != tc.stakes {
				t.Fatalf("stakes %v want %d", metres(p.Stakes), tc.stakes)
			}
			if tc.stakes > 0 {
				if p.Stakes[0].KM != tc.km0 {
					t.Fatalf("km %v", p.Stakes[0].KM)
				}
				got := -1
				if p.Stakes[0].Metres != nil {
					got = *p.Stakes[0].Metres
				}
				if got != tc.m0 {
					t.Fatalf("metres %d want %d", got, tc.m0)
				}
			}
			if p.Range != tc.rangeOn {
				t.Fatalf("range %v", p.Range)
			}
			if tc.dir != "" && !contains(p.Direction, tc.dir) {
				t.Fatalf("dir %q", p.Direction)
			}
			if tc.facility != "" && p.Facility != tc.facility {
				t.Fatalf("facility %q portal %q", p.Facility, p.Portal)
			}
			if tc.portal != "" && p.Portal != tc.portal {
				t.Fatalf("portal %q", p.Portal)
			}
			if tc.glued != 0 && len(p.Glued) != tc.glued {
				t.Fatalf("glued %#v", p.Glued)
			}
			full := tc.full
			if full == "" {
				full = tc.text
			}
			sig := ExtractSignals(full, tc.overlay)
			if tc.wantProv != "" {
				found := false
				for _, h := range sig.Provinces {
					if h.Name == tc.wantProv {
						found = true
					}
				}
				if !found {
					t.Fatalf("provinces %#v", sig.Provinces)
				}
			}
			if tc.name == "B" && sig.Plate != "" {
				t.Fatalf("plate must not be required, got %s", sig.Plate)
			}
		})
	}
	b2 := ExtractSignals("《山东省高速公路交通安全条例》G201152公里700米", "地点:G2011青新K52+700 行驶方向:上行 设备编号:370100")
	if b2.Overlay == nil || b2.Overlay.Code != "G2011" || b2.Overlay.KM != 52 || b2.Overlay.M != 700 {
		t.Fatalf("overlay %#v", b2.Overlay)
	}
	found := false
	for _, h := range b2.Provinces {
		if h.Kind == "device" && h.Name == "山东" {
			found = true
		}
		if h.Kind == "plate" {
			t.Fatal("plate used as region")
		}
	}
	if !found {
		t.Fatalf("device %#v", b2.Provinces)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && (stringIndex(s, sub) >= 0)))
}

func stringIndex(s, sub string) int {
	return len([]rune(s[:])) - len([]rune(s[:])) + indexBytes(s, sub)
}

func indexBytes(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestPlateIgnored(t *testing.T) {
	sig := ExtractSignals("京A12345 在杭瑞高速73公里", "")
	if sig.Plate != "京" {
		t.Fatalf("plate recorded %q", sig.Plate)
	}
	for _, h := range sig.Provinces {
		if h.Name == "京" {
			t.Fatal("plate became a province")
		}
	}
}

func TestLevelOf(t *testing.T) {
	if LevelOf("stake", "high", "marker K73->K75 x0.30") != "exact" {
		t.Fatal("stake")
	}
	if LevelOf("glued-stake", "medium", "interp K51..K54") != "exact" {
		t.Fatal("glued interp")
	}
	if LevelOf("geocode", "low", "level=区县") != "low" {
		t.Fatal("county")
	}
	if LevelOf("facility-poi", "high", "青田收费站") != "exact" {
		t.Fatal("poi")
	}
	if LevelOf("facility-poi", "medium", "江南大道隧道") != "low" {
		t.Fatal("medium poi")
	}
	if LevelOf("stake", "medium", "marker K73 (metres dropped)") != "low" {
		t.Fatal("dropped metres")
	}
}
