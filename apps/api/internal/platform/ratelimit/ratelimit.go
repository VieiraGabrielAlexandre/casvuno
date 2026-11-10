// Package ratelimit provides an in-memory keyed token bucket for a single API instance.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const idleEviction = time.Hour

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Limiter allows `burst` events per key, refilled evenly over `per`.
type Limiter struct {
	mu        sync.Mutex
	every     rate.Limit
	burst     int
	entries   map[string]*entry
	lastPrune time.Time
}

// New returns a limiter allowing burst events per period for each key.
func New(burst int, per time.Duration) *Limiter {
	return &Limiter{
		every:   rate.Every(per / time.Duration(burst)),
		burst:   burst,
		entries: make(map[string]*entry),
	}
}

// Allow reports whether one more event for key is allowed now, consuming a token if so.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastPrune) > time.Minute {
		for k, e := range l.entries {
			if now.Sub(e.lastSeen) > idleEviction {
				delete(l.entries, k)
			}
		}
		l.lastPrune = now
	}

	e, ok := l.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.every, l.burst)}
		l.entries[key] = e
	}
	e.lastSeen = now
	return e.limiter.AllowN(now, 1)
}
