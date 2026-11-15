package domain

import (
	"errors"
	"fmt"
	"time"
)

const OrderIdempotencyTTL = 24 * time.Hour

var (
	ErrInvalidOrder          = errors.New("invalid order")
	ErrRestaurantClosed      = errors.New("restaurant is closed")
	ErrAddressOutOfRange     = errors.New("delivery address is outside the restaurant delivery area")
	ErrItemUnavailable       = errors.New("one or more items are unavailable")
	ErrInsufficientStock     = errors.New("insufficient stock")
	ErrIdempotencyConflict   = errors.New("idempotency key is already in progress")
	ErrAddressNotFound       = errors.New("delivery address not found")
	ErrCODOrderLimit         = errors.New("cash-on-delivery orders cannot exceed 200000 santim")
	ErrCODFirstOrder         = errors.New("cash on delivery requires a previously delivered order")
	ErrCODUnpaidCancellation = errors.New("cash on delivery is unavailable after an unpaid cancellation")
	ErrCODSuspended          = errors.New("cash-on-delivery privileges are suspended")
)

const MaxCODOrderMinor int64 = 200_000

// ValidateCODEligibility applies the customer and amount policy using values
// loaded while the checkout transaction holds the customer row lock.
func ValidateCODEligibility(totalMinor int64, completedOrders, unpaidCancellations int64, suspended bool) error {
	if totalMinor < 0 || totalMinor > MaxCODOrderMinor {
		return ErrCODOrderLimit
	}
	if suspended {
		return ErrCODSuspended
	}
	if completedOrders < 1 {
		return ErrCODFirstOrder
	}
	if unpaidCancellations > 0 {
		return ErrCODUnpaidCancellation
	}
	return nil
}

type PaymentMethod string

const (
	PaymentTelebirr PaymentMethod = "TELEBIRR"
	PaymentChapa    PaymentMethod = "CHAPA"
	PaymentCOD      PaymentMethod = "COD"
)

func (p PaymentMethod) Valid() bool {
	return p == PaymentTelebirr || p == PaymentChapa || p == PaymentCOD
}

type OrderStatus string

const (
	OrderPending              OrderStatus = "pending"
	OrderPaid                 OrderStatus = "PAID"
	OrderConfirmedCOD         OrderStatus = "CONFIRMED_COD"
	OrderAccepted             OrderStatus = "ACCEPTED"
	OrderKitchenPreparing     OrderStatus = "PREPARING"
	OrderConfirmed            OrderStatus = "confirmed"
	OrderPreparing            OrderStatus = "preparing"
	OrderReadyForPickup       OrderStatus = "READY_FOR_PICKUP"
	OrderRejectedByRestaurant OrderStatus = "REJECTED_BY_RESTAURANT"
	OrderCancelledTimeout     OrderStatus = "CANCELLED_TIMEOUT"
	OrderReady                OrderStatus = "ready"
	OrderDispatched           OrderStatus = "dispatched"
	OrderDelivered            OrderStatus = "delivered"
	OrderCancelled            OrderStatus = "cancelled"
)

func (s OrderStatus) CanTransition(next OrderStatus) bool {
	switch s {
	case OrderPaid, OrderConfirmedCOD:
		return next == OrderAccepted || next == OrderRejectedByRestaurant || next == OrderCancelledTimeout
	case OrderAccepted:
		return next == OrderKitchenPreparing || next == OrderRejectedByRestaurant
	case OrderKitchenPreparing:
		return next == OrderReadyForPickup || next == OrderRejectedByRestaurant
	case OrderPending:
		return next == OrderConfirmed || next == OrderCancelled
	case OrderConfirmed:
		return next == OrderPreparing || next == OrderCancelled
	case OrderPreparing:
		return next == OrderReady || next == OrderCancelled
	case OrderReady:
		return next == OrderDispatched || next == OrderCancelled
	case OrderDispatched:
		return next == OrderDelivered
	default:
		return false
	}
}

type KitchenOrder struct {
	Order
	CustomerName    string `json:"customer_name"`
	CustomerPhone   string `json:"customer_phone"`
	PrepMinutes     *int   `json:"prep_minutes,omitempty"`
	RejectionReason string `json:"rejection_reason,omitempty"`
}

type RestaurantRejectionReason string

const (
	RejectOutOfStock       RestaurantRejectionReason = "OUT_OF_STOCK"
	RejectTooBusy          RestaurantRejectionReason = "TOO_BUSY"
	RejectRestaurantClosed RestaurantRejectionReason = "RESTAURANT_CLOSED"
	RejectOther            RestaurantRejectionReason = "OTHER"
)

func (r RestaurantRejectionReason) Valid() bool {
	return r == RejectOutOfStock || r == RejectTooBusy || r == RejectRestaurantClosed || r == RejectOther
}

type CheckoutItem struct {
	MenuItemID string           `json:"menu_item_id"`
	Quantity   int              `json:"quantity"`
	Options    []SelectedOption `json:"options,omitempty"`
}

type CheckoutRequest struct {
	AddressID      string         `json:"address_id"`
	PaymentMethod  PaymentMethod  `json:"payment_method"`
	GroupCartToken string         `json:"group_cart_token,omitempty"`
	Items          []CheckoutItem `json:"items,omitempty"`
}

type OrderItem struct {
	ID             string           `json:"id"`
	MenuItemID     string           `json:"menu_item_id,omitempty"`
	ItemName       string           `json:"item_name"`
	UnitPriceMinor int64            `json:"unit_price_minor"`
	Quantity       int              `json:"quantity"`
	Options        []SelectedOption `json:"options"`
	LineTotalMinor int64            `json:"line_total_minor"`
}

type Order struct {
	ID               string        `json:"id"`
	UserID           string        `json:"user_id"`
	RestaurantID     string        `json:"restaurant_id"`
	AddressID        string        `json:"address_id"`
	Status           OrderStatus   `json:"status"`
	PaymentMethod    PaymentMethod `json:"payment_method"`
	PaymentStatus    string        `json:"payment_status"`
	SubtotalMinor    int64         `json:"subtotal_minor"`
	DeliveryFeeMinor int64         `json:"delivery_fee_minor"`
	CommissionMinor  int64         `json:"commission_minor"`
	TotalMinor       int64         `json:"total_minor"`
	Currency         string        `json:"currency"`
	Items            []OrderItem   `json:"items"`
	PlacedAt         string        `json:"placed_at"`
}

func (o *Order) Transition(next OrderStatus) error {
	if !o.Status.CanTransition(next) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidOrder, o.Status, next)
	}
	o.Status = next
	return nil
}

func ValidateCheckout(req CheckoutRequest) error {
	if req.AddressID == "" || !req.PaymentMethod.Valid() {
		return ErrInvalidOrder
	}
	if req.GroupCartToken == "" && len(req.Items) == 0 {
		return ErrInvalidOrder
	}
	if req.GroupCartToken != "" && len(req.Items) != 0 {
		return ErrInvalidOrder
	}
	for _, item := range req.Items {
		if item.MenuItemID == "" || item.Quantity <= 0 {
			return ErrInvalidOrder
		}
	}
	return nil
}

func ValidateSelections(item MenuItem, selected []SelectedOption) bool {
	return validateOptions(item.OptionGroups, selected)
}
