package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
	max     int
	window  time.Duration
	logger  *slog.Logger
}

type rateEntry struct {
	count       int
	windowStart time.Time
}

func NewRateLimiter(max int, window time.Duration, logger *slog.Logger) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rateEntry),
		max:     max,
		window:  window,
		logger:  logger,
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := extractBearerToken(r)
		if key == "" {
			key = r.RemoteAddr
		}
		if !rl.allow(key) {
			rl.logger.Warn("rate limit exceeded", "key", key[:min(len(key), 8)]+"...")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	entry, ok := rl.entries[key]
	if !ok || now.Sub(entry.windowStart) > rl.window {
		rl.entries[key] = &rateEntry{count: 1, windowStart: now}
		return true
	}
	if entry.count >= rl.max {
		return false
	}
	entry.count++
	return true
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for k, e := range rl.entries {
			if now.Sub(e.windowStart) > rl.window*2 {
				delete(rl.entries, k)
			}
		}
		rl.mu.Unlock()
	}
}
