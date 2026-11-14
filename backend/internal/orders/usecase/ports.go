package usecase

import (
	"context"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"time"
)

type CartRepository interface {
	Create(context.Context, domain.GroupCart, time.Duration) (domain.GroupCart, error)
	Get(context.Context, string) (domain.GroupCart, error)
	AddItem(context.Context, string, domain.CartItem, domain.MenuItem) (domain.GroupCart, error)
	RemoveItem(context.Context, string, string) (domain.GroupCart, error)
	Lock(context.Context, string) (domain.GroupCart, error)
}
type Inventory interface {
	Reserve(context.Context, []domain.LineItem) error
	Release(context.Context, []domain.LineItem) error
}
type OrderRepository interface {
	Create(context.Context, domain.Order) error
}

type MenuCatalog interface {
	GetMenuItem(context.Context, string, string) (domain.MenuItem, error)
}
