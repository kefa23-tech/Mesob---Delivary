package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mesob-delivery/backend/internal/realtime"
	redisclient "github.com/redis/go-redis/v9"
)

const liveCourierLocationTTL = 2 * time.Minute

type Hub struct{ client *redisclient.Client }

func New(url string) (*Hub, error) {
	opts, err := redisclient.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse realtime Redis URL: %w", err)
	}
	return NewWithClient(redisclient.NewClient(opts)), nil
}

func NewWithClient(client *redisclient.Client) *Hub { return &Hub{client: client} }
func (h *Hub) Close() error                         { return h.client.Close() }

func (h *Hub) Publish(ctx context.Context, event realtime.Event) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal realtime event: %w", err)
	}
	channels := make([]string, 0, 3)
	if event.OrderID != "" {
		channels = append(channels, realtime.OrderChannel(event.OrderID))
	}
	if event.RestaurantID != "" {
		channels = append(channels, realtime.RestaurantChannel(event.RestaurantID))
	}
	if event.CourierID != "" {
		channels = append(channels, realtime.CourierChannel(event.CourierID))
	}
	if len(channels) == 0 {
		return errors.New("realtime event has no destination")
	}
	if event.Type == realtime.CourierLocationUpdate && event.CourierID != "" && event.Location != nil {
		location, _ := json.Marshal(event.Location)
		if err := h.client.Set(ctx, courierLocationKey(event.CourierID), location, liveCourierLocationTTL).Err(); err != nil {
			return fmt.Errorf("save courier live location: %w", err)
		}
	}
	pipe := h.client.Pipeline()
	for _, channel := range channels {
		pipe.Publish(ctx, channel, encoded)
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("publish realtime event: %w", err)
	}
	return nil
}

func (h *Hub) SubscribeOrder(ctx context.Context, orderID string) (<-chan realtime.Event, error) {
	return h.subscribe(ctx, realtime.OrderChannel(orderID), orderID)
}

func (h *Hub) SubscribeRestaurant(ctx context.Context, restaurantID string) (<-chan realtime.Event, error) {
	return h.subscribe(ctx, realtime.RestaurantChannel(restaurantID), restaurantID)
}
func (h *Hub) SubscribeCourier(ctx context.Context, courierID string) (<-chan realtime.Event, error) {
	return h.subscribe(ctx, realtime.CourierChannel(courierID), courierID)
}

func (h *Hub) subscribe(ctx context.Context, channel, id string) (<-chan realtime.Event, error) {
	if id == "" {
		return nil, errors.New("subscription id is required")
	}
	pubsub := h.client.Subscribe(ctx, channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribe to order events: %w", err)
	}
	out := make(chan realtime.Event)
	go func() {
		defer close(out)
		defer pubsub.Close()
		for message := range pubsub.Channel() {
			var event realtime.Event
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				continue
			}
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (h *Hub) CourierLocation(ctx context.Context, courierID string) (*realtime.CourierLocation, error) {
	encoded, err := h.client.Get(ctx, courierLocationKey(courierID)).Bytes()
	if errors.Is(err, redisclient.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var location realtime.CourierLocation
	if err := json.Unmarshal(encoded, &location); err != nil {
		return nil, err
	}
	return &location, nil
}

func courierLocationKey(courierID string) string { return "courier:live_location:" + courierID }
