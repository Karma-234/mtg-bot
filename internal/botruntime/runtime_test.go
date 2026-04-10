package botruntime

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

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
