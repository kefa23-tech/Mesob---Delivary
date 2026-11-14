package usecase

import (
	"context"
	"github.com/mesob-delivery/backend/internal/orders/domain"
)

type CartRepository interface {
	Get(context.Context, string) (domain.Order, error)
	Save(context.Context, string, domain.Order) error
}
type Inventory interface {
	Reserve(context.Context, []domain.LineItem) error
	Release(context.Context, []domain.LineItem) error
}
type OrderRepository interface {
	Create(context.Context, domain.Order) error
}
