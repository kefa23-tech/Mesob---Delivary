package usecase

import (
	"context"
	"time"

	"github.com/mesob-delivery/backend/internal/dispatch/domain"
)

type CourierMatcher interface {
	Match(context.Context, domain.OrderID) (domain.Assignment, error)
}
type TrackingRepository interface {
	Save(context.Context, domain.OrderID, string) error
	Current(context.Context, domain.OrderID) (string, error)
}

type Repository interface {
	UpdateCourierStatus(context.Context, domain.CourierID, bool, domain.VehicleType) error
	EnsureCourierOnline(context.Context, domain.CourierID) error
	DispatchOrder(context.Context, domain.OrderID) (domain.Order, error)
	EligibleCouriers(context.Context, domain.OrderID, []domain.CourierID) ([]domain.CourierID, error)
	ClaimCourier(context.Context, domain.OrderID, domain.CourierID) error
	Pickup(context.Context, domain.OrderID, domain.CourierID) error
	Complete(context.Context, domain.OrderID, domain.CourierID, string) error
	MarkManualIntervention(context.Context, domain.OrderID) error
}

type PresenceRegistry interface {
	UpdateStatus(context.Context, domain.CourierStatus) error
	UpdateLocation(context.Context, domain.Location) error
	Nearby(context.Context, float64, float64, float64, int) ([]domain.NearbyCourier, error)
	HasHeartbeat(context.Context, domain.CourierID) (bool, error)
	SetOffers(context.Context, domain.OrderID, []domain.CourierID, time.Duration) error
	IsOffered(context.Context, domain.OrderID, domain.CourierID) (bool, error)
	WasOffered(context.Context, domain.OrderID, domain.CourierID) (bool, error)
	OffersForCourier(context.Context, domain.CourierID) ([]domain.OrderID, error)
	SubscribeOffers(context.Context, domain.CourierID) (<-chan domain.Offer, error)
	DeleteOffers(context.Context, domain.OrderID) error
	AcquireOrderLock(context.Context, domain.OrderID, string, time.Duration) (bool, error)
	ReleaseOrderLock(context.Context, domain.OrderID, string) error
}

type TaskQueue interface {
	ScheduleFindCourier(context.Context, domain.OrderID, domain.Tier, time.Duration) error
	ScheduleOfferTimeout(context.Context, domain.OrderID, domain.Tier, time.Duration) error
}

type OfferNotifier interface {
	NotifyOffer(context.Context, domain.Offer) error
}

type DispatchStarter interface {
	StartDispatch(context.Context, string) error
}
