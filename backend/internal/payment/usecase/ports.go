package usecase

import (
	"context"
	"time"

	"github.com/mesob-delivery/backend/internal/payment/domain"
)

type Repository interface {
	CreateIntent(context.Context, string, string, domain.Provider) (domain.Transaction, error)
	UpdateInitialized(context.Context, string, domain.InitializeResult) error
	GetTransaction(context.Context, domain.Provider, string) (domain.Transaction, error)
	Confirm(context.Context, domain.Provider, string, int64, string) (orderID string, transitioned bool, err error)
	FailAndRestore(context.Context, domain.Provider, string) (orderID string, transitioned bool, err error)
}

type Queue interface {
	SchedulePoll(context.Context, domain.Provider, string, int, time.Duration) error
	CancelPolling(context.Context, domain.Provider, string) error
	NotifyRestaurantPaid(context.Context, string) error
}

type GatewayRegistry interface {
	Gateway(domain.Provider) (domain.Gateway, bool)
}
