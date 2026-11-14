package domain_test

import (
	"errors"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	"testing"
)

func menuItem() domain.MenuItem {
	return domain.MenuItem{ID: "item", RestaurantID: "restaurant-a", Name: "Shiro", PriceMinor: 10000, OptionGroups: []domain.OptionGroup{{ID: "spice", MinSelect: 1, MaxSelect: 1, Options: []domain.Option{{ID: "mild", Name: "Mild", IsAvailable: true, PriceDeltaMinor: 0}}}}}
}
func TestGroupCartEnforcesRestaurantAndSelections(t *testing.T) {
	cart := domain.GroupCart{RestaurantID: "restaurant-a", Status: domain.CartOpen}
	item := menuItem()
	err := cart.AddItem(domain.CartItem{ID: "one", RestaurantID: "restaurant-b", UnitPriceMinor: 10000, Quantity: 1, Participant: domain.Participant{DisplayName: "Abel"}}, item)
	if !errors.Is(err, domain.ErrRestaurantMismatch) {
		t.Fatalf("expected restaurant mismatch, got %v", err)
	}
	err = cart.AddItem(domain.CartItem{ID: "one", RestaurantID: "restaurant-a", UnitPriceMinor: 10000, Quantity: 1, Participant: domain.Participant{DisplayName: "Abel"}}, item)
	if !errors.Is(err, domain.ErrInvalidSelection) {
		t.Fatalf("expected required option validation, got %v", err)
	}
}
func TestGroupCartDiscountsDeliveryAtThreshold(t *testing.T) {
	cart := domain.GroupCart{RestaurantID: "restaurant-a", Status: domain.CartOpen}
	item := domain.MenuItem{ID: "item", RestaurantID: "restaurant-a", Name: "Feast", PriceMinor: 120000}
	if err := cart.AddItem(domain.CartItem{ID: "one", RestaurantID: "restaurant-a", UnitPriceMinor: 120000, Quantity: 1, Participant: domain.Participant{DisplayName: "Abel"}}, item); err != nil {
		t.Fatal(err)
	}
	if cart.DeliveryDiscountMinor != 7500 || cart.DeliveryFeeMinor != 7500 || cart.TotalMinor != 127500 {
		t.Fatalf("unexpected totals: %+v", cart)
	}
}
func TestLockedCartCannotChange(t *testing.T) {
	cart := domain.GroupCart{RestaurantID: "restaurant-a", Status: domain.CartOpen}
	if err := cart.Lock(); err != nil {
		t.Fatal(err)
	}
	err := cart.RemoveItem("missing")
	if !errors.Is(err, domain.ErrCartLocked) {
		t.Fatalf("expected lock error, got %v", err)
	}
}
