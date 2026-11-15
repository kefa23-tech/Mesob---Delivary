package redis

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
func (s *CartStore) Close() error           { return s.client.Close() }
func cartKey(token string) string           { return "mesob:group-cart:" + token }
func cartConversionKey(token string) string { return "mesob:group-cart-conversion:" + token }

func idempotencyKey(userID, key string) string {
	digest := sha256.Sum256([]byte(userID + ":" + key))
	return "mesob:order-idempotency:" + base64.RawURLEncoding.EncodeToString(digest[:])
}

func (s *CartStore) Begin(ctx context.Context, userID, key string) ([]byte, bool, error) {
	redisKey := idempotencyKey(userID, key)
	claimed, err := s.client.SetNX(ctx, redisKey, "processing", domain.OrderIdempotencyTTL).Result()
	if err != nil {
		return nil, false, err
	}
	if claimed {
		return nil, false, nil
	}
	value, err := s.client.Get(ctx, redisKey).Bytes()
	if err == redisclient.Nil {
		// The entry expired between SETNX and GET; compete for a fresh claim.
		claimed, err = s.client.SetNX(ctx, redisKey, "processing", domain.OrderIdempotencyTTL).Result()
		if err != nil {
			return nil, false, err
		}
		return nil, !claimed, nil
	}
	if err != nil {
		return nil, false, err
	}
	if string(value) == "processing" {
		return nil, true, nil
	}
	return value, false, nil
}

func (s *CartStore) Complete(ctx context.Context, userID, key string, body []byte) error {
	return s.client.Set(ctx, idempotencyKey(userID, key), body, domain.OrderIdempotencyTTL).Err()
}

func (s *CartStore) Release(ctx context.Context, userID, key string) error {
	return s.client.Del(ctx, idempotencyKey(userID, key)).Err()
}

// CleanupEphemeralLocks removes malformed lock keys that lost their required TTL.
// Normal abandoned locks expire automatically; this is a periodic safety repair.
func (s *CartStore) CleanupEphemeralLocks(ctx context.Context) error {
	for _, pattern := range []string{"mesob:group-cart-conversion:*", "mesob:order-idempotency:*"} {
		var cursor uint64
		for {
			keys, next, err := s.client.Scan(ctx, cursor, pattern, 200).Result()
			if err != nil {
				return err
			}
			for _, key := range keys {
				ttl, err := s.client.TTL(ctx, key).Result()
				if err != nil {
					return err
				}
				if ttl == -1 {
					if err := s.client.Del(ctx, key).Err(); err != nil {
						return err
					}
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	return nil
}

func (s *CartStore) ExpireConverted(ctx context.Context, token string) error {
	return s.client.Del(ctx, cartKey(token), cartConversionKey(token)).Err()
}

func (s *CartStore) ClaimConversion(ctx context.Context, token string) (domain.GroupCart, error) {
	claimed, err := s.client.SetNX(ctx, cartConversionKey(token), "converting", domain.OrderIdempotencyTTL).Result()
	if err != nil {
		return domain.GroupCart{}, err
	}
	if !claimed {
		return domain.GroupCart{}, domain.ErrCartLocked
	}
	cart, err := s.Get(ctx, token)
	if err != nil {
		_ = s.ReleaseConversion(context.Background(), token)
		return domain.GroupCart{}, err
	}
	if cart.Status != domain.CartLocked || len(cart.Items) == 0 {
		_ = s.ReleaseConversion(context.Background(), token)
		return domain.GroupCart{}, domain.ErrCartLocked
	}
	return cart, nil
}

func (s *CartStore) ReleaseConversion(ctx context.Context, token string) error {
	return s.client.Del(ctx, cartConversionKey(token)).Err()
}
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
