package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/mesob-delivery/backend/internal/dispatch/domain"
)

const (
	OfferWindow       = 30 * time.Second
	OfferBatchSize    = 100
	CourierOfferLimit = 3
	DispatchLockTTL   = 10 * time.Second
)

type Service struct {
	repository Repository
	presence   PresenceRegistry
	tasks      TaskQueue
	notifier   OfferNotifier
}

func NewService(repository Repository, presence PresenceRegistry, tasks TaskQueue, notifier OfferNotifier) *Service {
	return &Service{repository: repository, presence: presence, tasks: tasks, notifier: notifier}
}

func (s *Service) SetStatus(ctx context.Context, id domain.CourierID, online bool, vehicle domain.VehicleType) error {
	if id == "" || !vehicle.Valid() {
		return domain.ErrInvalidCourier
	}
	if err := s.repository.UpdateCourierStatus(ctx, id, online, vehicle); err != nil {
		return err
	}
	return s.presence.UpdateStatus(ctx, domain.CourierStatus{Courier: id, Online: online, Vehicle: vehicle})
}
func (s *Service) SetLocation(ctx context.Context, id domain.CourierID, lat, lng float64) error {
	if id == "" || math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return domain.ErrInvalidCourier
	}
	if err := s.repository.EnsureCourierOnline(ctx, id); err != nil {
		return err
	}
	return s.presence.UpdateLocation(ctx, domain.Location{Courier: id, Latitude: lat, Longitude: lng})
}
func (s *Service) Offers(ctx context.Context, id domain.CourierID) ([]domain.OrderID, error) {
	if id == "" {
		return nil, domain.ErrInvalidCourier
	}
	return s.presence.OffersForCourier(ctx, id)
}

func (s *Service) OfferStream(ctx context.Context, id domain.CourierID) (<-chan domain.Offer, error) {
	if id == "" {
		return nil, domain.ErrInvalidCourier
	}
	return s.presence.SubscribeOffers(ctx, id)
}

func (s *Service) StartDispatch(ctx context.Context, orderID string) error {
	return s.tasks.ScheduleFindCourier(ctx, domain.OrderID(orderID), domain.TierOne, 0)
}

func (s *Service) FindCourier(ctx context.Context, orderID domain.OrderID, tier domain.Tier) error {
	if tier.RadiusKM() == 0 {
		return domain.ErrInvalidOrder
	}
	order, err := s.repository.DispatchOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order.CourierID != "" || !(order.Status == "ACCEPTED" || order.Status == "PREPARING" || order.Status == "READY_FOR_PICKUP") {
		return nil
	}
	nearby, err := s.presence.Nearby(ctx, order.RestaurantLat, order.RestaurantLng, tier.RadiusKM(), OfferBatchSize)
	if err != nil {
		return err
	}
	active := make([]domain.CourierID, 0, len(nearby))
	distances := make(map[domain.CourierID]float64, len(nearby))
	for _, candidate := range nearby {
		has, err := s.presence.HasHeartbeat(ctx, candidate.ID)
		if err != nil {
			return err
		}
		if !has {
			continue
		}
		wasOffered, err := s.presence.WasOffered(ctx, orderID, candidate.ID)
		if err != nil {
			return err
		}
		if wasOffered {
			continue
		}
		active = append(active, candidate.ID)
		distances[candidate.ID] = candidate.DistanceKM
	}
	eligible, err := s.repository.EligibleCouriers(ctx, orderID, active)
	if err != nil {
		return err
	}
	eligibleSet := make(map[domain.CourierID]bool, len(eligible))
	for _, id := range eligible {
		eligibleSet[id] = true
	}
	selected := make([]domain.CourierID, 0, CourierOfferLimit)
	for _, candidate := range nearby {
		if eligibleSet[candidate.ID] && len(selected) < CourierOfferLimit {
			selected = append(selected, candidate.ID)
		}
	}
	if err := s.presence.SetOffers(ctx, orderID, selected, OfferWindow); err != nil {
		return err
	}
	var notifyErr error
	for _, id := range selected {
		if s.notifier != nil {
			if err := s.notifier.NotifyOffer(ctx, domain.Offer{OrderID: orderID, CourierID: id, DistanceKM: distances[id], ExpiresInSeconds: int(OfferWindow.Seconds())}); err != nil {
				notifyErr = errors.Join(notifyErr, fmt.Errorf("publish courier offer to %s: %w", id, err))
			}
		}
	}
	if err := s.tasks.ScheduleOfferTimeout(ctx, orderID, tier, OfferWindow); err != nil {
		return errors.Join(notifyErr, fmt.Errorf("schedule courier offer timeout: %w", err))
	}
	return notifyErr
}

func (s *Service) HandleOfferTimeout(ctx context.Context, orderID domain.OrderID, tier domain.Tier) error {
	if err := s.presence.DeleteOffers(ctx, orderID); err != nil {
		return err
	}
	order, err := s.repository.DispatchOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order.CourierID != "" {
		return nil
	}
	if next, ok := tier.Next(); ok {
		return s.tasks.ScheduleFindCourier(ctx, orderID, next, 0)
	}
	return s.repository.MarkManualIntervention(ctx, orderID)
}

func (s *Service) Claim(ctx context.Context, orderID domain.OrderID, courierID domain.CourierID) error {
	if orderID == "" || courierID == "" {
		return domain.ErrInvalidCourier
	}
	offered, err := s.presence.IsOffered(ctx, orderID, courierID)
	if err != nil {
		return err
	}
	if !offered {
		return domain.ErrOfferExpired
	}
	heartbeat, err := s.presence.HasHeartbeat(ctx, courierID)
	if err != nil {
		return err
	}
	if !heartbeat {
		return domain.ErrOfferExpired
	}
	token := uuid.NewString()
	locked, err := s.presence.AcquireOrderLock(ctx, orderID, token, DispatchLockTTL)
	if err != nil {
		return err
	}
	if !locked {
		return domain.ErrOrderClaimed
	}
	defer func() { _ = s.presence.ReleaseOrderLock(context.Background(), orderID, token) }()
	if err := s.repository.ClaimCourier(ctx, orderID, courierID); err != nil {
		return err
	}
	return s.presence.DeleteOffers(ctx, orderID)
}
func (s *Service) Pickup(ctx context.Context, orderID domain.OrderID, courierID domain.CourierID) error {
	return s.repository.Pickup(ctx, orderID, courierID)
}
func (s *Service) Complete(ctx context.Context, orderID domain.OrderID, courierID domain.CourierID, pin string) error {
	if len(pin) != 4 {
		return domain.ErrInvalidPIN
	}
	if err := s.repository.Complete(ctx, orderID, courierID, pin); err != nil {
		return err
	}
	return s.presence.DeleteOffers(ctx, orderID)
}

func IsClaimFailure(err error) bool {
	return errors.Is(err, domain.ErrOrderClaimed) || errors.Is(err, domain.ErrOfferExpired) || errors.Is(err, domain.ErrOrderNotAssignable) || errors.Is(err, domain.ErrCourierCODLimit)
}
