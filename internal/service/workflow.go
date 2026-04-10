package service

import (
	"fmt"
	"strconv"
	"time"
)

type OrderState string

type OrderEvent string

const (
	StateDetected               OrderState = "DETECTED"
	StateDetailFetching         OrderState = "DETAIL_FETCHING"
	StateDetailReady            OrderState = "DETAIL_READY"
	StatePaymentPendingExternal OrderState = "PAYMENT_PENDING_EXTERNAL"
	StateRetryingDetail         OrderState = "RETRYING_DETAIL"
	StateFailedDetail           OrderState = "FAILED_DETAIL"
	StateTimedOut               OrderState = "TIMED_OUT"
	StatePaid                   OrderState = "PAID"
)

const (
	EventOrderIngested        OrderEvent = "ORDER_INGESTED"
	EventDetailFetchOK        OrderEvent = "DETAIL_FETCH_OK"
	EventDetailFetchRetryable OrderEvent = "DETAIL_FETCH_RETRYABLE_ERR"
	EventDetailFetchFatal     OrderEvent = "DETAIL_FETCH_FATAL_ERR"
	EventRetryTimerFired      OrderEvent = "RETRY_TIMER_FIRED"
	EventOrderExpired         OrderEvent = "ORDER_EXPIRED"
	EventHandoffToPayment     OrderEvent = "HANDOFF_TO_PAYMENT"
)

var terminalOrderStates = map[OrderState]bool{
	StateFailedDetail: true,
	StateTimedOut:     true,
	StatePaid:         true,
}

var orderTransitions = map[OrderState]map[OrderEvent]OrderState{
	StateDetected: {
		EventOrderIngested: StateDetailFetching,
		EventOrderExpired:  StateTimedOut,
	},
	StateDetailFetching: {
		EventDetailFetchOK:        StateDetailReady,
		EventDetailFetchRetryable: StateRetryingDetail,
		EventDetailFetchFatal:     StateFailedDetail,
		EventOrderExpired:         StateTimedOut,
	},
	StateRetryingDetail: {
		EventRetryTimerFired: StateDetailFetching,
		EventOrderExpired:    StateTimedOut,
	},
	StateDetailReady: {
		EventHandoffToPayment: StatePaymentPendingExternal,
		EventOrderExpired:     StateTimedOut,
	},
	StatePaymentPendingExternal: {
		EventOrderExpired: StateTimedOut,
	},
}

type OrderWorkflowRecord struct {
	OrderID           string     `json:"orderId"`
	ChatID            int64      `json:"chatId"`
	State             OrderState `json:"state"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	RetryCount        int        `json:"retryCount"`
	NextRetryAt       time.Time  `json:"nextRetryAt,omitempty"`
	LastError         string     `json:"lastError,omitempty"`
	LastNotifiedState OrderState `json:"lastNotifiedState,omitempty"`
	UserID            string     `json:"userId,omitempty"`
	TargetUserID      string     `json:"targetUserId,omitempty"`
	TargetNickName    string     `json:"targetNickName,omitempty"`
	TargetFirstName   string     `json:"targetFirstName,omitempty"`
	TargetLastName    string     `json:"targetLastName,omitempty"`
	AccountNo         string     `json:"accountNo,omitempty"`
	OrderAmount       string     `json:"orderAmount,omitempty"`
	CurrencyID        string     `json:"currencyId,omitempty"`
	OrderDate         time.Time  `json:"orderDate"`
}

func IsTerminalOrderState(state OrderState) bool {
	return terminalOrderStates[state]
}

func ValidOrderState(state OrderState) bool {
	if IsTerminalOrderState(state) {
		return true
	}
	_, ok := orderTransitions[state]
	return ok
}

func ValidOrderEvent(event OrderEvent) bool {
	switch event {
	case EventOrderIngested, EventDetailFetchOK, EventDetailFetchRetryable, EventDetailFetchFatal, EventRetryTimerFired, EventOrderExpired, EventHandoffToPayment:
		return true
	default:
		return false
	}
}

func CanTransitionOrderState(from OrderState, event OrderEvent) (OrderState, bool) {
	nextByEvent, ok := orderTransitions[from]
	if !ok {
		return "", false
	}
	to, ok := nextByEvent[event]
	return to, ok
}

func ApplyOrderEvent(from OrderState, event OrderEvent) (OrderState, error) {
	if !ValidOrderState(from) {
		return "", fmt.Errorf("invalid order state %q", from)
	}
	if !ValidOrderEvent(event) {
		return "", fmt.Errorf("invalid order event %q", event)
	}
	if IsTerminalOrderState(from) {
		return from, fmt.Errorf("order state %q is terminal", from)
	}
	to, ok := CanTransitionOrderState(from, event)
	if !ok {
		return from, fmt.Errorf("invalid transition from %q via %q", from, event)
	}
	return to, nil
}

func NewOrderWorkflowRecord(chatID int64, order Order, now time.Time) *OrderWorkflowRecord {
	orderDate := now
	if parsedOrderDate, err := ParseOrderTimestamp(order.CreateDate, now); err == nil {
		orderDate = parsedOrderDate
	}

	return &OrderWorkflowRecord{
		OrderID:        order.ID,
		ChatID:         chatID,
		State:          StateDetected,
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      orderDate.Add(30 * time.Minute),
		UserID:         order.UserID,
		TargetUserID:   order.TargetUserID,
		TargetNickName: order.TargetNickName,
		OrderAmount:    order.Amount,
		CurrencyID:     order.CurrencyID,
		OrderDate:      orderDate,
	}
}

func ParseOrderTimestamp(raw string, fallback time.Time) (time.Time, error) {
	if raw == "" {
		return fallback, fmt.Errorf("empty order timestamp")
	}

	milliseconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback, err
	}

	return time.UnixMilli(milliseconds), nil
}

func (r *OrderWorkflowRecord) IsExpired(now time.Time) bool {
	return !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)
}
