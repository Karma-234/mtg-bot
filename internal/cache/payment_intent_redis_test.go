package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/karma-234/mtg-bot/internal/service"
	"github.com/redis/go-redis/v9"
)

func newTestRedisPaymentIntentStore(t *testing.T) (*RedisPaymentIntentStore, *redis.Client, func()) {
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

	return NewRedisPaymentIntentStore(client), client, cleanup
}

func testIntent(now time.Time, chatID int64, orderID, reference string) *service.PaymentIntentRecord {
	return &service.PaymentIntentRecord{
		PaymentID:         "pid-" + reference,
		ChatID:            chatID,
		OrderID:           orderID,
		Provider:          "bybit",
		PaystackReference: reference,
		AmountKobo:        120000,
		Currency:          "NGN",
		Status:            service.PaymentIntentInitiated,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func TestRedisPaymentIntentStore_CreateGetSaveList(t *testing.T) {
	store, _, cleanup := newTestRedisPaymentIntentStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	intent := testIntent(now, 77, "order-1", "ref-1")

	if err := store.Create(ctx, intent); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	stored, found, err := store.GetByReference(ctx, intent.PaystackReference)
	if err != nil {
		t.Fatalf("GetByReference returned error: %v", err)
	}
	if !found {
		t.Fatalf("GetByReference did not find intent")
	}
	if stored.OrderID != intent.OrderID {
		t.Fatalf("stored orderID = %s, want %s", stored.OrderID, intent.OrderID)
	}

	stored.Status = service.PaymentIntentTransferPending
	stored.TransferCode = "TRF_test"
	stored.UpdatedAt = now.Add(2 * time.Minute)
	if err := store.Save(ctx, stored); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	list, err := store.ListByChat(ctx, intent.ChatID, 10)
	if err != nil {
		t.Fatalf("ListByChat returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListByChat len = %d, want 1", len(list))
	}
	if list[0].Status != service.PaymentIntentTransferPending {
		t.Fatalf("ListByChat status = %s, want %s", list[0].Status, service.PaymentIntentTransferPending)
	}
}

func TestRedisPaymentIntentStore_WebhookIdempotency(t *testing.T) {
	store, _, cleanup := newTestRedisPaymentIntentStore(t)
	defer cleanup()

	ctx := context.Background()

	first, err := store.MarkWebhookProcessed(ctx, "evt-1", time.Hour)
	if err != nil {
		t.Fatalf("MarkWebhookProcessed first returned error: %v", err)
	}
	if !first {
		t.Fatalf("first webhook mark should be true")
	}

	second, err := store.MarkWebhookProcessed(ctx, "evt-1", time.Hour)
	if err != nil {
		t.Fatalf("MarkWebhookProcessed second returned error: %v", err)
	}
	if second {
		t.Fatalf("second webhook mark should be false")
	}
}

func TestRedisPaymentIntentStore_ListByChatPrunesMissing(t *testing.T) {
	store, client, cleanup := newTestRedisPaymentIntentStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	intent := testIntent(now, 42, "order-live", "ref-live")
	if err := store.Create(ctx, intent); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if err := client.ZAdd(ctx, store.paymentChatIndexKey(intent.ChatID), redis.Z{Score: float64(now.Unix()), Member: "ref-missing"}).Err(); err != nil {
		t.Fatalf("ZAdd missing member returned error: %v", err)
	}

	list, err := store.ListByChat(ctx, intent.ChatID, 10)
	if err != nil {
		t.Fatalf("ListByChat returned error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListByChat len = %d, want 1", len(list))
	}
	if list[0].PaystackReference != intent.PaystackReference {
		t.Fatalf("remaining reference = %s, want %s", list[0].PaystackReference, intent.PaystackReference)
	}
}
