package usecase

import (
	"context"
	"github.com/mesob-delivery/backend/internal/admin/domain"
)

type KYCRepository interface {
	Get(context.Context, domain.CourierID) (domain.KYCReview, error)
	Save(context.Context, domain.KYCReview) error
}
type DisputeRepository interface {
	Save(context.Context, domain.Dispute) error
}
type DeadLetterQueue interface {
	Depth(context.Context) (int64, error)
	Replay(context.Context, int64) error
}

type DLQInspector interface {
	List(context.Context) ([]domain.DeadLetterTask, map[string]int64, error)
	Retry(context.Context, string, string) error
	Delete(context.Context, string, string) error
}
