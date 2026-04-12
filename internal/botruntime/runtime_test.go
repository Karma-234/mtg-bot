package botruntime

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

type mockPaymentExecutor struct {
	result  *service.AutoTransferResult
	err     error
	calls   int
	lastReq service.AutoTransferRequest
}

func (m *mockPaymentExecutor) AutoTransferToOrder(ctx context.Context, bankLookup service.BankLookup, req service.AutoTransferRequest) (*service.AutoTransferResult, error) {
	m.calls++
	m.lastReq = req
	return m.result, m.err
}

type mockPaymentIntentStore struct {
	mu      sync.Mutex
	records map[string]*service.PaymentIntentRecord
}

type mockProviderPaidMarker struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (m *mockProviderPaidMarker) MarkOrderPaid(opts service.MarkOrderPaidRequest) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, m.err
}

func newMockPaymentIntentStore() *mockPaymentIntentStore {
	return &mockPaymentIntentStore{records: make(map[string]*service.PaymentIntentRecord)}
}

func cloneIntent(r *service.PaymentIntentRecord) *service.PaymentIntentRecord { cp := *r; return &cp }

func (s *mockPaymentIntentStore) Create(ctx context.Context, intent *service.PaymentIntentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[intent.PaystackReference] = cloneIntent(intent)
	return nil
}

func (s *mockPaymentIntentStore) GetByReference(ctx context.Context, ref string) (*service.PaymentIntentRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[ref]
	if !ok {
		return nil, false, nil
	}
	return cloneIntent(r), true, nil
}

func (s *mockPaymentIntentStore) GetByOrderID(ctx context.Context, orderID string) (*service.PaymentIntentRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.OrderID == orderID {
			return cloneIntent(r), true, nil
		}
	}
	return nil, false, nil
}

func (s *mockPaymentIntentStore) Save(ctx context.Context, intent *service.PaymentIntentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[intent.PaystackReference] = cloneIntent(intent)
	return nil
}

func (s *mockPaymentIntentStore) MarkWebhookProcessed(ctx context.Context, eventID string, ttl time.Duration) (bool, error) {
	return true, nil
}

func (s *mockPaymentIntentStore) ListByChat(ctx context.Context, chatID int64, limit int) ([]*service.PaymentIntentRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*service.PaymentIntentRecord
	for _, r := range s.records {
		if r.ChatID == chatID {
			result = append(result, cloneIntent(r))
		}
	}
	return result, nil
}

type mockMerchantProvider struct {
	pending     *service.OrdersResponse
	pendingErr  error
	detail      *service.OrderDetailResponse
	detailErr   error
	detailCalls int
}

func (m *mockMerchantProvider) GetPendingOrders(opts *service.OrderQueryRequest) (*service.OrdersResponse, error) {
	if m.pendingErr != nil {
		return nil, m.pendingErr
	}
	if m.pending != nil {
		return m.pending, nil
	}
	return nil, fmt.Errorf("not implemented in this test")
}

func (m *mockMerchantProvider) GetOrderDetail(opts service.SingleOrderQueryRequest) (*service.OrderDetailResponse, error) {
	m.detailCalls++
	if m.detailErr != nil {
		return nil, m.detailErr
	}
	return m.detail, nil
}

func makePendingOrdersResponse(order service.Order) *service.OrdersResponse {
	resp := &service.OrdersResponse{}
	resp.RetCode = 0
	resp.Result.Items = []service.Order{order}
	resp.Result.Count = 1
	return resp
}

type mockWorkflowStore struct {
	mu      sync.Mutex
	records map[string]*service.OrderWorkflowRecord
}

func newMockWorkflowStore() *mockWorkflowStore {
	return &mockWorkflowStore{records: make(map[string]*service.OrderWorkflowRecord)}
}

func cloneRecord(record *service.OrderWorkflowRecord) *service.OrderWorkflowRecord {
	copyRecord := *record
	return &copyRecord
}

func (s *mockWorkflowStore) CreateIfAbsent(ctx context.Context, record *service.OrderWorkflowRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[record.OrderID]; ok {
		return false, nil
	}
	s.records[record.OrderID] = cloneRecord(record)
	return true, nil
}

func (s *mockWorkflowStore) Get(ctx context.Context, orderID string) (*service.OrderWorkflowRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[orderID]
	if !ok {
		return nil, false, nil
	}
	return cloneRecord(record), true, nil
}

