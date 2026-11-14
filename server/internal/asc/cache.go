package asc

import (
	"context"
	"log"
	"sync"
	"time"
)

const cacheTTL = 10 * time.Minute

// Source loads one TestFlight snapshot. Tests supply a fake; production uses Client.
type Source interface {
	Fetch(context.Context) (Summary, error)
}

// Result is what the admin page renders. A nil Cache is not configured.
type Result struct {
	Configured bool
	Summary    *Summary
	Failed     bool
}

// Cache keeps the last successful snapshot for cacheTTL.
type Cache struct {
	src    Source
	mu     sync.Mutex
	now    func() time.Time
	last   Summary
	has    bool
	failed bool
	at     time.Time
}

func NewCache(src Source) *Cache {
	return &Cache{src: src, now: time.Now}
}

// SetClock overrides the clock used for freshness and FetchedAt. Tests only.
func (c *Cache) SetClock(now func() time.Time) {
	if c == nil || now == nil {
		return
	}
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

// Get returns an unconfigured result when c or its source is nil, so a missing
// ASC_* setup never panics and the page can say 未配置.
func (c *Cache) Get(ctx context.Context) Result {
	if c == nil || c.src == nil {
		return Result{}
	}
	c.mu.Lock()
	now := c.now()
	if !c.at.IsZero() && now.Sub(c.at) < cacheTTL {
		res := c.snapshot()
		c.mu.Unlock()
		return res
	}
	src := c.src
	c.mu.Unlock()

	sum, err := src.Fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.now()
	if err != nil {
		log.Printf("App Store Connect 读取失败: %v", err)
		c.failed = true
		return c.snapshot()
	}
	sum.FetchedAt = c.now()
	c.last = sum
	c.has = true
	c.failed = false
	return c.snapshot()
}

func (c *Cache) snapshot() Result {
	res := Result{Configured: true, Failed: c.failed}
	if c.has {
		cp := c.last
		res.Summary = &cp
	}
	return res
}
