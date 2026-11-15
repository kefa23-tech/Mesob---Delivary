package usecase

import (
	"context"

	"github.com/mesob-delivery/backend/internal/ledger/domain"
)

type Row interface{ Scan(...any) error }

// Transaction is the narrow SQL transaction surface needed to append journal
// rows. Adapters wrap their database transaction without exposing it here.
type Transaction interface {
	Exec(context.Context, string, ...any) error
	QueryRow(context.Context, string, ...any) Row
}

type Repository interface {
	Record(context.Context, Transaction, domain.Entry) error
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Record(ctx context.Context, tx Transaction, entry domain.Entry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	return s.repository.Record(ctx, tx, entry)
}
