package ingest

import (
	"net/http"
	"sync"
	"time"
)

const (
	defaultRequestsPerSecond  = 1
	defaultRequestBurst       = 10
	defaultConcurrentRequests = 8
)

type requestLimiter struct {
	mu            sync.Mutex
	now           func() time.Time
	rate          float64
	burst         float64
	tokens        float64
	last          time.Time
	inFlight      int
	maxConcurrent int
}

// RateLimit bounds work performed by the public ingest endpoint independently
// of any reverse proxy. A small burst accommodates simultaneous regional probes.
func RateLimit(next http.Handler) http.Handler {
	return newRateLimitedHandler(next, time.Now, defaultRequestsPerSecond, defaultRequestBurst, defaultConcurrentRequests)
}

func newRateLimitedHandler(next http.Handler, now func() time.Time, rate float64, burst, maxConcurrent int) http.Handler {
	limiter := &requestLimiter{
		now:           now,
		rate:          rate,
		burst:         float64(burst),
		tokens:        float64(burst),
		last:          now(),
		maxConcurrent: maxConcurrent,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.acquire() {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		defer limiter.release()
		next.ServeHTTP(w, r)
	})
}

func (limiter *requestLimiter) acquire() bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	if elapsed := now.Sub(limiter.last).Seconds(); elapsed > 0 {
		limiter.tokens += elapsed * limiter.rate
		if limiter.tokens > limiter.burst {
			limiter.tokens = limiter.burst
		}
		limiter.last = now
	}
	if limiter.inFlight >= limiter.maxConcurrent || limiter.tokens < 1 {
		return false
	}
	limiter.tokens--
	limiter.inFlight++
	return true
}

func (limiter *requestLimiter) release() {
	limiter.mu.Lock()
	limiter.inFlight--
	limiter.mu.Unlock()
}
