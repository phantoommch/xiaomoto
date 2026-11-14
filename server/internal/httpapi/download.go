package httpapi

import (
	_ "embed"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"motocam/server/internal/store"
)

// Public download page. The URL stays the same across releases:
//   GET /download          page (no login)
//   GET /download/android  302 to the newest Android package in the catalog
//   GET /download/icon.png app icon

const testFlightURL = "https://testflight.apple.com/join/YAFXPPtM"
const testFlightAppURL = "https://apps.apple.com/cn/app/testflight/id899247664"

//go:embed download_icon.png
var downloadIcon []byte

func (s *Server) downloadAndroid(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.apps != nil {
		if rel, ok := s.apps.Latest("android"); ok {
			http.Redirect(w, r, "/v1/app/download/android/"+url.PathEscape(rel.File), http.StatusFound)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(downloadDocument(false, false, `<p class="card">安卓版还没有发布。<a href="/download">返回下载页</a></p>`)))
}

func (s *Server) downloadIconPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(downloadIcon)
}

func (s *Server) downloadSlash(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/download", http.StatusMovedPermanently)
}

// downloadIOS counts a TestFlight button tap, then sends the phone on.
func (s *Server) downloadIOS(w http.ResponseWriter, r *http.Request) {
	s.countDownload(r, store.DownloadTestFlight)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, testFlightURL, http.StatusFound)
}

func (s *Server) downloadPage(w http.ResponseWriter, r *http.Request) {
	s.countDownload(r, store.DownloadPageView)
	ua := r.Header.Get("User-Agent")
	wechat := strings.Contains(ua, "MicroMessenger")
	ios := strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod")

	version := "暂未发布"
	ready := false
	if s.apps != nil {
		if rel, ok := s.apps.Latest("android"); ok {
			ready = true
			version = fmt.Sprintf("第 %d 版", rel.VersionCode)
			if rel.Size > 0 {
				version += fmt.Sprintf(" · %.0f MB", float64(rel.Size)/(1<<20))
			}
			if t, err := time.Parse(time.RFC3339, rel.PublishedAt); err == nil {
				version += " · " + t.In(shanghai).Format("2006-01-02") + " 更新"
			}
		}
	}

	android := `<section class="card" id="android">
<h2>安卓手机</h2>
`
	if ready {
		android += `<a class="btn" id="apk" href="/download/android">下载安卓版</a>
`
	} else {
		android += `<p class="btn off">安卓版即将发布</p>
`
	}
	android += `<p class="ver">当前版本：` + html.EscapeString(version) + `</p>
<ol>
<li>下载完成后，点开安装包。</li>
<li>如果提示「禁止安装未知来源应用」，按提示允许浏览器（或文件管理）安装应用，再返回安装。</li>
<li>荣耀、华为手机如果弹出风险提醒，选「继续安装」。</li>
</ol>
<p class="small">以后有新版本，App 里会提示更新；这个页面也总是最新版。</p>
</section>
`
	iphone := `<section class="card" id="iphone">
<h2>iPhone</h2>
<ol>
<li>先在 App Store 安装苹果官方的「<a href="` + testFlightAppURL + `">TestFlight</a>」。</li>
<li>再点下面的按钮，在 TestFlight 里点「接受」和「安装」。</li>
</ol>
<a class="btn" id="tf" href="/download/ios">加入 iPhone 测试版</a>
<p class="small">App Store 正式版即将上线。</p>
</section>
`
	body := ""
	if ios {
		body = iphone + android
	} else {
		body = android + iphone
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Vary", "User-Agent")
	writeHTML(w, downloadDocument(wechat, true, body))
}

var shanghai = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()

func downloadDocument(wechat, intro bool, body string) string {
	htmlClass := ""
	if wechat {
		htmlClass = ` class="wx"`
	}
	head := ""
	if intro {
		head = `<p class="lead">摩托车骑行安全提醒：地图上标出禁摩收费站、禁摩路段和抓拍点，骑行时提前语音提醒。</p>
`
	}
	return `<!DOCTYPE html>
<html lang="zh-CN"` + htmlClass + `>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>下载 · 小moto安全驾驶指南</title>
<link rel="icon" href="/download/icon.png">
<link rel="apple-touch-icon" href="/download/icon.png">
<script>if(/MicroMessenger/i.test(navigator.userAgent))document.documentElement.className+=" wx"</script>
<style>
body{margin:0;padding:1rem;font:16px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Noto Sans SC","Microsoft YaHei",sans-serif;color:#1c1917;background:#f7f4ef}
main{max-width:30rem;margin:0 auto}
header{display:flex;align-items:center;gap:.9rem;margin:.5rem 0 .6rem}
header img{width:64px;height:64px;border-radius:14px;box-shadow:0 1px 4px rgba(0,0,0,.15)}
h1{font-size:1.35rem;line-height:1.3;margin:0}
h2{font-size:1.15rem;margin:0 0 .6rem}
.lead{margin:.2rem 0 1rem;color:#44403c}
.card{background:#fff;border-radius:14px;padding:1rem 1.1rem;margin:0 0 1rem;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.btn{display:block;text-align:center;background:#c2410c;color:#fff;text-decoration:none;font-weight:600;font-size:1.1rem;padding:.8rem;border-radius:10px;margin:.3rem 0 .5rem}
.btn.off{background:#a8a29e}
.ver{margin:0 0 .5rem;color:#57534e;font-size:.95rem}
ol{padding-left:1.25rem;margin:.4rem 0}
li{margin:.25rem 0}
.small{font-size:.9rem;color:#78716c;margin:.5rem 0 0}
a{color:#9a3412}
.wxtip{display:none;position:sticky;top:0;z-index:9;background:#1c1917;color:#fff;padding:.8rem 1rem;margin:-1rem -1rem 1rem;font-size:1.05rem;line-height:1.5}
.wx .wxtip{display:block}
.wxtip b{color:#fdba74}
footer{text-align:center;font-size:.9rem;color:#78716c;margin:1.5rem 0 .5rem}
</style>
</head>
<body>
<div class="wxtip">微信里不能下载安装包。请<b>点右上角「···」</b>，选<b>「在浏览器打开」</b>，再下载。</div>
<main>
<header><img src="/download/icon.png" alt=""><h1>小moto安全驾驶指南</h1></header>
` + head + body + `<footer><a href="/privacy">隐私政策</a> · <a href="/terms">用户协议</a> · <a href="/support">支持</a></footer>
</main>
</body>
</html>
`
}
