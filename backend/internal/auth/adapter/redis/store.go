package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mesob-delivery/backend/internal/auth/domain"
	"github.com/mesob-delivery/backend/internal/platform/redisconfig"
	"github.com/redis/go-redis/v9"
)

type Store struct{ client *redis.Client }

func New(url string) (*Store, error) {
	options, err := redisconfig.ParseGoRedisURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	return &Store{client: redis.NewClient(options)}, nil
}
func (s *Store) Close() error { return s.client.Close() }
func (s *Store) Save(ctx context.Context, otp domain.OTP) error {
	value, err := json.Marshal(otp)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, "mesob:otp:"+otp.Destination, value, time.Until(otp.ExpiresAt)).Err()
}
func (s *Store) Find(ctx context.Context, phone string) (domain.OTP, error) {
	value, err := s.client.Get(ctx, "mesob:otp:"+phone).Bytes()
	if err != nil {
		return domain.OTP{}, err
	}
	var otp domain.OTP
	return otp, json.Unmarshal(value, &otp)
}
func (s *Store) Delete(ctx context.Context, phone string) error {
	return s.client.Del(ctx, "mesob:otp:"+phone).Err()
}
func (s *Store) IsLocked(ctx context.Context, phone string) (bool, error) {
	count, err := s.client.Get(ctx, "mesob:otp:failures:"+phone).Int()
	return count >= 5, normalizeRedisNil(err)
}
func (s *Store) RecordFailure(ctx context.Context, phone string) (bool, error) {
	key := "mesob:otp:failures:" + phone
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := s.client.Expire(ctx, key, 15*time.Minute).Err(); err != nil {
			return false, err
		}
	}
	return count >= 5, nil
}
func (s *Store) Reset(ctx context.Context, phone string) error {
	return s.client.Del(ctx, "mesob:otp:failures:"+phone).Err()
}
func (s *Store) SaveRefresh(ctx context.Context, key string, user domain.User, ttl time.Duration) error {
	value, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, "mesob:refresh:"+key, value, ttl).Err()
}
func (s *Store) Take(ctx context.Context, key string) (domain.User, error) {
	value, err := s.client.GetDel(ctx, "mesob:refresh:"+key).Bytes()
	if err != nil {
		return domain.User{}, err
	}
	var user domain.User
	return user, json.Unmarshal(value, &user)
}
func (s *Store) DeleteRefresh(ctx context.Context, key string) error {
	return s.client.Del(ctx, "mesob:refresh:"+key).Err()
}
func normalizeRedisNil(err error) error {
	if err == redis.Nil {
		return nil
	}
	return err
}
