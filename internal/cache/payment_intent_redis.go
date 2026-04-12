package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"github.com/redis/go-redis/v9"
)

type RedisPaymentIntentStore struct {
	rdb *redis.Client
}

func NewRedisPaymentIntentStore(rdb *redis.Client) *RedisPaymentIntentStore {
	return &RedisPaymentIntentStore{rdb: rdb}
}

func (s *RedisPaymentIntentStore) paymentIntentKey(reference string) string {
	return fmt.Sprintf("mtg:payment:intent:ref:%s", reference)
}

func (s *RedisPaymentIntentStore) paymentChatIndexKey(chatID int64) string {
	return fmt.Sprintf("mtg:payment:intent:chat:%d", chatID)
}

func (s *RedisPaymentIntentStore) paymentWebhookEventKey(eventID string) string {
	return fmt.Sprintf("mtg:payment:webhook:event:%s", eventID)
}

func (s *RedisPaymentIntentStore) Create(ctx context.Context, intent *service.PaymentIntentRecord) error {
	if intent == nil {
		return fmt.Errorf("payment intent is nil")
	}
	if intent.PaystackReference == "" {
		return fmt.Errorf("payment intent reference is required")
	}

	payload, err := json.Marshal(intent)
	if err != nil {
		return err
	}

	created, err := s.rdb.SetNX(ctx, s.paymentIntentKey(intent.PaystackReference), payload, 0).Result()
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("payment intent already exists for reference %s", intent.PaystackReference)
	}

	score := float64(intent.CreatedAt.Unix())
	if !intent.UpdatedAt.IsZero() {
		score = float64(intent.UpdatedAt.Unix())
	}

	return s.rdb.ZAdd(ctx, s.paymentChatIndexKey(intent.ChatID), redis.Z{Score: score, Member: intent.PaystackReference}).Err()
}

func (s *RedisPaymentIntentStore) GetByReference(ctx context.Context, reference string) (*service.PaymentIntentRecord, bool, error) {
	payload, err := s.rdb.Get(ctx, s.paymentIntentKey(reference)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var intent service.PaymentIntentRecord
	if err := json.Unmarshal([]byte(payload), &intent); err != nil {
		return nil, false, err
	}

	return &intent, true, nil
}

func (s *RedisPaymentIntentStore) Save(ctx context.Context, intent *service.PaymentIntentRecord) error {
	if intent == nil {
		return fmt.Errorf("payment intent is nil")
	}
	if intent.PaystackReference == "" {
		return fmt.Errorf("payment intent reference is required")
	}

	payload, err := json.Marshal(intent)
	if err != nil {
		return err
	}

	if err := s.rdb.Set(ctx, s.paymentIntentKey(intent.PaystackReference), payload, 0).Err(); err != nil {
		return err
	}

	score := float64(intent.UpdatedAt.Unix())
	if intent.UpdatedAt.IsZero() {
		score = float64(intent.CreatedAt.Unix())
	}

	return s.rdb.ZAdd(ctx, s.paymentChatIndexKey(intent.ChatID), redis.Z{Score: score, Member: intent.PaystackReference}).Err()
}

func (s *RedisPaymentIntentStore) MarkWebhookProcessed(ctx context.Context, eventID string, ttl time.Duration) (bool, error) {
	if eventID == "" {
		return false, fmt.Errorf("eventID is required")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	applied, err := s.rdb.SetNX(ctx, s.paymentWebhookEventKey(eventID), "1", ttl).Result()
	if err != nil {
		return false, err
	}

	return applied, nil
}

func (s *RedisPaymentIntentStore) ListByChat(ctx context.Context, chatID int64, limit int) ([]*service.PaymentIntentRecord, error) {
	stop := int64(-1)
	if limit > 0 {
		stop = int64(limit - 1)
	}

	references, err := s.rdb.ZRevRange(ctx, s.paymentChatIndexKey(chatID), 0, stop).Result()
	if err != nil {
		return nil, err
	}

	intents := make([]*service.PaymentIntentRecord, 0, len(references))
	for _, reference := range references {
		intent, found, err := s.GetByReference(ctx, reference)
		if err != nil {
			return nil, err
		}
		if !found {
			if err := s.rdb.ZRem(ctx, s.paymentChatIndexKey(chatID), reference).Err(); err != nil {
				return nil, err
			}
			continue
		}
		intents = append(intents, intent)
	}

	sort.Slice(intents, func(i, j int) bool {
		left := intents[i].UpdatedAt
		if left.IsZero() {
			left = intents[i].CreatedAt
		}
		right := intents[j].UpdatedAt
		if right.IsZero() {
			right = intents[j].CreatedAt
		}
		return left.After(right)
	})

	return intents, nil
}
