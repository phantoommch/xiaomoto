package httpapi

import (
	"html"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
)

// defaultContactEmail is a placeholder. Set CONTACT_EMAIL in the server
// environment to the address shown on /privacy, /terms and /support.
const defaultContactEmail = "contact@example.com"

const contactToken = "{{CONTACT_EMAIL}}"

func contactEmail() string {
	if v := strings.TrimSpace(os.Getenv("CONTACT_EMAIL")); v != "" {
		return v
	}
	return defaultContactEmail
}

func withContact(body string) string {
	return strings.ReplaceAll(body, contactToken, html.EscapeString(contactEmail()))
}

func (s *Server) privacyPage(w http.ResponseWriter, r *http.Request) {
	writeHTML(w, publicDocument("隐私政策", withContact(privacyBody)))
}

func (s *Server) termsPage(w http.ResponseWriter, r *http.Request) {
	doc := s.currentTerms()
	title := doc.Title
	if title == "" {
		title = "用户协议与免责声明"
	}
	body := "<p>以下与应用内《用户协议与免责声明》是同一份文本。</p>\n" + markdownHTML(doc.Markdown) +
		withContact("<p>联系邮箱：<a href=\"mailto:"+contactToken+"\">"+contactToken+"</a></p>\n")
	writeHTML(w, publicDocument(title, body))
}

func (s *Server) supportPage(w http.ResponseWriter, r *http.Request) {
	writeHTML(w, publicDocument("支持", withContact(supportBody)))
}

