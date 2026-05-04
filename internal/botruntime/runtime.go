package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/karma-234/mtg-bot/internal/cache"
	"github.com/karma-234/mtg-bot/internal/observability"
	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

// MerchantOrdersProvider defines the interface for fetching orders from merchant services.
type MerchantOrdersProvider interface {
	GetPendingOrders(opts *service.OrderQueryRequest) (*service.OrdersResponse, error)
	GetOrderDetail(opts service.SingleOrderQueryRequest) (*service.OrderDetailResponse, error)
}

// PaymentExecutor defines the interface for executing payment transfers.
type PaymentExecutor interface {
	AutoTransferToOrder(ctx context.Context, bankLookup service.BankLookup, req service.AutoTransferRequest) (*service.AutoTransferResult, error)
}

type scheduledTask struct {
	cancel   context.CancelFunc
	provider string
	deadline time.Time
	id       uint64
}

// TaskManager orchestrates poll-based order discovery, workflow state transitions, and payment initiation.
// It maintains consistent retry policy semantics with exponential backoff and jitter.
// Reconciliation is ownership-aware: polling remains mandatory for discovery; completion
// side effects are delegated to webhook + worker for event-driven handlers.
type TaskManager struct {
	workflowStore cache.WorkflowStore      // Persistent workflow state store
	retryPolicy   RetryPolicy              // Unified retry configuration (backoff, exhaustion)
	mu            sync.RWMutex             // Protects tasks and nextTaskID
	paymentExec   PaymentExecutor          // Transfer execution service
	paymentStore  cache.PaymentIntentStore // Payment intent state store
	bankLookup    service.BankLookup       // Bank metadata provider
	tasks         map[int64]scheduledTask  // Active polling tasks by chat ID
	processing    map[string]struct{}      // Lock set for per-order exclusive processing
	nextTaskID    uint64                   // Monotonic task ID counter
	now           func() time.Time         // Mock-friendly time source

}

func NewTaskManager(workflowStore cache.WorkflowStore, retryPolicy RetryPolicy) *TaskManager {
	return &TaskManager{
		tasks:         make(map[int64]scheduledTask),
		processing:    make(map[string]struct{}),
		workflowStore: workflowStore,
		retryPolicy:   retryPolicy,
		now:           time.Now,
	}
}

func (m *TaskManager) SetPaymentDeps(exec PaymentExecutor, store cache.PaymentIntentStore, lookup service.BankLookup) {
	m.paymentExec = exec
	m.paymentStore = store
	m.bankLookup = lookup
}

