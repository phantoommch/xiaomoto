package httpapi

import (
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"motocam/server/internal/recog"
	"motocam/server/internal/store"
)

// Cookie-session wrappers under /admin/* so the stats page can review reports
// and export points without putting ADMIN_TOKEN in browser JS.

func (s *Server) requireAdminSession(w http.ResponseWriter, r *http.Request) bool {
	if !s.adminAllowed(w, r) {
		return false
	}
	if _, ok := s.adminSession(r); !ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return false
	}
	return true
}

func (s *Server) adminSessionImage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminSession(w, r) {
		return
	}
	adminHeaders(w)
	path, err := s.db.ImagePath(r.PathValue("id"))
	if err != nil {
		writeStored(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

func (s *Server) adminSessionApprove(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminSession(w, r) {
		return
	}
	adminHeaders(w)
	if !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin?err="+url.QueryEscape("表单无效")+"#pending", http.StatusSeeOther)
		return
	}
	id := r.PathValue("id")
	latRaw := strings.TrimSpace(r.PostForm.Get("latitude"))
	lonRaw := strings.TrimSpace(r.PostForm.Get("longitude"))
	if latRaw != "" || lonRaw != "" {
		lat, err1 := strconv.ParseFloat(latRaw, 64)
		lon, err2 := strconv.ParseFloat(lonRaw, 64)
		if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			http.Redirect(w, r, "/admin?err="+url.QueryEscape("坐标无效")+"#pending", http.StatusSeeOther)
			return
		}
		// Form posts 高德 GCJ-02; DB keeps WGS-84 (display-only conversion).
		wlat, wlon := recog.GCJToWGS(lat, lon)
		if err := s.db.EditReport(id, store.ReportEdit{Latitude: &wlat, Longitude: &wlon}); err != nil {
			log.Printf("admin approve edit id=%s err=%v", id, err)
			http.Redirect(w, r, "/admin?err="+url.QueryEscape(adminErrMsg(err))+"#pending", http.StatusSeeOther)
			return
		}
	}
	if err := s.db.Approve(id); err != nil {
		log.Printf("admin approve id=%s err=%v", id, err)
		http.Redirect(w, r, "/admin?err="+url.QueryEscape(adminErrMsg(err))+"#pending", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?ok="+url.QueryEscape("已通过")+"#pending", http.StatusSeeOther)
}

func (s *Server) adminSessionReject(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminSession(w, r) {
		return
	}
	adminHeaders(w)
	if !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin?err="+url.QueryEscape("表单无效")+"#pending", http.StatusSeeOther)
		return
	}
	id := r.PathValue("id")
	reason := r.PostForm.Get("reason")
	if err := s.db.Reject(id, reason); err != nil {
		log.Printf("admin reject id=%s err=%v", id, err)
		http.Redirect(w, r, "/admin?err="+url.QueryEscape(adminErrMsg(err))+"#pending", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin?ok="+url.QueryEscape("已驳回")+"#pending", http.StatusSeeOther)
}

func (s *Server) adminSessionPointsCSV(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminSession(w, r) {
		return
	}
	adminHeaders(w)
	q := r.URL.Query().Get("q")
	typ := r.URL.Query().Get("type")
	points, err := s.db.AdminPointsQuery(q, typ, 5000)
	if err != nil {
		log.Printf("admin points csv err=%v", err)
		http.Error(w, "读取失败", http.StatusInternalServerError)
		return
	}
	name := "违法点位-" + time.Now().In(shanghai).Format("20060102-1504") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	// UTF-8 BOM so Excel on Windows opens Chinese correctly.
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"名称", "类型", "公路编号", "公路名", "桩号", "纬度(高德)", "经度(高德)", "违法行为", "罚款(元)", "备注", "来源", "更新时间"})
	for _, p := range points {
		fine := ""
		if p.FineYuan != nil {
			fine = strconv.Itoa(*p.FineYuan)
		}
		gLat, gLon := recog.WGSToGCJ(p.Latitude, p.Longitude)
		_ = cw.Write([]string{
			p.Name, typeLabel(p.Type), p.RoadCode, p.RoadName, p.Stake,
			fmt.Sprintf("%.6f", gLat), fmt.Sprintf("%.6f", gLon),
			p.Violation, fine, p.Notes, sourceLabel(p.Source), fmtAdminTime(p.UpdatedAt),
		})
	}
	cw.Flush()
}

func adminErrMsg(err error) string {
	var ie store.InputError
	if errors.As(err, &ie) && ie.Message != "" {
		return ie.Message
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "记录不存在"
	case errors.Is(err, store.ErrConflict):
		return "当前状态不能这样处理"
	default:
		return "操作失败，请稍后重试"
	}
}

func typeLabel(t string) string {
	switch t {
	case "banned_toll":
		return "禁摩收费站"
	case "banned_segment":
		return "禁摩路段"
	case "camera":
		return "抓拍摄像头"
	case "other":
		return "其他"
	default:
		if t == "" {
			return "—"
		}
		return t
	}
}

func sourceLabel(s string) string {
	switch s {
	case "import":
		return "批量导入"
	case "user_upload":
		return "用户上传"
	case "":
		return "—"
	default:
		return s
	}
}

func confidenceLabel(c string) string {
	switch c {
	case "exact":
		return "定位准"
	case "low":
		return "不太准"
	case "none":
		return "没定位"
	case "":
		return "—"
	default:
		return c
	}
}

func fmtAdminTime(raw string) string {
	if raw == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", raw)
		if err != nil {
			return raw
		}
	}
	return t.In(shanghai).Format("2006-01-02 15:04")
}

