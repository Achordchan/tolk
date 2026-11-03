package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// routes lists the ways to reach u: itself, then through each mirror when u
// is under the origin they proxy.
func (c *Checker) routes(u string) []string {
	out := []string{u}
	if c.Origin == "" || !strings.HasPrefix(u, c.Origin) {
		return out
	}
	for _, m := range c.Mirrors {
		if m != "" {
			out = append(out, m+u)
		}
	}
	return out
}

// routeName is what the log and the About pane call a route.
func routeName(u, origin string) string {
	if origin != "" && strings.HasPrefix(u, origin) {
		return "GitHub"
	}
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return "中转 " + p.Host
	}
	return u
}

type raceResult struct {
	url  string
	body []byte
	err  error
}

// race asks every url at once. An answer counts only if check (when set)
// accepts it: a proxy may answer 200 with an error or challenge page. It
// returns once all have answered, or grace after the first good answer, with
// the failures and the good answers so far; the rest are cancelled.
func (c *Checker) race(ctx context.Context, urls []string, limit int64, grace time.Duration, check func([]byte) error) []raceResult {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan raceResult, len(urls))
	for _, u := range urls {
		go func(u string) {
			b, err := c.get(ctx, u, limit)
			if err == nil && check != nil {
				err = check(b)
			}
			ch <- raceResult{u, b, err}
		}(u)
	}
	var out []raceResult
	var deadline <-chan time.Time
	for range urls {
		select {
		case r := <-ch:
			out = append(out, r)
			if r.err == nil && deadline == nil {
				if grace <= 0 {
					return out
				}
				deadline = time.After(grace)
			}
		case <-deadline:
			return out
		}
	}
	return out
}

// fetch returns the first answer for u over any route that check accepts.
func (c *Checker) fetch(ctx context.Context, u string, limit int64, check func([]byte) error) ([]byte, error) {
	var errs []string
	for _, r := range c.race(ctx, c.routes(u), limit, 0, check) {
		if r.err == nil {
			return r.body, nil
		}
		errs = append(errs, fmt.Sprintf("%s：%v", routeName(r.url, c.Origin), r.err))
	}
	return nil, errors.New(strings.Join(errs, "；"))
}

// probeBytes is how much of the package each route gets to show its speed.
const probeBytes = 256 << 10

// probeTimeout bounds the speed test; a route that has not delivered the
// sample by then is tried last.
var probeTimeout = 8 * time.Second

// rank orders the routes to u by how fast each delivers the first part of
// it. Routes that fail the test stay at the end, in their original order, as
// a last resort.
func (c *Checker) rank(ctx context.Context, urls []string) []string {
	if len(urls) < 2 {
		return urls
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	speed := make([]float64, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			speed[i] = c.probe(ctx, u)
		}(i, u)
	}
	wg.Wait()
	idx := make([]int, len(urls))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return speed[idx[a]] > speed[idx[b]] })
	out := make([]string, len(urls))
	for i, j := range idx {
		out[i] = urls[j]
		if speed[j] > 0 {
			c.logf("线路测速：%s %.0f KB/s", routeName(urls[j], c.Origin), speed[j]/1024)
		} else {
			c.logf("线路测速：%s 不通", routeName(urls[j], c.Origin))
		}
	}
	return out
}

// probe downloads the start of u and returns bytes per second, or 0 when the
// route fails.
func (c *Checker) probe(ctx context.Context, u string) float64 {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", probeBytes-1))
	client := *c.Client
	client.Timeout = 0 // ctx bounds it
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0
	}
	// A slow route that runs out of time still counts, at its speed.
	n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, probeBytes))
	if n == 0 {
		return 0
	}
	return float64(n) / max(time.Since(start).Seconds(), 1e-3)
}
