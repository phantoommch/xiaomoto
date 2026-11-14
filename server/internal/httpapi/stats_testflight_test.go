package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"motocam/server/internal/asc"
)

type fakeApple struct {
	mu    sync.Mutex
	sum   asc.Summary
	err   error
	calls int
}

func (f *fakeApple) Fetch(context.Context) (asc.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.sum, f.err
}

func sampleSummary() asc.Summary {
	return asc.Summary{
		Testers: 12, Installed: 11, Accepted: 1,
		Builds:      []asc.BuildCount{{Build: "34", Version: "1.0", Testers: 10}, {Build: "28", Version: "1.0", Testers: 1}},
		LatestBuild: "34",
		Groups: []asc.Group{
			{Name: "1组", Testers: 10, PublicLink: true, LimitOn: true, Limit: 10, LinkViews: 11, LinkJoined: 10, LinkMetrics: true},
			{Name: "公开测试", Testers: 2, PublicLink: true, LimitOn: true, Limit: 500, LinkMetrics: true},
			{Name: "Xmoto", Internal: true},
		},
		Usage7:      &asc.Usage{Sessions: 143, ActiveTesters: 9},
		Usage30:     &asc.Usage{Sessions: 150, Crashes: 2, Feedback: 1, ActiveTesters: 10},
		Devices:     []asc.NameCount{{Name: "iPhone 16 Pro Max", Count: 3}, {Name: "iPhone <b>", Count: 1}},
		OSVersions:  []asc.NameCount{{Name: "iOS 27", Count: 7}, {Name: "iOS 18", Count: 2}},
		DeviceTotal: 11,
		Review:      &asc.Review{Version: "1.0", Build: "34", State: "WAITING_FOR_REVIEW"},
	}
}

func adminLoggedIn(t *testing.T, cache *asc.Cache) (*statsEnv, map[string]string) {
	t.Helper()
	const pw = "tf-test-password"
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	e := newStatsEnv(t, HandlerOptions{AdminPasswordHash: string(hash), AdminHost: "dl.xiaomoto.cloud", TestFlight: cache})
	hdr := map[string]string{"Host": "dl.xiaomoto.cloud", "Origin": "https://dl.xiaomoto.cloud", "X-Forwarded-For": "203.0.113.9"}
	r := e.do(http.MethodPost, "/admin/login", hdr, url.Values{"password": {pw}})
	if r.Code != http.StatusSeeOther {
		t.Fatalf("login %d", r.Code)
	}
	return e, map[string]string{"Host": "dl.xiaomoto.cloud", "Cookie": adminCookie + "=" + r.Result().Cookies()[0].Value}
}

func TestStatsPageTestFlightSection(t *testing.T) {
	fa := &fakeApple{sum: sampleSummary()}
	cache := asc.NewCache(fa)
	now := time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC) // 12:00 北京时间
	cache.SetClock(func() time.Time { return now })
	e, sess := adminLoggedIn(t, cache)

	body := e.do(http.MethodGet, "/admin", sess, nil).Body.String()
	for _, want := range []string{
		"iPhone 测试版（TestFlight）",
		`<span class="num">12</span> 人 <span class="lbl">测试人数`,
		`App Store 上架审核：1.0（第 34 版） <b class="st">等待审核</b>`,
		`<div class="num">11<small>人</small></div><div class="lbl">已安装`,
		`<div class="num">1<small>人</small></div><div class="lbl">已接受、还没安装`,
		`<div class="num">9<small>人</small></div><div class="lbl">近 7 天用过的人`,
		`<div class="num">143<small>次</small></div><div class="lbl">近 7 天使用次数`,
		`第 34 版<em class="tag ok">最新</em></span><span class="cnt">10 人`,
		`第 28 版</span><span class="cnt">1 人`,
		"1 人还在用旧版本",
		`<tr><th>1组</th><td>10</td><td>10 / 10</td></tr>`,
		`<tr><th>公开测试</th><td>2</td><td>2 / 500</td></tr>`,
		`<tr><th>Xmoto（内部）</th><td>0</td><td>—</td></tr>`,
		"公开链接近 30 天被打开：1组 11 次，公开测试 0 次。",
		`<tr><th>使用次数</th><td>143</td><td>150</td></tr>`,
		`<tr><th>崩溃</th><td>0</td><td>2</td></tr>`,
		`<tr><th>反馈</th><td>0</td><td>1</td></tr>`,
		"手机型号（共 11 台）",
		`iPhone 16 Pro Max</span><span class="cnt">3 台`,
		"iPhone &lt;b&gt;",
		"系统版本：iOS 27 有 7 台，iOS 18 有 2 台。",
		"苹果数据每 10 分钟更新，可能比苹果后台晚一点。这次是 2026-10-04 12:00 取的。",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(body, "未配置") || strings.Contains(body, "暂时取不到") || strings.Contains(body, "https://") {
		t.Fatal("unexpected text")
	}
	// Second view within 10 minutes uses the cache.
	e.do(http.MethodGet, "/admin", sess, nil)
	if fa.calls != 1 {
		t.Fatalf("apple called %d times", fa.calls)
	}

	// Apple fails after the cache expires: old numbers + notice.
	fa.mu.Lock()
	fa.err = errors.New("boom")
	fa.mu.Unlock()
	now = now.Add(11 * time.Minute)
	body = e.do(http.MethodGet, "/admin", sess, nil).Body.String()
	if !strings.Contains(body, "暂时取不到苹果数据，下面是 2026-10-04 12:00 取到的数据。") || !strings.Contains(body, `<span class="num">12</span> 人 <span class="lbl">测试人数`) {
		t.Fatal("stale data notice")
	}
	if !strings.Contains(body, "注册总人数") {
		t.Fatal("rest of page missing")
	}
}

func TestStatsPageTestFlightNeverFetched(t *testing.T) {
	e, sess := adminLoggedIn(t, asc.NewCache(&fakeApple{err: errors.New("down")}))
	r := e.do(http.MethodGet, "/admin", sess, nil)
	body := r.Body.String()
	if r.Code != 200 || !strings.Contains(body, "暂时取不到苹果数据，请稍后刷新。") || !strings.Contains(body, "注册总人数") {
		t.Fatalf("never fetched %d", r.Code)
	}
}

func TestTestFlightNotOnLoginPage(t *testing.T) {
	fa := &fakeApple{sum: sampleSummary()}
	e, _ := adminLoggedIn(t, asc.NewCache(fa))
	body := e.do(http.MethodGet, "/admin", map[string]string{"Host": "dl.xiaomoto.cloud"}, nil).Body.String()
	if strings.Contains(body, "TestFlight") || fa.calls != 0 {
		t.Fatal("TestFlight data shown or fetched without login")
	}
}
