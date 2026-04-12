package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedisRecipientCodeCache(t *testing.T) (*RedisRecipientCodeCache, *miniredis.Miniredis, func()) {
	t.Helper()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	cleanup := func() {
		_ = client.Close()
		mini.Close()
	}

	return NewRedisRecipientCodeCache(client), mini, cleanup
}

func TestRedisRecipientCodeCache_GetSet(t *testing.T) {
	cache, mini, cleanup := newTestRedisRecipientCodeCache(t)
	defer cleanup()

	ctx := context.Background()
	if err := cache.SetRecipientCode(ctx, "ng", "011", "0001234567", "RCP_123", time.Hour); err != nil {
		t.Fatalf("SetRecipientCode returned error: %v", err)
	}

	code, found, err := cache.GetRecipientCode(ctx, "NG", "011", "0001234567")
	if err != nil {
		t.Fatalf("GetRecipientCode returned error: %v", err)
	}
	if !found {
		t.Fatalf("GetRecipientCode did not find value")
	}
	if code != "RCP_123" {
		t.Fatalf("recipient code = %s, want RCP_123", code)
	}

	ttl := mini.TTL(cache.key("NG", "011", "0001234567"))
	if ttl <= 0 || ttl > time.Hour {
		t.Fatalf("ttl = %s, want between 0 and 1h", ttl)
	}
}
