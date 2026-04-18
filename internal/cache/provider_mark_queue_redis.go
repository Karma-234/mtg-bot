package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultProviderMarkStreamKey  = "mtg:provider_mark:stream"
	defaultProviderMarkDelayedKey = "mtg:provider_mark:delayed"
	defaultProviderMarkDLQKey     = "mtg:provider_mark:dlq"
	defaultProviderMarkGroup      = "provider-mark-workers"
)

type RedisProviderMarkQueue struct {
	rdb        *redis.Client
	streamKey  string
	delayedKey string
	dlqKey     string
	group      string

	pendingMinIdle    time.Duration
	pendingClaimCount int64
}

func NewRedisProviderMarkQueue(rdb *redis.Client) *RedisProviderMarkQueue {
	return &RedisProviderMarkQueue{
		rdb:        rdb,
		streamKey:  defaultProviderMarkStreamKey,
		delayedKey: defaultProviderMarkDelayedKey,
		dlqKey:     defaultProviderMarkDLQKey,
		group:      defaultProviderMarkGroup,

		pendingMinIdle:    30 * time.Second,
		pendingClaimCount: 20,
	}
}

func (q *RedisProviderMarkQueue) Enqueue(ctx context.Context, job ProviderMarkJob) error {
	now := time.Now().UTC()
	if job.OrderID == "" {
		return fmt.Errorf("orderID is required")
	}
	if job.PaymentReference == "" {
		return fmt.Errorf("payment reference is required")
	}
	if job.EnqueuedAt.IsZero() {
		job.EnqueuedAt = now
	}
	if job.EarliestProcessAt.IsZero() {
		job.EarliestProcessAt = now
	}

	_, err := q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: q.streamKey,
		Values: q.jobFields(job),
	}).Result()
	return err
}

func (q *RedisProviderMarkQueue) Dequeue(ctx context.Context, consumer string, block time.Duration) (*ProviderMarkMessage, error) {
	if consumer == "" {
		return nil, fmt.Errorf("consumer is required")
	}
	if block < 0 {
		block = 0
	}
	if err := q.ensureGroup(ctx); err != nil {
		return nil, err
	}
	if err := q.promoteDue(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := q.reclaimStalePending(ctx, consumer); err != nil {
		return nil, err
	}

	streamResults, err := q.readGroup(ctx, consumer, []string{q.streamKey, "0"}, 0)
	if err != nil {
		return nil, err
	}
	if len(streamResults) == 0 || len(streamResults[0].Messages) == 0 {
		streamResults, err = q.readGroup(ctx, consumer, []string{q.streamKey, ">"}, block)
		if err != nil {
			return nil, err
		}
	}
	if len(streamResults) == 0 || len(streamResults[0].Messages) == 0 {
		return nil, nil
	}

	xmsg := streamResults[0].Messages[0]
	job, err := q.parseJob(xmsg.Values)
	if err != nil {
		return nil, err
	}
	if !job.EarliestProcessAt.IsZero() && time.Now().UTC().Before(job.EarliestProcessAt) {
		if err := q.Ack(ctx, xmsg.ID); err != nil {
			return nil, err
		}
		return nil, q.Requeue(ctx, job, time.Until(job.EarliestProcessAt))
	}

	return &ProviderMarkMessage{ID: xmsg.ID, Job: job, Consumer: consumer}, nil
}

func (q *RedisProviderMarkQueue) readGroup(ctx context.Context, consumer string, streams []string, block time.Duration) ([]redis.XStream, error) {
	streamResults, err := q.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.group,
		Consumer: consumer,
		Streams:  streams,
		Count:    1,
		Block:    block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return streamResults, nil
}

func (q *RedisProviderMarkQueue) Ack(ctx context.Context, messageID string) error {
	if messageID == "" {
		return fmt.Errorf("messageID is required")
	}
	if err := q.rdb.XAck(ctx, q.streamKey, q.group, messageID).Err(); err != nil {
		return err
	}
	return q.rdb.XDel(ctx, q.streamKey, messageID).Err()
}

func (q *RedisProviderMarkQueue) Requeue(ctx context.Context, job ProviderMarkJob, delay time.Duration) error {
	now := time.Now().UTC()
	if delay <= 0 {
		job.EarliestProcessAt = now
		job.EnqueuedAt = now
		return q.Enqueue(ctx, job)
	}

	job.EarliestProcessAt = now.Add(delay)
	if job.EnqueuedAt.IsZero() {
		job.EnqueuedAt = now
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}

	return q.rdb.ZAdd(ctx, q.delayedKey, redis.Z{
		Score:  float64(job.EarliestProcessAt.UnixMilli()),
		Member: string(payload),
	}).Err()
}

func (q *RedisProviderMarkQueue) DeadLetter(ctx context.Context, job ProviderMarkJob, reason string) error {
	now := time.Now().UTC()
	if job.EnqueuedAt.IsZero() {
		job.EnqueuedAt = now
	}
	if job.EarliestProcessAt.IsZero() {
		job.EarliestProcessAt = now
	}
	if reason == "" {
		reason = "unspecified"
	}

	values := q.jobFields(job)
	values["deadletter_reason"] = reason
	values["deadletter_at"] = now.Format(time.RFC3339Nano)

	_, err := q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: q.dlqKey,
		Values: values,
	}).Result()
	return err
}

