package httpmiddleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSecurityHeaders(t *testing.T) {
	response := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	for key, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
	} {
		if got := response.Header().Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestCORSAllowlistAndPreflight(t *testing.T) {
	handler := CORS([]string{"https://app.example", "https://admin.example"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	allowed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/api", nil)
	request.Header.Set("Origin", "https://app.example")
	handler.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusNoContent || allowed.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("unexpected preflight response: status=%d headers=%v", allowed.Code, allowed.Header())
	}
	denied := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api", nil)
	request.Header.Set("Origin", "https://attacker.example")
	handler.ServeHTTP(denied, request)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unlisted origin status=%d, want 403", denied.Code)
	}
}

func TestDrainCancelsSSEAndRejectsNewStreams(t *testing.T) {
	drain := NewDrainController()
	started := make(chan struct{})
	finished := make(chan struct{})
	handler := drain.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(finished)
		w.WriteHeader(http.StatusOK)
	}))
	request := httptest.NewRequest(http.MethodGet, "/stream", nil)
	request.Header.Set("Accept", "text/event-stream")
	go handler.ServeHTTP(httptest.NewRecorder(), request)
	<-started
	drain.BeginDrain()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("SSE stream context was not canceled")
	}
	late := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(context.Background())
	request.Header.Set("Accept", "text/event-stream")
	handler.ServeHTTP(late, request)
	if late.Code != http.StatusServiceUnavailable {
		t.Fatalf("new stream during drain status=%d, want 503", late.Code)
	}
}

func TestRateLimitClasses(t *testing.T) {
	cases := []struct {
		method, path, bucket string
		limit                int
	}{
		{http.MethodPost, "/api/v1/auth/login", "auth", 10},
		{http.MethodPost, "/api/v1/orders/", "checkout", 8},
		{http.MethodPost, "/api/v1/payments/webhook/chapa", "webhook", 30},
		{http.MethodGet, "/api/v1/restaurants", "standard", 120},
	}
	for _, tc := range cases {
		limit, _, bucket := rateLimitFor(tc.method, tc.path)
		if limit != tc.limit || bucket != tc.bucket {
			t.Errorf("rateLimitFor(%s %s) = %d/%s, want %d/%s", tc.method, tc.path, limit, bucket, tc.limit, tc.bucket)
		}
	}
}
