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

// RemittanceBalance applies a positive cash payment against courier exposure.
func RemittanceBalance(balance, amount int64) (int64, error) {
	if amount <= 0 {
		return balance, ErrInvalidEntry
	}
	if amount > balance {
		return balance, ErrInsufficientCash
	}
	return balance - amount, nil
}

// RefundEntry builds a balanced reversal against credit destinations from the original payment.
// Source credits are supplied as negative signed lines and reversed in order until the refund is covered.
func RefundEntry(sourceKey, orderID, currency, description string, amount int64, sourceCredits []Line) (Entry, error) {
	if amount <= 0 {
		return Entry{}, ErrInvalidEntry
	}
	left := amount
	lines := []Line{}
	for _, source := range sourceCredits {
		if source.AmountMinor >= 0 || source.AccountType == CustomerClearing {
			continue
		}
		credit := -source.AmountMinor
		if credit > left {
			credit = left
		}
		if credit > 0 {
			source.AmountMinor = credit
			lines = append(lines, source)
			left -= credit
		}
		if left == 0 {
			break
		}
	}
	if left > 0 {
		lines = append(lines, Line{AccountKey: AccountKey(PlatformRevenue, ""), AccountType: PlatformRevenue, AmountMinor: left})
	}
	lines = append(lines, Line{AccountKey: AccountKey(CustomerClearing, ""), AccountType: CustomerClearing, AmountMinor: -amount})
	e := Entry{SourceKey: sourceKey, OrderID: orderID, EntryType: "admin_refund", Currency: currency, Description: description, Lines: lines}
	return e, e.Validate()
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
