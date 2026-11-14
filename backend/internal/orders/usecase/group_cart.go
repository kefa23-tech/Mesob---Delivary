package usecase

import (
	"context"
	"fmt"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"strings"
	"time"
)

const DefaultGroupCartTTL = 2 * time.Hour

type GroupCartService struct {
	carts CartRepository
	menu  MenuCatalog
	ttl   time.Duration
}

func NewGroupCartService(carts CartRepository, menu MenuCatalog, ttl time.Duration) *GroupCartService {
	if ttl <= 0 {
		ttl = DefaultGroupCartTTL
	}
	return &GroupCartService{carts: carts, menu: menu, ttl: ttl}
}

func (s *GroupCartService) Create(ctx context.Context, restaurantID string, host domain.Participant) (domain.GroupCart, error) {
	if strings.TrimSpace(restaurantID) == "" {
		return domain.GroupCart{}, domain.ErrRestaurantMismatch
	}
	if strings.TrimSpace(host.DisplayName) == "" && strings.TrimSpace(host.UserID) == "" {
		host.DisplayName = "Guest host"
	}
	return s.carts.Create(ctx, domain.GroupCart{RestaurantID: restaurantID, Host: host, Status: domain.CartOpen, Items: []domain.CartItem{}, Participants: []domain.ParticipantSummary{}}, s.ttl)
}
func (s *GroupCartService) Get(ctx context.Context, token string) (domain.GroupCart, error) {
	return s.carts.Get(ctx, strings.TrimSpace(token))
}
func (s *GroupCartService) AddItem(ctx context.Context, token string, item domain.CartItem) (domain.GroupCart, error) {
	if s.menu == nil {
		return domain.GroupCart{}, fmt.Errorf("menu catalog unavailable")
	}
	menuItem, err := s.menu.GetMenuItem(ctx, item.RestaurantID, item.MenuItemID)
	if err != nil {
		return domain.GroupCart{}, err
	}
	item.Name = menuItem.Name
	item.UnitPriceMinor = menuItem.PriceMinor
	return s.carts.AddItem(ctx, token, item, menuItem)
}
func (s *GroupCartService) RemoveItem(ctx context.Context, token, itemID string) (domain.GroupCart, error) {
	return s.carts.RemoveItem(ctx, token, itemID)
}
func (s *GroupCartService) Lock(ctx context.Context, token string) (domain.GroupCart, error) {
	return s.carts.Lock(ctx, token)
}
