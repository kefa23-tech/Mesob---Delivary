package usecase

import (
	"context"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"github.com/mesob-delivery/backend/internal/realtime"
)

type TrackingRepository interface {
	GetTrackingSnapshot(context.Context, string, string) (domain.TrackingSnapshot, error)
}
type CourierLocationReader interface {
	CourierLocation(context.Context, string) (*realtime.CourierLocation, error)
}

type TrackingService struct {
	repository TrackingRepository
	hub        realtime.Subscription
	locations  CourierLocationReader
}

func NewTrackingService(repository TrackingRepository, hub realtime.Subscription, locations CourierLocationReader) *TrackingService {
	return &TrackingService{repository: repository, hub: hub, locations: locations}
}
func (s *TrackingService) Snapshot(ctx context.Context, userID, orderID string) (domain.TrackingSnapshot, error) {
	snapshot, err := s.repository.GetTrackingSnapshot(ctx, userID, orderID)
	if err != nil {
		return snapshot, err
	}
	if snapshot.CourierID != "" && s.locations != nil {
		location, err := s.locations.CourierLocation(ctx, snapshot.CourierID)
		if err != nil {
			return snapshot, err
		}
		if location != nil {
			snapshot.CourierLat = &location.Lat
			snapshot.CourierLng = &location.Lng
			snapshot.CourierHeading = &location.Heading
			if !location.ETAAt.IsZero() {
				snapshot.ETAAt = &location.ETAAt
			}
		}
	}
	return snapshot, nil
}

// Connect subscribes before taking the snapshot, so events racing with hydration
// remain buffered for the client after its initial state is written.
func (s *TrackingService) Connect(ctx context.Context, userID, orderID string) (domain.TrackingSnapshot, <-chan realtime.Event, context.CancelFunc, error) {
	if _, err := s.repository.GetTrackingSnapshot(ctx, userID, orderID); err != nil {
		return domain.TrackingSnapshot{}, nil, nil, err
	}
	subctx, cancel := context.WithCancel(ctx)
	events, err := s.hub.SubscribeOrder(subctx, orderID)
	if err != nil {
		cancel()
		return domain.TrackingSnapshot{}, nil, nil, err
	}
	snapshot, err := s.Snapshot(ctx, userID, orderID)
	if err != nil {
		cancel()
		return domain.TrackingSnapshot{}, nil, nil, err
	}
	return snapshot, events, cancel, nil
}
