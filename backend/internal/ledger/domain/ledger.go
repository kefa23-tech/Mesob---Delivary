package domain

import (
	"errors"
	"math/big"
	"strings"
)

type AccountType string

const (
	SystemClearing        AccountType = "system_clearing"
	CustomerClearing      AccountType = "customer_clearing"
	PlatformRevenue       AccountType = "platform_revenue"
	RestaurantPayable     AccountType = "restaurant_payable"
	CourierPayable        AccountType = "courier_payable"
	CourierCashReceivable AccountType = "courier_cash_receivable"
	DeliveryFeePool       AccountType = "delivery_fee_pool"
)

var (
	ErrInvalidEntry        = errors.New("invalid ledger entry")
	ErrUnbalancedEntry     = errors.New("ledger entry must have at least two lines and balance to zero")
	ErrInsufficientPayable = errors.New("settlement exceeds payable balance")
	ErrInsufficientCash    = errors.New("remittance exceeds courier cash balance")
)

type Line struct {
	AccountKey        string
	AccountType       AccountType
	OwnerUserID       string
	OwnerRestaurantID string
	AmountMinor       int64 // positive is debit; negative is credit.
}

type Entry struct {
	SourceKey   string
	OrderID     string
	EntryType   string
	Currency    string
	Description string
	Lines       []Line
}

func (e Entry) Validate() error {
	if strings.TrimSpace(e.SourceKey) == "" || strings.TrimSpace(e.EntryType) == "" || len(e.Currency) != 3 || strings.TrimSpace(e.Description) == "" {
		return ErrInvalidEntry
	}
	if len(e.Lines) < 2 {
		return ErrUnbalancedEntry
	}
	total := new(big.Int)
	for _, line := range e.Lines {
		if line.AccountKey == "" || line.AccountType == "" || line.AmountMinor == 0 {
			return ErrInvalidEntry
		}
		total.Add(total, big.NewInt(line.AmountMinor))
	}
	if total.Sign() != 0 {
		return ErrUnbalancedEntry
	}
	return nil
}

func AccountKey(kind AccountType, ownerID string) string {
	if ownerID == "" {
		return string(kind)
	}
	return string(kind) + ":" + ownerID
}

func OnlinePaymentEntry(orderID, provider, reference, restaurantID string, total, subtotal, commission, delivery int64) Entry {
	entry := Entry{
		SourceKey: "payment:" + strings.ToLower(provider) + ":" + reference + ":confirmed",
		OrderID:   orderID, EntryType: "online_payment", Currency: "ETB", Description: "Online payment confirmed",
	}
	lines := []Line{
		{AccountKey: AccountKey(CustomerClearing, ""), AccountType: CustomerClearing, AmountMinor: total},
		{AccountKey: AccountKey(RestaurantPayable, restaurantID), AccountType: RestaurantPayable, OwnerRestaurantID: restaurantID, AmountMinor: -(subtotal - commission)},
		{AccountKey: AccountKey(PlatformRevenue, ""), AccountType: PlatformRevenue, AmountMinor: -commission},
		{AccountKey: AccountKey(DeliveryFeePool, ""), AccountType: DeliveryFeePool, AmountMinor: -delivery},
	}
	for _, line := range lines {
		if line.AmountMinor != 0 {
			entry.Lines = append(entry.Lines, line)
		}
	}
	return entry
}
