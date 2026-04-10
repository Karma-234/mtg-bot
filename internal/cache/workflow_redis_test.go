package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/karma-234/mtg-bot/internal/service"
	"github.com/redis/go-redis/v9"
)

func newTestRedisWorkflowStore(t *testing.T) (*RedisWorkflowStore, *redis.Client, func()) {
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

	return NewRedisWorkflowStore(client), client, cleanup
}

func TestRedisWorkflowStoreSaveIfState(t *testing.T) {
	store, _, cleanup := newTestRedisWorkflowStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	record := &service.OrderWorkflowRecord{
		OrderID:   "order-1",
		ChatID:    42,
		State:     service.StateDetected,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
	}

	created, err := store.CreateIfAbsent(ctx, record)
	if err != nil {
		t.Fatalf("CreateIfAbsent returned error: %v", err)
	}
	if !created {
		t.Fatalf("CreateIfAbsent did not create record")
	}

	next := *record
	next.State = service.StateDetailFetching
	next.UpdatedAt = now.Add(1 * time.Second)

	applied, err := store.SaveIfState(ctx, &next, service.StateDetected)
	if err != nil {
		t.Fatalf("SaveIfState returned error: %v", err)
	}
	if !applied {
		t.Fatalf("SaveIfState should have applied transition")
	}

	stale := *record
	stale.State = service.StateRetryingDetail
	stale.UpdatedAt = now.Add(2 * time.Second)

	applied, err = store.SaveIfState(ctx, &stale, service.StateDetected)
	if err != nil {
		t.Fatalf("SaveIfState stale write returned error: %v", err)
	}
	if applied {
		t.Fatalf("SaveIfState should reject stale state write")
	}

	stored, found, err := store.Get(ctx, record.OrderID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !found {
		t.Fatalf("Get did not find workflow record")
	}
	if stored.State != service.StateDetailFetching {
		t.Fatalf("stored state = %s, want %s", stored.State, service.StateDetailFetching)
	}
}

func TestRedisWorkflowStoreSaveSetsTTL(t *testing.T) {
	store, client, cleanup := newTestRedisWorkflowStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	record := &service.OrderWorkflowRecord{
		OrderID:   "order-ttl",
		ChatID:    42,
		State:     service.StateDetected,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
	}

	if err := store.Save(ctx, record); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	ttl, err := client.TTL(ctx, store.workflowKey(record.OrderID)).Result()
	if err != nil {
		t.Fatalf("TTL returned error: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("workflow key TTL = %s, want > 0", ttl)
	}
}

func TestRedisWorkflowStoreListByChatPrunesMissingMembers(t *testing.T) {
	store, client, cleanup := newTestRedisWorkflowStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	record := &service.OrderWorkflowRecord{
		OrderID:   "order-live",
		ChatID:    99,
		State:     service.StateDetected,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
	}

	created, err := store.CreateIfAbsent(ctx, record)
	if err != nil {
		t.Fatalf("CreateIfAbsent returned error: %v", err)
	}
	if !created {
		t.Fatalf("CreateIfAbsent did not create live record")
	}

	if err := client.SAdd(ctx, store.workflowChatIndexKey(record.ChatID), "order-ghost").Err(); err != nil {
		t.Fatalf("SAdd ghost member returned error: %v", err)
	}

	records, err := store.ListByChat(ctx, record.ChatID)
	if err != nil {
		t.Fatalf("ListByChat returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ListByChat length = %d, want 1", len(records))
	}
	if records[0].OrderID != record.OrderID {
		t.Fatalf("ListByChat first id = %s, want %s", records[0].OrderID, record.OrderID)
	}

	members, err := client.SMembers(ctx, store.workflowChatIndexKey(record.ChatID)).Result()
	if err != nil {
		t.Fatalf("SMembers returned error: %v", err)
	}
	if len(members) != 1 || members[0] != record.OrderID {
		t.Fatalf("chat index members = %v, want [%s]", members, record.OrderID)
	}
}
