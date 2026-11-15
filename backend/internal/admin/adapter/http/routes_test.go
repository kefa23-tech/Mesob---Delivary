package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mesob-delivery/backend/internal/admin/domain"
)

type adminCheckerFunc func(context.Context, string) (bool, error)

func (f adminCheckerFunc) IsActiveAdmin(ctx context.Context, id string) (bool, error) {
	return f(ctx, id)
}

type fakeDLQ struct{ retries, deletes int }

func (f *fakeDLQ) List(context.Context) ([]domain.DeadLetterTask, map[string]int64, error) {
	return []domain.DeadLetterTask{{ID: "t-1", Queue: "default", Type: "order:notify", PayloadSummary: "order_id=1", LastFailedAt: time.Now()}}, map[string]int64{"default": 2}, nil
}
func (f *fakeDLQ) Retry(_ context.Context, queue, id string) error {
	if queue != "default" || id != "t-1" {
		return errors.New("unexpected task")
	}
	f.retries++
	return nil
}
func (f *fakeDLQ) Delete(_ context.Context, queue, id string) error {
	if queue != "default" || id != "t-1" {
		return errors.New("unexpected task")
	}
	f.deletes++
	return nil
}

func TestAdminMiddlewareRequiresActiveAdmin(t *testing.T) {
	cases := []struct {
		name, user string
		allowed    bool
		want       int
	}{{name: "missing identity", want: http.StatusUnauthorized}, {name: "customer role", user: "customer-id", allowed: false, want: http.StatusForbidden}, {name: "active admin", user: "admin-id", allowed: true, want: http.StatusNoContent}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{admins: adminCheckerFunc(func(_ context.Context, id string) (bool, error) { return id == "admin-id" && tc.allowed, nil })}
			next := h.adminRequired(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			req := httptest.NewRequest(http.MethodGet, "/admin/ledger/summary", nil)
			if tc.user != "" {
				req.Header.Set("X-User-ID", tc.user)
			}
			res := httptest.NewRecorder()
			next.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status %d, want %d", res.Code, tc.want)
			}
		})
	}
}

func TestDLQEndpointsRequireAdminAndSupportReviewActions(t *testing.T) {
	dlq := &fakeDLQ{}
	handler := NewHandlerWithAdminChecker(nil, adminCheckerFunc(func(_ context.Context, id string) (bool, error) { return id == "admin", nil })).WithDLQ(dlq)
	for _, tc := range []struct {
		method, path, body, user string
		status                   int
	}{{http.MethodGet, "/ops/dlq", "", "customer", http.StatusForbidden}, {http.MethodGet, "/ops/dlq", "", "admin", http.StatusOK}, {http.MethodPost, "/ops/dlq/default/t-1/retry", "", "admin", http.StatusOK}, {http.MethodDelete, "/ops/dlq/default/t-1", "", "admin", http.StatusOK}} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("X-User-ID", tc.user)
		res := httptest.NewRecorder()
		handler.Routes().ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("%s %s returned %d, wanted %d: %s", tc.method, tc.path, res.Code, tc.status, res.Body.String())
		}
	}
	if dlq.retries != 1 || dlq.deletes != 1 {
		t.Fatalf("retry=%d delete=%d", dlq.retries, dlq.deletes)
	}
}
