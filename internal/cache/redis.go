package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisCache(client *redis.Client, ttl time.Duration) *RedisCache {
	return &RedisCache{client: client, ttl: ttl}
}

func (c *RedisCache) Get(ctx context.Context, code string) (string, error) {
	return c.client.Get(ctx, "link:"+code).Result()
}

func (c *RedisCache) Set(ctx context.Context, code, url string) error {
	return c.client.Set(ctx, "link:"+code, url, c.ttl).Err()
}

func (c *RedisCache) SetWithTTL(ctx context.Context, code, url string, ttl time.Duration) error {
	return c.client.Set(ctx, "link:"+code, url, ttl).Err()
}

func (c *RedisCache) Delete(ctx context.Context, code string) error {
	return c.client.Del(ctx, "link:"+code).Err()
}
