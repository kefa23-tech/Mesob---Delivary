package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
	redisclient "github.com/redis/go-redis/v9"
	"time"
)

const namespaceKey = "mesob:restaurants:cache:version"

type Cache struct {
	client *redisclient.Client
	ttl    time.Duration
}

func New(url string) (*Cache, error) {
	options, err := redisclient.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	return &Cache{client: redisclient.NewClient(options), ttl: 30 * time.Second}, nil
}
func (c *Cache) Close() error { return c.client.Close() }
func (c *Cache) version(ctx context.Context) string {
	version, err := c.client.Get(ctx, namespaceKey).Result()
	if err != nil {
		return "0"
	}
	return version
}
func (c *Cache) versionedKey(ctx context.Context, key string) string {
	return "mesob:restaurants:cache:" + c.version(ctx) + ":" + key
}
func (c *Cache) Get(ctx context.Context, key string) (domain.SearchResult, bool, error) {
	value, err := c.client.Get(ctx, c.versionedKey(ctx, key)).Bytes()
	if err == redisclient.Nil {
		return domain.SearchResult{}, false, nil
	}
	if err != nil {
		return domain.SearchResult{}, false, err
	}
	var result domain.SearchResult
	return result, true, json.Unmarshal(value, &result)
}
func (c *Cache) Set(ctx context.Context, key string, result domain.SearchResult) error {
	value, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.versionedKey(ctx, key), value, c.ttl).Err()
}
func (c *Cache) Invalidate(ctx context.Context, _ domain.RestaurantID) error {
	return c.client.Incr(ctx, namespaceKey).Err()
}
