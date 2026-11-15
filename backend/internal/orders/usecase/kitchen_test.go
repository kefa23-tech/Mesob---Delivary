package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mesob-delivery/backend/internal/orders/domain"
)

type fakeKitchenRepository struct {
	timeout string
	orders  []domain.KitchenOrder
}

func (f *fakeKitchenRepository) ListRestaurantOrders(context.Context, string) ([]domain.KitchenOrder, error) {
	return f.orders, nil
}
func (f *fakeKitchenRepository) AcceptRestaurantOrder(context.Context, string, string, int) (domain.KitchenOrder, error) {
	return domain.KitchenOrder{}, nil
}
func (f *fakeKitchenRepository) StartRestaurantOrder(context.Context, string, string) (domain.KitchenOrder, error) {
	return domain.KitchenOrder{}, nil
}
func (f *fakeKitchenRepository) RejectRestaurantOrder(context.Context, string, string, domain.RestaurantRejectionReason) error {
	return nil
}
func (f *fakeKitchenRepository) MarkRestaurantOrderReady(context.Context, string, string) (domain.KitchenOrder, error) {
	return domain.KitchenOrder{}, nil
}
func (f *fakeKitchenRepository) TimeoutRestaurantOrder(_ context.Context, id string) error {
	f.timeout = id
	return nil
}

func TestRestaurantSLATimeoutUsesOrderRepository(t *testing.T) {
	repo := &fakeKitchenRepository{}
	svc := NewKitchenService(repo, nil)
	if err := svc.HandleSLATimeout(context.Background(), "order-123"); err != nil {
		t.Fatal(err)
	}
	if repo.timeout != "order-123" {
		t.Fatalf("timeout order=%q", repo.timeout)
	}
	if err := svc.HandleSLATimeout(context.Background(), ""); err == nil {
		t.Fatal("empty order id accepted")
	}
}
func TestRestaurantAcceptanceSLAIsFiveMinutes(t *testing.T) {
	if RestaurantAcceptanceSLA != 5*time.Minute {
		t.Fatalf("SLA=%s", RestaurantAcceptanceSLA)
	}
}
