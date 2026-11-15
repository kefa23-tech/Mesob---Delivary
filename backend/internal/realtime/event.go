package realtime

import (
	"context"
	"time"
)

type EventType string

const (
	OrderCreated               EventType = "ORDER_CREATED"
	OrderStatusUpdated         EventType = "ORDER_STATUS_UPDATED"
	CourierLocationUpdate      EventType = "COURIER_LOCATION_UPDATED"
	CourierOfferCreated        EventType = "COURIER_OFFER_CREATED"
	CourierAvailabilityUpdated EventType = "COURIER_AVAILABILITY_UPDATED"
)

type CourierLocation struct {
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	Heading    float64   `json:"heading"`
	ETASeconds int       `json:"eta_seconds"`
	ETAAt      time.Time `json:"eta_at"`
}

type Event struct {
	ID           string           `json:"id"`
	Type         EventType        `json:"type"`
	OrderID      string           `json:"order_id,omitempty"`
	RestaurantID string           `json:"restaurant_id,omitempty"`
	CourierID    string           `json:"courier_id,omitempty"`
	Status       string           `json:"status,omitempty"`
	OccurredAt   time.Time        `json:"occurred_at"`
	Location     *CourierLocation `json:"location,omitempty"`
	Data         map[string]any   `json:"data,omitempty"`
}

type Publisher interface {
	Publish(context.Context, Event) error
}

type Subscription interface {
	SubscribeOrder(context.Context, string) (<-chan Event, error)
	SubscribeRestaurant(context.Context, string) (<-chan Event, error)
	SubscribeCourier(context.Context, string) (<-chan Event, error)
}

type Hub interface {
	Publisher
	Subscription
}

func OrderChannel(orderID string) string           { return "order:events:" + orderID }
func RestaurantChannel(restaurantID string) string { return "restaurant:events:" + restaurantID }
func CourierChannel(courierID string) string       { return "courier:events:" + courierID }
