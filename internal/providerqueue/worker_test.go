package providerqueue

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/karma-234/mtg-bot/internal/botruntime"
	"github.com/karma-234/mtg-bot/internal/cache"
	"github.com/karma-234/mtg-bot/internal/service"
)

type mockQueue struct {
	mu       sync.Mutex
	messages []*cache.ProviderMarkMessage
	acks     []string
	requeued []cache.ProviderMarkJob
}

func (q *mockQueue) Enqueue(ctx context.Context, job cache.ProviderMarkJob) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	id := fmt.Sprintf("msg-%d", len(q.messages)+1)
	q.messages = append(q.messages, &cache.ProviderMarkMessage{ID: id, Job: job, Consumer: "test"})
	return nil
}

func (q *mockQueue) Dequeue(ctx context.Context, consumer string, block time.Duration) (*cache.ProviderMarkMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return nil, nil
	}
	msg := q.messages[0]
	q.messages = q.messages[1:]
	return msg, nil
}

func (q *mockQueue) Ack(ctx context.Context, messageID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acks = append(q.acks, messageID)
	return nil
}

func (q *mockQueue) Requeue(ctx context.Context, job cache.ProviderMarkJob, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.requeued = append(q.requeued, job)
	return nil
}

type mockIntentStore struct {
	mu      sync.Mutex
	records map[string]*service.PaymentIntentRecord
}

func newMockIntentStore() *mockIntentStore {
	return &mockIntentStore{records: map[string]*service.PaymentIntentRecord{}}
}

func (s *mockIntentStore) Create(ctx context.Context, intent *service.PaymentIntentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *intent
	s.records[intent.PaystackReference] = &cp
	return nil
}

func (s *mockIntentStore) GetByReference(ctx context.Context, reference string) (*service.PaymentIntentRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[reference]
	if !ok {
		return nil, false, nil
	}
	cp := *rec
	return &cp, true, nil
}

func (s *mockIntentStore) GetByOrderID(ctx context.Context, orderID string) (*service.PaymentIntentRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		if rec.OrderID == orderID {
			cp := *rec
			return &cp, true, nil
		}
	}
	return nil, false, nil
}

func (s *mockIntentStore) Save(ctx context.Context, intent *service.PaymentIntentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *intent
	s.records[intent.PaystackReference] = &cp
	return nil
}

func (s *mockIntentStore) MarkWebhookProcessed(ctx context.Context, eventID string, ttl time.Duration) (bool, error) {
	return true, nil
}

func (s *mockIntentStore) ListByChat(ctx context.Context, chatID int64, limit int) ([]*service.PaymentIntentRecord, error) {
	return nil, nil
}

type mockWorkflowStore struct {
	mu      sync.Mutex
	records map[string]*service.OrderWorkflowRecord
}

func newMockWorkflowStore() *mockWorkflowStore {
	return &mockWorkflowStore{records: map[string]*service.OrderWorkflowRecord{}}
}

func (s *mockWorkflowStore) CreateIfAbsent(ctx context.Context, record *service.OrderWorkflowRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[record.OrderID]; ok {
		return false, nil
	}
	cp := *record
	s.records[record.OrderID] = &cp
	return true, nil
}

func (s *mockWorkflowStore) Get(ctx context.Context, orderID string) (*service.OrderWorkflowRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[orderID]
	if !ok {
		return nil, false, nil
	}
	cp := *rec
	return &cp, true, nil
}

func (s *mockWorkflowStore) Save(ctx context.Context, record *service.OrderWorkflowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *record
	s.records[record.OrderID] = &cp
	return nil
}

func (s *mockWorkflowStore) SaveIfState(ctx context.Context, record *service.OrderWorkflowRecord, expectedState service.OrderState) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.records[record.OrderID]
	if !ok || stored.State != expectedState {
		return false, nil
	}
	cp := *record
	s.records[record.OrderID] = &cp
	return true, nil
}

func (s *mockWorkflowStore) ListByChat(ctx context.Context, chatID int64) ([]*service.OrderWorkflowRecord, error) {
	return nil, nil
}

type mockMarker struct {
	err   error
	calls int
}

func (m *mockMarker) MarkOrderPaid(opts service.MarkOrderPaidRequest) (*http.Response, error) {
	m.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, m.err
}

