package usecase

import (
	"context"
	"github.com/mesob-delivery/backend/internal/dispatch/domain"
)

type CourierMatcher interface {
	Match(context.Context, domain.OrderID) (domain.Assignment, error)
}
type TrackingRepository interface {
	Save(context.Context, domain.OrderID, string) error
	Current(context.Context, domain.OrderID) (string, error)
}
