package cache

import (
	"context"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
)

type OrdersCache interface {
	GetLatestOrders(ctx context.Context, chatID int64) (*service.OrdersResponse, bool, error)
	SetLatestOrders(ctx context.Context, chatID int64, data *service.OrdersResponse, ttl time.Duration) error
}

type WorkflowStore interface {
	CreateIfAbsent(ctx context.Context, record *service.OrderWorkflowRecord) (bool, error)
	Get(ctx context.Context, orderID string) (*service.OrderWorkflowRecord, bool, error)
	Save(ctx context.Context, record *service.OrderWorkflowRecord) error
	SaveIfState(ctx context.Context, record *service.OrderWorkflowRecord, expectedState service.OrderState) (bool, error)
	ListByChat(ctx context.Context, chatID int64) ([]*service.OrderWorkflowRecord, error)
}

type UserStateCache interface {
	SetSelectedDuration(ctx context.Context, chatID int64, d time.Duration, ttl time.Duration) error
	GetSelectedDuration(ctx context.Context, chatID int64) (time.Duration, bool, error)
}

type BankCache interface {
	GetBanks(ctx context.Context, country string) ([]service.BankEntry, bool, error)
	SetBanks(ctx context.Context, country string, banks []service.BankEntry, ttl time.Duration) error
}

type RecipientCodeCache interface {
	GetRecipientCode(ctx context.Context, country, bankCode, accountNumber string) (string, bool, error)
	SetRecipientCode(ctx context.Context, country, bankCode, accountNumber, recipientCode string, ttl time.Duration) error
}

type PaymentIntentStore interface {
	Create(ctx context.Context, intent *service.PaymentIntentRecord) error
	GetByReference(ctx context.Context, reference string) (*service.PaymentIntentRecord, bool, error)
	GetByOrderID(ctx context.Context, orderID string) (*service.PaymentIntentRecord, bool, error)
	Save(ctx context.Context, intent *service.PaymentIntentRecord) error
	MarkWebhookProcessed(ctx context.Context, eventID string, ttl time.Duration) (bool, error)
	ListByChat(ctx context.Context, chatID int64, limit int) ([]*service.PaymentIntentRecord, error)
}

type ProviderMarkJob struct {
	OrderID           string    `json:"orderId"`
	PaymentReference  string    `json:"paymentReference"`
	ChatID            int64     `json:"chatId"`
	Attempt           int       `json:"attempt"`
	EarliestProcessAt time.Time `json:"earliestProcessAt"`
	EnqueuedAt        time.Time `json:"enqueuedAt"`
}

type ProviderMarkMessage struct {
	ID       string
	Job      ProviderMarkJob
	Consumer string
}

type ProviderMarkQueue interface {
	Enqueue(ctx context.Context, job ProviderMarkJob) error
	Dequeue(ctx context.Context, consumer string, block time.Duration) (*ProviderMarkMessage, error)
	Ack(ctx context.Context, messageID string) error
	Requeue(ctx context.Context, job ProviderMarkJob, delay time.Duration) error
}