func (s *mockWorkflowStore) Save(ctx context.Context, record *service.OrderWorkflowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.OrderID] = cloneRecord(record)
	return nil
}

func (s *mockWorkflowStore) SaveIfState(ctx context.Context, record *service.OrderWorkflowRecord, expectedState service.OrderState) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.records[record.OrderID]
	if !ok {
		return false, nil
	}
	if stored.State != expectedState {
		return false, nil
	}
	s.records[record.OrderID] = cloneRecord(record)
	return true, nil
}

func (s *mockWorkflowStore) ListByChat(ctx context.Context, chatID int64) ([]*service.OrderWorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*service.OrderWorkflowRecord, 0)
	for _, record := range s.records {
		if record.ChatID == chatID {
			result = append(result, cloneRecord(record))
		}
	}
	return result, nil
}

func TestAdvanceWorkflowRecord_DetectedStopsAtDetailReady(t *testing.T) {
	now := time.Now().UTC()
	store := newMockWorkflowStore()
	manager := NewTaskManager(store, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }

	order := service.Order{
		ID:             "order-1",
		Amount:         "100.00",
		CurrencyID:     "USD",
		CreateDate:     strconv.FormatInt(now.UnixMilli(), 10),
		UserID:         "user-1",
		TargetUserID:   "target-user-1",
		TargetNickName: "target",
	}
	record := service.NewOrderWorkflowRecord(7, order, now)
	store.records[record.OrderID] = cloneRecord(record)

	provider := &mockMerchantProvider{
		detail: &service.OrderDetailResponse{
			BaseResponse: service.BaseResponse{RetCode: 0},
			Result: service.OrderDetail{
				CreateDate:        strconv.FormatInt(now.UnixMilli(), 10),
				TargetFirstName:   "Alice",
				TargetSecondName:  "Smith",
				Amount:            "100.00",
				CurrencyID:        "USD",
				PaymentTermResult: service.PaymentTerm{AccountNo: "ACC-123", BankName: "MyBank"},
			},
		},
	}

	if err := manager.advanceWorkflowRecord(context.Background(), nil, nil, provider, record); err != nil {
		t.Fatalf("advanceWorkflowRecord returned error: %v", err)
	}

	if record.State != service.StateDetailReady {
		t.Fatalf("record state = %s, want %s", record.State, service.StateDetailReady)
	}
	if record.BankName != "MyBank" {
		t.Fatalf("record bank name = %s, want %s", record.BankName, "MyBank")
	}

	stored, found, err := store.Get(context.Background(), record.OrderID)
	if err != nil {
		t.Fatalf("store.Get returned error: %v", err)
	}
	if !found {
		t.Fatalf("store.Get did not find record")
	}
	if stored.State != service.StateDetailReady {
		t.Fatalf("stored state = %s, want %s", stored.State, service.StateDetailReady)
	}
	if stored.BankName != "MyBank" {
		t.Fatalf("stored bank name = %s, want %s", stored.BankName, "MyBank")
	}
}

func TestPollAndProcess_SkipsResumeForSeenOrders(t *testing.T) {
	now := time.Now().UTC()
	store := newMockWorkflowStore()
	manager := NewTaskManager(store, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }

	order := service.Order{
		ID:             "order-seen",
		Amount:         "100.00",
		CurrencyID:     "USD",
		CreateDate:     strconv.FormatInt(now.UnixMilli(), 10),
		UserID:         "user-1",
		TargetUserID:   "target-user-1",
		TargetNickName: "target",
	}

	record := service.NewOrderWorkflowRecord(7, order, now)
	record.State = service.StateDetailFetching
	store.records[record.OrderID] = cloneRecord(record)

	provider := &mockMerchantProvider{
		pending: makePendingOrdersResponse(order),
		detail: &service.OrderDetailResponse{
			BaseResponse: service.BaseResponse{RetCode: 0},
			Result: service.OrderDetail{
				CreateDate:        strconv.FormatInt(now.UnixMilli(), 10),
				TargetFirstName:   "Alice",
				TargetSecondName:  "Smith",
				Amount:            "100.00",
				CurrencyID:        "USD",
				PaymentTermResult: service.PaymentTerm{AccountNo: "ACC-123", BankName: "MyBank"},
			},
		},
	}

	chat := &telebot.Chat{ID: 7, Username: "test-user"}
	if err := manager.pollAndProcess(context.Background(), nil, chat, provider, nil, 30*time.Second); err != nil {
		t.Fatalf("pollAndProcess returned error: %v", err)
	}

	if provider.detailCalls != 1 {
		t.Fatalf("GetOrderDetail calls = %d, want 1", provider.detailCalls)
	}

	stored, found, err := store.Get(context.Background(), record.OrderID)
	if err != nil {
		t.Fatalf("store.Get returned error: %v", err)
	}
	if !found {
		t.Fatalf("store.Get did not find record")
	}
	if stored.State != service.StateDetailReady {
		t.Fatalf("stored state = %s, want %s", stored.State, service.StateDetailReady)
	}
	if stored.BankName != "MyBank" {
		t.Fatalf("stored bank name = %s, want %s", stored.BankName, "MyBank")
	}
}