func (m *TaskManager) Schedule(b *telebot.Bot, duration time.Duration, chat *telebot.Chat, provider string, srv MerchantOrdersProvider, ordersCache cache.OrdersCache) {
	now := m.now()

	m.mu.Lock()
	if existing, exists := m.tasks[chat.ID]; exists {
		remaining := max(existing.deadline.Sub(now), 0)
		existingProvider := existing.provider
		m.mu.Unlock()

		messageProvider := existingProvider
		if existingProvider == provider {
			messageProvider = provider
		}
		if b != nil {
			if _, err := b.Send(chat, fmt.Sprintf("You already have an active %s task running.\nTime left: %s", messageProvider, remaining.Round(time.Second))); err != nil {
				log.Printf("Error sending active-task warning to chat %d: %v", chat.ID, err)
			}
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	m.nextTaskID++
	taskID := m.nextTaskID
	m.tasks[chat.ID] = scheduledTask{
		id:       taskID,
		cancel:   cancel,
		provider: provider,
		deadline: now.Add(duration),
	}
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		cacheTTL := 30 * time.Second
		defer func() {
			ticker.Stop()
			m.mu.Lock()
			if current, ok := m.tasks[chat.ID]; ok && current.id == taskID {
				delete(m.tasks, chat.ID)
			}
			m.mu.Unlock()
			log.Printf("Task for chat %d completed or cancelled", chat.ID)
		}()

		for {
			select {
			case <-ticker.C:
				start := time.Now()
				log.Printf("Executing scheduled task for chat %s", chat.Username)
				if err := m.pollAndProcess(ctx, b, chat, srv, ordersCache, cacheTTL); err != nil {
					log.Printf("Workflow poll failed for chat %d: %v", chat.ID, err)
				}
				observability.Global().PollCycleDurationMS.RecordDuration(start)
				observability.Global().PollCycleCount.Inc()
			case <-ctx.Done():
				log.Printf("Task for chat %v Completed", chat.Username)
				if _, err := b.Send(chat, "Task for user "+chat.Username+" completed"); err != nil {
					log.Printf("Error sending completion message to chat %d: %v", chat.ID, err)
				}
				return
			}
		}
	}()
}

func (m *TaskManager) pollAndProcess(
	ctx context.Context,
	b *telebot.Bot,
	chat *telebot.Chat,
	srv MerchantOrdersProvider,
	ordersCache cache.OrdersCache,
	cacheTTL time.Duration,
) error {
	resp, err := m.fetchPendingOrders(ctx, chat.ID, srv, ordersCache, cacheTTL)
	if err != nil {
		return err
	}

	if resp == nil || !resp.OK() {
		if resp != nil {
			return resp.Error()
		}
		return fmt.Errorf("nil pending orders response")
	}

	seenOrderIDs := make(map[string]struct{}, len(resp.Result.Items))
	for _, order := range resp.Result.Items {
		seenOrderIDs[order.ID] = struct{}{}
		if err := m.processPendingOrder(ctx, b, chat, srv, order); err != nil {
			log.Printf("Failed to process order %s for chat %d: %v", order.ID, chat.ID, err)
		}
	}

	records, err := m.workflowStore.ListByChat(ctx, chat.ID)
	if err != nil {
		return err
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].UpdatedAt.Before(records[j].UpdatedAt)
	})

	for _, record := range records {
		if _, seen := seenOrderIDs[record.OrderID]; seen {
			continue
		}
		if service.IsTerminalOrderState(record.State) {
			continue
		}
		if err := m.resumeWorkflowRecord(ctx, b, chat, srv, record); err != nil {
			log.Printf("Failed to resume order %s for chat %d: %v", record.OrderID, chat.ID, err)
		}
	}

	return nil
}

func (m *TaskManager) fetchPendingOrders(
	ctx context.Context,
	chatID int64,
	srv MerchantOrdersProvider,
	ordersCache cache.OrdersCache,
	cacheTTL time.Duration,
) (*service.OrdersResponse, error) {
	resp, err := srv.GetPendingOrders(nil)
	if err == nil {
		if ordersCache != nil {
			if cacheErr := ordersCache.SetLatestOrders(ctx, chatID, resp, cacheTTL); cacheErr != nil {
				log.Printf("Orders cache write failed for chat %d: %v", chatID, cacheErr)
			}
		}
		return resp, nil
	}

	if ordersCache != nil {
		cachedResp, found, cacheErr := ordersCache.GetLatestOrders(ctx, chatID)
		if cacheErr != nil {
			return nil, fmt.Errorf("pending orders fetch failed: %w; cache fallback failed: %v", err, cacheErr)
		}
		if found {
			log.Printf("Using cached pending orders snapshot for chat %d after fetch failure", chatID)
			return cachedResp, nil
		}
	}

	return nil, err
}

func (m *TaskManager) processPendingOrder(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, srv MerchantOrdersProvider, order service.Order) error {
	if !m.lockOrder(order.ID) {
		return nil
	}
	defer m.unlockOrder(order.ID)

	now := m.now()
	record, found, err := m.workflowStore.Get(ctx, order.ID)
	if err != nil {
		return err
	}
	if !found {
		record = service.NewOrderWorkflowRecord(chat.ID, order, now)
		created, err := m.workflowStore.CreateIfAbsent(ctx, record)
		if err != nil {
			return err
		}
		if created {
			m.notifyState(ctx, b, chat, record, fmt.Sprintf("New pending order detected: %s\nAmount: %s %s", order.ID, order.Amount, order.CurrencyID))
		} else {
			record, found, err = m.workflowStore.Get(ctx, order.ID)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("workflow record %s disappeared after duplicate create", order.ID)
			}
		}
	}

	return m.advanceWorkflowRecord(ctx, b, chat, srv, record)
}

func (m *TaskManager) resumeWorkflowRecord(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, srv MerchantOrdersProvider, record *service.OrderWorkflowRecord) error {
	if !m.lockOrder(record.OrderID) {
		return nil
	}
	defer m.unlockOrder(record.OrderID)

	return m.advanceWorkflowRecord(ctx, b, chat, srv, record)
}

