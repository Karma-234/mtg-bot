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