func TestSchedule_ExistingSameProviderDoesNotReplaceTask(t *testing.T) {
	now := time.Now().UTC()
	store := newMockWorkflowStore()
	manager := NewTaskManager(store, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }

	chat := &telebot.Chat{ID: 88, Username: "same-provider"}
	existingCanceled := false
	existingTask := scheduledTask{
		id:       7,
		cancel:   func() { existingCanceled = true },
		provider: "bybit",
		deadline: now.Add(2 * time.Minute),
	}
	manager.tasks[chat.ID] = existingTask
	manager.nextTaskID = 7

	manager.Schedule(nil, 5*time.Minute, chat, "bybit", nil, nil)

	if existingCanceled {
		t.Fatalf("existing task cancel callback should not be called")
	}

	storedTask, ok := manager.tasks[chat.ID]
	if !ok {
		t.Fatalf("existing task should remain in task map")
	}
	if storedTask.id != existingTask.id {
		t.Fatalf("task id changed from %d to %d", existingTask.id, storedTask.id)
	}
	if storedTask.provider != existingTask.provider {
		t.Fatalf("task provider changed from %s to %s", existingTask.provider, storedTask.provider)
	}
	if !storedTask.deadline.Equal(existingTask.deadline) {
		t.Fatalf("task deadline changed from %s to %s", existingTask.deadline, storedTask.deadline)
	}
}

func TestSchedule_ExistingDifferentProviderDoesNotReplaceTask(t *testing.T) {
	now := time.Now().UTC()
	store := newMockWorkflowStore()
	manager := NewTaskManager(store, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }

	chat := &telebot.Chat{ID: 99, Username: "different-provider"}
	existingCanceled := false
	existingTask := scheduledTask{
		id:       9,
		cancel:   func() { existingCanceled = true },
		provider: "bybit",
		deadline: now.Add(3 * time.Minute),
	}
	manager.tasks[chat.ID] = existingTask
	manager.nextTaskID = 9

	manager.Schedule(nil, 10*time.Minute, chat, "binance", nil, nil)

	if existingCanceled {
		t.Fatalf("existing task cancel callback should not be called")
	}

	storedTask, ok := manager.tasks[chat.ID]
	if !ok {
		t.Fatalf("existing task should remain in task map")
	}
	if storedTask.id != existingTask.id {
		t.Fatalf("task id changed from %d to %d", existingTask.id, storedTask.id)
	}
	if storedTask.provider != existingTask.provider {
		t.Fatalf("task provider changed from %s to %s", existingTask.provider, storedTask.provider)
	}
	if !storedTask.deadline.Equal(existingTask.deadline) {
		t.Fatalf("task deadline changed from %s to %s", existingTask.deadline, storedTask.deadline)
	}
}

