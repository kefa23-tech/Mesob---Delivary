package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/orders/usecase"
)

type checkoutRepo struct{ calls int }

func (r *checkoutRepo) Place(context.Context, string, string, domain.CheckoutRequest, []domain.CartItem) (domain.Order, error) {
	r.calls++
	return domain.Order{ID: "order-1", Status: domain.OrderPending, Items: []domain.OrderItem{}}, nil
}

type idemStore struct {
	value      []byte
	processing bool
}

func (s *idemStore) Begin(context.Context, string, string) ([]byte, bool, error) {
	if len(s.value) > 0 {
		return append([]byte(nil), s.value...), false, nil
	}
	if s.processing {
		return nil, true, nil
	}
	s.processing = true
	return nil, false, nil
}
func (s *idemStore) Complete(_ context.Context, _, _ string, body []byte) error {
	s.value = append([]byte(nil), body...)
	s.processing = false
	return nil
}
func (s *idemStore) Release(context.Context, string, string) error { s.processing = false; return nil }

func TestPlaceReplaysCachedOrderWithoutRunningCheckoutAgain(t *testing.T) {
	repo, idem := &checkoutRepo{}, &idemStore{}
	service := usecase.NewCheckoutService(repo, idem, nil)
	request := domain.CheckoutRequest{AddressID: "address", PaymentMethod: domain.PaymentCOD, Items: []domain.CheckoutItem{{MenuItemID: "item", Quantity: 1}}}
	first, replay, err := service.Place(context.Background(), "user", "key", request)
	if err != nil || replay {
		t.Fatalf("first Place = replay %v, err %v", replay, err)
	}
	second, replay, err := service.Place(context.Background(), "user", "key", request)
	if err != nil || !replay {
		t.Fatalf("second Place = replay %v, err %v", replay, err)
	}
	if string(first) != string(second) {
		t.Fatalf("replay differs: %s != %s", first, second)
	}
	if repo.calls != 1 {
		t.Fatalf("checkout calls=%d; want 1", repo.calls)
	}
}

func TestPlaceReturnsConflictForInFlightIdempotencyKey(t *testing.T) {
	repo, idem := &checkoutRepo{}, &idemStore{processing: true}
	service := usecase.NewCheckoutService(repo, idem, nil)
	request := domain.CheckoutRequest{AddressID: "address", PaymentMethod: domain.PaymentCOD, Items: []domain.CheckoutItem{{MenuItemID: "item", Quantity: 1}}}
	_, _, err := service.Place(context.Background(), "user", "key", request)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected in-flight conflict, got %v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("checkout calls=%d; want 0", repo.calls)
	}
}
