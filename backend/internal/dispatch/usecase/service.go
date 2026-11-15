package usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/mesob-delivery/backend/internal/dispatch/domain"
	"github.com/mesob-delivery/backend/internal/realtime"
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
	publisher  realtime.Publisher
}

func NewService(repository Repository, presence PresenceRegistry, tasks TaskQueue, notifier OfferNotifier, publishers ...realtime.Publisher) *Service {
	s := &Service{repository: repository, presence: presence, tasks: tasks, notifier: notifier}
	if len(publishers) > 0 {
		s.publisher = publishers[0]
	}
	return s
}

func (s *Service) SetStatus(ctx context.Context, id domain.CourierID, online bool, vehicle domain.VehicleType) error {
	if id == "" || !vehicle.Valid() {
		return domain.ErrInvalidCourier
	}
	if err := s.repository.UpdateCourierStatus(ctx, id, online, vehicle); err != nil {
		return err
	}
	if err := s.presence.UpdateStatus(ctx, domain.CourierStatus{Courier: id, Online: online, Vehicle: vehicle}); err != nil {
		return err
	}
	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.CourierAvailabilityUpdated, CourierID: string(id), Data: map[string]any{"online": online, "vehicle_type": vehicle}})
	}
	return nil
}
func (s *Service) SetLocation(ctx context.Context, id domain.CourierID, lat, lng float64, headings ...float64) error {
	if id == "" || math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return domain.ErrInvalidCourier
	}
	if err := s.repository.EnsureCourierOnline(ctx, id); err != nil {
		return err
	}
	if err := s.presence.UpdateLocation(ctx, domain.Location{Courier: id, Latitude: lat, Longitude: lng}); err != nil {
		return err
	}
	if reader, ok := s.repository.(ActiveDeliveryReader); ok && s.publisher != nil {
		orderID, restaurantID, destLat, destLng, err := reader.ActiveDelivery(ctx, id)
		if err != nil {
			return err
		}
		if orderID != "" {
			distance := haversineKM(lat, lng, destLat, destLng) * 1.3
			seconds := int(distance / 20 * 3600)
			eta := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
			var heading float64
			if len(headings) > 0 {
				heading = headings[0]
			}
			_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.CourierLocationUpdate, OrderID: orderID, RestaurantID: restaurantID, CourierID: string(id), Location: &realtime.CourierLocation{Lat: lat, Lng: lng, Heading: heading, ETASeconds: seconds, ETAAt: eta}})
		}
	}
	return nil
}

func haversineKM(lat1, lng1, lat2, lng2 float64) float64 {
	const radius = 6371
	toRad := func(v float64) float64 { return v * math.Pi / 180 }
	dLat, dLng := toRad(lat2-lat1), toRad(lng2-lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * radius * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
func (s *Service) Offers(ctx context.Context, id domain.CourierID) ([]domain.OrderID, error) {
	if id == "" {
		return nil, domain.ErrInvalidCourier
	}
	return s.presence.OffersForCourier(ctx, id)
}

func (s *Service) CourierDashboard(ctx context.Context, id domain.CourierID) (domain.CourierDashboard, error) {
	if id == "" {
		return domain.CourierDashboard{}, domain.ErrInvalidCourier
	}
	reader, ok := s.repository.(CourierConsoleReader)
	if !ok {
		return domain.CourierDashboard{}, errors.New("courier dashboard unavailable")
	}
	return reader.CourierDashboard(ctx, id)
}
func (s *Service) OfferDetails(ctx context.Context, courierID domain.CourierID, orderID domain.OrderID) (domain.OfferDetails, error) {
	if courierID == "" || orderID == "" {
		return domain.OfferDetails{}, domain.ErrInvalidCourier
	}
	offered, err := s.presence.IsOffered(ctx, orderID, courierID)
	if err != nil {
		return domain.OfferDetails{}, err
	}
	if !offered {
		return domain.OfferDetails{}, domain.ErrOfferExpired
	}
	reader, ok := s.repository.(CourierConsoleReader)
	if !ok {
		return domain.OfferDetails{}, errors.New("courier offer details unavailable")
	}
	details, err := reader.OfferDetails(ctx, orderID)
	if err != nil {
		return domain.OfferDetails{}, err
	}
	return domain.OfferDetails{DeliveryDetails: details, ExpiresInSeconds: int(OfferWindow.Seconds())}, nil
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

func (s *Service) EscalateStaleDispatches(ctx context.Context) error {
	reader, ok := s.repository.(StaleDispatchReader)
	if !ok {
		return errors.New("stale dispatch reconciliation unavailable")
	}
	orders, err := reader.StaleDispatchOrders(ctx, time.Now().UTC().Add(-10*time.Minute))
	if err != nil {
		return err
	}
	var failures []error
	for _, orderID := range orders {
		if err := s.tasks.ScheduleFindCourier(ctx, orderID, domain.TierThree, 0); err != nil {
			_ = s.repository.MarkManualIntervention(ctx, orderID)
			failures = append(failures, fmt.Errorf("escalate order %s: %w", orderID, err))
		}
	}
	return errors.Join(failures...)
}

func (s *Service) FindCourier(ctx context.Context, orderID domain.OrderID, tier domain.Tier) error {
	if tier.RadiusKM() == 0 {
		return domain.ErrInvalidOrder
	}
	order, err := s.repository.DispatchOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order.CourierID != "" || (order.Status != "ACCEPTED" && order.Status != "PREPARING" && order.Status != "READY_FOR_PICKUP") {
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
		if s.publisher != nil {
			_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.CourierOfferCreated, OrderID: string(orderID), RestaurantID: order.RestaurantID, CourierID: string(id), Data: map[string]any{"distance_km": distances[id], "expires_in_seconds": int(OfferWindow.Seconds())}})
		}
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
	order, err := s.repository.DispatchOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if err := s.repository.ClaimCourier(ctx, orderID, courierID); err != nil {
		return err
	}
	if s.publisher != nil {
		_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: string(orderID), RestaurantID: order.RestaurantID, CourierID: string(courierID), Status: "COURIER_ASSIGNED"})
	}
	return s.presence.DeleteOffers(ctx, orderID)
}
func (s *Service) Pickup(ctx context.Context, orderID domain.OrderID, courierID domain.CourierID) error {
	err := s.repository.Pickup(ctx, orderID, courierID)
	if err == nil && s.publisher != nil {
		order, _ := s.repository.DispatchOrder(ctx, orderID)
		_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: string(orderID), RestaurantID: order.RestaurantID, CourierID: string(courierID), Status: "PICKED_UP"})
	}
	return err
}
func (s *Service) Complete(ctx context.Context, orderID domain.OrderID, courierID domain.CourierID, pin string) error {
	if len(pin) != 4 {
		return domain.ErrInvalidPIN
	}
	if err := s.repository.Complete(ctx, orderID, courierID, pin); err != nil {
		return err
	}
	if s.publisher != nil {
		order, _ := s.repository.DispatchOrder(ctx, orderID)
		_ = s.publisher.Publish(ctx, realtime.Event{Type: realtime.OrderStatusUpdated, OrderID: string(orderID), RestaurantID: order.RestaurantID, CourierID: string(courierID), Status: "DELIVERED"})
	}
	return s.presence.DeleteOffers(ctx, orderID)
}

func IsClaimFailure(err error) bool {
	return errors.Is(err, domain.ErrOrderClaimed) || errors.Is(err, domain.ErrOfferExpired) || errors.Is(err, domain.ErrOrderNotAssignable) || errors.Is(err, domain.ErrCourierCODLimit)
}