func TestInitiatePayment_InsufficientFundsKeepsPendingExternal(t *testing.T) {
	now := time.Now().UTC()
	wfStore := newMockWorkflowStore()
	piStore := newMockPaymentIntentStore()
	exec := &mockPaymentExecutor{err: fmt.Errorf("wrap: %w", service.ErrInsufficientBalance)}
	manager := NewTaskManager(wfStore, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }
	manager.SetPaymentDeps(exec, piStore, nil)

	order := service.Order{
		ID:         "ord-insuf",
		Amount:     "50000.00",
		CurrencyID: "NGN",
		CreateDate: strconv.FormatInt(now.UnixMilli(), 10),
	}
	record := service.NewOrderWorkflowRecord(1, order, now)
	record.State = service.StateDetailReady
	record.AccountNo = "0123456789"
	record.BankName = "Test Bank"
	record.OrderAmount = "50000.00"
	wfStore.records[record.OrderID] = cloneRecord(record)

	if err := manager.advanceWorkflowRecord(context.Background(), nil, &telebot.Chat{ID: 1}, nil, record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, found, _ := wfStore.Get(context.Background(), record.OrderID)
	if !found {
		t.Fatal("record not found")
	}
	if got.State != service.StatePaymentPendingExternal {
		t.Fatalf("state = %s, want PAYMENT_PENDING_EXTERNAL", got.State)
	}

	intents, _ := piStore.ListByChat(context.Background(), 1, 10)
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	if intents[0].Status != service.PaymentIntentInsufficientFund {
		t.Fatalf("intent status = %s, want INSUFFICIENT_FUNDS", intents[0].Status)
	}
	if intents[0].RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1", intents[0].RetryCount)
	}
	if intents[0].NextRetryAt != now.Add(manager.retryPolicy.NextDelay(1)) {
		t.Fatalf("next retry = %s, want %s", intents[0].NextRetryAt, now.Add(manager.retryPolicy.NextDelay(1)))
	}
}

func TestInitiatePayment_SuccessCreatesTransferPendingIntent(t *testing.T) {
	now := time.Now().UTC()
	wfStore := newMockWorkflowStore()
	piStore := newMockPaymentIntentStore()
	exec := &mockPaymentExecutor{result: &service.AutoTransferResult{Reference: "ref-ok", TransferCode: "TRF_123", Status: "pending"}}
	manager := NewTaskManager(wfStore, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }
	manager.SetPaymentDeps(exec, piStore, nil)

	order := service.Order{
		ID:         "ord-ok",
		Amount:     "10000.00",
		CurrencyID: "NGN",
		CreateDate: strconv.FormatInt(now.UnixMilli(), 10),
	}
	record := service.NewOrderWorkflowRecord(2, order, now)
	record.State = service.StateDetailReady
	record.AccountNo = "9876543210"
	record.BankName = "Success Bank"
	record.OrderAmount = "10000.00"
	wfStore.records[record.OrderID] = cloneRecord(record)

	if err := manager.advanceWorkflowRecord(context.Background(), nil, &telebot.Chat{ID: 2}, nil, record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	intents, _ := piStore.ListByChat(context.Background(), 2, 10)
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	if intents[0].Status != service.PaymentIntentTransferPending {
		t.Fatalf("intent status = %s, want TRANSFER_PENDING", intents[0].Status)
	}
	if intents[0].TransferCode != "TRF_123" {
		t.Fatalf("TransferCode = %s, want TRF_123", intents[0].TransferCode)
	}
	if intents[0].RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0", intents[0].RetryCount)
	}
	if !intents[0].NextRetryAt.IsZero() {
		t.Fatalf("next retry = %s, want zero", intents[0].NextRetryAt)
	}
}

func TestRetryPayment_ResumesWhenBalanceRestored(t *testing.T) {
	now := time.Now().UTC()
	wfStore := newMockWorkflowStore()
	piStore := newMockPaymentIntentStore()
	exec := &mockPaymentExecutor{result: &service.AutoTransferResult{Reference: "ref-retry", TransferCode: "TRF_retry", Status: "pending"}}
	manager := NewTaskManager(wfStore, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }
	manager.SetPaymentDeps(exec, piStore, nil)

	order := service.Order{
		ID:         "ord-retry",
		Amount:     "20000.00",
		CurrencyID: "NGN",
		CreateDate: strconv.FormatInt(now.UnixMilli(), 10),
	}
	record := service.NewOrderWorkflowRecord(3, order, now)
	record.State = service.StatePaymentPendingExternal
	record.AccountNo = "1122334455"
	record.BankName = "Retry Bank"
	record.OrderAmount = "20000.00"
	wfStore.records[record.OrderID] = cloneRecord(record)

	_ = piStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-1",
		ChatID:            3,
		OrderID:           record.OrderID,
		PaystackReference: "ref-retry",
		AmountKobo:        2000000,
		Currency:          "NGN",
		Status:            service.PaymentIntentInsufficientFund,
		RetryCount:        1,
		CreatedAt:         now,
		UpdatedAt:         now,
		NextRetryAt:       now,
	})

	if err := manager.advanceWorkflowRecord(context.Background(), nil, &telebot.Chat{ID: 3}, nil, record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	intents, _ := piStore.ListByChat(context.Background(), 3, 10)
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	if intents[0].Status != service.PaymentIntentTransferPending {
		t.Fatalf("intent status = %s, want TRANSFER_PENDING", intents[0].Status)
	}
	if exec.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", exec.calls)
	}
	if intents[0].RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0", intents[0].RetryCount)
	}
	if !intents[0].NextRetryAt.IsZero() {
		t.Fatalf("next retry = %s, want zero", intents[0].NextRetryAt)
	}
}

