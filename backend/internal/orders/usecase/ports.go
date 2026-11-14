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
type CheckoutRepository interface {
	Place(context.Context, string, string, domain.CheckoutRequest, []domain.CartItem) (domain.Order, error)
}

type IdempotencyStore interface {
	Begin(context.Context, string, string) (cached []byte, inFlight bool, err error)
	Complete(context.Context, string, string, []byte) error
	Release(context.Context, string, string) error
}

type GroupCartConverter interface {
	ClaimConversion(context.Context, string) (domain.GroupCart, error)
	ReleaseConversion(context.Context, string) error
	ExpireConverted(context.Context, string) error
}

type MenuCatalog interface {
	GetMenuItem(context.Context, string, string) (domain.MenuItem, error)
}
