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

type UserStateCache interface {
	SetSelectedDuration(ctx context.Context, chatID int64, d time.Duration, ttl time.Duration) error
	GetSelectedDuration(ctx context.Context, chatID int64) (time.Duration, bool, error)
}