func TestRetryPayment_InsufficientFundsWaitsForNextRetryAt(t *testing.T) {
	now := time.Now().UTC()
	wfStore := newMockWorkflowStore()
	piStore := newMockPaymentIntentStore()
	exec := &mockPaymentExecutor{result: &service.AutoTransferResult{Reference: "ref-wait", TransferCode: "TRF_wait", Status: "pending"}}
	manager := NewTaskManager(wfStore, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }
	manager.SetPaymentDeps(exec, piStore, nil)

	order := service.Order{ID: "ord-wait", Amount: "20000.00", CurrencyID: "NGN", CreateDate: strconv.FormatInt(now.UnixMilli(), 10)}
	record := service.NewOrderWorkflowRecord(4, order, now)
	record.State = service.StatePaymentPendingExternal
	record.AccountNo = "1122334455"
	record.BankName = "Retry Bank"
	record.OrderAmount = "20000.00"
	wfStore.records[record.OrderID] = cloneRecord(record)

	_ = piStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-wait",
		ChatID:            4,
		OrderID:           record.OrderID,
		PaystackReference: "ref-wait",
		AmountKobo:        2000000,
		Currency:          "NGN",
		Status:            service.PaymentIntentInsufficientFund,
		RetryCount:        1,
		CreatedAt:         now,
		UpdatedAt:         now,
		NextRetryAt:       now.Add(time.Minute),
	})

	if err := manager.advanceWorkflowRecord(context.Background(), nil, &telebot.Chat{ID: 4}, nil, record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", exec.calls)
	}
}

func TestRetryPayment_RecoverableTransferFailureRetriesAndBacksOff(t *testing.T) {
	now := time.Now().UTC()
	wfStore := newMockWorkflowStore()
	piStore := newMockPaymentIntentStore()
	exec := &mockPaymentExecutor{err: fmt.Errorf("temporary upstream timeout")}
	manager := NewTaskManager(wfStore, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }
	manager.SetPaymentDeps(exec, piStore, nil)

	order := service.Order{ID: "ord-temp", Amount: "20000.00", CurrencyID: "NGN", CreateDate: strconv.FormatInt(now.UnixMilli(), 10)}
	record := service.NewOrderWorkflowRecord(5, order, now)
	record.State = service.StatePaymentPendingExternal
	record.AccountNo = "1122334455"
	record.BankName = "Retry Bank"
	record.OrderAmount = "20000.00"
	wfStore.records[record.OrderID] = cloneRecord(record)

	_ = piStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-temp",
		ChatID:            5,
		OrderID:           record.OrderID,
		PaystackReference: "ref-temp",
		AmountKobo:        2000000,
		Currency:          "NGN",
		Status:            service.PaymentIntentTransferFailed,
		RetryCount:        1,
		LastError:         "temporary upstream timeout",
		CreatedAt:         now,
		UpdatedAt:         now,
		NextRetryAt:       now,
	})

	if err := manager.advanceWorkflowRecord(context.Background(), nil, &telebot.Chat{ID: 5}, nil, record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	intents, _ := piStore.ListByChat(context.Background(), 5, 10)
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	if intents[0].Status != service.PaymentIntentTransferFailed {
		t.Fatalf("intent status = %s, want TRANSFER_FAILED", intents[0].Status)
	}
	if intents[0].RetryCount != 2 {
		t.Fatalf("retry count = %d, want 2", intents[0].RetryCount)
	}
	if intents[0].NextRetryAt != now.Add(manager.retryPolicy.NextDelay(2)) {
		t.Fatalf("next retry = %s, want %s", intents[0].NextRetryAt, now.Add(manager.retryPolicy.NextDelay(2)))
	}
	if exec.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", exec.calls)
	}
}

