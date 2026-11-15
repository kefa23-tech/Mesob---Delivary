package domain

import "testing"

func TestRefundEntryProducesBalancedPartialReversal(t *testing.T) {
	entry, err := RefundEntry("admin-dispute:one", "order-one", "ETB", "Admin dispute refund", 700, []Line{
		{AccountKey: AccountKey(RestaurantPayable, "restaurant-one"), AccountType: RestaurantPayable, OwnerRestaurantID: "restaurant-one", AmountMinor: -600},
		{AccountKey: AccountKey(PlatformRevenue, ""), AccountType: PlatformRevenue, AmountMinor: -100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := entry.Validate(); err != nil {
		t.Fatalf("refund does not balance: %v", err)
	}
	if len(entry.Lines) != 3 || entry.Lines[0].AmountMinor != 600 || entry.Lines[1].AmountMinor != 100 || entry.Lines[2].AmountMinor != -700 {
		t.Fatalf("unexpected reversal lines: %#v", entry.Lines)
	}
}

func TestRefundEntryUsesPlatformForUnbackedCredit(t *testing.T) {
	entry, err := RefundEntry("admin-dispute:two", "order-two", "ETB", "Admin dispute refund", 900, []Line{{AccountKey: AccountKey(RestaurantPayable, "restaurant-two"), AccountType: RestaurantPayable, OwnerRestaurantID: "restaurant-two", AmountMinor: -500}})
	if err != nil {
		t.Fatal(err)
	}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	if entry.Lines[1].AccountType != PlatformRevenue || entry.Lines[1].AmountMinor != 400 {
		t.Fatalf("expected remaining refund to be funded from platform revenue: %#v", entry.Lines)
	}
}

func TestRemittanceBalance(t *testing.T) {
	got, err := RemittanceBalance(12500, 4500)
	if err != nil {
		t.Fatal(err)
	}
	if got != 8000 {
		t.Fatalf("got %d, want 8000", got)
	}
	if _, err := RemittanceBalance(1000, 1001); err != ErrInsufficientCash {
		t.Fatalf("expected insufficient cash, got %v", err)
	}
	if _, err := RemittanceBalance(1000, 0); err != ErrInvalidEntry {
		t.Fatalf("expected invalid amount, got %v", err)
	}
}
