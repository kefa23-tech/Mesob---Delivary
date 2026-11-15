package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mesob-delivery/backend/internal/dispatch/domain"
)

type claimRepository struct {
	mu      sync.Mutex
	winner  domain.CourierID
	claimed int
}

func (*claimRepository) UpdateCourierStatus(context.Context, domain.CourierID, bool, domain.VehicleType) error {
	return nil
}
func (*claimRepository) EnsureCourierOnline(context.Context, domain.CourierID) error { return nil }
func (*claimRepository) DispatchOrder(context.Context, domain.OrderID) (domain.Order, error) {
	return domain.Order{}, nil
}
func (*claimRepository) EligibleCouriers(context.Context, domain.OrderID, []domain.CourierID) ([]domain.CourierID, error) {
	return nil, nil
}
func (r *claimRepository) ClaimCourier(_ context.Context, _ domain.OrderID, courier domain.CourierID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.winner != "" {
		return domain.ErrOrderClaimed
	}
	r.winner = courier
	r.claimed++
	return nil
}
func (*claimRepository) Pickup(context.Context, domain.OrderID, domain.CourierID) error { return nil }
func (*claimRepository) Complete(context.Context, domain.OrderID, domain.CourierID, string) error {
	return nil
}
func (*claimRepository) MarkManualIntervention(context.Context, domain.OrderID) error { return nil }

type claimPresence struct {
	mu     sync.Mutex
	locked bool
}

func (*claimPresence) UpdateStatus(context.Context, domain.CourierStatus) error { return nil }
func (*claimPresence) UpdateLocation(context.Context, domain.Location) error    { return nil }
func (*claimPresence) Nearby(context.Context, float64, float64, float64, int) ([]domain.NearbyCourier, error) {
	return nil, nil
}
func (*claimPresence) HasHeartbeat(context.Context, domain.CourierID) (bool, error) { return true, nil }
func (*claimPresence) SetOffers(context.Context, domain.OrderID, []domain.CourierID, time.Duration) error {
	return nil
}
func (*claimPresence) IsOffered(context.Context, domain.OrderID, domain.CourierID) (bool, error) {
	return true, nil
}
func (*claimPresence) WasOffered(context.Context, domain.OrderID, domain.CourierID) (bool, error) {
	return false, nil
}
func (*claimPresence) OffersForCourier(context.Context, domain.CourierID) ([]domain.OrderID, error) {
	return nil, nil
}
func (*claimPresence) SubscribeOffers(context.Context, domain.CourierID) (<-chan domain.Offer, error) {
	return nil, nil
}
func (*claimPresence) DeleteOffers(context.Context, domain.OrderID) error { return nil }
func (p *claimPresence) AcquireOrderLock(context.Context, domain.OrderID, string, time.Duration) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locked {
		return false, nil
	}
	p.locked = true
	return true, nil
}
func (p *claimPresence) ReleaseOrderLock(context.Context, domain.OrderID, string) error {
	p.mu.Lock()
	p.locked = false
	p.mu.Unlock()
	return nil
}

func TestConcurrentCourierClaimsHaveExactlyOneWinner(t *testing.T) {
	repository := &claimRepository{}
	service := NewService(repository, &claimPresence{}, nil, nil)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, courier := range []domain.CourierID{"courier-a", "courier-b"} {
		courier := courier
		go func() {
			<-start
			results <- service.Claim(context.Background(), "order-1", courier)
		}()
	}
	close(start)

	winners := 0
	for range 2 {
		if err := <-results; err == nil {
			winners++
		} else if err != domain.ErrOrderClaimed {
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one winning courier, got %d", winners)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.claimed != 1 || repository.winner == "" {
		t.Fatalf("expected one persisted assignment, claimed=%d winner=%q", repository.claimed, repository.winner)
	}
}