func TestRetryPayment_TerminalTransferFailureStopsRetrying(t *testing.T) {
	now := time.Now().UTC()
	wfStore := newMockWorkflowStore()
	piStore := newMockPaymentIntentStore()
	exec := &mockPaymentExecutor{err: fmt.Errorf("transfer otp required")}
	manager := NewTaskManager(wfStore, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }
	manager.SetPaymentDeps(exec, piStore, nil)

	order := service.Order{ID: "ord-terminal", Amount: "20000.00", CurrencyID: "NGN", CreateDate: strconv.FormatInt(now.UnixMilli(), 10)}
	record := service.NewOrderWorkflowRecord(6, order, now)
	record.State = service.StatePaymentPendingExternal
	record.AccountNo = "1122334455"
	record.BankName = "Retry Bank"
	record.OrderAmount = "20000.00"
	wfStore.records[record.OrderID] = cloneRecord(record)

	_ = piStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-terminal",
		ChatID:            6,
		OrderID:           record.OrderID,
		PaystackReference: "ref-terminal",
		AmountKobo:        2000000,
		Currency:          "NGN",
		Status:            service.PaymentIntentTransferFailed,
		RetryCount:        1,
		LastError:         "transfer otp required",
		CreatedAt:         now,
		UpdatedAt:         now,
		NextRetryAt:       now,
	})

	if err := manager.advanceWorkflowRecord(context.Background(), nil, &telebot.Chat{ID: 6}, nil, record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	intents, _ := piStore.ListByChat(context.Background(), 6, 10)
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	if intents[0].RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1", intents[0].RetryCount)
	}
	if !intents[0].NextRetryAt.IsZero() {
		t.Fatalf("next retry = %s, want zero", intents[0].NextRetryAt)
	}
	if exec.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", exec.calls)
	}
}

func TestRetryProviderMarkPaid_SuccessAdvancesWorkflow(t *testing.T) {
	now := time.Now().UTC()
	wfStore := newMockWorkflowStore()
	piStore := newMockPaymentIntentStore()
	marker := &mockProviderPaidMarker{}
	manager := NewTaskManager(wfStore, DefaultRetryPolicy())
	manager.now = func() time.Time { return now }
	manager.SetPaymentDeps(&mockPaymentExecutor{}, piStore, nil)
	manager.SetProviderPaidMarker(marker)

	order := service.Order{ID: "ord-provider", Amount: "20000.00", CurrencyID: "NGN", CreateDate: strconv.FormatInt(now.UnixMilli(), 10)}
	record := service.NewOrderWorkflowRecord(7, order, now)
	record.State = service.StatePaymentPendingExternal
	record.AccountNo = "1122334455"
	record.BankName = "Retry Bank"
	record.OrderAmount = "20000.00"
	wfStore.records[record.OrderID] = cloneRecord(record)

	_ = piStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-provider",
		ChatID:            7,
		OrderID:           record.OrderID,
		PaystackReference: "ref-provider",
		AmountKobo:        2000000,
		Currency:          "NGN",
		Status:            service.PaymentIntentProviderFailed,
		RetryCount:        1,
		LastError:         "provider temporarily unavailable",
		CreatedAt:         now,
		UpdatedAt:         now,
		NextRetryAt:       now,
	})

	if err := manager.advanceWorkflowRecord(context.Background(), nil, &telebot.Chat{ID: 7}, nil, record); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updatedRecord, found, _ := wfStore.Get(context.Background(), record.OrderID)
	if !found {
		t.Fatal("workflow record not found")
	}
	if updatedRecord.State != service.StatePaid {
		t.Fatalf("workflow state = %s, want PAID", updatedRecord.State)
	}
	intents, _ := piStore.ListByChat(context.Background(), 7, 10)
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	if intents[0].Status != service.PaymentIntentProviderPaid {
		t.Fatalf("intent status = %s, want PROVIDER_MARKED_PAID", intents[0].Status)
	}
	if intents[0].RetryCount != 0 || !intents[0].NextRetryAt.IsZero() {
		t.Fatalf("retry metadata not cleared: count=%d next=%s", intents[0].RetryCount, intents[0].NextRetryAt)
	}
	if marker.calls != 1 {
		t.Fatalf("marker calls = %d, want 1", marker.calls)
	}
}
