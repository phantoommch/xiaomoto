package recog

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Stake struct {
	KM     float64
	Metres *int
}

type Glued struct {
	Code   string
	KM     float64
	Metres *int
}

type Parsed struct {
	Norm           string   `json:"norm"`
	Code           string   `json:"code,omitempty"`
	RoadName       string   `json:"road_name,omitempty"`
	NameCode       string   `json:"name_code,omitempty"`
	NameCanon      string   `json:"name_canon,omitempty"`
	NameHow        string   `json:"name_how,omitempty"`
	Stakes         []Stake  `json:"stakes,omitempty"`
	Glued          []Glued  `json:"glued,omitempty"`
	Range          bool     `json:"range,omitempty"`
	RangeTruncated bool     `json:"range_truncated,omitempty"`
	Direction      string   `json:"direction,omitempty"`
	Facility       string   `json:"facility,omitempty"`
	FacilityType   string   `json:"facility_type,omitempty"`
	Portal         string   `json:"portal,omitempty"`
	Admin          []string `json:"admin,omitempty"`
}

type ProvHit struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type OverlayLoc struct {
	Code string  `json:"code,omitempty"`
	Name string  `json:"name,omitempty"`
	KM   float64 `json:"km"`
	M    int     `json:"m"`
}

type Signals struct {
	Provinces  []ProvHit   `json:"provinces,omitempty"`
	Plate      string      `json:"plate,omitempty"`
	Overlay    *OverlayLoc `json:"overlay_loc,omitempty"`
	OverlayDir string      `json:"overlay_dir,omitempty"`
}

func Normalize(t string) string {
	t = foldWidth(t)
	t = strings.ReplaceAll(t, "\r", "")
	t = joinBroken(t)
	t = regexp.MustCompile(`\s*\n\s*`).ReplaceAllString(t, " ")
	t = collapseHanSpaces(t)
	repl := [][2]string{
		{"高遠", "高速"}, {"高远", "高速"}, {"高逮", "高速"}, {"高建", "高速"}, {"公理", "公里"},
		{"千來", "千米"}, {"kM", "KM"}, {"Km", "KM"}, {"km", "KM"},
	}
	for _, pair := range repl {
		t = strings.ReplaceAll(t, pair[0], pair[1])
	}
	// lone k before a stake, after the longer KM replacements
	t = replaceLoneK(t)
	t = fixOO(t)
	return strings.TrimSpace(t)
}

func foldWidth(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		case r == 0x3000:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func joinBroken(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); {
		r := runes[i]
		if isJoinLeft(r) {
			j := i + 1
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t' || runes[j] == '\n') {
				j++
			}
			if j > i+1 && j < len(runes) && isJoinRight(runes[j], r) {
				b.WriteRune(r)
				i = j
				continue
			}
		}
		b.WriteRune(r)
		i++
	}
	return b.String()
}

func isJoinLeft(r rune) bool {
	return (r >= '0' && r <= '9') || r == 'K' || r == '+' || r == '公' || r == '千' || r == '米'
}

func isJoinRight(next, prev rune) bool {
	digitOrPlus := (next >= '0' && next <= '9') || next == '+'
	unit := next == '公' || next == '千' || next == '米' || next == 'K' || next == 'M'
	if (prev >= '0' && prev <= '9') || prev == 'K' || prev == '+' {
		return digitOrPlus || unit
	}
	if prev == '公' || prev == '千' || prev == '米' || prev == '+' {
		return next >= '0' && next <= '9'
	}
	return false
}

func collapseHanSpaces(s string) string {
	re := regexp.MustCompile(`([\p{Han}])[ \t]+([\p{Han}])`)
	for {
		next := re.ReplaceAllString(s, "$1$2")
		if next == s {
			return s
		}
		s = next
	}
}

