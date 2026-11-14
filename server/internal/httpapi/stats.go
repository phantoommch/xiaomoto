package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"motocam/server/internal/asc"
	"motocam/server/internal/mail"
	"motocam/server/internal/store"
)

// Owner stats page:
//   GET  /admin         login form, or the numbers once logged in
//   POST /admin/login   password -> HttpOnly session cookie (30 days)
//   POST /admin/logout
// Download counting hooks live in download.go and app.go (countDownload).

const (
	adminCookie     = "xm_admin"
	adminSessionTTL = 30 * 24 * time.Hour
	defaultReview   = "review@xiaomoto.app"
)

type statsSettings struct {
	passwordHash []byte
	passwordSum  []byte // sha256 of ADMIN_PASSWORD when no hash is set
	host         string
	reviewEmail  string
	testEmails   []string
}

func newStatsSettings(opts HandlerOptions) statsSettings {
	st := statsSettings{
		host:        strings.ToLower(strings.TrimSpace(opts.AdminHost)),
		reviewEmail: strings.ToLower(strings.TrimSpace(opts.ReviewEmail)),
		testEmails:  opts.StatsTestEmails,
	}
	if st.reviewEmail == "" {
		st.reviewEmail = defaultReview
	}
	if h := strings.TrimSpace(opts.AdminPasswordHash); h != "" {
		st.passwordHash = []byte(h)
	} else if p := opts.AdminPassword; p != "" {
		sum := sha256.Sum256([]byte(p))
		st.passwordSum = sum[:]
	}
	return st
}

func (st statsSettings) enabled() bool { return len(st.passwordHash) > 0 || len(st.passwordSum) > 0 }

func (st statsSettings) check(password string) bool {
	if len(password) == 0 || len(password) > 200 {
		return false
	}
	if len(st.passwordHash) > 0 {
		return bcrypt.CompareHashAndPassword(st.passwordHash, []byte(password)) == nil
	}
	if len(st.passwordSum) > 0 {
		sum := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare(sum[:], st.passwordSum) == 1
	}
	return false
}

// --- download counting ---

// countable is a real start of a download: GET (not HEAD), no prefetch, and
// either no Range or a Range that starts at byte 0.
func countable(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	purpose := strings.ToLower(r.Header.Get("Sec-Purpose") + r.Header.Get("Purpose") + r.Header.Get("X-Moz"))
	if strings.Contains(purpose, "prefetch") || strings.Contains(purpose, "prerender") {
		return false
	}
	rng := strings.ReplaceAll(strings.ToLower(r.Header.Get("Range")), " ", "")
	return rng == "" || strings.HasPrefix(rng, "bytes=0-")
}

func uaKind(r *http.Request) string {
	ua := r.Header.Get("User-Agent")
	low := strings.ToLower(ua)
	switch {
	case strings.HasPrefix(ua, "XiaoMoto/") || strings.TrimSpace(r.Header.Get("X-App-Build")) != "":
		return store.UAKindApp
	case low == "" || strings.Contains(low, "bot") || strings.Contains(low, "spider") || strings.Contains(low, "crawl") ||
		strings.Contains(low, "slurp") || strings.Contains(low, "facebookexternalhit") || strings.Contains(low, "preview"):
		return store.UAKindBot
	case strings.Contains(ua, "MicroMessenger"):
		return "wechat"
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod"):
		return "ios"
	case strings.Contains(ua, "Android"):
		return "android"
	case strings.Contains(ua, "Windows") || strings.Contains(ua, "Macintosh") || strings.Contains(ua, "Linux") || strings.Contains(ua, "CrOS"):
		return "desktop"
	default:
		return "other"
	}
}

func (s *Server) countDownload(r *http.Request, kind string) {
	if s.db == nil || !countable(r) {
		return
	}
	if _, err := s.db.RecordDownload(kind, uaKind(r), clientIP(r), time.Now()); err != nil {
		log.Printf("记录下载统计失败 kind=%s err=%v", kind, err)
	}
}

// --- admin page ---

func (s *Server) adminAllowed(w http.ResponseWriter, r *http.Request) bool {
	ok := s.stats.enabled()
	if ok && s.stats.host != "" {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ok = host == s.stats.host
	}
	if !ok {
		http.NotFound(w, r)
	}
	return ok
}

func adminHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}

func (s *Server) adminSession(r *http.Request) (string, bool) {
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return "", false
	}
	return c.Value, s.db.AdminSessionValid(c.Value, time.Now())
}

