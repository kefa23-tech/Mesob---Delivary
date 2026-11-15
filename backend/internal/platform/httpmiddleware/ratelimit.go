package httpmiddleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local member = ARGV[3]
local limit = tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
  return 0
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, window)
return 1
`)

// RateLimiter uses Redis sorted sets and an atomic Lua script for a true
// sliding-window limit shared by all API replicas.
type RateLimiter struct {
	client *redis.Client
	seq    atomic.Uint64
}

func NewRateLimiter(redisURL string) (*RateLimiter, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse rate limiter redis URL: %w", err)
	}
	return &RateLimiter{client: redis.NewClient(options)}, nil
}

func (l *RateLimiter) Close() error { return l.client.Close() }

func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit, window, bucket := rateLimitFor(r.Method, r.URL.Path)
		clientIP := requestIP(r)
		now := time.Now()
		key := "mesob:ratelimit:" + bucket + ":" + clientIP
		member := fmt.Sprintf("%d-%d", now.UnixNano(), l.seq.Add(1))
		allowed, err := slidingWindowScript.Run(r.Context(), l.client, []string{key}, now.UnixMilli(), window.Milliseconds(), member, limit).Int()
		if err != nil {
			http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
			return
		}
		if allowed == 0 {
			w.Header().Set("Retry-After", fmt.Sprint(max(1, int(window.Seconds()))))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func rateLimitFor(method, path string) (int, time.Duration, string) {
	switch {
	case strings.HasPrefix(path, "/api/v1/auth/"):
		return 10, time.Minute, "auth"
	case strings.Contains(path, "/payments/webhook/") || strings.HasSuffix(path, "/payments/webhook"):
		return 30, time.Minute, "webhook"
	case strings.HasPrefix(path, "/api/v1/orders") && method == http.MethodPost:
		return 8, time.Minute, "checkout"
	default:
		return 120, time.Minute, "standard"
	}
}

func requestIP(r *http.Request) string {
	// X-Real-IP is set by the trusted Caddy ingress; direct connections fall
	// back to RemoteAddr. Caddy is the only published ingress in prod compose.
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
		return ip.String()
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return "unknown"
}
