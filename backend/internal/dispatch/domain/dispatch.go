package domain

import "errors"

var (
	ErrInvalidCourier = errors.New("invalid courier data")
	ErrInvalidOrder   = errors.New("order is not dispatchable")
	ErrOfferExpired   = errors.New("courier offer is expired or unavailable")
	ErrOrderClaimed   = errors.New("order has already been claimed")
	ErrInvalidPIN     = errors.New("delivery PIN is invalid")
)

type CourierID string
type OrderID string
type Tier int

const (
	TierOne Tier = iota + 1
	TierTwo
	TierThree
)

func (t Tier) RadiusKM() float64 {
	switch t {
	case TierOne:
		return 1.5
	case TierTwo:
		return 3
	case TierThree:
		return 5
	default:
		return 0
	}
}
func (t Tier) Next() (Tier, bool) {
	if t < TierThree {
		return t + 1, true
	}
	return 0, false
}

type VehicleType string

const (
	VehicleBicycle   VehicleType = "bicycle"
	VehicleMotorbike VehicleType = "motorbike"
	VehicleCar       VehicleType = "car"
)

func (v VehicleType) Valid() bool {
	return v == VehicleBicycle || v == VehicleMotorbike || v == VehicleCar
}

type CourierStatus struct {
	Courier CourierID
	Online  bool
	Vehicle VehicleType
}
type Location struct {
	Courier   CourierID
	Latitude  float64
	Longitude float64
}
type Order struct {
	ID            OrderID
	RestaurantID  string
	RestaurantLat float64
	RestaurantLng float64
	PaymentMethod string
	TotalMinor    int64
	Status        string
	KitchenStatus string
	CourierID     CourierID
}
type Offer struct {
	OrderID          OrderID   `json:"order_id"`
	CourierID        CourierID `json:"courier_id"`
	DistanceKM       float64   `json:"distance_km"`
	ExpiresInSeconds int       `json:"expires_in_seconds"`
}
type NearbyCourier struct {
	ID         CourierID
	DistanceKM float64
}

type Courier struct {
	ID        CourierID
	Tier      Tier
	Available bool
}
type Assignment struct {
	Order   OrderID
	Courier CourierID
}