func (m *TaskManager) advanceWorkflowRecord(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, srv MerchantOrdersProvider, record *service.OrderWorkflowRecord) error {
	now := m.now()
	if record.IsExpired(now) {
		return m.markTimedOut(ctx, b, chat, record, "Order expired before payment handoff")
	}

	switch record.State {
	case service.StateDetected:
		if err := m.transitionRecord(ctx, record, service.EventOrderIngested); err != nil {
			return err
		}
		return m.fetchOrderDetail(ctx, b, chat, srv, record)
	case service.StateDetailFetching:
		return m.fetchOrderDetail(ctx, b, chat, srv, record)
	case service.StateRetryingDetail:
		if now.Before(record.NextRetryAt) {
			return nil
		}
		if err := m.transitionRecord(ctx, record, service.EventRetryTimerFired); err != nil {
			return err
		}
		return m.fetchOrderDetail(ctx, b, chat, srv, record)
	case service.StateDetailReady:
		if err := m.transitionRecord(ctx, record, service.EventHandoffToPayment); err != nil {
			return err
		}
		if m.paymentExec == nil || m.paymentStore == nil {
			message := fmt.Sprintf(
				"Order ready for payment handoff\nID: %s\nBeneficiary: %s %s\nBank: %s\nAccount: %s\nAmount: %s\nOrder Time: %s",
				record.OrderID,
				record.TargetFirstName,
				record.TargetLastName,
				record.BankName,
				record.AccountNo,
				record.OrderAmount,
				record.OrderDate.Format(time.RFC3339),
			)
			m.notifyState(ctx, b, chat, record, message)
			return nil
		}
		return m.initiatePayment(ctx, b, chat, record)
	case service.StatePaymentPendingExternal:
		if m.paymentExec == nil || m.paymentStore == nil {
			return nil
		}
		// Polling remains mandatory for discovery/reconciliation. Completion and provider
		// confirmation side effects are event-owned (webhook + provider worker).
		return m.reconcilePaymentPendingExternal(ctx, b, chat, record)
	default:
		return nil
	}
}

func (m *TaskManager) fetchOrderDetail(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, srv MerchantOrdersProvider, record *service.OrderWorkflowRecord) error {
	detail, err := srv.GetOrderDetail(service.SingleOrderQueryRequest{OrderID: record.OrderID})
	if err != nil {
		return m.handleDetailFetchError(ctx, b, chat, record, err)
	}

	now := m.now()
	orderDate, parseErr := service.ParseOrderTimestamp(detail.Result.CreateDate, now)
	if parseErr != nil {
		log.Printf("Invalid createDate for order %s; using fallback timestamp: %v", record.OrderID, parseErr)
		orderDate = now
	}

	record.TargetFirstName = detail.Result.TargetFirstName
	record.TargetLastName = detail.Result.TargetSecondName
	record.AccountNo, record.BankName = extractPaymentDetails(detail.Result)
	record.OrderAmount = detail.Result.Amount
	record.CurrencyID = detail.Result.CurrencyID
	record.OrderDate = orderDate
	record.LastError = ""
	record.RetryCount = 0
	record.NextRetryAt = time.Time{}
	if err := m.transitionRecord(ctx, record, service.EventDetailFetchOK); err != nil {
		return err
	}

	return nil
}

func (m *TaskManager) handleDetailFetchError(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, record *service.OrderWorkflowRecord, err error) error {
	now := m.now()
	if record.IsExpired(now) {
		return m.markTimedOut(ctx, b, chat, record, "Order expired while fetching details")
	}

	record.LastError = err.Error()
	if !isRetryableDetailError(err) {
		if transitionErr := m.transitionRecord(ctx, record, service.EventDetailFetchFatal); transitionErr != nil {
			return transitionErr
		}
		m.notifyState(ctx, b, chat, record, fmt.Sprintf("Order detail fetch failed for %s: %v", record.OrderID, err))
		return nil
	}

	nextAttempt := record.RetryCount + 1
	if nextAttempt > m.retryPolicy.MaxAttempts {
		if transitionErr := m.transitionRecord(ctx, record, service.EventDetailFetchFatal); transitionErr != nil {
			return transitionErr
		}
		m.notifyState(ctx, b, chat, record, fmt.Sprintf("Order detail fetch exhausted retries for %s: %v", record.OrderID, err))
		return nil
	}

	delay := m.retryPolicy.NextDelay(nextAttempt)
	if !record.ExpiresAt.IsZero() && !now.Add(delay).Before(record.ExpiresAt) {
		return m.markTimedOut(ctx, b, chat, record, "Order expired before next detail retry")
	}

	record.RetryCount = nextAttempt
	record.NextRetryAt = now.Add(delay)
	if transitionErr := m.transitionRecord(ctx, record, service.EventDetailFetchRetryable); transitionErr != nil {
		return transitionErr
	}

	m.notifyState(ctx, b, chat, record, fmt.Sprintf("Retrying order detail fetch for %s in %s (attempt %d/%d)", record.OrderID, delay.String(), record.RetryCount, m.retryPolicy.MaxAttempts))
	return nil
}

