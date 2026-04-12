package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"github.com/redis/go-redis/v9"
)

type RedisBankCache struct {
	rdb *redis.Client
}

func NewRedisBankCache(rdb *redis.Client) *RedisBankCache {
	return &RedisBankCache{rdb: rdb}
}

func (c *RedisBankCache) key(country string) string {
	return fmt.Sprintf("mtg:banks:list:%s", country)
}

func (c *RedisBankCache) GetBanks(ctx context.Context, country string) ([]service.BankEntry, bool, error) {
	v, err := c.rdb.Get(ctx, c.key(country)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var out []service.BankEntry
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return nil, false, err
	}

	return out, true, nil
}

func (c *RedisBankCache) SetBanks(ctx context.Context, country string, banks []service.BankEntry, ttl time.Duration) error {
	b, err := json.Marshal(banks)
	if err != nil {
		return err
	}

	return c.rdb.Set(ctx, c.key(country), b, ttl).Err()
}
