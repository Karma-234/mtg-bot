package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"github.com/redis/go-redis/v9"
)

type RedisOrdersCache struct {
	rdb *redis.Client
}

func NewRedisOrdersCache(rdb *redis.Client) *RedisOrdersCache {
	return &RedisOrdersCache{rdb: rdb}
}

func (c *RedisOrdersCache) key(chatID int64) string {
	return fmt.Sprintf("mtg:orders:latest:%d", chatID)
}

func (c *RedisOrdersCache) GetLatestOrders(ctx context.Context, chatID int64) (*service.OrdersResponse, bool, error) {
	v, err := c.rdb.Get(ctx, c.key(chatID)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var out service.OrdersResponse
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return nil, false, err
	}

	return &out, true, nil
}

func (c *RedisOrdersCache) SetLatestOrders(ctx context.Context, chatID int64, data *service.OrdersResponse, ttl time.Duration) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return c.rdb.Set(ctx, c.key(chatID), b, ttl).Err()
}
