package usecase

import (
	"context"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
)

type RestaurantRepository interface {
	Search(context.Context, domain.SearchFilter) (domain.SearchResult, error)
}
type RestaurantCache interface {
	Get(context.Context, string) (domain.SearchResult, bool, error)
	Set(context.Context, string, domain.SearchResult) error
	Invalidate(context.Context, domain.RestaurantID) error
}