func (m *TaskManager) markTimedOut(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, record *service.OrderWorkflowRecord, reason string) error {
	if service.IsTerminalOrderState(record.State) {
		return nil
	}
	if record.State == service.StateDetected || record.State == service.StateRetryingDetail || record.State == service.StateDetailFetching || record.State == service.StateDetailReady || record.State == service.StatePaymentPendingExternal {
		nextState, err := service.ApplyOrderEvent(record.State, service.EventOrderExpired)
		if err != nil {
			return err
		}
		record.State = nextState
		record.UpdatedAt = m.now()
		record.LastError = reason
		if err := m.workflowStore.Save(ctx, record); err != nil {
			return err
		}
	}

	m.notifyState(ctx, b, chat, record, fmt.Sprintf("Order %s timed out: %s", record.OrderID, reason))
	return nil
}

func (m *TaskManager) transitionRecord(ctx context.Context, record *service.OrderWorkflowRecord, event service.OrderEvent) error {
	const maxAttempts = 2

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fromState := record.State
		nextState, err := service.ApplyOrderEvent(fromState, event)
		if err != nil {
			return err
		}

		record.State = nextState
		record.UpdatedAt = m.now()

		applied, err := m.workflowStore.SaveIfState(ctx, record, fromState)
		if err != nil {
			return err
		}
		if applied {
			return nil
		}

		latest, found, err := m.workflowStore.Get(ctx, record.OrderID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("workflow record %s not found during transition retry", record.OrderID)
		}
		*record = *latest
	}

	return fmt.Errorf("workflow transition conflict for order %s via event %s", record.OrderID, event)
}

func (m *TaskManager) notifyState(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, record *service.OrderWorkflowRecord, message string) {
	if record.LastNotifiedState == record.State {
		return
	}
	if b == nil {
		return
	}
	if _, err := b.Send(chat, message); err != nil {
		log.Printf("Failed to send workflow update for order %s to chat %d: %v", record.OrderID, chat.ID, err)
		return
	}
	record.LastNotifiedState = record.State
	if err := m.workflowStore.Save(ctx, record); err != nil {
		log.Printf("Failed to persist notification state for order %s: %v", record.OrderID, err)
	}
}

func (m *TaskManager) lockOrder(orderID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.processing[orderID]; exists {
		return false
	}
	m.processing[orderID] = struct{}{}
	return true
}

func (m *TaskManager) unlockOrder(orderID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processing, orderID)
}

func extractPaymentDetails(detail service.OrderDetail) (accountNo string, bankName string) {
	if detail.PaymentTermResult.AccountNo != "" || detail.PaymentTermResult.BankName != "" {
		return detail.PaymentTermResult.AccountNo, detail.PaymentTermResult.BankName
	}
	if detail.ConfirmedPayTerm.AccountNo != "" || detail.ConfirmedPayTerm.BankName != "" {
		return detail.ConfirmedPayTerm.AccountNo, detail.ConfirmedPayTerm.BankName
	}
	for _, paymentTerm := range detail.PaymentTermList {
		if paymentTerm.AccountNo != "" || paymentTerm.BankName != "" {
			return paymentTerm.AccountNo, paymentTerm.BankName
		}
	}
	return "", ""
}

func isRetryableDetailError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	message := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "tempor", "connection reset", "eof", "unavailable", "rate limit", "429", "502", "503", "504"} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isRetryableTransferError uses unified error classification from RetryPolicy.
func isRetryableTransferError(err error) bool {
	return ClassifyTransferError(err) == ErrorTypeTransient
}

