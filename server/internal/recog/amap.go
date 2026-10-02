package recog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Map is the AMap web-service surface the locator needs.
type Map interface {
	Get(path string, query map[string]string) (map[string]any, error)
	Cities() []string
}

type Client struct {
	Key    string
	HTTP   *http.Client
	Ctx    context.Context
	Base   string // override for tests; default is the AMap REST host
	cities []string
	once   sync.Once
	cache  sync.Map
}

// AMapTimeout is the per-request limit for one Web-service call.
const AMapTimeout = 5 * time.Second

// amapSlots caps in-flight Web-service calls so a stake search cannot open
// dozens of connections at once. The slot is taken per attempt.
var amapSlots = make(chan struct{}, 4)

var (
	cityMu    sync.Mutex
	cityCache []string
)

func (c *Client) client() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: AMapTimeout}
}

func mapContext(amap Map) context.Context {
	c, ok := amap.(*Client)
	if !ok || c == nil || c.Ctx == nil {
		return context.Background()
	}
	return c.Ctx
}

func (c *Client) Get(path string, query map[string]string) (map[string]any, error) {
	if c == nil || c.Key == "" {
		return nil, fmt.Errorf("no amap key")
	}
	cacheKey := path + "?" + sortedQuery(query)
	if v, ok := c.cache.Load(cacheKey); ok {
		return v.(map[string]any), nil
	}
	out, err := c.getAttempt(path, query)
	if err == nil {
		if str(out["status"]) == "1" {
			c.cache.Store(cacheKey, out)
		}
		return out, nil
	}
	if !retryableTimeout(err) || (c.Ctx != nil && c.Ctx.Err() != nil) {
		return nil, err
	}
	out, err = c.getAttempt(path, query)
	if err == nil && str(out["status"]) == "1" {
		c.cache.Store(cacheKey, out)
	}
	return out, err
}

func retryableTimeout(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func (c *Client) getAttempt(path string, query map[string]string) (map[string]any, error) {
	ctx := c.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case amapSlots <- struct{}{}:
		defer func() { <-amapSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base := "https://restapi.amap.com/"
	if c.Base != "" {
		base = c.Base
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("key", c.Key)
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "XiaoMoto/32")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sortedQuery(query map[string]string) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(query[k])
		b.WriteByte('&')
	}
	return b.String()
}

func (c *Client) Cities() []string {
	if c == nil {
		return nil
	}
	cityMu.Lock()
	if len(cityCache) > 0 {
		out := cityCache
		cityMu.Unlock()
		return out
	}
	cityMu.Unlock()
	c.once.Do(func() {
		d, err := c.Get("v3/config/district", map[string]string{
			"keywords": "中国", "subdistrict": "2", "extensions": "base",
		})
		if err != nil || d == nil {
			return
		}
		root, _ := d["districts"].([]any)
		if len(root) == 0 {
			return
		}
		top, _ := root[0].(map[string]any)
		provs, _ := top["districts"].([]any)
		for _, pv := range provs {
			pm, _ := pv.(map[string]any)
			cities, _ := pm["districts"].([]any)
			for _, city := range cities {
				cm, _ := city.(map[string]any)
				name, _ := cm["name"].(string)
				core := trimAdminSuffix(name)
				if len([]rune(core)) >= 2 && !provOK[core] {
					c.cities = append(c.cities, core)
				}
			}
		}
	})
	if len(c.cities) > 0 {
		cityMu.Lock()
		cityCache = append([]string(nil), c.cities...)
		cityMu.Unlock()
	}
	return c.cities
}

func trimAdminSuffix(name string) string {
	for _, suf := range []string{"自治州", "地区", "市", "盟"} {
		if len(name) > len(suf) && stringsHasSuffix(name, suf) {
			return name[:len(name)-len(suf)]
		}
	}
	return name
}

func stringsHasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