func mapLink(lat, lon float64, name string) string {
	if lat == 0 && lon == 0 {
		return ""
	}
	// Stored coords are WGS-84; AMap URI expects GCJ-02.
	gLat, gLon := recog.WGSToGCJ(lat, lon)
	u := "https://uri.amap.com/marker?position=" + strconv.FormatFloat(gLon, 'f', 6, 64) +
		"," + strconv.FormatFloat(gLat, 'f', 6, 64)
	if name != "" {
		u += "&name=" + url.QueryEscape(name)
	}
	return u
}

// stakeNeedsManualPin: report has a stake but AMap had no km marker (or no usable pin).
// Does not loosen auto-review — display/UX only.
func stakeNeedsManualPin(r store.AdminReport) bool {
	if strings.Contains(r.ReviewReason, "高德无此桩号") || strings.Contains(r.ReviewReason, "人工定点") || strings.Contains(r.ReviewReason, "no AMap km marker") {
		return true
	}
	hasStake := strings.TrimSpace(r.Stake) != "" || looksLikeStake(r.Name) || looksLikeStake(r.OcrLocation)
	if !hasStake {
		return false
	}
	if r.Confidence == "none" {
		return true
	}
	if !r.Located || (r.Latitude == 0 && r.Longitude == 0) {
		return true
	}
	return false
}

var stakeLike = regexp.MustCompile(`(?i)K\s*\d+\s*\+\s*\d+|\d+\s*公里(?:\s*\d+\s*米)?`)

func looksLikeStake(s string) bool {
	return stakeLike.MatchString(s)
}

func fmtGCJLine(lat, lon float64) (gLat, gLon float64, primary, secondary string) {
	gLat, gLon = recog.WGSToGCJ(lat, lon)
	primary = fmt.Sprintf("%.5f, %.5f", gLat, gLon)
	secondary = fmt.Sprintf("WGS %.5f, %.5f", lat, lon)
	return
}

