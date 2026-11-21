//go:build !nochat

package chat

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// limiter keeps a service's own rate limits: the waits its answers name.
// Discord says, in each answer's X-RateLimit headers, how many more calls a
// route's bucket takes and when it fills again; both services answer a call
// too many with a 429 and how long to wait. A call waits its turn, up to
// maxWait, rather than be refused.
type limiter struct {
	mu     sync.Mutex
	until  map[string]time.Time // a route's next free moment, by route
	global time.Time            // every route's, after a global 429
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
}

func newLimiter() *limiter {
	return &limiter{until: map[string]time.Time{}, now: time.Now, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// wait waits until route may be called, or says how long that would be
// when it is longer than maxWait.
func (l *limiter) wait(ctx context.Context, route string) (time.Duration, error) {
	l.mu.Lock()
	at := l.until[route]
	if l.global.After(at) {
		at = l.global
	}
	l.mu.Unlock()
	d := at.Sub(l.now())
	if d <= 0 {
		return 0, nil
	}
	if d > maxWait {
		return d, nil
	}
	return 0, l.sleep(ctx, d)
}

// hold keeps route, or every route when global, from being called for d.
func (l *limiter) hold(route string, d time.Duration, global bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	at := l.now().Add(d)
	if global {
		if at.After(l.global) {
			l.global = at
		}
		return
	}
	if at.After(l.until[route]) {
		l.until[route] = at
	}
}

// heard takes what Discord's headers say of route's bucket: with none of
// it left, the route waits until the bucket fills again.
func (l *limiter) heard(route string, h http.Header) {
	if h.Get("X-RateLimit-Remaining") != "0" {
		return
	}
	if d, ok := seconds(h.Get("X-RateLimit-Reset-After")); ok {
		l.hold(route, d, false)
	}
}

// seconds reads a header's seconds, which may have decimals.
func seconds(s string) (time.Duration, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 || f > 24*60*60 {
		return 0, false
	}
	return time.Duration(f * float64(time.Second)), true
}

// said is how long to wait, in words: "12 seconds", "3 minutes".
func said(d time.Duration) string {
	switch s := int(d.Round(time.Second) / time.Second); {
	case s < 2:
		return "a second"
	case s < 120:
		return strconv.Itoa(s) + " seconds"
	default:
		return strconv.Itoa((s+30)/60) + " minutes"
	}
}
