package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestProviderMarkQueue(t *testing.T) (*RedisProviderMarkQueue, func()) {
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

	return NewRedisProviderMarkQueue(client), cleanup
}

func TestRedisProviderMarkQueue_EnqueueDequeueAck(t *testing.T) {
	queue, cleanup := newTestProviderMarkQueue(t)
	defer cleanup()

	ctx := context.Background()
	job := ProviderMarkJob{
		OrderID:          "order-1",
		PaymentReference: "ref-1",
		ChatID:           101,
	}
	if err := queue.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue returned error: %v", err)
	}

	msg, err := queue.Dequeue(ctx, "consumer-a", 5*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue returned error: %v", err)
	}
	if msg == nil {
		t.Fatal("Dequeue returned nil message")
	}
	if msg.Job.OrderID != job.OrderID {
		t.Fatalf("OrderID = %s, want %s", msg.Job.OrderID, job.OrderID)
	}

	if err := queue.Ack(ctx, msg.ID); err != nil {
		t.Fatalf("Ack returned error: %v", err)
	}
}

func TestRedisProviderMarkQueue_RequeueDelayed(t *testing.T) {
	queue, cleanup := newTestProviderMarkQueue(t)
	defer cleanup()

	ctx := context.Background()
	job := ProviderMarkJob{
		OrderID:          "order-delay",
		PaymentReference: "ref-delay",
		ChatID:           202,
		Attempt:          1,
	}
	if err := queue.Requeue(ctx, job, 50*time.Millisecond); err != nil {
		t.Fatalf("Requeue returned error: %v", err)
	}

	msg, err := queue.Dequeue(ctx, "consumer-b", 5*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue returned error: %v", err)
	}
	if msg != nil {
		t.Fatal("expected nil message before delay expires")
	}

	time.Sleep(60 * time.Millisecond)

	msg, err = queue.Dequeue(ctx, "consumer-b", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue after delay returned error: %v", err)
	}
	if msg == nil {
		t.Fatal("expected delayed message after delay")
	}
	if msg.Job.OrderID != job.OrderID {
		t.Fatalf("OrderID = %s, want %s", msg.Job.OrderID, job.OrderID)
	}
}