func (s *Server) adminSlash(w http.ResponseWriter, r *http.Request) {
	if !s.adminAllowed(w, r) {
		return
	}
	http.Redirect(w, r, "/admin", http.StatusMovedPermanently)
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	if !s.adminAllowed(w, r) {
		return
	}
	adminHeaders(w)
	if _, ok := s.adminSession(r); !ok {
		writeAdminHTML(w, http.StatusOK, loginDocument(""))
		return
	}
	now := time.Now()
	st, err := s.db.Stats(now, store.StatsOptions{
		Loc: shanghai, Days: 30, RecentLimit: 20,
		ReviewEmail: s.stats.reviewEmail, TestEmails: s.stats.testEmails,
	})
	if err != nil {
		log.Printf("统计页查询失败 err=%v", err)
		writeAdminHTML(w, http.StatusInternalServerError, adminDocument("出错了", `<p class="card">读取数据出错，请稍后刷新。</p>`))
		return
	}
	pending, err := s.db.AdminList("pending", 50)
	if err != nil {
		log.Printf("待审列表失败 err=%v", err)
		pending = nil
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	points, err := s.db.AdminPointsQuery(q, typ, 200)
	if err != nil {
		log.Printf("点位列表失败 err=%v", err)
		points = nil
	}
	notice := ""
	if ok := r.URL.Query().Get("ok"); ok != "" {
		notice = ok
	} else if er := r.URL.Query().Get("err"); er != "" {
		notice = er
	}
	tf := s.testflight.Get(r.Context())
	writeAdminHTML(w, http.StatusOK, statsDocument(st, tf, now, pending, points, q, typ, notice))
}

// sameOrigin rejects cross-site form posts. Browsers send Sec-Fetch-Site
// and/or Origin; "null" Origin (privacy modes) is judged by Sec-Fetch-Site.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if !s.adminAllowed(w, r) {
		return
	}
	adminHeaders(w)
	if !sameOrigin(r) {
		writeAdminHTML(w, http.StatusForbidden, loginDocument("请从这个页面登录。"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		writeAdminHTML(w, http.StatusBadRequest, loginDocument("请重新输入密码。"))
		return
	}
	ip := clientIP(r)
	now := time.Now()
	if err := s.db.AdminLoginAllowed(ip, now); err != nil {
		writeAdminHTML(w, http.StatusTooManyRequests, loginDocument("试错次数太多，请 15 分钟后再试。"))
		return
	}
	if !s.stats.check(r.PostForm.Get("password")) {
		s.db.AdminLoginFailed(ip, now)
		log.Printf("统计页密码错误 ip=%s", ip)
		writeAdminHTML(w, http.StatusUnauthorized, loginDocument("密码不对，请再试一次。"))
		return
	}
	token, err := s.db.CreateAdminSession(now, adminSessionTTL)
	if err != nil {
		writeAdminHTML(w, http.StatusInternalServerError, loginDocument("服务器出错，请稍后再试。"))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminCookie, Value: token, Path: "/admin",
		MaxAge: int(adminSessionTTL / time.Second), Expires: now.Add(adminSessionTTL),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if !s.adminAllowed(w, r) {
		return
	}
	adminHeaders(w)
	if !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if token, _ := s.adminSession(r); token != "" {
		s.db.DeleteAdminSession(token)
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/admin", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func writeAdminHTML(w http.ResponseWriter, status int, page string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(page))
}

// --- HTML ---

func loginDocument(msg string) string {
	errLine := ""
	if msg != "" {
		errLine = `<p class="err">` + html.EscapeString(msg) + `</p>`
	}
	return adminDocument("登录", `<section class="card login">
<h2>请输入密码</h2>
`+errLine+`<form method="post" action="/admin/login">
<input type="text" name="username" value="admin" autocomplete="username" hidden>
<input type="password" name="password" autocomplete="current-password" placeholder="密码" required autofocus>
<button type="submit" class="btn">登录</button>
</form>
<p class="small">登录一次，30 天内不用再输。</p>
</section>`)
}

func n(v int) string { return fmt.Sprintf("%d", v) }

func tile(label string, v int, unit string) string {
	return `<div class="tile"><div class="num">` + n(v) + `<small>` + unit + `</small></div><div class="lbl">` + label + `</div></div>`
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(shanghai).Format("2006-01-02 15:04")
}

func barChart(days []store.DayCount) string {
	max := 1
	for _, d := range days {
		if d.Count > max {
			max = d.Count
		}
	}
	var b strings.Builder
	b.WriteString(`<div class="chart" role="img" aria-label="近 30 天每天新注册人数">`)
	for _, d := range days {
		h := d.Count * 100 / max
		label := ""
		if d.Count > 0 {
			label = n(d.Count)
		}
		title := d.Day.Format("1月2日") + " " + n(d.Count) + " 人"
		fmt.Fprintf(&b, `<div class="bar" title="%s"><span>%s</span><i style="height:%d%%"></i></div>`, title, label, h)
	}
	b.WriteString(`</div>`)
	if len(days) > 0 {
		mid := days[len(days)/2].Day
		fmt.Fprintf(&b, `<div class="axis"><span>%s</span><span>%s</span><span>今天</span></div>`,
			days[0].Day.Format("1月2日"), mid.Format("1月2日"))
	}
	return b.String()
}

func statsDocument(st store.Stats, tf asc.Result, now time.Time, pending []store.AdminReport, points []store.AdminPoint, q, typ, notice string) string {
	var b strings.Builder
	b.WriteString(`<div class="top"><p class="upd">更新于 ` + fmtTime(now) + `（北京时间）</p>
<div class="acts"><a class="btn sm" href="/admin">刷新</a><form method="post" action="/admin/logout"><button class="btn sm ghost" type="submit">退出</button></form></div></div>
`)

	// Registrations.
	real := st.Users - st.TestUsers
	b.WriteString(`<section class="card"><h2>注册用户</h2>
<div class="big"><span class="num">` + n(st.Users) + `</span> 人 <span class="lbl">注册总人数</span></div>
`)
	if st.TestUsers > 0 {
		b.WriteString(`<p class="small">其中审核/测试账号 ` + n(st.TestUsers) + ` 个，其他用户 ` + n(real) + ` 人。</p>`)
	}
	b.WriteString(`<div class="grid">` +
		tile("今天新增", st.UsersToday, "人") +
		tile("近 7 天新增", st.UsersWeek, "人") +
		tile("设了密码的人数", st.WithPassword, "人") +
		tile("最近 7 天登录过", st.Active7d, "人") +
		`</div>
<h3>近 30 天每天新注册</h3>
` + barChart(st.Daily) + `
<h3>最近注册</h3>
<ul class="list">`)
	for _, u := range st.Recent {
		tags := ""
		if u.Review {
			tags += `<em class="tag">审核账号</em>`
		} else if u.Test {
			tags += `<em class="tag">测试账号</em>`
		}
		if u.HasPassword {
			tags += `<em class="tag ok">已设密码</em>`
		}
		if u.Disabled {
			tags += `<em class="tag off">已停用</em>`
		}
		b.WriteString(`<li><span class="who">` + html.EscapeString(mail.MaskEmail(u.Email)) + tags + `</span><span class="when">` + fmtTime(u.CreatedAt) + `</span></li>`)
	}
	if len(st.Recent) == 0 {
		b.WriteString(`<li>还没有人注册</li>`)
	}
	b.WriteString(`</ul>`)
	if st.Users > len(st.Recent) {
		b.WriteString(`<p class="small">只列出最近 ` + n(len(st.Recent)) + ` 位。</p>`)
	}
	b.WriteString(`</section>
`)

	// Downloads.
	since := "部署后"
	if !st.Since.IsZero() {
		since = st.Since.In(shanghai).Format("2006-01-02")
	}
	row := func(label string, w store.Window) string {
		return `<tr><th>` + label + `</th><td>` + n(w.Today) + `</td><td>` + n(w.Week) + `</td><td>` + n(w.Total) + `</td></tr>`
	}
	b.WriteString(`<section class="card"><h2>下载</h2>
<div class="big"><span class="num">` + n(st.AndroidWeb.Total+st.AndroidApp.Total) + `</span> 次 <span class="lbl">安卓下载（累计）</span></div>
<table><thead><tr><th></th><th>今天</th><th>近 7 天</th><th>累计</th></tr></thead><tbody>` +
		row("安卓·网页下载", st.AndroidWeb) +
		row("安卓·App 内更新", st.AndroidApp) +
		row("下载页打开次数", st.PageViews) +
		row("iPhone 测试版按钮点击", st.TestFlight) +
		`</tbody></table>
<p class="small">按网络粗略去重：网页下载约 ` + n(st.AndroidWeb.Unique) + ` 人，App 内更新约 ` + n(st.AndroidApp.Unique) + ` 人，下载页约 ` + n(st.PageViews.Unique) + ` 人。同一网络 2 分钟内重复只算 1 次。</p>
<p class="small">iPhone 按钮点击只说明点了「加入 iPhone 测试版」，不等于已经装好。</p>
<p class="note">从 ` + since + ` 开始统计，之前的下载没有记录。</p>
</section>
`)

	b.WriteString(testflightSection(tf))

	// Reports.
	r := st.Reports
	b.WriteString(`<section class="card"><h2>用户上传点位</h2>
<div class="big"><span class="num">` + n(r.Total) + `</span> 条 <span class="lbl">用户上传总数</span></div>
<p class="small">今天新增 ` + n(r.Today) + ` 条，近 7 天新增 ` + n(r.Week) + ` 条。`)
	if r.Imported > 0 {
		b.WriteString(`另有批量导入 ` + n(r.Imported) + ` 条，不算在内。`)
	}
	b.WriteString(`</p>
<div class="grid">` +
		tile("已公开", r.Merged, "条") +
		tile("等待审核", r.Pending, "条") +
		tile("没通过", r.Rejected, "条") +
		tile("点位已删除", r.Archived, "条") +
		`</div>
<p class="small">地图上公开的点位共 <b>` + n(st.Points) + `</b> 个（用户上传 ` + n(st.PointsUser) + ` 个，批量导入 ` + n(st.PointsImport) + ` 个）。</p>
</section>
`)
	b.WriteString(pendingSection(pending, notice))
	b.WriteString(pointsSection(points, q, typ, len(points)))
	b.WriteString(`<p class="foot">这个页面只有知道密码的人能看到。邮箱只显示首字母。时间都是北京时间。</p>
`)
	return adminDocument("数据", b.String())
}

func adminDocument(title, body string) string {
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<meta name="format-detection" content="telephone=no">
<title>` + html.EscapeString(title) + ` · 小moto</title>
<link rel="apple-touch-icon" href="/download/icon.png">
<style>
*{box-sizing:border-box}
body{margin:0;padding:1rem;font:16px/1.5 -apple-system,BlinkMacSystemFont,"PingFang SC","Noto Sans SC","Microsoft YaHei",sans-serif;color:#1c1917;background:#f5f5f4;-webkit-text-size-adjust:100%}
main{max-width:34rem;margin:0 auto}
h1{font-size:1.35rem;margin:.2rem 0 .4rem}
h2{font-size:1.15rem;margin:0 0 .5rem}
h3{font-size:.95rem;color:#57534e;margin:1.1rem 0 .4rem;font-weight:600}
.card{background:#fff;border-radius:14px;padding:1rem 1.1rem;margin:0 0 1rem;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.top{display:flex;align-items:center;justify-content:space-between;gap:.5rem;margin:0 0 .8rem}
.upd{margin:0;color:#78716c;font-size:.88rem}
.acts{display:flex;gap:.4rem}
.acts form{margin:0}
.btn{display:inline-block;border:0;background:#c2410c;color:#fff;text-decoration:none;font:inherit;font-weight:600;padding:.7rem 1rem;border-radius:10px;cursor:pointer}
.btn.sm{padding:.4rem .8rem;font-size:.92rem;white-space:nowrap}
.btn.ghost{background:#e7e5e4;color:#44403c}
.big{margin:.1rem 0 .3rem;color:#44403c}
.big .num{font-size:2.4rem;font-weight:700;color:#1c1917;line-height:1.1}
.big .lbl{color:#78716c;font-size:.95rem;margin-left:.3rem}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:.6rem;margin:.6rem 0 .2rem}
.tile{background:#fafaf9;border:1px solid #e7e5e4;border-radius:10px;padding:.6rem .7rem}
.tile .num{font-size:1.5rem;font-weight:700;line-height:1.2}
.tile .num small{font-size:.8rem;font-weight:400;color:#78716c;margin-left:.15rem}
.tile .lbl{font-size:.88rem;color:#57534e}
.chart{display:flex;align-items:flex-end;gap:2px;height:110px;padding-top:14px;border-bottom:1px solid #d6d3d1}
.bar{flex:1;height:100%;display:flex;flex-direction:column;justify-content:flex-end;align-items:center;min-width:0}
.bar i{display:block;width:100%;background:#ea580c;border-radius:2px 2px 0 0;min-height:0}
.bar span{font-size:10px;color:#57534e;line-height:1;margin-bottom:2px}
.axis{display:flex;justify-content:space-between;font-size:.78rem;color:#78716c;margin-top:.2rem}
.list{list-style:none;padding:0;margin:0}
.list li{display:flex;justify-content:space-between;align-items:baseline;gap:.5rem;padding:.45rem 0;border-bottom:1px solid #f0efee;font-size:.93rem}
.list li:last-child{border-bottom:0}
.who{word-break:break-all}
.when{color:#78716c;font-size:.85rem;white-space:nowrap}
.cnt{font-weight:600;font-variant-numeric:tabular-nums;white-space:nowrap}
.st{color:#9a3412}
.tag{font-style:normal;font-size:.72rem;background:#fde68a;color:#78350f;border-radius:6px;padding:.05rem .35rem;margin-left:.35rem;white-space:nowrap}
.tag.ok{background:#dcfce7;color:#166534}
.tag.off{background:#e7e5e4;color:#57534e}
table{width:100%;border-collapse:collapse;margin:.5rem 0;font-size:.93rem}
th,td{padding:.45rem .2rem;border-bottom:1px solid #f0efee;text-align:right}
th:first-child{text-align:left;font-weight:500}
thead th{color:#78716c;font-weight:500;font-size:.82rem}
td{font-variant-numeric:tabular-nums;font-weight:600}
.small{font-size:.86rem;color:#78716c;margin:.45rem 0 0}
.note{font-size:.86rem;color:#9a3412;background:#fff7ed;border-radius:8px;padding:.4rem .6rem;margin:.6rem 0 0}
.foot{text-align:center;font-size:.82rem;color:#a8a29e;margin:1rem 0 .5rem}
.login input[type=password]{width:100%;font:inherit;font-size:1.1rem;padding:.7rem;border:1px solid #d6d3d1;border-radius:10px;margin:.3rem 0 .7rem}
.login .btn{width:100%;font-size:1.05rem}
.err{color:#b91c1c;margin:.2rem 0 .4rem}

.pend{border-top:1px solid #f0efee;padding:1rem 0 0;margin:1rem 0 0}
.pend:first-of-type{border-top:0;margin-top:.4rem;padding-top:.2rem}
.pend-top{display:flex;gap:.7rem;align-items:flex-start}
.thumb{flex:0 0 4.5rem;width:4.5rem;height:4.5rem;border-radius:10px;overflow:hidden;background:#e7e5e4;display:block}
.thumb img{width:100%;height:100%;object-fit:cover;display:block}
.thumb.empty{display:flex;align-items:center;justify-content:center;font-size:.75rem;color:#78716c}
.pend-meta{flex:1;min-width:0}
.pend-meta strong{font-size:1.02rem}
.pend-form{display:flex;flex-wrap:wrap;gap:.4rem;align-items:flex-end;margin:.55rem 0 0}
.pend-form label{font-size:.82rem;color:#57534e;display:flex;flex-direction:column;gap:.2rem}
.pend-form label.grow{flex:1;min-width:8rem}
.pend-form input{font:inherit;font-size:.92rem;padding:.4rem .5rem;border:1px solid #d6d3d1;border-radius:8px;min-width:5.5rem;width:7rem}
.pend-form label.grow input{width:100%}
.pend-form.need-pin{background:#fff7ed;border:1px solid #fdba74;border-radius:10px;padding:.55rem .6rem;margin-top:.55rem}
.pend-form.need-pin label{color:#9a3412;font-weight:600}
.pend-form.need-pin input{border-color:#ea580c;background:#fff;min-width:7rem;width:8.5rem}
.coords-wgs{font-size:.78rem;color:#a8a29e;margin:.15rem 0 0}
.btn.ok{background:#15803d}
.search{display:flex;flex-wrap:wrap;gap:.4rem;margin:.5rem 0 .7rem;align-items:center}
.search input[type=search]{flex:1;min-width:8rem;font:inherit;padding:.45rem .6rem;border:1px solid #d6d3d1;border-radius:8px}
.search select{font:inherit;padding:.45rem .5rem;border:1px solid #d6d3d1;border-radius:8px;background:#fff}
.pts .dim{color:#78716c;font-weight:400;font-size:.86rem}
.pend-meta a,.pts a{color:#c2410c}
</style>
</head>
<body>
<main>
<h1>小moto 数据</h1>
` + body + `
</main>
</body>
</html>
`
}
