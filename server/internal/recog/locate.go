package recog

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Result is one location attempt. Level is "exact" or "low" for publish vs review.
type Result struct {
	Lat       float64 `json:"latitude,omitempty"`
	Lon       float64 `json:"longitude,omitempty"`
	HasPoint  bool    `json:"has_point,omitempty"`
	EndLat    float64 `json:"end_latitude,omitempty"`
	EndLon    float64 `json:"end_longitude,omitempty"`
	HasEnd    bool    `json:"has_end,omitempty"`
	Method    string  `json:"method,omitempty"`
	How       string  `json:"how,omitempty"`
	Addr      string  `json:"addr,omitempty"`
	Prov      string  `json:"prov,omitempty"`
	Conf      string  `json:"conf,omitempty"`
	Road      string  `json:"road,omitempty"`
	RoadName  string  `json:"road_name,omitempty"`
	StakeKM   float64 `json:"stake_km,omitempty"`
	StakeM    int     `json:"stake_m,omitempty"`
	HasStake  bool    `json:"has_stake,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Parsed    Parsed  `json:"parsed"`
	Level     string  `json:"confidence"`
}

func (r Result) stakeLabel() string {
	if !r.HasStake {
		return ""
	}
	if r.StakeM > 0 || r.How != "" {
		return fmt.Sprintf("K%d+%d", int(r.StakeKM), r.StakeM)
	}
	return fmt.Sprintf("K%d", int(r.StakeKM))
}

// LevelOf maps the prototype confidence onto the two publish buckets.
// Exact is a km-marker interpolation (including a route between markers) or an exact POI / tunnel portal.
// Road-level and county-level geocodes, unresolved stakes, and ambiguous hits stay low.
func LevelOf(method, conf, how string) string {
	switch method {
	case "overlay-stake", "stake", "glued-stake":
		if conf == "low" || conf == "none" || conf == "" {
			return "low"
		}
		if strings.Contains(how, "metres dropped") {
			return "low"
		}
		return "exact"
	case "tunnel-portal":
		// A named-span end can sit a kilometre past the bore. Exact only after
		// the driving mouth and an OSM endpoint agree.
		if conf == "low" || conf == "none" || conf == "" || !strings.Contains(how, "已核对") {
			return "low"
		}
		return "exact"
	case "facility-poi":
		if conf == "high" {
			return "exact"
		}
		return "low"
	default:
		return "low"
	}
}

type marker struct {
	lat, lon float64
	addr     string
	prov     string
	clusters int
}

func (m marker) ambiguous() bool { return m.clusters > 1 }

type point struct {
	wgs             [2]float64
	how, addr, prov string
	conf            string
	ambiguous       bool
}

// Locate runs the prototype pipeline. amap may be nil; then only parsing is filled in.
func Locate(text, full, overlay string, amap Map) Result {
	p := Parse(text)
	sig := ExtractSignals(full, overlay)
	if sig.Overlay == nil && full != "" && full != text {
		if extra := ExtractSignals(full, full); extra.Overlay != nil {
			sig.Overlay = extra.Overlay
		}
		if sig.OverlayDir == "" {
			sig.OverlayDir = ExtractSignals(full, full).OverlayDir
		}
	}
	region := regionWords(p, sig, full, amap)
	res := Result{Parsed: p, Direction: p.Direction, RoadName: p.NameCanon}
	if res.RoadName == "" {
		res.RoadName = p.RoadName
	}
	if sig.OverlayDir != "" && !strings.Contains(res.Direction, sig.OverlayDir) {
		if res.Direction != "" {
			res.Direction += " "
		}
		res.Direction += sig.OverlayDir
	}
	finish := func(r Result) Result {
		if r.Road == "" {
			r.Road = p.Code
		}
		if r.RoadName == "" {
			r.RoadName = p.NameCanon
		}
		r.Level = LevelOf(r.Method, r.Conf, r.How)
		if !r.HasPoint {
			r.Level = "low"
		}
		return r
	}
	if amap == nil {
		res.Method = "none"
		res.Conf = "none"
		return finish(res)
	}
	if ol := sig.Overlay; ol != nil && ol.Code != "" {
		if sp := stakePoint(amap, ol.Code, ol.KM, &ol.M, region); sp != nil {
			res = applyPoint(res, *sp)
			res.Method = "overlay-stake"
			res.Road = ol.Code
			res.StakeKM, res.StakeM, res.HasStake = ol.KM, ol.M, true
			res.Conf = "high"
			return finish(res)
		}
	}
	if len(p.Glued) > 0 {
		type cand struct {
			score int
			code  string
			km    float64
			me    *int
			sp    point
		}
		located := locateGlued(amap, p.Glued)
		var cands []cand
		for i, g := range p.Glued {
			sp := located[i]
			if sp == nil {
				continue
			}
			score := 1
			blob := sp.prov + sp.addr
			if len(region) > 0 && containsAny(blob, region) {
				score += 5
			} else if len(region) > 0 {
				score -= 5
			}
			cands = append(cands, cand{score, g.Code, g.KM, g.Metres, *sp})
		}
		sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
		if len(cands) > 0 {
			top := cands[0]
			ambiguous := len(cands) > 1 && cands[1].score == top.score
			res = applyPoint(res, top.sp)
			res.Method = "glued-stake"
			res.Road = top.code
			me := 0
			if top.me != nil {
				me = *top.me
			}
			res.StakeKM, res.StakeM, res.HasStake = top.km, me, true
			if ambiguous || (len(region) > 0 && top.score < 6) {
				res.Conf = "low"
			}
			if len(region) == 0 && len(cands) > 1 {
				res.Conf = "low"
			}
			return finish(res)
		}
	}
	if p.Code != "" && len(p.Stakes) > 0 {
		km, me := p.Stakes[0].KM, p.Stakes[0].Metres
		sp := stakePoint(amap, p.Code, km, me, region)
		if sp == nil && len(region) > 0 {
			sp = stakePoint(amap, p.Code, km, me, nil)
		}
		if sp != nil {
			if (sp.ambiguous || strings.HasPrefix(p.Code, "S")) && len(region) == 0 {
				sp.conf = "low"
			}
			if len(region) > 0 && !containsAny(sp.prov+sp.addr, region) {
				sp.conf = "low"
			}
			res = applyPoint(res, *sp)
			res.Method = "stake"
			res.Road = p.Code
			metres := 0
			if me != nil {
				metres = *me
			}
			res.StakeKM, res.StakeM, res.HasStake = km, metres, true
			if p.Range && len(p.Stakes) > 1 {
				km2, me2 := p.Stakes[1].KM, p.Stakes[1].Metres
				if sp2 := stakePoint(amap, p.Code, km2, me2, region); sp2 != nil {
					res.EndLat, res.EndLon, res.HasEnd = sp2.wgs[0], sp2.wgs[1], true
				}
			}
			return finish(res)
		}
		if p.Facility == "" {
			res.Method = "stake-unresolved"
			res.Conf = "none"
			res.How = fmt.Sprintf("no AMap km marker for %s near K%d", p.Code, int(km))
			res.Road = p.Code
			metres := 0
			if me != nil {
				metres = *me
			}
			res.StakeKM, res.StakeM, res.HasStake = km, metres, true
			return finish(res)
		}
	}
	if p.Facility != "" {
		if b := poiFacility(amap, p.Facility, p.FacilityType, region, p.Code, p.NameCanon); b != nil {
			conf := "medium"
			if b.score >= 15 || (b.score >= 10 && len(region) > 0) {
				conf = "high"
			}
			if b.far {
				conf = "low"
			}
			res.Lat, res.Lon, res.HasPoint = b.lat, b.lon, true
			res.Method = "facility-poi"
			res.Addr = b.admin
			res.How = b.name
			res.Conf = conf
			res.Road = p.Code
			side := entranceSide(p.Portal, p.Direction)
			if p.FacilityType == "隧道" && side != "" {
				if tp := tunnelPortal(amap, p.Facility, b.gcjLon, b.gcjLat, side, p.Direction); tp != nil {
					res.Lat, res.Lon = tp.lat, tp.lon
					res.Method = "tunnel-portal"
					if tp.confirmed {
						res.How = fmt.Sprintf("%s %s端 (驾车路线已核对)", b.name, side)
						res.Conf = "high"
					} else {
						res.How = fmt.Sprintf("%s %s端 (驾车路线未核对)", b.name, side)
						res.Conf = "low"
					}
				}
			}
			return finish(res)
		}
	}
	d, err := amap.Get("v3/geocode/geo", map[string]string{"address": p.Norm})
	var g map[string]any
	if err == nil && d != nil {
		if rows, ok := d["geocodes"].([]any); ok && len(rows) > 0 {
			g, _ = rows[0].(map[string]any)
		}
	}
	if g != nil {
		lon, lat, ok := splitLoc(str(g["location"]))
		if ok {
			wlat, wlon := gcjToWGS(lat, lon)
			level := str(g["level"])
			addr := str(g["formatted_address"])
			conf := "medium"
			adminLevel := map[string]bool{"省": true, "市": true, "区县": true, "乡镇": true, "村庄": true, "道路": true, "开发区": true}
			if adminLevel[level] || p.Facility != "" || (len(region) > 0 && !containsAny(addr, region)) {
				conf = "low"
			}
			res.Lat, res.Lon, res.HasPoint = wlat, wlon, true
			res.Method = "geocode"
			res.Addr = addr
			res.How = "level=" + level
			res.Conf = conf
			return finish(res)
		}
	}
	res.Method = "none"
	res.Conf = "none"
	return finish(res)
}

func applyPoint(res Result, sp point) Result {
	res.Lat, res.Lon, res.HasPoint = sp.wgs[0], sp.wgs[1], true
	res.How, res.Addr, res.Prov, res.Conf = sp.how, sp.addr, sp.prov, sp.conf
	return res
}

func containsAny(blob string, words []string) bool {
	for _, w := range words {
		if w != "" && strings.Contains(blob, w) {
			return true
		}
	}
	return false
}

type geoHit struct {
	lat, lon float64
	addr     string
	prov     string
}

func hits(amap Map, code string, km int) []geoHit {
	d, err := amap.Get("v3/geocode/geo", map[string]string{"address": fmt.Sprintf("%s %d公里", code, km)})
	if err != nil || d == nil {
		return nil
	}
	rows, _ := d["geocodes"].([]any)
	re := regexp.MustCompile(fmt.Sprintf(`(?:^|[^\d])%d公里`, km))
	var out []geoHit
	for _, row := range rows {
		g, _ := row.(map[string]any)
		fa := str(g["formatted_address"])
		if !strings.Contains(fa, code) || !re.MatchString(fa) {
			continue
		}
		lon, lat, ok := splitLoc(str(g["location"]))
		if !ok {
			continue
		}
		wlat, wlon := gcjToWGS(lat, lon)
		out = append(out, geoHit{wlat, wlon, fa, str(g["province"])})
	}
	return out
}

func kmMarker(amap Map, code string, km int, region []string) *marker {
	raw := hits(amap, code, km)
	if len(raw) == 0 {
		return nil
	}
	var clusters [][]geoHit
	for _, h := range raw {
		placed := false
		for i := range clusters {
			if hav(clusters[i][0].lat, clusters[i][0].lon, h.lat, h.lon) < 20000 {
				clusters[i] = append(clusters[i], h)
				placed = true
				break
			}
		}
		if !placed {
			clusters = append(clusters, []geoHit{h})
		}
	}
	region = nonempty(region)
	if len(region) > 0 {
		var keep [][]geoHit
		for _, c := range clusters {
			if containsAny(c[0].addr+c[0].prov, region) {
				keep = append(keep, c)
			}
		}
		if len(keep) > 0 {
			clusters = keep
		} else {
			return nil
		}
	}
	c := clusters[0]
	var la, lo float64
	for _, h := range c {
		la += h.lat
		lo += h.lon
	}
	n := float64(len(c))
	return &marker{lat: la / n, lon: lo / n, addr: c[0].addr, prov: c[0].prov, clusters: len(clusters)}
}

func nonempty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func routeInterp(amap Map, aLat, aLon, bLat, bLon, frac, gapKM float64) (float64, float64, bool) {
	gaLat, gaLon := wgsToGCJ(aLat, aLon)
	gbLat, gbLon := wgsToGCJ(bLat, bLon)
	d, err := amap.Get("v3/direction/driving", map[string]string{
		"origin":      fmt.Sprintf("%.6f,%.6f", gaLon, gaLat),
		"destination": fmt.Sprintf("%.6f,%.6f", gbLon, gbLat),
		"strategy":    "2",
		"extensions":  "base",
	})
	if err != nil || d == nil {
		return 0, 0, false
	}
	route, _ := d["route"].(map[string]any)
	paths, _ := route["paths"].([]any)
	if len(paths) == 0 {
		return 0, 0, false
	}
	path, _ := paths[0].(map[string]any)
	dist, _ := strconv.ParseFloat(str(path["distance"]), 64)
	distKM := dist / 1000
	if gapKM <= 0 || !(distKM/gapKM > 0.8 && distKM/gapKM < 1.25) {
		return 0, 0, false
	}
	steps, _ := path["steps"].([]any)
	var pl [][2]float64
	for _, st := range steps {
		sm, _ := st.(map[string]any)
		for _, q := range strings.Split(str(sm["polyline"]), ";") {
			lon, lat, ok := splitLoc(q)
			if !ok {
				continue
			}
			if len(pl) == 0 || pl[len(pl)-1] != [2]float64{lon, lat} {
				pl = append(pl, [2]float64{lon, lat})
			}
		}
	}
	if len(pl) < 2 {
		return 0, 0, false
	}
	seg := make([]float64, len(pl)-1)
	total := 0.0
	for i := 0; i < len(pl)-1; i++ {
		seg[i] = hav(pl[i][1], pl[i][0], pl[i+1][1], pl[i+1][0])
		total += seg[i]
	}
	target := total * frac
	acc := 0.0
	for i, sl := range seg {
		if acc+sl >= target {
			u := 0.0
			if sl > 0 {
				u = (target - acc) / sl
			}
			glon := pl[i][0] + u*(pl[i+1][0]-pl[i][0])
			glat := pl[i][1] + u*(pl[i+1][1]-pl[i][1])
			wlat, wlon := gcjToWGS(glat, glon)
			return wlat, wlon, true
		}
		acc += sl
	}
	return 0, 0, false
}

func locateGlued(amap Map, glued []Glued) []*point {
	out := make([]*point, len(glued))
	if _, ok := amap.(*Client); !ok {
		for i, g := range glued {
			out[i] = stakePoint(amap, g.Code, g.KM, g.Metres, nil)
		}
		return out
	}
	var wg sync.WaitGroup
	for i, g := range glued {
		wg.Add(1)
		go func(i int, g Glued) {
			defer wg.Done()
			out[i] = stakePoint(amap, g.Code, g.KM, g.Metres, nil)
		}(i, g)
	}
	wg.Wait()
	return out
}

func prefetchKM(amap Map, code string, kms []int, region []string) {
	if _, ok := amap.(*Client); !ok {
		return
	}
	seen := map[int]bool{}
	var wg sync.WaitGroup
	for _, km := range kms {
		if km < 0 || seen[km] {
			continue
		}
		seen[km] = true
		wg.Add(1)
		go func(km int) {
			defer wg.Done()
			_ = kmMarker(amap, code, km, region)
		}(km)
	}
	wg.Wait()
}

func stakePoint(amap Map, code string, km float64, metres *int, region []string) *point {
	kmi := int(km)
	me := 0
	if metres != nil {
		me = *metres
	}
	frac := (km - float64(kmi)) + float64(me)/1000.0
	prefetchKM(amap, code, []int{kmi - 1, kmi, kmi + 1}, region)
	a := kmMarker(amap, code, kmi, region)
	if a == nil {
		prefetchKM(amap, code, []int{kmi - 2, kmi + 2, kmi - 3, kmi + 3, kmi - 5, kmi + 5, kmi - 8, kmi + 8, kmi - 12, kmi + 12}, region)
		steps := []int{1, 2, 3, 5, 8, 12}
		var lo, hi *struct {
			k int
			m marker
		}
		for _, k := range steps {
			if r := kmMarker(amap, code, kmi-k, region); r != nil {
				lo = &struct {
					k int
					m marker
				}{kmi - k, *r}
				break
			}
		}
		for _, k := range steps {
			if r := kmMarker(amap, code, kmi+k, region); r != nil {
				hi = &struct {
					k int
					m marker
				}{kmi + k, *r}
				break
			}
		}
		if lo != nil && hi != nil && (hi.k-lo.k) > 3 {
			f := (km + float64(me)/1000 - float64(lo.k)) / float64(hi.k-lo.k)
			if lat, lon, ok := routeInterp(amap, lo.m.lat, lo.m.lon, hi.m.lat, hi.m.lon, f, float64(hi.k-lo.k)); ok {
				return &point{[2]float64{lat, lon}, fmt.Sprintf("route-interp K%d..K%d", lo.k, hi.k), lo.m.addr, lo.m.prov, "medium", lo.m.ambiguous()}
			}
		}
		if lo != nil && hi != nil && hav(lo.m.lat, lo.m.lon, hi.m.lat, hi.m.lon) < 1500*float64(hi.k-lo.k) {
			f := (km + float64(me)/1000 - float64(lo.k)) / float64(hi.k-lo.k)
			lat := lo.m.lat + f*(hi.m.lat-lo.m.lat)
			lon := lo.m.lon + f*(hi.m.lon-lo.m.lon)
			return &point{[2]float64{lat, lon}, fmt.Sprintf("interp K%d..K%d", lo.k, hi.k), lo.m.addr, lo.m.prov, "medium", lo.m.ambiguous()}
		}
		return nil
	}
	if frac < 0.05 {
		return &point{[2]float64{a.lat, a.lon}, fmt.Sprintf("marker K%d", kmi), a.addr, a.prov, "high", a.ambiguous()}
	}
	for _, step := range []int{1, 2, 3} {
		if b := kmMarker(amap, code, kmi+step, region); b != nil && hav(a.lat, a.lon, b.lat, b.lon) < 1500*float64(step) {
			f := frac / float64(step)
			lat := a.lat + f*(b.lat-a.lat)
			lon := a.lon + f*(b.lon-a.lon)
			return &point{[2]float64{lat, lon}, fmt.Sprintf("marker K%d->K%d x%.2f", kmi, kmi+step, frac), a.addr, a.prov, "high", a.ambiguous()}
		}
		if c := kmMarker(amap, code, kmi-step, region); c != nil && hav(a.lat, a.lon, c.lat, c.lon) < 1500*float64(step) {
			f := frac / float64(step)
			lat := a.lat + f*(a.lat-c.lat)
			lon := a.lon + f*(a.lon-c.lon)
			return &point{[2]float64{lat, lon}, fmt.Sprintf("marker K%d extrap K%d x%.2f", kmi, kmi-step, frac), a.addr, a.prov, "high", a.ambiguous()}
		}
	}
	return &point{[2]float64{a.lat, a.lon}, fmt.Sprintf("marker K%d (metres dropped)", kmi), a.addr, a.prov, "medium", a.ambiguous()}
}

type poiHit struct {
	score          int
	name, admin    string
	lat, lon       float64
	gcjLon, gcjLat float64
	far            bool
}

var facSyn = map[string][]string{
	"服务区": {"服务区", "停车区"},
	"停车区": {"停车区", "服务区"},
	"站口":  {"收费站"},
	"收费站": {"收费站"},
}

func poiFacility(amap Map, name, ftype string, provs []string, code, roadname string) *poiHit {
	base := name
	if strings.HasSuffix(name, ftype) {
		base = strings.TrimSuffix(name, ftype)
	}
	base = regexp.MustCompile(`^.*?(?:段|高速|方向)`).ReplaceAllString(base, "")
	if base == "" {
		if strings.HasSuffix(name, ftype) {
			base = strings.TrimSuffix(name, ftype)
		} else {
			base = name
		}
	}
	types := facSyn[ftype]
	if types == nil {
		types = []string{ftype}
	}
	var names []string
	for _, t := range types {
		names = append(names, base+t)
	}
	var queries []string
	queries = append(queries, names...)
	if roadname != "" && len(names) > 0 {
		queries = append(queries, roadname+names[0])
	}
	regs := append([]string{}, nonempty(provs)...)
	sort.SliceStable(regs, func(i, j int) bool {
		return provOK[regs[i]] && !provOK[regs[j]]
	})
	// cities before provinces: the sort above puts provinces first because true < false is false...
	// Python: sorted(provs, key=lambda r: r in PROV) → False (cities) before True (provinces).
	sort.SliceStable(regs, func(i, j int) bool {
		return !provOK[regs[i]] && provOK[regs[j]]
	})
	type req struct{ kw, city string }
	var reqs []req
	limit := regs
	if len(limit) > 3 {
		limit = limit[:3]
	}
	for _, pv := range limit {
		for _, kw := range names {
			reqs = append(reqs, req{kw, pv})
		}
	}
	for _, kw := range queries {
		reqs = append(reqs, req{kw, ""})
	}
	var cands []poiHit
	for _, rq := range reqs {
		q := map[string]string{"keywords": rq.kw, "offset": "20", "extensions": "base"}
		if rq.city != "" {
			q["city"] = rq.city
			q["citylimit"] = "true"
		}
		d, err := amap.Get("v3/place/text", q)
		if err != nil || d == nil {
			continue
		}
		pois, _ := d["pois"].([]any)
		for _, row := range pois {
			p, _ := row.(map[string]any)
			nm := str(p["name"])
			tp := str(p["type"])
			core := regexp.MustCompile(`[（(].*?[)）]`).ReplaceAllString(nm, "")
			okName := false
			for _, n := range names {
				if core == n || strings.HasPrefix(core, n) || (strings.HasSuffix(core, n) && len([]rune(core))-len([]rune(n)) <= 3) {
					okName = true
					break
				}
			}
			if !okName {
				continue
			}
			if ftype == "隧道" && !strings.Contains(tp+nm, "隧道") {
				continue
			}
			sc := 5
			for _, n := range names {
				if core == n {
					sc = 10
					break
				}
			}
			addr := str(p["address"])
			if code != "" && strings.Contains(nm+addr, code) {
				sc += 5
			}
			if roadname != "" && len([]rune(roadname)) >= 2 && strings.Contains(nm, string([]rune(roadname)[:2])) {
				sc += 3
			}
			lon, lat, ok := splitLoc(str(p["location"]))
			if !ok {
				continue
			}
			wlat, wlon := gcjToWGS(lat, lon)
			admin := str(p["pname"]) + str(p["cityname"]) + str(p["adname"])
			cands = append(cands, poiHit{sc, nm, admin, wlat, wlon, lon, lat, false})
		}
		best := 0
		for _, c := range cands {
			if c.score > best {
				best = c.score
			}
		}
		if best >= 15 {
			break
		}
	}
	if len(cands) == 0 {
		return nil
	}
	if len(nonempty(provs)) > 0 {
		var in []poiHit
		for _, c := range cands {
			if containsAny(c.admin, provs) {
				c.score += 5
				in = append(in, c)
			}
		}
		if len(in) == 0 {
			return nil
		}
		cands = in
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	top := cands[0]
	for _, c := range cands {
		if hav(c.lat, c.lon, top.lat, top.lon) > 30000 && c.score >= top.score-5 {
			top.far = true
			break
		}
	}
	return &top
}

// routeStep is one driving-route step. pts are GCJ-02 (lon, lat).
type routeStep struct {
	road, instruction, action, assistant string
	pts                                  [][2]float64
}

type portalFix struct {
	lat, lon  float64
	confirmed bool
}

// portalAgreeM is how close two portal checks must be before a tunnel is exact.
const portalAgreeM = 150.0

func tunnelPortal(amap Map, name string, centerLon, centerLat float64, side, travel string) *portalFix {
	wlat, wlon := gcjToWGS(centerLat, centerLon)
	ctx := mapContext(amap)
	osmCh := make(chan [][2]float64, 1)
	go func() {
		osmCh <- osmEndsGCJ(ctx, wlat, wlon, name)
	}()
	var osm [][2]float64
	var once sync.Once
	takeOSM := func() [][2]float64 {
		once.Do(func() { osm = <-osmCh })
		return osm
	}
	for _, pair := range crossingPairs(centerLon, centerLat, side) {
		d, err := amap.Get("v3/direction/driving", map[string]string{
			"origin":      fmt.Sprintf("%.6f,%.6f", pair[0][0], pair[0][1]),
			"destination": fmt.Sprintf("%.6f,%.6f", pair[1][0], pair[1][1]),
			"waypoints":   fmt.Sprintf("%.6f,%.6f", centerLon, centerLat),
			"strategy":    "2",
			"extensions":  "all",
		})
		if err != nil || d == nil {
			continue
		}
		lon, lat, confirmed, ok := portalOnRoute(parseDrivingSteps(d), name, side, takeOSM())
		if !ok {
			continue
		}
		outLat, outLon := gcjToWGS(lat, lon)
		return &portalFix{lat: outLat, lon: outLon, confirmed: confirmed}
	}
	_ = takeOSM()
	if tp := osmTunnelPortal(ctx, wlat, wlon, side, travel, name); tp != nil {
		return &portalFix{lat: tp.lat, lon: tp.lon, confirmed: false}
	}
	return nil
}

// crossingPairs places the origin and destination on opposite sides of the
// tunnel POI so the driving route has to cross it. The portal axis comes first.
func crossingPairs(lon, lat float64, side string) [][2][2]float64 {
	const d = 0.045
	horiz := [][2][2]float64{
		{{lon - d, lat}, {lon + d, lat}},
		{{lon + d, lat}, {lon - d, lat}},
	}
	vert := [][2][2]float64{
		{{lon, lat - d}, {lon, lat + d}},
		{{lon, lat + d}, {lon, lat - d}},
	}
	if side == "北" || side == "南" {
		return append(vert, horiz...)
	}
	return append(horiz, vert...)
}

func parseDrivingSteps(d map[string]any) []routeStep {
	route, _ := d["route"].(map[string]any)
	paths, _ := route["paths"].([]any)
	if len(paths) == 0 {
		return nil
	}
	path, _ := paths[0].(map[string]any)
	raw, _ := path["steps"].([]any)
	var steps []routeStep
	for _, st := range raw {
		sm, _ := st.(map[string]any)
		var pts [][2]float64
		for _, q := range strings.Split(str(sm["polyline"]), ";") {
			lon, lat, ok := splitLoc(q)
			if ok {
				pts = append(pts, [2]float64{lon, lat})
			}
		}
		steps = append(steps, routeStep{
			road: str(sm["road"]), instruction: str(sm["instruction"]),
			action: str(sm["action"]), assistant: str(sm["assistant_action"]), pts: pts,
		})
	}
	return steps
}

// portalOnRoute returns the portal on the requested side. The road name can
// continue past the bore, so a 进入/驶出/通过隧道 mouth, or an OSM endpoint
// within 150 m of the polyline, wins over the name-span extremity. confirmed
// is set only when that mouth and the OSM endpoint agree within 150 m.
// osmGCJ holds GCJ-02 (lon, lat) way endpoints.
func portalOnRoute(steps []routeStep, facility, side string, osmGCJ [][2]float64) (lon, lat float64, confirmed, ok bool) {
	span := tunnelPolyline(steps, facility)
	if len(span) < 2 || side == "" {
		return 0, 0, false, false
	}
	mouth, hasMouth := mouthForSide(actionMouths(steps, facility), span, side)
	osm, hasOSM := osmForSide(osmGCJ, span, side)
	switch {
	case hasMouth && hasOSM && hav(mouth[1], mouth[0], osm[1], osm[0]) <= portalAgreeM:
		return mouth[0], mouth[1], true, true
	case hasOSM:
		// The driving polyline passes the OSM portal. That is the check.
		// A bore mouth farther than 150 m does not pull the point back to the
		// name-span end, and it does not count as agreement.
		agreed := !hasMouth || hav(mouth[1], mouth[0], osm[1], osm[0]) <= portalAgreeM
		return osm[0], osm[1], agreed, true
	case hasMouth:
		return mouth[0], mouth[1], false, true
	default:
		end, endOK := endForSide(span, side)
		if !endOK {
			return 0, 0, false, false
		}
		return end[0], end[1], false, true
	}
}

func actionMouths(steps []routeStep, facility string) [][2]float64 {
	var mouths [][2]float64
	for i, st := range steps {
		blob := st.action + st.assistant + st.instruction
		if !boreAction(blob) || len(st.pts) == 0 {
			continue
		}
		related := sameTunnel(st.road, blob, facility)
		if !related && i > 0 {
			prev := steps[i-1]
			related = sameTunnel(prev.road, prev.instruction+prev.action, facility)
		}
		if !related && i+1 < len(steps) && strings.Contains(blob, "进入隧道") {
			next := steps[i+1]
			related = sameTunnel(next.road, next.instruction, facility)
		}
		if related {
			// An approach step ends at the bore. A step already named for the
			// tunnel starts at the bore. 驶出/通过 starts at the bore.
			at := st.pts[0]
			onTunnel := sameTunnel(st.road, st.road, facility)
			if strings.Contains(blob, "进入隧道") && !strings.Contains(blob, "驶出隧道") && !onTunnel {
				at = st.pts[len(st.pts)-1]
			}
			mouths = append(mouths, at)
		}
	}
	return mouths
}

func boreAction(s string) bool {
	return strings.Contains(s, "进入隧道") || strings.Contains(s, "驶出隧道") || strings.Contains(s, "通过隧道")
}

func endForSide(span [][2]float64, side string) ([2]float64, bool) {
	if len(span) == 0 {
		return [2]float64{}, false
	}
	a, b := span[0], span[len(span)-1]
	if sideScore(side, b[0], b[1]) > sideScore(side, a[0], a[1]) {
		return b, true
	}
	return a, true
}

// mouthForSide keeps a bore mouth only when it sits closer to the requested
// name-span end than to the opposite end. The returned point is the mouth,
// not that name-span end.
func mouthForSide(mouths, span [][2]float64, side string) ([2]float64, bool) {
	if len(mouths) == 0 || len(span) < 2 {
		return [2]float64{}, false
	}
	want, _ := endForSide(span, side)
	other := span[0]
	if other == want {
		other = span[len(span)-1]
	}
	best := mouths[0]
	bestD := math.MaxFloat64
	found := false
	for _, m := range mouths {
		dWant := hav(m[1], m[0], want[1], want[0])
		dOther := hav(m[1], m[0], other[1], other[0])
		if dWant <= dOther && dWant < bestD {
			best = m
			bestD = dWant
			found = true
		}
	}
	return best, found
}

func osmForSide(osmGCJ, span [][2]float64, side string) ([2]float64, bool) {
	if len(osmGCJ) == 0 || len(span) < 2 {
		return [2]float64{}, false
	}
	var near [][2]float64
	for _, e := range osmGCJ {
		if nearPolyline(e[0], e[1], span, portalAgreeM) {
			near = append(near, e)
		}
	}
	return mouthForSide(near, span, side)
}

func nearPolyline(lon, lat float64, pts [][2]float64, meters float64) bool {
	for i, p := range pts {
		if hav(lat, lon, p[1], p[0]) <= meters {
			return true
		}
		if i+1 >= len(pts) {
			continue
		}
		q := pts[i+1]
		for s := 1; s < 8; s++ {
			u := float64(s) / 8
			if hav(lat, lon, p[1]+u*(q[1]-p[1]), p[0]+u*(q[0]-p[0])) <= meters {
				return true
			}
		}
	}
	return false
}

func tunnelPolyline(steps []routeStep, facility string) [][2]float64 {
	var pts [][2]float64
	collecting := false
	for _, st := range steps {
		if sameTunnel(st.road, st.instruction, facility) && len(st.pts) > 0 {
			collecting = true
			pts = append(pts, st.pts...)
			continue
		}
		if collecting {
			break
		}
	}
	return pts
}

func sameTunnel(road, instruction, facility string) bool {
	core := tunnelCore(facility)
	if len([]rune(core)) < 3 {
		return false
	}
	return strings.Contains(road+instruction, core)
}

func tunnelCore(name string) string {
	name = strings.TrimSpace(name)
	for _, suf := range []string{"东入口", "西入口", "南入口", "北入口", "东出口", "西出口", "南出口", "北出口", "入口", "出口", "洞口"} {
		name = strings.TrimSuffix(name, suf)
	}
	return name
}

// entranceSide is the portal compass point. An explicit 东入口 wins. Otherwise
// 由东向西 / 东向西 means the rider enters from the east.
func entranceSide(portal, direction string) string {
	if portal != "" {
		r := []rune(portal)
		if strings.ContainsRune("东西南北", r[0]) {
			return string(r[0])
		}
	}
	if m := regexp.MustCompile(`由([东南西北])向`).FindStringSubmatch(direction); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`([东南西北])向[东南西北]`).FindStringSubmatch(direction); m != nil {
		return m[1]
	}
	return ""
}

func sideScore(side string, lon, lat float64) float64 {
	switch side {
	case "东":
		return lon
	case "西":
		return -lon
	case "北":
		return lat
	case "南":
		return -lat
	default:
		return 0
	}
}

func regionWords(p Parsed, sig Signals, full string, amap Map) []string {
	var w []string
	for _, h := range sig.Provinces {
		w = append(w, h.Name)
	}
	if amap != nil && (strings.Contains(full, "市") || strings.Contains(p.Norm, "市")) {
		for _, c := range amap.Cities() {
			if strings.Contains(full, c) || strings.Contains(p.Norm, c) {
				w = append(w, c)
			}
		}
	}
	rePiece := regexp.MustCompile(`[\p{Han}]+?(?:省|自治区|自治州|市|地区|县|区)`)
	reCore := regexp.MustCompile(`(省|自治区|自治州|市|地区|县|区)$`)
	for _, a := range p.Admin {
		for _, piece := range rePiece.FindAllString(a, -1) {
			core := reCore.ReplaceAllString(piece, "")
			if len([]rune(core)) >= 2 {
				w = append(w, core)
			}
		}
	}
	return unique(w)
}

func provinceOf(p Parsed, sig Signals) []string {
	var pv []string
	for _, h := range sig.Provinces {
		pv = append(pv, h.Name)
	}
	for _, a := range p.Admin {
		for _, P := range provinces {
			if strings.HasPrefix(a, P) {
				pv = append(pv, P)
			}
		}
	}
	return unique(pv)
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func splitLoc(s string) (lon, lat float64, ok bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	lon, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lat, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
		return 0, 0, false
	}
	return lon, lat, true
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}
