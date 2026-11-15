package httpmiddleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
)

// SecurityHeaders applies conservative browser security headers to API responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// CORS permits only explicitly configured browser origins. Empty origins are
// allowed for server-to-server requests; unknown browser origins are denied.
func CORS(origins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := allowed[origin]; !ok {
					http.Error(w, "origin not allowed", http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions && origin != "" {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID, X-User-ID, X-Guest-Name")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// DrainController provides graceful cancellation for long-lived HTTP streams.
// BeginDrain cancels SSE request contexts so server.Shutdown can finish cleanly.
type DrainController struct {
	mu       sync.Mutex
	draining bool
	nextID   uint64
	streams  map[uint64]context.CancelFunc
}

func NewDrainController() *DrainController {
	return &DrainController{streams: make(map[uint64]context.CancelFunc)}
}

func (d *DrainController) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		d.mu.Lock()
		if d.draining {
			d.mu.Unlock()
			cancel()
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			return
		}
		d.nextID++
		streamID := d.nextID
		d.streams[streamID] = cancel
		d.mu.Unlock()
		defer func() {
			d.mu.Lock()
			delete(d.streams, streamID)
			d.mu.Unlock()
			cancel()
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (d *DrainController) BeginDrain() {
	d.mu.Lock()
	d.draining = true
	streams := make([]context.CancelFunc, 0, len(d.streams))
	for _, cancel := range d.streams {
		streams = append(streams, cancel)
	}
	d.mu.Unlock()
	for _, cancel := range streams {
		cancel()
	}
}