// scheduleIntentRetry schedules the next retry attempt with exponential backoff + jitter.
func (m *TaskManager) scheduleIntentRetry(intent *service.PaymentIntentRecord, now time.Time) {
	intent.RetryCount++
	intent.NextRetryAt = now.Add(m.retryPolicy.NextDelay(intent.RetryCount))
	intent.UpdatedAt = now
}

// clearIntentRetry resets retry state (success path).
func (m *TaskManager) clearIntentRetry(intent *service.PaymentIntentRecord, now time.Time) {
	intent.RetryCount = 0
	intent.NextRetryAt = time.Time{}
	intent.UpdatedAt = now
}

// stopIntentRetry halts retry attempts without clearing counts (terminal error path).
func (m *TaskManager) stopIntentRetry(intent *service.PaymentIntentRecord, now time.Time) {
	intent.NextRetryAt = time.Time{}
	intent.UpdatedAt = now
}

func (m *TaskManager) initiatePayment(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, record *service.OrderWorkflowRecord) error {
	amtFloat, err := strconv.ParseFloat(record.OrderAmount, 64)
	if err != nil {
		log.Printf("initiatePayment: bad OrderAmount %q for order %s: %v", record.OrderAmount, record.OrderID, err)
		return nil
	}
	amountKobo := int64(amtFloat * 100)
	now := m.now()
	ref := fmt.Sprintf("mtg-%s-%d", safePrefix(record.OrderID, 8), now.UnixMilli())

	intent := &service.PaymentIntentRecord{
		PaymentID:         fmt.Sprintf("pi-%s-%d", safePrefix(record.OrderID, 8), now.UnixNano()),
		ChatID:            record.ChatID,
		OrderID:           record.OrderID,
		Provider:          "bybit",
		PaystackReference: ref,
		AmountKobo:        amountKobo,
		Currency:          "NGN",
		Status:            service.PaymentIntentInitiated,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := m.paymentStore.Create(ctx, intent); err != nil {
		log.Printf("initiatePayment: failed to create intent for order %s: %v", record.OrderID, err)
		return nil
	}
	observability.Global().PaymentIntentCreated.Inc()
	observability.Info("payment_intent_created", observability.LogFields{
		Component: "runtime",
		OrderID:   record.OrderID,
		ChatID:    record.ChatID,
		Intent:    intent.PaymentID,
		Extra: map[string]any{
			"amount_kobo": amountKobo,
			"currency":    "NGN",
			"reference":   ref,
		},
	})

	result, transferErr := m.paymentExec.AutoTransferToOrder(ctx, m.bankLookup, service.AutoTransferRequest{
		OrderID:       record.OrderID,
		ChatID:        record.ChatID,
		Provider:      "bybit",
		Beneficiary:   strings.TrimSpace(record.TargetFirstName + " " + record.TargetLastName),
		AccountNumber: record.AccountNo,
		BankName:      record.BankName,
		AmountKobo:    amountKobo,
		Currency:      "NGN",
		Reference:     ref,
		Reason:        fmt.Sprintf("Bybit P2P order %s", record.OrderID),
		Country:       "NG",
	})

	if transferErr != nil {
		if errors.Is(transferErr, service.ErrInsufficientBalance) {
			intent.Status = service.PaymentIntentInsufficientFund
			intent.LastError = transferErr.Error()
			m.scheduleIntentRetry(intent, m.now())
			_ = m.paymentStore.Save(ctx, intent)
			m.notifyState(ctx, b, chat, record,
				fmt.Sprintf("Insufficient Paystack balance for order %s (%.2f NGN required). Please top up your Paystack balance.",
					record.OrderID, amtFloat))
			return nil
		}
		intent.Status = service.PaymentIntentTransferFailed
		intent.LastError = transferErr.Error()
		now := m.now()
		if isRetryableTransferError(transferErr) {
			m.scheduleIntentRetry(intent, now)
		} else {
			m.stopIntentRetry(intent, now)
		}
		_ = m.paymentStore.Save(ctx, intent)
		m.notifyState(ctx, b, chat, record, fmt.Sprintf("Transfer failed for order %s: %v", record.OrderID, transferErr))
		return nil
	}

	intent.Status = service.PaymentIntentTransferPending
	intent.TransferCode = result.TransferCode
	intent.LastError = ""
	m.clearIntentRetry(intent, m.now())
	_ = m.paymentStore.Save(ctx, intent)
	if b != nil {
		if _, sendErr := b.Send(chat, fmt.Sprintf("Transfer initiated for order %s\nReference: %s\nCode: %s",
			record.OrderID, ref, result.TransferCode)); sendErr != nil {
			log.Printf("initiatePayment: notify failed for order %s: %v", record.OrderID, sendErr)
		}
	}
	return nil
}

func shouldReconcilePaymentIntent(status service.PaymentIntentStatus) bool {
	return status == service.PaymentIntentInsufficientFund || status == service.PaymentIntentTransferFailed
}

func (m *TaskManager) reconcilePaymentPendingExternal(ctx context.Context, b *telebot.Bot, chat *telebot.Chat, record *service.OrderWorkflowRecord) error {
	intent, found, err := m.paymentStore.GetByOrderID(ctx, record.OrderID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	now := m.now()

	// Event-driven completion statuses are owned by webhook + worker and should not
	// be mutated by poll-based reconciliation.
	if !m.isEligibleForReconciliation(intent, now) {
		return nil
	}

	result, transferErr := m.paymentExec.AutoTransferToOrder(ctx, m.bankLookup, service.AutoTransferRequest{
		OrderID:       record.OrderID,
		ChatID:        record.ChatID,
		Provider:      "bybit",
		Beneficiary:   strings.TrimSpace(record.TargetFirstName + " " + record.TargetLastName),
		AccountNumber: record.AccountNo,
		BankName:      record.BankName,
		AmountKobo:    intent.AmountKobo,
		Currency:      intent.Currency,
		Reference:     intent.PaystackReference,
		Reason:        fmt.Sprintf("Bybit P2P order %s (retry)", record.OrderID),
		Country:       "NG",
	})
	if transferErr != nil {
		m.handleReconciliationError(ctx, b, chat, record, intent, transferErr, now)
		return nil
	}

	// Transfer succeeded; update intent and notify user
	intent.Status = service.PaymentIntentTransferPending
	intent.TransferCode = result.TransferCode
	intent.LastError = ""
	m.clearIntentRetry(intent, now)
	_ = m.paymentStore.Save(ctx, intent)
	if b != nil {
		if _, sendErr := b.Send(chat, fmt.Sprintf("Balance restored — transfer re-initiated for order %s\nRef: %s",
			record.OrderID, intent.PaystackReference)); sendErr != nil {
			log.Printf("retryPayment: notify failed for order %s: %v", record.OrderID, sendErr)
		}
	}
	return nil
}

// isEligibleForReconciliation checks if an intent can be retried by polling.
func (m *TaskManager) isEligibleForReconciliation(intent *service.PaymentIntentRecord, now time.Time) bool {
	// Only retry if status is in reconciliation-friendly states
	if !shouldReconcilePaymentIntent(intent.Status) {
		return false
	}
	// Don't retry if next attempt is scheduled in future
	if !intent.NextRetryAt.IsZero() && now.Before(intent.NextRetryAt) {
		return false
	}
	// Don't retry if already exhausted max attempts
	if intent.Status == service.PaymentIntentTransferFailed && m.retryPolicy.IsExhausted(intent.RetryCount) {
		return false
	}
	return true
}

// handleReconciliationError processes transfer errors during reconciliation.
func (m *TaskManager) handleReconciliationError(
	ctx context.Context,
	b *telebot.Bot,
	chat *telebot.Chat,
	record *service.OrderWorkflowRecord,
	intent *service.PaymentIntentRecord,
	transferErr error,
	now time.Time,
) {
	intent.LastError = transferErr.Error()
	intent.Status = service.PaymentIntentTransferFailed

	if errors.Is(transferErr, service.ErrInsufficientBalance) {
		intent.Status = service.PaymentIntentInsufficientFund
		m.scheduleIntentRetry(intent, now)
		_ = m.paymentStore.Save(ctx, intent)
		return // Silently wait; user already notified during initial attempt
	}

	// Handle transient vs terminal errors
	if isRetryableTransferError(transferErr) {
		m.scheduleIntentRetry(intent, now)
	} else {
		m.stopIntentRetry(intent, now)
	}
	_ = m.paymentStore.Save(ctx, intent)
	m.notifyState(ctx, b, chat, record, fmt.Sprintf("Transfer retry failed for order %s: %v", record.OrderID, transferErr))
}

func (m *TaskManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for chatID, task := range m.tasks {
		task.cancel()
		log.Printf("Cancelled task for chat %d", chatID)
	}
}
