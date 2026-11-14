package httpapi

import (
	"html"
	"strings"

	"motocam/server/internal/asc"
)

// testflightSection renders "iPhone 测试版（TestFlight）" on the stats page.
func testflightSection(tf asc.Result) string {
	var b strings.Builder
	b.WriteString(`<section class="card" id="testflight"><h2>iPhone 测试版（TestFlight）</h2>
`)
	if !tf.Configured {
		b.WriteString(`<p class="small">未配置</p>
</section>
`)
		return b.String()
	}
	s := tf.Summary
	if s == nil {
		b.WriteString(`<p class="note">暂时取不到苹果数据，请稍后刷新。</p>
</section>
`)
		return b.String()
	}
	if tf.Failed {
		b.WriteString(`<p class="note">暂时取不到苹果数据，下面是 ` + fmtTime(s.FetchedAt) + ` 取到的数据。</p>
`)
	}
	esc := html.EscapeString

	b.WriteString(`<div class="big"><span class="num">` + n(s.Testers) + `</span> 人 <span class="lbl">测试人数</span></div>
`)
	if rv := s.Review; rv != nil {
		ver := esc(rv.Version)
		if rv.Build != "" {
			ver += `（第 ` + esc(rv.Build) + ` 版）`
		}
		b.WriteString(`<p class="small">App Store 上架审核：` + ver + ` <b class="st">` + esc(asc.StateName(rv.State)) + `</b></p>
`)
	}
	active, sessions := "—", "—"
	if s.Usage7 != nil {
		active, sessions = n(s.Usage7.ActiveTesters), n(s.Usage7.Sessions)
	}
	b.WriteString(`<div class="grid">` +
		tile("已安装", s.Installed, "人") +
		tile("已接受、还没安装", s.Accepted, "人") +
		tileS("近 7 天用过的人", active, "人") +
		tileS("近 7 天使用次数", sessions, "次") +
		`</div>
`)
	var extra []string
	if s.Invited > 0 {
		extra = append(extra, n(s.Invited)+` 人收到邀请还没接受`)
	}
	if s.OtherSt > 0 {
		extra = append(extra, n(s.OtherSt)+` 人是其他状态（比如已移除）`)
	}
	if len(extra) > 0 {
		b.WriteString(`<p class="small">另外 ` + strings.Join(extra, `，`) + `。</p>
`)
	}

	// Builds.
	b.WriteString(`<h3>装的是哪一版</h3>
<ul class="list">`)
	withBuild, old := 0, 0
	for _, bc := range s.Builds {
		tag := ""
		if s.LatestBuild != "" && bc.Build == s.LatestBuild {
			tag = `<em class="tag ok">最新</em>`
		} else if s.LatestBuild != "" {
			old += bc.Testers
		}
		withBuild += bc.Testers
		b.WriteString(`<li><span class="who">第 ` + esc(bc.Build) + ` 版` + tag + `</span><span class="cnt">` + n(bc.Testers) + ` 人</span></li>`)
	}
	if len(s.Builds) == 0 {
		b.WriteString(`<li>还没有人装好</li>`)
	}
	b.WriteString(`</ul>
`)
	var bnotes []string
	if old > 0 {
		bnotes = append(bnotes, n(old)+` 人还在用旧版本`)
	}
	if miss := s.Installed - withBuild; miss > 0 {
		bnotes = append(bnotes, n(miss)+` 人苹果没给出版本`)
	}
	if len(bnotes) > 0 {
		b.WriteString(`<p class="small">` + strings.Join(bnotes, `，`) + `。</p>
`)
	}

	// Groups.
	b.WriteString(`<h3>分组</h3>
<table><thead><tr><th></th><th>人数</th><th>公开链接名额</th></tr></thead><tbody>`)
	var views []string
	for _, g := range s.Groups {
		name := esc(g.Name)
		if g.Internal {
			name += `（内部）`
		}
		quota := "—"
		if g.PublicLink {
			if g.LimitOn && g.Limit > 0 {
				quota = n(g.Testers) + ` / ` + n(g.Limit)
			} else {
				quota = n(g.Testers) + `（不限）`
			}
			if g.LinkMetrics {
				views = append(views, esc(g.Name)+` `+n(g.LinkViews)+` 次`)
			}
		} else if !g.Internal {
			quota = `链接没开`
		}
		b.WriteString(`<tr><th>` + name + `</th><td>` + n(g.Testers) + `</td><td>` + quota + `</td></tr>`)
	}
	b.WriteString(`</tbody></table>
`)
	if len(views) > 0 {
		b.WriteString(`<p class="small">公开链接近 30 天被打开：` + strings.Join(views, `，`) + `。</p>
`)
	}

	// Usage.
	if s.Usage7 != nil || s.Usage30 != nil {
		u := func(p *asc.Usage, f func(*asc.Usage) int) string {
			if p == nil {
				return "—"
			}
			return n(f(p))
		}
		row := func(label string, f func(*asc.Usage) int) string {
			return `<tr><th>` + label + `</th><td>` + u(s.Usage7, f) + `</td><td>` + u(s.Usage30, f) + `</td></tr>`
		}
		b.WriteString(`<h3>使用情况</h3>
<table><thead><tr><th></th><th>近 7 天</th><th>近 30 天</th></tr></thead><tbody>` +
			row("使用次数", func(x *asc.Usage) int { return x.Sessions }) +
			row("用过的人", func(x *asc.Usage) int { return x.ActiveTesters }) +
			row("崩溃", func(x *asc.Usage) int { return x.Crashes }) +
			row("反馈", func(x *asc.Usage) int { return x.Feedback }) +
			`</tbody></table>
<p class="small">「使用次数」就是苹果后台的「App 使用次数」。</p>
`)
	} else {
		b.WriteString(`<p class="small">使用次数、崩溃、反馈这次没取到。</p>
`)
	}

	// Devices.
	if s.DeviceTotal > 0 {
		b.WriteString(`<h3>手机型号（共 ` + n(s.DeviceTotal) + ` 台）</h3>
<ul class="list">`)
		for _, d := range s.Devices {
			b.WriteString(`<li><span class="who">` + esc(d.Name) + `</span><span class="cnt">` + n(d.Count) + ` 台</span></li>`)
		}
		b.WriteString(`</ul>
`)
		if len(s.OSVersions) > 0 {
			parts := make([]string, 0, len(s.OSVersions))
			for _, o := range s.OSVersions {
				parts = append(parts, esc(o.Name)+` 有 `+n(o.Count)+` 台`)
			}
			b.WriteString(`<p class="small">系统版本：` + strings.Join(parts, `，`) + `。</p>
`)
		}
	}

	b.WriteString(`<p class="note">苹果数据每 10 分钟更新，可能比苹果后台晚一点。这次是 ` + fmtTime(s.FetchedAt) + ` 取的。</p>
</section>
`)
	return b.String()
}

func tileS(label, v, unit string) string {
	return `<div class="tile"><div class="num">` + v + `<small>` + unit + `</small></div><div class="lbl">` + label + `</div></div>`
}
