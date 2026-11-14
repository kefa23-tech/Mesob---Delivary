package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mesob-delivery/backend/internal/orders/domain"
	redisclient "github.com/redis/go-redis/v9"
	"time"
)

type CartStore struct{ client *redisclient.Client }

func New(url string) (*CartStore, error) {
	options, err := redisclient.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	return &CartStore{client: redisclient.NewClient(options)}, nil
}
func (s *CartStore) Close() error { return s.client.Close() }
func cartKey(token string) string { return "mesob:group-cart:" + token }
func (s *CartStore) Create(ctx context.Context, cart domain.GroupCart, ttl time.Duration) (domain.GroupCart, error) {
	tokenBytes := make([]byte, 18)
	if _, err := rand.Read(tokenBytes); err != nil {
		return domain.GroupCart{}, err
	}
	cart.Token = hex.EncodeToString(tokenBytes)
	value, err := json.Marshal(cart)
	if err != nil {
		return domain.GroupCart{}, err
	}
	if err := s.client.Set(ctx, cartKey(cart.Token), value, ttl).Err(); err != nil {
		return domain.GroupCart{}, err
	}
	return cart, nil
}
func (s *CartStore) Get(ctx context.Context, token string) (domain.GroupCart, error) {
	value, err := s.client.Get(ctx, cartKey(token)).Bytes()
	if err == redisclient.Nil {
		return domain.GroupCart{}, domain.ErrCartNotFound
	}
	if err != nil {
		return domain.GroupCart{}, err
	}
	var cart domain.GroupCart
	if err := json.Unmarshal(value, &cart); err != nil {
		return domain.GroupCart{}, err
	}
	return cart, nil
}
func (s *CartStore) AddItem(ctx context.Context, token string, item domain.CartItem, menuItem domain.MenuItem) (domain.GroupCart, error) {
	return s.update(ctx, token, func(cart *domain.GroupCart) error { return cart.AddItem(item, menuItem) })
}
func (s *CartStore) RemoveItem(ctx context.Context, token, itemID string) (domain.GroupCart, error) {
	return s.update(ctx, token, func(cart *domain.GroupCart) error { return cart.RemoveItem(itemID) })
}
func (s *CartStore) Lock(ctx context.Context, token string) (domain.GroupCart, error) {
	return s.update(ctx, token, func(cart *domain.GroupCart) error { return cart.Lock() })
}
func (s *CartStore) update(ctx context.Context, token string, mutate func(*domain.GroupCart) error) (domain.GroupCart, error) {
	var result domain.GroupCart
	err := s.client.Watch(ctx, func(tx *redisclient.Tx) error {
		value, err := tx.Get(ctx, cartKey(token)).Bytes()
		if err == redisclient.Nil {
			return domain.ErrCartNotFound
		}
		if err != nil {
			return err
		}
		var cart domain.GroupCart
		if err := json.Unmarshal(value, &cart); err != nil {
			return err
		}
		if err := mutate(&cart); err != nil {
			return err
		}
		encoded, err := json.Marshal(cart)
		if err != nil {
			return err
		}
		ttl, err := tx.TTL(ctx, cartKey(token)).Result()
		if err != nil {
			return err
		}
		if ttl <= 0 {
			return domain.ErrCartNotFound
		}
		if _, err := tx.TxPipelined(ctx, func(pipe redisclient.Pipeliner) error { pipe.Set(ctx, cartKey(token), encoded, ttl); return nil }); err != nil {
			return err
		}
		result = cart
		return nil
	}, cartKey(token))
	if err != nil {
		return domain.GroupCart{}, err
	}
	return result, nil
}
