package usecase

import (
	"context"
	"time"

	"github.com/mesob-delivery/backend/internal/orders/domain"
)

const RestaurantAcceptanceSLA = 5 * time.Minute

type KitchenService struct {
	repository KitchenRepository
	scheduler  RestaurantSLAScheduler
}

func NewKitchenService(repository KitchenRepository, scheduler RestaurantSLAScheduler) *KitchenService {
	return &KitchenService{repository: repository, scheduler: scheduler}
}

func (s *KitchenService) List(ctx context.Context, partnerID string) ([]domain.KitchenOrder, error) {
	if partnerID == "" {
		return nil, domain.ErrInvalidOrder
	}
	return s.repository.ListRestaurantOrders(ctx, partnerID)
}

func (s *KitchenService) Accept(ctx context.Context, partnerID, orderID string, prepMinutes int) (domain.KitchenOrder, error) {
	if partnerID == "" || orderID == "" || prepMinutes < 1 || prepMinutes > 240 {
		return domain.KitchenOrder{}, domain.ErrInvalidOrder
	}
	order, err := s.repository.AcceptRestaurantOrder(ctx, partnerID, orderID, prepMinutes)
	if err != nil {
		return domain.KitchenOrder{}, err
	}
	if s.scheduler != nil {
		_ = s.scheduler.CancelRestaurantSLA(ctx, orderID)
	}
	return order, nil
}

func (s *KitchenService) Reject(ctx context.Context, partnerID, orderID string, reason domain.RestaurantRejectionReason) error {
	if partnerID == "" || orderID == "" || !reason.Valid() {
		return domain.ErrInvalidOrder
	}
	err := s.repository.RejectRestaurantOrder(ctx, partnerID, orderID, reason)
	if err == nil && s.scheduler != nil {
		_ = s.scheduler.CancelRestaurantSLA(ctx, orderID)
	}
	return err
}

func (s *KitchenService) Ready(ctx context.Context, partnerID, orderID string) (domain.KitchenOrder, error) {
	if partnerID == "" || orderID == "" {
		return domain.KitchenOrder{}, domain.ErrInvalidOrder
	}
	return s.repository.MarkRestaurantOrderReady(ctx, partnerID, orderID)
}

func (s *KitchenService) Preparing(ctx context.Context, partnerID, orderID string) (domain.KitchenOrder, error) {
	if partnerID == "" || orderID == "" {
		return domain.KitchenOrder{}, domain.ErrInvalidOrder
	}
	return s.repository.StartRestaurantOrder(ctx, partnerID, orderID)
}

func (s *KitchenService) HandleSLATimeout(ctx context.Context, orderID string) error {
	if orderID == "" {
		return domain.ErrInvalidOrder
	}
	return s.repository.TimeoutRestaurantOrder(ctx, orderID)
}
