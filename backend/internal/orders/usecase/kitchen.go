package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/realtime"
)

const RestaurantAcceptanceSLA = 5 * time.Minute

type KitchenService struct {
	repository KitchenRepository
	scheduler  RestaurantSLAScheduler
	dispatcher DispatchStarter
	publisher  realtime.Publisher
}

func NewKitchenService(repository KitchenRepository, scheduler RestaurantSLAScheduler, extras ...any) *KitchenService {
	s := &KitchenService{repository: repository, scheduler: scheduler}
	for _, extra := range extras {
		if dispatcher, ok := extra.(DispatchStarter); ok && s.dispatcher == nil {
			s.dispatcher = dispatcher
		}
		if publisher, ok := extra.(realtime.Publisher); ok {
			s.publisher = publisher
		}
	}
	return s
}

func (s *KitchenService) publish(ctx context.Context, order domain.KitchenOrder) {
	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: order.ID, RestaurantID: order.RestaurantID, Status: string(order.Status)})
	}
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
	s.publish(ctx, order)
	if s.scheduler != nil {
		_ = s.scheduler.CancelRestaurantSLA(ctx, orderID)
	}
	if s.dispatcher != nil {
		if err := s.dispatcher.StartDispatch(ctx, orderID); err != nil {
			return domain.KitchenOrder{}, fmt.Errorf("schedule courier dispatch: %w", err)
		}
	}
	return order, nil
}

func (s *KitchenService) Reject(ctx context.Context, partnerID, orderID string, reason domain.RestaurantRejectionReason) error {
	if partnerID == "" || orderID == "" || !reason.Valid() {
		return domain.ErrInvalidOrder
	}
	err := s.repository.RejectRestaurantOrder(ctx, partnerID, orderID, reason)
	if err == nil && s.publisher != nil {
		_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: orderID, Status: "REJECTED_BY_RESTAURANT"})
	}
	if err == nil && s.scheduler != nil {
		_ = s.scheduler.CancelRestaurantSLA(ctx, orderID)
	}
	return err
}

func (s *KitchenService) Ready(ctx context.Context, partnerID, orderID string) (domain.KitchenOrder, error) {
	if partnerID == "" || orderID == "" {
		return domain.KitchenOrder{}, domain.ErrInvalidOrder
	}
	order, err := s.repository.MarkRestaurantOrderReady(ctx, partnerID, orderID)
	if err == nil {
		s.publish(ctx, order)
	}
	return order, err
}

func (s *KitchenService) Preparing(ctx context.Context, partnerID, orderID string) (domain.KitchenOrder, error) {
	if partnerID == "" || orderID == "" {
		return domain.KitchenOrder{}, domain.ErrInvalidOrder
	}
	order, err := s.repository.StartRestaurantOrder(ctx, partnerID, orderID)
	if err == nil {
		s.publish(ctx, order)
	}
	return order, err
}

func (s *KitchenService) HandleSLATimeout(ctx context.Context, orderID string) error {
	if orderID == "" {
		return domain.ErrInvalidOrder
	}
	err := s.repository.TimeoutRestaurantOrder(ctx, orderID)
	if err == nil && s.publisher != nil {
		_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: orderID, Status: "CANCELLED_TIMEOUT"})
	}
	return err
}
