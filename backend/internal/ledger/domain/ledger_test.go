package domain_test

import (
	"errors"
	"testing"

	"github.com/mesob-delivery/backend/internal/ledger/domain"
)

func TestEntryValidateRequiresBalancedDebitsAndCredits(t *testing.T) {
	base := domain.Entry{SourceKey: "payment:test", EntryType: "test", Currency: "ETB", Description: "test", Lines: []domain.Line{
		{AccountKey: "cash", AccountType: domain.SystemClearing, AmountMinor: 100},
		{AccountKey: "payable", AccountType: domain.RestaurantPayable, AmountMinor: -100},
	}}
	if err := base.Validate(); err != nil {
		t.Fatalf("balanced entry rejected: %v", err)
	}
	base.Lines[1].AmountMinor = -99
	if err := base.Validate(); !errors.Is(err, domain.ErrUnbalancedEntry) {
		t.Fatalf("expected unbalanced entry rejection, got %v", err)
	}
	base.Lines = base.Lines[:1]
	if err := base.Validate(); !errors.Is(err, domain.ErrUnbalancedEntry) {
		t.Fatalf("expected single-line entry rejection, got %v", err)
	}
}

func TestOnlinePaymentEntryBalancesSettlementSplits(t *testing.T) {
	entry := domain.OnlinePaymentEntry("order-1", "CHAPA", "ref-1", "restaurant-1", 115000, 100000, 10000, 15000)
	if err := entry.Validate(); err != nil {
		t.Fatalf("online payment entry is invalid: %v", err)
	}
}
