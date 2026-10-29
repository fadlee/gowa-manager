package users

import (
	"sync"
	"time"
)

// LoginLimiter is an in-memory fixed-window limiter for failed login
// attempts. Keys are caller-chosen (e.g. client IP + username).
type LoginLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string]limiterEntry
}

type limiterEntry struct {
	count int
	start time.Time
}

func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{max: max, window: window, now: time.Now, failures: map[string]limiterEntry{}}
}

// Allowed reports whether another attempt is permitted for key.
func (l *LoginLimiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.failures[key]
	if !ok {
		return true
	}
	if l.now().Sub(entry.start) >= l.window {
		delete(l.failures, key)
		return true
	}
	return entry.count < l.max
}

// Fail records a failed attempt for key.
func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry, ok := l.failures[key]
	if !ok || now.Sub(entry.start) >= l.window {
		entry = limiterEntry{start: now}
	}
	entry.count++
	l.failures[key] = entry
	// Bound memory: drop expired entries opportunistically.
	if len(l.failures) > 10000 {
		for k, e := range l.failures {
			if now.Sub(e.start) >= l.window {
				delete(l.failures, k)
			}
		}
	}
}

// Reset clears failures for key after a successful login.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
