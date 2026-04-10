package botruntime

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
)

type mockMerchantProvider struct {
	detail *service.OrderDetailResponse
	err    error
}

func (m mockMerchantProvider) GetPendingOrders(opts *service.OrderQueryRequest) (*service.OrdersResponse, error) {
	return nil, fmt.Errorf("not implemented in this test")
}

func (m mockMerchantProvider) GetOrderDetail(opts service.SingleOrderQueryRequest) (*service.OrderDetailResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.detail, nil
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

	provider := mockMerchantProvider{
		detail: &service.OrderDetailResponse{
			BaseResponse: service.BaseResponse{RetCode: 0},
			Result: service.OrderDetail{
				CreateDate:        strconv.FormatInt(now.UnixMilli(), 10),
				TargetFirstName:   "Alice",
				TargetSecondName:  "Smith",
				Amount:            "100.00",
				CurrencyID:        "USD",
				PaymentTermResult: service.PaymentTerm{AccountNo: "ACC-123"},
			},
		},
	}

	if err := manager.advanceWorkflowRecord(context.Background(), nil, nil, provider, record); err != nil {
		t.Fatalf("advanceWorkflowRecord returned error: %v", err)
	}

	if record.State != service.StateDetailReady {
		t.Fatalf("record state = %s, want %s", record.State, service.StateDetailReady)
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
}
