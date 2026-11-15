package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mesob-delivery/backend/internal/dispatch/domain"
	redisclient "github.com/redis/go-redis/v9"
)

const (
	CourierGeoKey       = "couriers:geo"
	CourierHeartbeatTTL = 45 * time.Second
)

type Registry struct{ client *redisclient.Client }

func New(url string) (*Registry, error) {
	opts, err := redisclient.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse dispatch Redis URL: %w", err)
	}
	return &Registry{client: redisclient.NewClient(opts)}, nil
}
func NewWithClient(client *redisclient.Client) *Registry { return &Registry{client: client} }
func (r *Registry) Close() error                         { return r.client.Close() }
func (r *Registry) UpdateStatus(ctx context.Context, status domain.CourierStatus) error {
	key := statusKey(status.Courier)
	if !status.Online {
		if err := r.client.Del(ctx, key, heartbeatKey(status.Courier)).Err(); err != nil {
			return err
		}
		return r.client.ZRem(ctx, CourierGeoKey, string(status.Courier)).Err()
	}
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, key, "online", "true", "vehicle", string(status.Vehicle))
	pipe.Expire(ctx, key, 2*time.Minute)
	_, err := pipe.Exec(ctx)
	return err
}
func (r *Registry) UpdateLocation(ctx context.Context, loc domain.Location) error {
	if err := r.client.GeoAdd(ctx, CourierGeoKey, &redisclient.GeoLocation{Name: string(loc.Courier), Longitude: loc.Longitude, Latitude: loc.Latitude}).Err(); err != nil {
		return err
	}
	return r.client.Set(ctx, heartbeatKey(loc.Courier), strconv.FormatInt(time.Now().Unix(), 10), CourierHeartbeatTTL).Err()
}
func (r *Registry) Nearby(ctx context.Context, lat, lng, radius float64, limit int) ([]domain.NearbyCourier, error) {
	locations := r.client.GeoSearchLocation(ctx, CourierGeoKey, &redisclient.GeoSearchLocationQuery{
		GeoSearchQuery: redisclient.GeoSearchQuery{
			Longitude: lng, Latitude: lat, Radius: radius, RadiusUnit: "km",
			Sort: "ASC", Count: limit,
		},
		WithDist: true,
	})
	if err := locations.Err(); err != nil {
		return nil, err
	}
	entries := locations.Val()
	out := make([]domain.NearbyCourier, 0, len(entries))
	for _, loc := range entries {
		out = append(out, domain.NearbyCourier{ID: domain.CourierID(loc.Name), DistanceKM: loc.Dist})
	}
	return out, nil
}
func (r *Registry) HasHeartbeat(ctx context.Context, id domain.CourierID) (bool, error) {
	n, err := r.client.Exists(ctx, heartbeatKey(id)).Result()
	return n > 0, err
}
func (r *Registry) SetOffers(ctx context.Context, order domain.OrderID, couriers []domain.CourierID, ttl time.Duration) error {
	orderKey := offerKey(order)
	historyKey := offerHistoryKey(order)
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, orderKey)
	for _, id := range couriers {
		pipe.SAdd(ctx, orderKey, string(id))
		pipe.SAdd(ctx, historyKey, string(id))
		courierKey := courierOffersKey(id)
		pipe.SAdd(ctx, courierKey, string(order))
		pipe.Expire(ctx, courierKey, ttl)
	}
	pipe.Expire(ctx, orderKey, ttl)
	pipe.Expire(ctx, historyKey, 10*time.Minute)
	_, err := pipe.Exec(ctx)
	return err
}
func (r *Registry) WasOffered(ctx context.Context, order domain.OrderID, courier domain.CourierID) (bool, error) {
	return r.client.SIsMember(ctx, offerHistoryKey(order), string(courier)).Result()
}
func (r *Registry) IsOffered(ctx context.Context, order domain.OrderID, courier domain.CourierID) (bool, error) {
	return r.client.SIsMember(ctx, offerKey(order), string(courier)).Result()
}
func (r *Registry) OffersForCourier(ctx context.Context, courier domain.CourierID) ([]domain.OrderID, error) {
	members, err := r.client.SMembers(ctx, courierOffersKey(courier)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]domain.OrderID, 0, len(members))
	for _, m := range members {
		out = append(out, domain.OrderID(m))
	}
	return out, nil
}

func (r *Registry) NotifyOffer(ctx context.Context, offer domain.Offer) error {
	payload, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, courierOfferChannel(offer.CourierID), payload).Err()
}

func (r *Registry) SubscribeOffers(ctx context.Context, courier domain.CourierID) (<-chan domain.Offer, error) {
	pubsub := r.client.Subscribe(ctx, courierOfferChannel(courier))
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, err
	}
	out := make(chan domain.Offer)
	go func() {
		defer close(out)
		defer func() {
			if err := pubsub.Close(); err != nil {
				slog.Warn("failed to close courier offer subscription", "error", err)
			}
		}()
		for message := range pubsub.Channel() {
			var offer domain.Offer
			if err := json.Unmarshal([]byte(message.Payload), &offer); err != nil {
				continue
			}
			select {
			case out <- offer:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
func (r *Registry) DeleteOffers(ctx context.Context, order domain.OrderID) error {
	key := offerKey(order)
	members, err := r.client.SMembers(ctx, key).Result()
	if err != nil && !errors.Is(err, redisclient.Nil) {
		return err
	}
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, key)
	for _, id := range members {
		pipe.SRem(ctx, courierOffersKey(domain.CourierID(id)), string(order))
	}
	_, err = pipe.Exec(ctx)
	return err
}
func (r *Registry) AcquireOrderLock(ctx context.Context, order domain.OrderID, token string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, dispatchLockKey(order), token, ttl).Result()
}
func (r *Registry) ReleaseOrderLock(ctx context.Context, order domain.OrderID, token string) error {
	const script = `if redis.call('GET',KEYS[1]) == ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end`
	return r.client.Eval(ctx, script, []string{dispatchLockKey(order)}, token).Err()
}
func statusKey(id domain.CourierID) string           { return "courier:status:" + string(id) }
func heartbeatKey(id domain.CourierID) string        { return "courier:heartbeat:" + string(id) }
func offerKey(id domain.OrderID) string              { return "dispatch:offers:" + string(id) }
func offerHistoryKey(id domain.OrderID) string       { return "dispatch:offered_history:" + string(id) }
func courierOffersKey(id domain.CourierID) string    { return "courier:offers:" + string(id) }
func courierOfferChannel(id domain.CourierID) string { return "courier:" + string(id) + ":offers" }
func dispatchLockKey(id domain.OrderID) string       { return "order:dispatch_lock:" + string(id) }
