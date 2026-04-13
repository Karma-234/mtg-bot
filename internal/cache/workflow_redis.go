package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	workflowRetentionWindow = 24 * time.Hour
	workflowFallbackTTL     = 48 * time.Hour
)

type RedisWorkflowStore struct {
	rdb *redis.Client
}

func NewRedisWorkflowStore(rdb *redis.Client) *RedisWorkflowStore {
	return &RedisWorkflowStore{rdb: rdb}
}

func (s *RedisWorkflowStore) workflowKey(orderID string) string {
	return fmt.Sprintf("mtg:workflow:order:%s", orderID)
}

func (s *RedisWorkflowStore) workflowChatIndexKey(chatID int64) string {
	return fmt.Sprintf("mtg:workflow:chat:%d", chatID)
}

func (s *RedisWorkflowStore) workflowTTL(record *service.OrderWorkflowRecord) time.Duration {
	if record != nil && !record.ExpiresAt.IsZero() {
		ttl := time.Until(record.ExpiresAt.Add(workflowRetentionWindow))
		if ttl > 0 {
			return ttl
		}
	}

	return workflowFallbackTTL
}

func (s *RedisWorkflowStore) CreateIfAbsent(ctx context.Context, record *service.OrderWorkflowRecord) (bool, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return false, err
	}

	// created, err := s.rdb.SetNX(ctx, s.workflowKey(record.OrderID), payload, s.workflowTTL(record)).Result()
	status, err := s.rdb.SetArgs(ctx, s.workflowKey(record.OrderID), payload, redis.SetArgs{
		Mode: "NX",
		TTL:  s.workflowTTL(record),
	}).Result()
	if err != nil && err != redis.Nil {
		return false, err
	}
	created := status == "OK" && err == nil
	if err != nil {
		return false, err
	}
	if !created {
		return false, nil
	}

	if err := s.rdb.SAdd(ctx, s.workflowChatIndexKey(record.ChatID), record.OrderID).Err(); err != nil {
		return false, err
	}

	return true, nil
}

func (s *RedisWorkflowStore) Get(ctx context.Context, orderID string) (*service.OrderWorkflowRecord, bool, error) {
	payload, err := s.rdb.Get(ctx, s.workflowKey(orderID)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var record service.OrderWorkflowRecord
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		return nil, false, err
	}

	return &record, true, nil
}

func (s *RedisWorkflowStore) Save(ctx context.Context, record *service.OrderWorkflowRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}

	if err := s.rdb.Set(ctx, s.workflowKey(record.OrderID), payload, s.workflowTTL(record)).Err(); err != nil {
		return err
	}

	return s.rdb.SAdd(ctx, s.workflowChatIndexKey(record.ChatID), record.OrderID).Err()
}

func (s *RedisWorkflowStore) SaveIfState(ctx context.Context, record *service.OrderWorkflowRecord, expectedState service.OrderState) (bool, error) {
	key := s.workflowKey(record.OrderID)
	applied := false

	err := s.rdb.Watch(ctx, func(tx *redis.Tx) error {
		raw, err := tx.Get(ctx, key).Result()
		if err == redis.Nil {
			applied = false
			return nil
		}
		if err != nil {
			return err
		}

		var current service.OrderWorkflowRecord
		if err := json.Unmarshal([]byte(raw), &current); err != nil {
			return err
		}
		if current.State != expectedState {
			applied = false
			return nil
		}

		payload, err := json.Marshal(record)
		if err != nil {
			return err
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, key, payload, s.workflowTTL(record))
			pipe.SAdd(ctx, s.workflowChatIndexKey(record.ChatID), record.OrderID)
			return nil
		})
		if err == redis.TxFailedErr {
			applied = false
			return nil
		}
		if err != nil {
			return err
		}

		applied = true
		return nil
	}, key)
	if err != nil {
		return false, err
	}

	return applied, nil
}

func (s *RedisWorkflowStore) ListByChat(ctx context.Context, chatID int64) ([]*service.OrderWorkflowRecord, error) {
	orderIDs, err := s.rdb.SMembers(ctx, s.workflowChatIndexKey(chatID)).Result()
	if err != nil {
		return nil, err
	}

	records := make([]*service.OrderWorkflowRecord, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		record, found, err := s.Get(ctx, orderID)
		if err != nil {
			return nil, err
		}
		if !found {
			if err := s.rdb.SRem(ctx, s.workflowChatIndexKey(chatID), orderID).Err(); err != nil {
				return nil, err
			}
			continue
		}
		records = append(records, record)
	}

	return records, nil
}