func TestWorkerProcessMessageSuccess(t *testing.T) {
	now := time.Now().UTC()
	queue := &mockQueue{}
	intentStore := newMockIntentStore()
	workflowStore := newMockWorkflowStore()
	marker := &mockMarker{}
	worker := NewWorker(queue, intentStore, workflowStore, marker, botruntime.DefaultRetryPolicy(), nil, "worker-a")

	intent := &service.PaymentIntentRecord{PaymentID: "pi-1", ChatID: 1, OrderID: "order-1", PaystackReference: "ref-1", Status: service.PaymentIntentTransferSuccess, AmountKobo: 1000, Currency: "NGN", CreatedAt: now, UpdatedAt: now}
	_ = intentStore.Create(context.Background(), intent)
	_ = workflowStore.Save(context.Background(), &service.OrderWorkflowRecord{OrderID: "order-1", ChatID: 1, State: service.StatePaymentPendingExternal, CreatedAt: now, UpdatedAt: now})

	msg := &cache.ProviderMarkMessage{ID: "msg-1", Job: cache.ProviderMarkJob{OrderID: "order-1", PaymentReference: "ref-1", ChatID: 1}}
	if err := worker.processMessage(context.Background(), msg); err != nil {
		t.Fatalf("processMessage returned error: %v", err)
	}

	updated, found, _ := intentStore.GetByReference(context.Background(), "ref-1")
	if !found || updated.Status != service.PaymentIntentProviderPaid {
		t.Fatalf("intent status = %v, want PROVIDER_MARKED_PAID", updated.Status)
	}
	record, found, _ := workflowStore.Get(context.Background(), "order-1")
	if !found || record.State != service.StatePaid {
		t.Fatalf("workflow state = %v, want PAID", record.State)
	}
	if marker.calls != 1 {
		t.Fatalf("marker calls = %d, want 1", marker.calls)
	}
	if len(queue.acks) != 1 || queue.acks[0] != "msg-1" {
		t.Fatalf("ack mismatch: %#v", queue.acks)
	}
}

func TestWorkerProcessMessageFailureRequeues(t *testing.T) {
	now := time.Now().UTC()
	queue := &mockQueue{}
	intentStore := newMockIntentStore()
	workflowStore := newMockWorkflowStore()
	marker := &mockMarker{err: fmt.Errorf("temporary provider error")}
	worker := NewWorker(queue, intentStore, workflowStore, marker, botruntime.DefaultRetryPolicy(), nil, "worker-b")

	intent := &service.PaymentIntentRecord{PaymentID: "pi-2", ChatID: 2, OrderID: "order-2", PaystackReference: "ref-2", Status: service.PaymentIntentTransferSuccess, AmountKobo: 1500, Currency: "NGN", CreatedAt: now, UpdatedAt: now}
	_ = intentStore.Create(context.Background(), intent)
	_ = workflowStore.Save(context.Background(), &service.OrderWorkflowRecord{OrderID: "order-2", ChatID: 2, State: service.StatePaymentPendingExternal, CreatedAt: now, UpdatedAt: now})

	msg := &cache.ProviderMarkMessage{ID: "msg-2", Job: cache.ProviderMarkJob{OrderID: "order-2", PaymentReference: "ref-2", ChatID: 2}}
	if err := worker.processMessage(context.Background(), msg); err != nil {
		t.Fatalf("processMessage returned error: %v", err)
	}

	updated, found, _ := intentStore.GetByReference(context.Background(), "ref-2")
	if !found || updated.Status != service.PaymentIntentProviderFailed {
		t.Fatalf("intent status = %v, want PROVIDER_MARK_FAILED", updated.Status)
	}
	if updated.RetryCount != 1 || updated.NextRetryAt.IsZero() {
		t.Fatalf("retry metadata not set: count=%d next=%s", updated.RetryCount, updated.NextRetryAt)
	}
	if len(queue.requeued) != 1 {
		t.Fatalf("requeue count = %d, want 1", len(queue.requeued))
	}
	if len(queue.acks) != 1 || queue.acks[0] != "msg-2" {
		t.Fatalf("ack mismatch: %#v", queue.acks)
	}
}
