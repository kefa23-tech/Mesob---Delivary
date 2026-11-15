package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type adminCheckerFunc func(context.Context, string) (bool, error)

func (f adminCheckerFunc) IsActiveAdmin(ctx context.Context, id string) (bool, error) {
	return f(ctx, id)
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