func replaceLoneK(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		if (runes[i] == 'k') && (i+1 >= len(runes) || (runes[i+1] != 'm' && runes[i+1] != 'M')) {
			b.WriteRune('K')
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

func fixOO(s string) string {
	runes := []rune(s)
	for i := 1; i+1 < len(runes); i++ {
		if runes[i] != 'O' && runes[i] != 'o' {
			continue
		}
		prev, next := runes[i-1], runes[i+1]
		if (prev >= '0' && prev <= '9' && next >= '0' && next <= '9') ||
			((prev == 'G' || prev == 'S') && next >= '0' && next <= '9') {
			runes[i] = '0'
		}
	}
	return string(runes)
}

var (
	reStakeKPlus = regexp.MustCompile(`K(\d{1,4})\s*\+\s*(\d{1,4})\s*(?:M|米)?`)
	reStakeKM    = regexp.MustCompile(`(\d{1,4}(?:\.\d+)?)\s*(?:公里|千米|KM)\s*\+?\s*(\d{1,4})?\s*(?:米|M)?`)
	reStakeKOnly = regexp.MustCompile(`K(\d{1,4})`)
	reRoadName   = regexp.MustCompile(`([\p{Han}]{2,6}?)(?:高速公路|高速|快速路)`)
	reAdminStrip = regexp.MustCompile(`^.*(?:省|市|县|区|州|盟|旗)`)
	reGlued      = regexp.MustCompile(`([GS])(\d{3,8})\s*(?:公里|千米|KM)\s*(?:(\d{1,3})\s*(?:米|M)?)?`)
	reCode       = regexp.MustCompile(`([GS]\d{1,4})`)
	reRangeSep   = regexp.MustCompile(`\s*(?:-|—|–|~|～|至|到|_)\s*`)
	reTruncEnd   = regexp.MustCompile(`-\s*[\p{Han}]{2,6}高速\s*$`)
	reTruncOne   = regexp.MustCompile(`\d(?:米|M)?\s*-\s*[\p{Han}]{2,6}高速\s*$`)
	reDir        = regexp.MustCompile(`([\p{Han}]{1,6}?)方向`)
	reTravel     = regexp.MustCompile(`由([东南西北])向([东南西北])`)
	reCompass    = regexp.MustCompile(`北向南|南向北|东向西|西向东`)
	reFacility   = regexp.MustCompile(`([\p{Han}]{2,10}?)(收费站|服务区|停车区|隧道|特大桥|大桥|枢纽|互通|立交|站口)(?:([东南西北])?(入口|出口|洞口|口|进口))?`)
	reFacStrip   = regexp.MustCompile(`^.*?(?:高速公路|高速|省|市|县|区段|区|段)`)
	reFacLead    = regexp.MustCompile(`^(?:米|公里|千米|处|附近|往|至)+`)
	reRoadBlank  = regexp.MustCompile(`[\p{Han}]{2,6}?(?:高速公路|高速|快速路)`)
	reFacBlank   = regexp.MustCompile(`[\p{Han}]{1,10}?(?:收费站|服务区|停车区|隧道|特大桥|大桥|枢纽|互通|立交|站口)`)
	reAdmin      = regexp.MustCompile(`([\p{Han}]{2,7}?(?:省|自治区|市|自治州|地区|县|区))`)
	reSection    = regexp.MustCompile(`([\p{Han}]{2,3})(?:段|界)`)
	reLegal      = regexp.MustCompile(`《(?:中华人民共和国)?([\p{Han}]{2,3}?)(?:省|自治区|市)[^》]{0,30}(?:条例|办法|规定)》`)
	reSMS        = regexp.MustCompile(`12123\s*(\d{2})\d`)
	rePlate      = regexp.MustCompile(`([京津沪渝冀晋辽吉黑苏浙皖闽赣鲁豫鄂湘粤琼川贵云陕甘青蒙桂藏宁新])[A-Z][A-Z0-9·]{4,6}`)
	reOverlay    = regexp.MustCompile(`地点[:：]?\s*([GS]\d{1,4})?([\p{Han}]{0,6})K(\d{1,4})\+(\d{1,4})`)
	reOverlayDir = regexp.MustCompile(`(?:通行|行驶)方向[:：]?\s*(上行|下行)`)
	reDevice     = regexp.MustCompile(`设备编号[:：]?\s*(\d{6})`)
	reHasKM      = regexp.MustCompile(`(?:公里|KM)`)
)

type span struct{ s, e int }

func overlaps(a, b span) bool { return !(a.e <= b.s || a.s >= b.e) }

func Parse(text string) Parsed {
	t := Normalize(text)
	p := Parsed{Norm: t}
	p.Code = firstCode(t)
	glued, gspan := gluedSplits(t)
	p.Glued = glued
	names := roadNames(t)
	if len(names) > 0 {
		p.RoadName = names[0]
		code, canon, how := NameToCode(p.RoadName, p.Code)
		p.NameCode, p.NameCanon, p.NameHow = code, canon, how
		if p.Code == "" && code != "" {
			p.Code = code
		}
	}
	t2 := t
	if gspan != nil {
		t2 = t[:gspan.s] + strings.Repeat(" ", gspan.e-gspan.s) + t[gspan.e:]
	}
	st := stakesIn(t2)
	for _, s := range st {
		p.Stakes = append(p.Stakes, Stake{KM: s.km, Metres: s.metres})
	}
	if len(st) >= 2 {
		between := t2[st[0].sp.e:st[1].sp.s]
		if reRangeSep.MatchString(between) || len([]rune(between)) < 12 {
			p.Range = true
		}
	}
	if len(st) == 0 && len(glued) == 0 && !reHasKM.MatchString(t) && reTruncEnd.MatchString(t) {
		p.RangeTruncated = true
	}
	if len(st) == 1 && reTruncOne.MatchString(t) {
		p.RangeTruncated = true
	}
	tnr := reRoadBlank.ReplaceAllString(t, " ")
	if m := reDir.FindStringSubmatchIndex(tnr); m != nil {
		p.Direction = tnr[m[0]:m[1]]
	}
	for _, w := range []string{"上行", "下行", "A道", "B道"} {
		if strings.Contains(t, w) {
			if p.Direction != "" {
				p.Direction += " "
			}
			p.Direction += w
		}
	}
	if m := reTravel.FindString(t); m != "" {
		p.Direction = m
	}
	if m := reCompass.FindString(t); m != "" && !strings.Contains(p.Direction, m) {
		if p.Direction != "" {
			p.Direction += " "
		}
		p.Direction += m
	}
	if f := reFacility.FindStringSubmatch(t); f != nil {
		base := reFacStrip.ReplaceAllString(f[1], "")
		if base == "" {
			base = f[1]
		}
		base = reFacLead.ReplaceAllString(base, "")
		ft := f[2]
		if ft == "站口" {
			ft = "收费站"
		}
		p.Facility = base + ft
		p.FacilityType = ft
		if f[4] != "" {
			p.Portal = f[3] + f[4]
		}
	}
	t3 := reRoadBlank.ReplaceAllString(t, " ")
	t3 = reFacBlank.ReplaceAllString(t3, " ")
	for _, a := range reAdmin.FindAllString(t3, -1) {
		if utf8.RuneCountInString(a) <= 8 && !strings.Contains(a, "方向") && !strings.Contains(a, "城区") && !strings.Contains(a, "区间") {
			p.Admin = append(p.Admin, a)
		}
	}
	for _, m := range reSection.FindAllStringSubmatch(t3, -1) {
		name := m[1]
		if strings.ContainsAny(name, "县区市") {
			continue
		}
		// skip when the section word is followed by 县/区 later in a short tail — prototype negative lookahead
		idx := strings.Index(t3, m[0])
		if idx >= 0 {
			tail := t3[idx+len(m[0]):]
			if len([]rune(tail)) > 0 {
				head := []rune(tail)
				if len(head) > 6 {
					head = head[:6]
				}
				ts := string(head)
				if strings.Contains(ts, "县") || strings.Contains(ts, "区") {
					continue
				}
			}
		}
		p.Admin = append(p.Admin, name+"市")
	}
	return p
}

func firstCode(t string) string {
	for _, m := range reCode.FindAllStringSubmatchIndex(t, -1) {
		s, e := m[2], m[3]
		if s > 0 {
			prev, _ := utf8.DecodeLastRuneInString(t[:s])
			if (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') {
				continue
			}
		}
		if e < len(t) {
			next, _ := utf8.DecodeRuneInString(t[e:])
			if !unicode.Is(unicode.Han, next) && next != ' ' && next != '\t' && next != 'K' && e != len(t) {
				continue
			}
		}
		return t[s:e]
	}
	return ""
}

func roadNames(t string) []string {
	var out []string
	for _, m := range reRoadName.FindAllStringSubmatch(t, -1) {
		nm := m[1]
		stripped := reAdminStrip.ReplaceAllString(nm, "")
		if stripped != "" {
			nm = stripped
		}
		out = append(out, nm)
	}
	return out
}

func NameToCode(nm, codeHint string) (code, canon, how string) {
	if nm == "" {
		return "", "", ""
	}
	if v, ok := nameToCode[nm]; ok {
		return v, nm, "exact"
	}
	for k, v := range nameToCode {
		if strings.HasSuffix(nm, k) && utf8.RuneCountInString(k) >= 2 {
			return v, k, "suffix"
		}
	}
	var cands [][2]string
	for k, v := range nameToCode {
		if edit1(k, nm) {
			cands = append(cands, [2]string{k, v})
		}
	}
	if codeHint != "" {
		for _, c := range cands {
			if c[1] == codeHint {
				return c[1], c[0], "ocrfix"
			}
		}
	}
	if len(cands) == 1 {
		return cands[0][1], cands[0][0], "fuzzy"
	}
	return "", "", ""
}

func edit1(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	if len(ra) != len(rb) {
		return false
	}
	diff := 0
	for i := range ra {
		if ra[i] != rb[i] {
			diff++
		}
	}
	return diff <= 1
}

type foundStake struct {
	km     float64
	metres *int
	sp     span
}

func stakesIn(t string) []foundStake {
	var found []foundStake
	add := func(km float64, metres *int, sp span) {
		for _, prev := range found {
			if overlaps(prev.sp, sp) {
				return
			}
		}
		found = append(found, foundStake{km: km, metres: metres, sp: sp})
	}
	for _, m := range reStakeKPlus.FindAllStringSubmatchIndex(t, -1) {
		km, _ := strconv.ParseFloat(t[m[2]:m[3]], 64)
		me, _ := strconv.Atoi(t[m[4]:m[5]])
		if me >= 1000 {
			continue
		}
		metres := me
		add(km, &metres, span{m[0], m[1]})
	}
	for _, m := range reStakeKM.FindAllStringSubmatchIndex(t, -1) {
		if m[0] > 0 {
			prev, _ := utf8.DecodeLastRuneInString(t[:m[0]])
			if (prev >= '0' && prev <= '9') || prev == 'G' || prev == 'S' {
				continue
			}
		}
		km, _ := strconv.ParseFloat(t[m[2]:m[3]], 64)
		var metres *int
		if m[4] >= 0 {
			me, _ := strconv.Atoi(t[m[4]:m[5]])
			if me >= 1000 {
				continue
			}
			metres = &me
		}
		add(km, metres, span{m[0], m[1]})
	}
	for _, m := range reStakeKOnly.FindAllStringSubmatchIndex(t, -1) {
		if m[0] > 0 {
			prev, _ := utf8.DecodeLastRuneInString(t[:m[0]])
			if (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') {
				continue
			}
		}
		end := m[1]
		if end < len(t) {
			next, _ := utf8.DecodeRuneInString(t[end:])
			if (next >= '0' && next <= '9') || next == '+' {
				continue
			}
		}
		km, _ := strconv.ParseFloat(t[m[2]:m[3]], 64)
		add(km, nil, span{m[0], m[1]})
	}
	// reading order
	for i := 1; i < len(found); i++ {
		j := i
		for j > 0 && found[j].sp.s < found[j-1].sp.s {
			found[j], found[j-1] = found[j-1], found[j]
			j--
		}
	}
	return found
}

func gluedSplits(t string) ([]Glued, *span) {
	m := reGlued.FindStringSubmatchIndex(t)
	if m == nil {
		return nil, nil
	}
	letter := t[m[2]:m[3]]
	digits := t[m[4]:m[5]]
	var metres *int
	if m[6] >= 0 {
		me, _ := strconv.Atoi(t[m[6]:m[7]])
		metres = &me
	}
	var out []Glued
	for n := 1; n <= 4; n++ {
		if n >= len(digits) {
			continue
		}
		kmDigits := digits[n:]
		if len(kmDigits) > 1 && kmDigits[0] == '0' {
			continue
		}
		km, err := strconv.Atoi(kmDigits)
		if err != nil || km <= 0 || km > 5000 {
			continue
		}
		out = append(out, Glued{Code: letter + digits[:n], KM: float64(km), Metres: metres})
	}
	sp := span{m[0], m[1]}
	return out, &sp
}

var provOK = provinceSet()

func ExtractSignals(fullText, overlay string) Signals {
	s := Signals{}
	t := Normalize(fullText)
	for _, m := range reLegal.FindAllStringSubmatch(t, -1) {
		if provOK[m[1]] {
			s.Provinces = append(s.Provinces, ProvHit{"legal", m[1]})
		}
	}
	for _, p := range provinces {
		switch {
		case strings.Contains(t, p+"交警") || strings.Contains(t, p+"高速交警"):
			s.Provinces = append(s.Provinces, ProvHit{"sender", p})
		case authorityHit(t, p):
			s.Provinces = append(s.Provinces, ProvHit{"authority", p})
		case strings.HasPrefix(t, p+"省") || strings.HasPrefix(t, p+"市") || strings.HasPrefix(t, p+"自治区"):
			s.Provinces = append(s.Provinces, ProvHit{"text", p})
		}
	}
	flat := strings.ReplaceAll(t, " ", " ")
	if m := reSMS.FindStringSubmatch(flat); m != nil {
		if name, ok := adcode2[m[1]]; ok {
			s.Provinces = append(s.Provinces, ProvHit{"sms_port", name})
		}
	}
	if m := rePlate.FindStringSubmatch(t); m != nil {
		s.Plate = m[1]
	}
	o := Normalize(overlay)
	if o == "" {
		o = t
	}
	if o != "" {
		if m := reOverlay.FindStringSubmatch(o); m != nil {
			km, _ := strconv.ParseFloat(m[3], 64)
			me, _ := strconv.Atoi(m[4])
			s.Overlay = &OverlayLoc{Code: m[1], Name: m[2], KM: km, M: me}
		}
		if m := reOverlayDir.FindStringSubmatch(o); m != nil {
			s.OverlayDir = m[1]
		}
		if m := reDevice.FindStringSubmatch(o); m != nil {
			if name, ok := adcode2[m[1][:2]]; ok {
				s.Provinces = append(s.Provinces, ProvHit{"device", name})
			}
		}
	}
	return s
}

func authorityHit(t, p string) bool {
	// province + (公安|交通|高速) but not 高速公路交通安全条例
	idx := strings.Index(t, p)
	if idx < 0 {
		return false
	}
	rest := t[idx+len(p):]
	rest = strings.TrimPrefix(rest, "省")
	rest = strings.TrimPrefix(rest, "市")
	rest = strings.TrimPrefix(rest, "自治区")
	if strings.HasPrefix(rest, "公安") || strings.HasPrefix(rest, "交通") {
		return true
	}
	if strings.HasPrefix(rest, "高速") && !strings.HasPrefix(rest, "高速公路交通安全条例") {
		return true
	}
	return false
}

func intPtr(n int) *int { return &n }