func trunc(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func pendingSection(reports []store.AdminReport, notice string) string {
	var b strings.Builder
	b.WriteString(`<section class="card" id="pending"><h2>待审点</h2>
`)
	if notice != "" {
		b.WriteString(`<p class="note">` + html.EscapeString(notice) + `</p>
`)
	}
	if len(reports) == 0 {
		b.WriteString(`<p class="small">现在没有等待审核的点。</p>
</section>
`)
		return b.String()
	}
	b.WriteString(`<p class="small">共 ` + n(len(reports)) + ` 条。坐标为高德 GCJ-02（库内仍存 WGS）。通过前可改高德坐标；驳回可填原因。</p>
`)
	for _, r := range reports {
		esc := html.EscapeString
		locBits := []string{}
		if r.RoadCode != "" {
			locBits = append(locBits, r.RoadCode)
		}
		if r.RoadName != "" {
			locBits = append(locBits, r.RoadName)
		}
		if r.Stake != "" {
			locBits = append(locBits, r.Stake)
		}
		if r.OcrLocation != "" && r.OcrLocation != r.Name {
			locBits = append(locBits, r.OcrLocation)
		}
		locLine := strings.Join(locBits, " · ")
		why := r.ReviewReason
		if why == "" {
			why = confidenceLabel(r.Confidence)
		} else {
			why = confidenceLabel(r.Confidence) + " · " + why
		}
		b.WriteString(`<article class="pend">
<div class="pend-top">`)
		if r.HasImage {
			b.WriteString(`<a class="thumb" href="/admin/reports/` + esc(r.ID) + `/image" target="_blank" rel="noopener"><img src="/admin/reports/` + esc(r.ID) + `/image" alt="上报照片"></a>`)
		} else {
			b.WriteString(`<div class="thumb empty">无图</div>`)
		}
		b.WriteString(`<div class="pend-meta">
<strong>` + esc(r.Name) + `</strong>
<em class="tag">` + esc(typeLabel(r.Type)) + `</em>
`)
		if locLine != "" {
			b.WriteString(`<p class="small">` + esc(locLine) + `</p>
`)
		}
		b.WriteString(`<p class="small">` + esc(trunc(why, 120)) + `</p>
<p class="when">` + esc(fmtAdminTime(r.CreatedAt)) + `</p>
`)
		needPin := stakeNeedsManualPin(r)
		hasCoord := r.Located && !(r.Latitude == 0 && r.Longitude == 0)
		if needPin {
			b.WriteString(`<p class="note">高德无此桩号，请在地图上人工定点。请在下方填写「高德坐标」后再通过。</p>
`)
		}
		if hasCoord {
			_, _, primary, secondary := fmtGCJLine(r.Latitude, r.Longitude)
			b.WriteString(`<p class="small"><strong>高德坐标</strong> ` + esc(primary))
			if link := mapLink(r.Latitude, r.Longitude, r.Name); link != "" {
				b.WriteString(` · <a href="` + esc(link) + `" target="_blank" rel="noopener">看地图</a>`)
			}
			b.WriteString(`</p>
<p class="coords-wgs">` + esc(secondary) + `</p>
`)
		} else if !needPin {
			b.WriteString(`<p class="note">还没有可用坐标，通过前请先填高德纬度经度。</p>
`)
		}
		b.WriteString(`</div></div>
<form class="pend-form`)
		if needPin {
			b.WriteString(` need-pin`)
		}
		b.WriteString(`" method="post" action="/admin/reports/` + esc(r.ID) + `/approve">
`)
		latPh, lonPh := "高德 GCJ", "高德 GCJ"
		if needPin && !hasCoord {
			latPh, lonPh = "必填 · 高德", "必填 · 高德"
		}
		b.WriteString(`<label>高德纬度 <input name="latitude" inputmode="decimal" placeholder="` + esc(latPh) + `" value="`)
		if hasCoord {
			gLat, _, _, _ := fmtGCJLine(r.Latitude, r.Longitude)
			b.WriteString(esc(strconv.FormatFloat(gLat, 'f', 6, 64)))
		}
		b.WriteString(`"></label>
<label>高德经度 <input name="longitude" inputmode="decimal" placeholder="` + esc(lonPh) + `" value="`)
		if hasCoord {
			_, gLon, _, _ := fmtGCJLine(r.Latitude, r.Longitude)
			b.WriteString(esc(strconv.FormatFloat(gLon, 'f', 6, 64)))
		}
		b.WriteString(`"></label>
<button type="submit" class="btn sm ok">通过</button>
</form>
<form class="pend-form" method="post" action="/admin/reports/` + esc(r.ID) + `/reject">
<label class="grow">驳回原因 <input name="reason" maxlength="200" placeholder="可选"></label>
<button type="submit" class="btn sm ghost">驳回</button>
</form>
</article>
`)
	}
	b.WriteString(`</section>
`)
	return b.String()
}

func pointsSection(points []store.AdminPoint, q, typ string, totalShown int) string {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString(`<section class="card" id="points"><h2>违法点位</h2>
<p class="small">地图上已公开的点。可按名称/公路/桩号搜索，也可导出 CSV（用 Excel 能直接打开）。</p>
<form class="search" method="get" action="/admin#points">
<input type="search" name="q" value="` + esc(q) + `" placeholder="搜名称、公路、桩号…" maxlength="80">
<select name="type">
<option value="">全部类型</option>`)
	for _, opt := range []struct{ v, label string }{
		{"banned_toll", "禁摩收费站"},
		{"banned_segment", "禁摩路段"},
		{"camera", "抓拍摄像头"},
		{"other", "其他"},
	} {
		sel := ""
		if typ == opt.v {
			sel = " selected"
		}
		b.WriteString(`<option value="` + opt.v + `"` + sel + `>` + opt.label + `</option>`)
	}
	b.WriteString(`</select>
<button type="submit" class="btn sm">筛选</button>
<a class="btn sm ghost" href="/admin/points.csv?q=` + url.QueryEscape(q) + `&type=` + url.QueryEscape(typ) + `">导出 CSV</a>
</form>
`)
	if len(points) == 0 {
		b.WriteString(`<p class="small">没有匹配的点位。</p>
</section>
`)
		return b.String()
	}
	b.WriteString(`<p class="small">列出 ` + n(len(points)) + ` 条`)
	if totalShown >= 200 && q == "" && typ == "" {
		b.WriteString(`（最多 200 条，导出可拿全部）`)
	}
	b.WriteString(`。</p>
<ul class="list pts">`)
	for _, p := range points {
		road := strings.TrimSpace(p.RoadCode + " " + p.RoadName)
		extra := []string{typeLabel(p.Type)}
		if p.Stake != "" {
			extra = append(extra, p.Stake)
		}
		if p.Violation != "" {
			extra = append(extra, trunc(p.Violation, 24))
		}
		extra = append(extra, sourceLabel(p.Source))
		b.WriteString(`<li><span class="who"><strong>` + esc(p.Name) + `</strong>`)
		if road != "" {
			b.WriteString(` <span class="dim">` + esc(road) + `</span>`)
		}
		b.WriteString(`<br><span class="dim">` + esc(strings.Join(extra, " · ")) + `</span>`)
		if p.Latitude != 0 || p.Longitude != 0 {
			_, _, primary, secondary := fmtGCJLine(p.Latitude, p.Longitude)
			b.WriteString(` · <strong>高德坐标</strong> ` + esc(primary))
			if link := mapLink(p.Latitude, p.Longitude, p.Name); link != "" {
				b.WriteString(` · <a href="` + esc(link) + `" target="_blank" rel="noopener">地图</a>`)
			}
			b.WriteString(` <span class="coords-wgs">` + esc(secondary) + `</span>`)
		}
		b.WriteString(`</span><span class="when">` + esc(fmtAdminTime(p.UpdatedAt)) + `</span></li>`)
	}
	b.WriteString(`</ul>
</section>
`)
	return b.String()
}