func writeHTML(w http.ResponseWriter, page string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

func publicDocument(title, body string) string {
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + ` · 小moto安全驾驶指南</title>
<style>
body{margin:0;padding:1.25rem;font:16px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Noto Sans SC",sans-serif;color:#1c1917;background:#f7f4ef}
main{max-width:40rem;margin:0 auto}
nav{margin:0 0 1.25rem;font-size:.95rem}
a{color:#9a3412}
h1{font-size:1.45rem;line-height:1.3;margin:.2rem 0 1rem}
h2{font-size:1.15rem;margin:1.4rem 0 .4rem}
p,li{margin:.4rem 0}
ol{padding-left:1.2rem}
</style>
</head>
<body>
<main>
<nav><a href="/privacy">隐私政策</a> · <a href="/terms">用户协议</a> · <a href="/support">支持</a></nav>
` + body + `
</main>
</body>
</html>
`
}

func markdownHTML(markdown string) string {
	var b strings.Builder
	list := false
	closeList := func() {
		if list {
			b.WriteString("</ol>\n")
			list = false
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			closeList()
		case strings.HasPrefix(trim, "# "):
			closeList()
			b.WriteString("<h1>" + html.EscapeString(strings.TrimPrefix(trim, "# ")) + "</h1>\n")
		case strings.HasPrefix(trim, "## "):
			closeList()
			b.WriteString("<h2>" + html.EscapeString(strings.TrimPrefix(trim, "## ")) + "</h2>\n")
		default:
			if item, ok := numberedItem(trim); ok {
				if !list {
					b.WriteString("<ol>\n")
					list = true
				}
				b.WriteString("<li>" + html.EscapeString(item) + "</li>\n")
				continue
			}
			closeList()
			b.WriteString("<p>" + html.EscapeString(trim) + "</p>\n")
		}
	}
	closeList()
	return b.String()
}

func numberedItem(line string) (string, bool) {
	i := 0
	for i < len(line) {
		r, size := utf8.DecodeRuneInString(line[i:])
		if r < '0' || r > '9' {
			break
		}
		i += size
	}
	if i == 0 || !strings.HasPrefix(line[i:], ". ") {
		return "", false
	}
	return strings.TrimSpace(line[i+2:]), true
}

const privacyBody = `<h1>隐私政策</h1>
<p>小moto安全驾驶指南。生效日期：2026 年 10 月。联系邮箱：<a href="mailto:{{CONTACT_EMAIL}}">{{CONTACT_EMAIL}}</a>。</p>
<p>本页与应用内《用户协议与免责声明》第八条一致，也与 iOS 隐私清单一致。我们不把数据用于跨应用跟踪或广告。</p>
<h2>收集什么，为什么</h2>
<ol>
<li>邮箱。用来注册、登录和发送激活码。与账号关联，只用于应用功能。</li>
<li>你主动上传的违法截图。用来核对地点。请在上传前自行遮挡号牌。服务器会去掉图片里的 EXIF 并重新编码。应用不读取整个相册。</li>
<li>你主动提交的点位坐标和文字。经处理后作为提示展示给其他骑手。这是你选择上传的地点，不是骑行轨迹。与账号关联，只用于应用功能。隐私清单里记为粗略位置。</li>
<li>设备标识。社区请求带 <code>X-Device-Id</code>，用来区分上报来自哪台设备。与账号关联，只用于应用功能，不出售。</li>
<li>骑行时的定位。只在手机上计算和已记录点位的距离。行驶轨迹不上传，服务器不保存。</li>
</ol>
<h2>不收集的内容</h2>
<p>不收集姓名、电话、通讯录、健康、财务、浏览记录或广告标识。不做跨应用跟踪。</p>
<h2>第三方</h2>
<p>按地点名称检索时，关键词会发给高德地图的 Web 服务，用来把罚单上的地点标到地图上。不发送邮箱、设备标识或骑行轨迹。</p>
<h2>保存多久</h2>
<ol>
<li>激活码大约保存 10 分钟，用过或过期后不再使用。</li>
<li>账号、邮箱和登录凭证保留到你注销。</li>
<li>待审核的截图在做出审核决定后从服务器删除。</li>
<li>已经公开的点位会继续保留，供其他骑手查看。公开内容不含邮箱、车牌或姓名。</li>
</ol>
<h2>在应用里注销账号</h2>
<p>打开应用，进入「设置」，点「注销账号」。注销后，这台手机上的登录会被清掉，账号和登录凭证从服务器删除，关联上报的设备标识改为 deleted。已经公开的点位可能以匿名形式保留。</p>
<h2>其他</h2>
<p>除法律要求外，不向第三方提供你的个人信息，也不出售。协议更新后，应用会请你重新同意。疑问或更正请求请写到 <a href="mailto:{{CONTACT_EMAIL}}">{{CONTACT_EMAIL}}</a>。</p>
<h2>English</h2>
<p>This policy matches section 8 of the in-app user agreement and the iOS privacy manifest. The app does not track you across apps and does not use data for advertising.</p>
<p>Collected for app functionality, and linked to the account: your email address, for sign-in codes; ticket screenshots you choose to upload, with EXIF removed; the coordinates and text of a point you choose to submit, which is not your ride track; and a device id (X-Device-Id) that separates reports. GPS used while riding stays on the phone and is not uploaded.</p>
<p>Not collected: name, phone number, contacts, health, financial information, browsing history, and advertising identifiers.</p>
<p>A place-name lookup may be sent to AMap. Email, device id, and the ride track are not sent.</p>
<p>Login codes last about 10 minutes. The account is kept until you delete it. Pending screenshots are deleted after a review decision. Published points may remain so other riders can see them, without your email, plate, or name.</p>
<p>Delete the account in the app: Settings, then Delete account. The session and account are removed, and the device id on related reports is set to deleted. Published points may stay in anonymous form.</p>
<p>Contact <a href="mailto:{{CONTACT_EMAIL}}">{{CONTACT_EMAIL}}</a>.</p>
`

const supportBody = `<h1>支持</h1>
<p>小moto安全驾驶指南提醒你前方的禁摩收费站、禁摩路段和抓拍点。它不是导航，也不能代替现场标志和交警指挥。</p>
<p>联系邮箱：<a href="mailto:{{CONTACT_EMAIL}}">{{CONTACT_EMAIL}}</a></p>
<h2>常见问题</h2>
<h2>怎么登录？</h2>
<p>打开应用，进入「设置」，点「注册 / 登录」。输入本人邮箱，查收 6 位激活码，勾选《用户协议与免责声明》后完成。iPhone 打开应用时不强制登录；更新点位或上传截图时才需要账号。</p>
<h2>提醒没有响怎么办？</h2>
<p>先在「骑行」里开始骑行，并把系统定位改成「始终」，同时允许通知。只给「使用期间」时，锁屏后不会继续算距离。外圈默认大约 2 公里，内圈大约 500 米。点过「不再提醒」的点位不再出声，可以在设置里全部恢复。请在停车后再看手机。</p>
<h2>怎么注销账号？</h2>
<p>在应用的「设置」里点「注销账号」。账号和登录凭证会被删除。已经公开的点位可能匿名保留。详见 <a href="/privacy">隐私政策</a>。</p>
<h2>协议和隐私在哪里？</h2>
<p><a href="/terms">用户协议</a>与应用内《用户协议与免责声明》是同一份文本。<a href="/privacy">隐私政策</a>说明收集的信息、用途、保存时间和注销方式。</p>
`
