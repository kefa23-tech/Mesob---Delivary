package domain

import (
	"errors"
	"strings"
)

var (
	ErrInvalidRequest = errors.New("invalid admin operation request")
	ErrNotFound       = errors.New("admin operation target not found")
	ErrConflict       = errors.New("admin operation conflicts with current state")
)

type KYCQueue struct {
	Restaurants []RestaurantKYC `json:"restaurants"`
	Couriers    []CourierKYC    `json:"couriers"`
}
type RestaurantKYC struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	OwnerName          string `json:"owner_name"`
	TIN                string `json:"tin"`
	BusinessLicenseURL string `json:"business_license_url"`
	Status             string `json:"status"`
	CreatedAt          string `json:"created_at"`
}
type CourierKYC struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Phone            string         `json:"phone"`
	NationalID       string         `json:"national_id"`
	DriverLicenseURL string         `json:"driver_license_url"`
	VehicleInfo      map[string]any `json:"vehicle_info"`
	Status           string         `json:"status"`
	CreatedAt        string         `json:"created_at"`
}
type StuckOrder struct {
	ID                 string            `json:"id"`
	RestaurantID       string            `json:"restaurant_id"`
	RestaurantName     string            `json:"restaurant_name"`
	CustomerName       string            `json:"customer_name"`
	Status             string            `json:"status"`
	PaymentMethod      string            `json:"payment_method"`
	TotalMinor         int64             `json:"total_minor"`
	PlacedAt           string            `json:"placed_at"`
	MinutesStuck       int               `json:"minutes_stuck"`
	ManualIntervention bool              `json:"manual_intervention"`
	EligibleCouriers   []EligibleCourier `json:"eligible_couriers"`
}
type EligibleCourier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	VehicleType string `json:"vehicle_type"`
}
type LedgerSummary struct {
	Accounts   map[string]int64 `json:"accounts"`
	TotalMinor int64            `json:"total_minor"`
	Balanced   bool             `json:"balanced"`
}
type LedgerLine struct {
	AccountKey  string `json:"account_key"`
	AccountType string `json:"account_type"`
	AmountMinor int64  `json:"amount_minor"`
}
type LedgerEntry struct {
	ID          string       `json:"id"`
	SourceKey   string       `json:"source_key"`
	OrderID     string       `json:"order_id,omitempty"`
	EntryType   string       `json:"entry_type"`
	Currency    string       `json:"currency"`
	Description string       `json:"description"`
	CreatedAt   string       `json:"created_at"`
	Lines       []LedgerLine `json:"lines"`
}
type LedgerPage struct {
	Data  []LedgerEntry `json:"data"`
	Page  int           `json:"page"`
	Limit int           `json:"limit"`
	Total int           `json:"total"`
}
type Overview struct {
	DailyGMVMinor int64 `json:"daily_gmv_minor"`
	ActiveOrders  int64 `json:"active_orders"`
	StuckOrders   int   `json:"stuck_orders"`
}

type ReviewRequest struct{ EntityType, ID, Decision, Notes string }
type AssignmentRequest struct{ OrderID, CourierID, AdminID string }
type DisputeRequest struct {
	OrderID, AdminID, Resolution, Notes string
	RefundMinor                         int64
}
type SettlementRequest struct {
	CourierID, AdminID, Reference string
	AmountMinor                   int64
}

func ValidDecision(decision string) bool { return decision == "approved" || decision == "rejected" }
func ValidNotes(notes string) bool       { return strings.TrimSpace(notes) != "" }
func ValidResolution(resolution string) bool {
	return resolution == "full_refund" || resolution == "partial_credit" || resolution == "cancellation"
}
