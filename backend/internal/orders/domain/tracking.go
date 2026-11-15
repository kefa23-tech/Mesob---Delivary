package domain

import (
	"errors"
	"time"
)

var ErrOrderNotFound = errors.New("order not found")

type TrackingSnapshot struct {
	ID               string     `json:"id"`
	Status           string     `json:"status"`
	PaymentStatus    string     `json:"payment_status"`
	RestaurantStatus string     `json:"restaurant_status,omitempty"`
	RestaurantID     string     `json:"restaurant_id"`
	RestaurantName   string     `json:"restaurant_name"`
	RestaurantLat    float64    `json:"restaurant_lat"`
	RestaurantLng    float64    `json:"restaurant_lng"`
	DeliveryLat      float64    `json:"delivery_lat"`
	DeliveryLng      float64    `json:"delivery_lng"`
	DeliveryAddress  string     `json:"delivery_address"`
	CourierID        string     `json:"courier_id,omitempty"`
	CourierName      string     `json:"courier_name,omitempty"`
	CourierPhone     string     `json:"courier_phone,omitempty"`
	CourierVehicle   string     `json:"courier_vehicle,omitempty"`
	CourierLat       *float64   `json:"courier_lat,omitempty"`
	CourierLng       *float64   `json:"courier_lng,omitempty"`
	CourierHeading   *float64   `json:"courier_heading,omitempty"`
	ETAAt            *time.Time `json:"eta_at,omitempty"`
}
