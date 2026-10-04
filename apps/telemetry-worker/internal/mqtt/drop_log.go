package mqtt

import (
	"sync"
	"time"
)

// dropLogLimiter lets a dropped-message warning through at most once per
// interval per key, so an unknown tenant/code pair that publishes at high
// rate cannot flood the logs. It returns the number of suppressed drops since
// the last emitted warning for that key.
type dropLogLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	maxKeys  int
	now      func() time.Time
	entries  map[string]*dropLogEntry
}

type dropLogEntry struct {
	last       time.Time
	suppressed int
}

func newDropLogLimiter(interval time.Duration) *dropLogLimiter {
	return &dropLogLimiter{
		interval: interval,
		maxKeys:  10000,
		now:      time.Now,
		entries:  make(map[string]*dropLogEntry),
	}
}

// allow reports whether to log now and how many drops were suppressed.
func (l *dropLogLimiter) allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= l.maxKeys {
			l.entries = make(map[string]*dropLogEntry)
		}
		l.entries[key] = &dropLogEntry{last: now}
		return true, 0
	}
	if now.Sub(e.last) < l.interval {
		e.suppressed++
		return false, 0
	}
	suppressed := e.suppressed
	e.last = now
	e.suppressed = 0
	return true, suppressed
}
