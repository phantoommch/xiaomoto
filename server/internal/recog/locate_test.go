package recog

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	if k := strings.TrimSpace(os.Getenv("AMAP_KEY")); k != "" {
		return k
	}
	f, err := os.Open("/workspace/android/secrets.properties")
	if err != nil {
		t.Skip("no AMAP key")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "AMAP_WEB_KEY=") {
			return strings.Trim(strings.TrimPrefix(line, "AMAP_WEB_KEY="), `"`)
		}
	}
	t.Skip("no AMAP key")
	return ""
}

func TestHardLocate(t *testing.T) {
	key := testKey(t)
	amap := &Client{Key: key}
	sms := "云南交警\n121235300\n您的普通摩托车于2026年9月22日13:58在汕昆高速1807\n公里910米-汕昆高速1798公里76\n0米"
	type exp struct {
		name    string
		text    string
		full    string
		overlay string
		lat     float64
		lon     float64
		endLat  float64
		endLon  float64
		hasEnd  bool
		road    string
		level   string
	}
	cases := []exp{
		{"T", "太湖隧道东入口,由东向西", "太湖隧道东入口,由东向西", "", 31.425402, 120.220966, 0, 0, false, "", "exact"},
		{"A", "汕昆高速1807公里910米-汕昆高速1798公里760米", "", "", 24.892194, 103.076795, 24.933079, 103.144746, true, "G78", "exact"},
		{"B", "G201152公里700米", "《山东省高速公路交通安全条例》\nG201152公里700米", "", 36.60141, 120.107082, 0, 0, false, "G2011", "exact"},
		{"B2", "G201152公里700米", "《山东省高速公路交通安全条例》", "地点:G2011青新K52+700 行驶方向:上行 设备编号:370211", 36.60141, 120.107082, 0, 0, false, "G2011", "exact"},
		{"C", sms, sms, "", 24.892194, 103.076795, 24.933079, 103.144746, true, "G78", "exact"},
		{"D", "G15沈海高速1709公里910米（北向南", "", "", 28.160801, 121.039371, 0, 0, false, "G15", "exact"},
		{"E", "杭瑞高速K2501+600M至杭瑞高速K2514+500M大理方向", "设备编号:532901 杭瑞高速K2501+600M至杭瑞高速K2514+500M大理方向", "", 25.421352, 100.633538, 25.501428, 100.549883, true, "G56", "exact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := tc.full
			if full == "" {
				full = tc.text
			}
			r := Locate(tc.text, full, tc.overlay, amap)
			if !r.HasPoint {
				t.Fatalf("no point method=%s how=%s conf=%s", r.Method, r.How, r.Conf)
			}
			d := hav(r.Lat, r.Lon, tc.lat, tc.lon)
			t.Logf("method=%s how=%s level=%s dist=%.0fm lat=%.6f lon=%.6f road=%s", r.Method, r.How, r.Level, d, r.Lat, r.Lon, r.Road)
			if tc.name == "T" {
				if d > 150 {
					t.Fatalf("tunnel off by %.0f m", d)
				}
				if r.Level == "exact" && !strings.Contains(r.How, "已核对") {
					t.Fatal("unconfirmed tunnel marked exact")
				}
				if strings.Contains(r.How, "已核对") && r.Level != "exact" {
					t.Fatalf("confirmed tunnel level %s", r.Level)
				}
				return
			}
			if d > 300 {
				t.Fatalf("off by %.0f m", d)
			}
			if tc.road != "" && r.Road != tc.road {
				t.Fatalf("road %s", r.Road)
			}
			if r.Level != tc.level {
				t.Fatalf("level %s conf %s", r.Level, r.Conf)
			}
			if tc.hasEnd {
				if !r.HasEnd {
					t.Fatal("missing range end")
				}
				de := hav(r.EndLat, r.EndLon, tc.endLat, tc.endLon)
				t.Logf("end dist=%.0fm", de)
				if de > 300 {
					t.Fatalf("end off by %.0f m", de)
				}
			}
		})
	}
}
