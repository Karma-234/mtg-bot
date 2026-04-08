package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisUserStateCache struct {
	rdb *redis.Client
}

func NewRedisUserStateCache(rdb *redis.Client) *RedisUserStateCache {
	return &RedisUserStateCache{rdb: rdb}
}

func (c *RedisUserStateCache) key(chatID int64) string {
	return fmt.Sprintf("mtg:user:duration:%d", chatID)
}

func (c *RedisUserStateCache) SetSelectedDuration(ctx context.Context, chatID int64, d time.Duration, ttl time.Duration) error {
	seconds := int64(d / time.Second)
	return c.rdb.Set(ctx, c.key(chatID), strconv.FormatInt(seconds, 10), ttl).Err()
}

func (c *RedisUserStateCache) GetSelectedDuration(ctx context.Context, chatID int64) (time.Duration, bool, error) {
	v, err := c.rdb.Get(ctx, c.key(chatID)).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	seconds, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, err
	}

	return time.Duration(seconds) * time.Second, true, nil
}
