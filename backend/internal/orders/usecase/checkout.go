package usecase

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mesob-delivery/backend/internal/orders/domain"
)

type CheckoutService struct {
	orders      CheckoutRepository
	idempotency IdempotencyStore
	carts       GroupCartConverter
	scheduler   RestaurantSLAScheduler
}

func NewCheckoutService(orders CheckoutRepository, idempotency IdempotencyStore, carts GroupCartConverter, schedulers ...RestaurantSLAScheduler) *CheckoutService {
	s := &CheckoutService{orders: orders, idempotency: idempotency, carts: carts}
	if len(schedulers) > 0 {
		s.scheduler = schedulers[0]
	}
	return s
}

// Place returns an already serialized order body on replay so the HTTP adapter can
// send the exact same successful representation without another checkout.
func (s *CheckoutService) Place(ctx context.Context, userID, key string, request domain.CheckoutRequest) ([]byte, bool, error) {
	userID, key = strings.TrimSpace(userID), strings.TrimSpace(key)
	if userID == "" || key == "" {
		return nil, false, domain.ErrInvalidOrder
	}
	if err := domain.ValidateCheckout(request); err != nil {
		return nil, false, err
	}
	cached, inFlight, err := s.idempotency.Begin(ctx, userID, key)
	if err != nil {
		return nil, false, err
	}
	if len(cached) > 0 {
		return cached, true, nil
	}
	if inFlight {
		return nil, false, domain.ErrIdempotencyConflict
	}
	complete := false
	conversionClaimed := false
	defer func() {
		if !complete {
			_ = s.idempotency.Release(context.Background(), userID, key)
		}
		if conversionClaimed && !complete && request.GroupCartToken != "" {
			_ = s.carts.ReleaseConversion(context.Background(), request.GroupCartToken)
		}
	}()
	var groupItems []domain.CartItem
	if request.GroupCartToken != "" {
		if s.carts == nil {
			return nil, false, domain.ErrCartNotFound
		}
		cart, err := s.carts.ClaimConversion(ctx, request.GroupCartToken)
		if err != nil {
			return nil, false, err
		}
		conversionClaimed = true
		groupItems = cart.Items
	}
	order, err := s.orders.Place(ctx, userID, key, request, groupItems)
	if err != nil {
		return nil, false, err
	}
	// From this point the database has committed; keep the Redis claim even if
	// response caching fails, so a retry cannot execute checkout a second time.
	complete = true
	if request.PaymentMethod == domain.PaymentCOD && s.scheduler != nil {
		_ = s.scheduler.ScheduleRestaurantSLA(ctx, order.ID, RestaurantAcceptanceSLA)
	}
	body, err := json.Marshal(order)
	if err != nil {
		return nil, false, err
	}
	if err := s.idempotency.Complete(ctx, userID, key, body); err != nil {
		return nil, false, err
	}
	if request.GroupCartToken != "" && s.carts != nil {
		if err := s.carts.ExpireConverted(ctx, request.GroupCartToken); err != nil {
			// The order has committed and its response is cached; conversion cleanup is best-effort.
			return body, false, nil
		}
	}
	return body, false, nil
}
