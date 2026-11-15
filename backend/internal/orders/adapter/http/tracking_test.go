package http

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/orders/usecase"
	"github.com/mesob-delivery/backend/internal/realtime"
)

const testJWTSecret = "tracking-test-secret"

type trackingRepoFake struct{}

func (trackingRepoFake) GetTrackingSnapshot(_ context.Context, userID, orderID string) (domain.TrackingSnapshot, error) {
	if userID != "customer-1" || orderID != "order-1" {
		return domain.TrackingSnapshot{}, domain.ErrOrderNotFound
	}
	return domain.TrackingSnapshot{ID: orderID, Status: "PAID"}, nil
}

type trackingHubFake struct{ events chan realtime.Event }

func (h trackingHubFake) SubscribeOrder(context.Context, string) (<-chan realtime.Event, error) {
	return h.events, nil
}
func signedCustomer(t *testing.T, user string) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user, "role": "customer", "exp": time.Now().Add(time.Minute).Unix()}).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func trackingRouter() chi.Router {
	hub := trackingHubFake{events: make(chan realtime.Event)}
	service := usecase.NewTrackingService(trackingRepoFake{}, hub, nil)
	handler := NewTrackingHandler(service, testJWTSecret)
	r := chi.NewRouter()
	handler.Register(r)
	return r
}

func TestTrackingSnapshotRequiresCustomerAuthentication(t *testing.T) {
	r := trackingRouter()
	req := httptest.NewRequest("GET", "/order-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("unauthenticated status=%d", rec.Code)
	}
}
func TestTrackingSnapshotEnforcesOrderOwnership(t *testing.T) {
	r := trackingRouter()
	req := httptest.NewRequest("GET", "/order-1", nil)
	req.Header.Set("Authorization", "Bearer "+signedCustomer(t, "customer-2"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("non-owner status=%d body=%s", rec.Code, rec.Body.String())
	}
}
func TestOrderStreamSendsSnapshotFirstForOwner(t *testing.T) {
	r := trackingRouter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/order-1/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+signedCustomer(t, "customer-1"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("owner stream status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); len(got) < 20 || got[:15] != "event: snapshot" {
		t.Fatalf("expected hydration snapshot first, got %q", got)
	}
}