func (q *RedisProviderMarkQueue) ensureGroup(ctx context.Context) error {
	err := q.rdb.XGroupCreateMkStream(ctx, q.streamKey, q.group, "0").Err()
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (q *RedisProviderMarkQueue) promoteDue(ctx context.Context, now time.Time) error {
	entries, err := q.rdb.ZRangeArgsWithScores(ctx, redis.ZRangeArgs{
		Key:    q.delayedKey,
		ByScore: true,
		Start:  "-inf",
		Stop:   strconv.FormatInt(now.UnixMilli(), 10),
		Offset: 0,
		Count:  100,
	}).Result()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	for _, entry := range entries {
		payload := entry.Member.(string)
		if err := q.rdb.ZRem(ctx, q.delayedKey, payload).Err(); err != nil {
			return err
		}
		var job ProviderMarkJob
		if err := json.Unmarshal([]byte(payload), &job); err != nil {
			return err
		}
		job.EarliestProcessAt = now
		if err := q.Enqueue(ctx, job); err != nil {
			return err
		}
	}

	return nil
}

func (q *RedisProviderMarkQueue) reclaimStalePending(ctx context.Context, consumer string) error {
	pending, err := q.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: q.streamKey,
		Group:  q.group,
		Start:  "-",
		End:    "+",
		Count:  q.pendingClaimCount,
	}).Result()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unknown") || strings.Contains(strings.ToLower(err.Error()), "not supported") {
			return nil
		}
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	ids := make([]string, 0, len(pending))
	for _, item := range pending {
		if item.Idle >= q.pendingMinIdle {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	_, err = q.rdb.XClaim(ctx, &redis.XClaimArgs{
		Stream:   q.streamKey,
		Group:    q.group,
		Consumer: consumer,
		MinIdle:  q.pendingMinIdle,
		Messages: ids,
	}).Result()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unknown") || strings.Contains(strings.ToLower(err.Error()), "not supported") {
			return nil
		}
		return err
	}

	return nil
}

func (q *RedisProviderMarkQueue) jobFields(job ProviderMarkJob) map[string]interface{} {
	fields := map[string]interface{}{
		"order_id":            job.OrderID,
		"payment_reference":   job.PaymentReference,
		"chat_id":             strconv.FormatInt(job.ChatID, 10),
		"attempt":             strconv.Itoa(job.Attempt),
		"earliest_process_at": job.EarliestProcessAt.UTC().Format(time.RFC3339Nano),
		"enqueued_at":         job.EnqueuedAt.UTC().Format(time.RFC3339Nano),
	}
	return fields
}

func (q *RedisProviderMarkQueue) parseJob(values map[string]interface{}) (ProviderMarkJob, error) {
	getString := func(key string) (string, error) {
		raw, ok := values[key]
		if !ok {
			return "", fmt.Errorf("missing stream field %q", key)
		}
		switch v := raw.(type) {
		case string:
			return v, nil
		case []byte:
			return string(v), nil
		default:
			return "", fmt.Errorf("stream field %q has unsupported type %T", key, raw)
		}
	}

	orderID, err := getString("order_id")
	if err != nil {
		return ProviderMarkJob{}, err
	}
	paymentReference, err := getString("payment_reference")
	if err != nil {
		return ProviderMarkJob{}, err
	}
	chatRaw, err := getString("chat_id")
	if err != nil {
		return ProviderMarkJob{}, err
	}
	attemptRaw, err := getString("attempt")
	if err != nil {
		return ProviderMarkJob{}, err
	}
	earliestRaw, err := getString("earliest_process_at")
	if err != nil {
		return ProviderMarkJob{}, err
	}
	enqueuedRaw, err := getString("enqueued_at")
	if err != nil {
		return ProviderMarkJob{}, err
	}

	chatID, err := strconv.ParseInt(chatRaw, 10, 64)
	if err != nil {
		return ProviderMarkJob{}, err
	}
	attempt, err := strconv.Atoi(attemptRaw)
	if err != nil {
		return ProviderMarkJob{}, err
	}
	earliestProcessAt, err := time.Parse(time.RFC3339Nano, earliestRaw)
	if err != nil {
		return ProviderMarkJob{}, err
	}
	enqueuedAt, err := time.Parse(time.RFC3339Nano, enqueuedRaw)
	if err != nil {
		return ProviderMarkJob{}, err
	}

	return ProviderMarkJob{
		OrderID:           orderID,
		PaymentReference:  paymentReference,
		ChatID:            chatID,
		Attempt:           attempt,
		EarliestProcessAt: earliestProcessAt,
		EnqueuedAt:        enqueuedAt,
	}, nil
}
