package cache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisRecipientCodeCache struct {
	rdb *redis.Client
}

func NewRedisRecipientCodeCache(rdb *redis.Client) *RedisRecipientCodeCache {
	return &RedisRecipientCodeCache{rdb: rdb}
}

func (c *RedisRecipientCodeCache) key(country, bankCode, accountNumber string) string {
	return fmt.Sprintf("mtg:paystack:recipient:%s:%s:%s", normalizeRecipientKeyPart(country), normalizeRecipientKeyPart(bankCode), strings.TrimSpace(accountNumber))
}

func (c *RedisRecipientCodeCache) GetRecipientCode(ctx context.Context, country, bankCode, accountNumber string) (string, bool, error) {
	value, err := c.rdb.Get(ctx, c.key(country, bankCode, accountNumber)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	return value, true, nil
}

func (c *RedisRecipientCodeCache) SetRecipientCode(ctx context.Context, country, bankCode, accountNumber, recipientCode string, ttl time.Duration) error {
	if recipientCode == "" {
		return fmt.Errorf("recipient code is required")
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}

	return c.rdb.Set(ctx, c.key(country, bankCode, accountNumber), recipientCode, ttl).Err()
}

func normalizeRecipientKeyPart(value string) string {
	return strings.TrimSpace(strings.ToUpper(value))
}
