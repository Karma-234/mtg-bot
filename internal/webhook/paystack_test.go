package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/karma-234/mtg-bot/internal/cache"
	"github.com/karma-234/mtg-bot/internal/service"
)

// ---- in-memory mocks ----

type mockIntentStore struct {
	mu            sync.Mutex
	intents       map[string]*service.PaymentIntentRecord
	processedKeys map[string]bool
	saveCalls     int
}

func newMockIntentStore() *mockIntentStore {
	return &mockIntentStore{
		intents:       make(map[string]*service.PaymentIntentRecord),
		processedKeys: make(map[string]bool),
	}
}

func (s *mockIntentStore) Create(ctx context.Context, intent *service.PaymentIntentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *intent
	s.intents[intent.PaystackReference] = &cp
	return nil
}

func (s *mockIntentStore) GetByReference(ctx context.Context, ref string) (*service.PaymentIntentRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.intents[ref]
	if !ok {
		return nil, false, nil
	}
	cp := *r
	return &cp, true, nil
}

func (s *mockIntentStore) GetByOrderID(ctx context.Context, orderID string) (*service.PaymentIntentRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.intents {
		if r.OrderID == orderID {
			cp := *r
			return &cp, true, nil
		}
	}
	return nil, false, nil
}

func (s *mockIntentStore) Save(ctx context.Context, intent *service.PaymentIntentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *intent
	s.intents[intent.PaystackReference] = &cp
	s.saveCalls++
	return nil
}

func (s *mockIntentStore) MarkWebhookProcessed(ctx context.Context, eventID string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processedKeys[eventID] {
		return false, nil
	}
	s.processedKeys[eventID] = true
	return true, nil
}

func (s *mockIntentStore) ListByChat(ctx context.Context, chatID int64, limit int) ([]*service.PaymentIntentRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*service.PaymentIntentRecord
	for _, r := range s.intents {
		if r.ChatID == chatID {
			cp := *r
			result = append(result, &cp)
		}
	}
	return result, nil
}

type mockWebhookWorkflowStore struct {
	mu      sync.Mutex
	records map[string]*service.OrderWorkflowRecord
}

type mockProviderMarkQueue struct {
	mu      sync.Mutex
	jobs    []cache.ProviderMarkJob
	ackErr  error
	enqErr  error
	deqMsgs []*cache.ProviderMarkMessage
}

func (m *mockProviderMarkQueue) Enqueue(ctx context.Context, job cache.ProviderMarkJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.enqErr != nil {
		return m.enqErr
	}
	m.jobs = append(m.jobs, job)
	return nil
}

func (m *mockProviderMarkQueue) Dequeue(ctx context.Context, consumer string, block time.Duration) (*cache.ProviderMarkMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.deqMsgs) == 0 {
		return nil, nil
	}
	msg := m.deqMsgs[0]
	m.deqMsgs = m.deqMsgs[1:]
	return msg, nil
}

func (m *mockProviderMarkQueue) Ack(ctx context.Context, messageID string) error {
	return m.ackErr
}

func (m *mockProviderMarkQueue) Requeue(ctx context.Context, job cache.ProviderMarkJob, delay time.Duration) error {
	return nil
}

type mockTransferVerifier struct {
	resp  *service.TransferResponse
	err   error
	calls int
}

func (m *mockTransferVerifier) VerifyTransfer(reference string) (*service.TransferResponse, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

func newMockWebhookWorkflowStore() *mockWebhookWorkflowStore {
	return &mockWebhookWorkflowStore{records: make(map[string]*service.OrderWorkflowRecord)}
}

func (s *mockWebhookWorkflowStore) CreateIfAbsent(ctx context.Context, record *service.OrderWorkflowRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[record.OrderID]; ok {
		return false, nil
	}
	cp := *record
	s.records[record.OrderID] = &cp
	return true, nil
}

func (s *mockWebhookWorkflowStore) Get(ctx context.Context, orderID string) (*service.OrderWorkflowRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[orderID]
	if !ok {
		return nil, false, nil
	}
	cp := *r
	return &cp, true, nil
}

func (s *mockWebhookWorkflowStore) Save(ctx context.Context, record *service.OrderWorkflowRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *record
	s.records[record.OrderID] = &cp
	return nil
}

func (s *mockWebhookWorkflowStore) SaveIfState(ctx context.Context, record *service.OrderWorkflowRecord, expectedState service.OrderState) (bool, error) {
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

func (s *mockWebhookWorkflowStore) ListByChat(ctx context.Context, chatID int64) ([]*service.OrderWorkflowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*service.OrderWorkflowRecord
	for _, r := range s.records {
		if r.ChatID == chatID {
			cp := *r
			result = append(result, &cp)
		}
	}
	return result, nil
}

// ---- helpers ----

func makeSignature(body []byte, secret string) string {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func makeEventBody(event, ref string) []byte {
	payload := map[string]interface{}{
		"event": event,
		"data": map[string]interface{}{
			"reference":     ref,
			"transfer_code": "TRF_test",
			"status":        "success",
			"amount":        100000,
			"currency":      "NGN",
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

// ---- tests ----

func TestVerifySignature_CorrectSecretMatches(t *testing.T) {
	body := []byte(`{"event":"transfer.success"}`)
	secret := "webhook-secret-123"
	sig := makeSignature(body, secret)

	if !VerifySignature(body, sig, secret) {
		t.Fatal("expected VerifySignature to return true for correct secret")
	}
	if VerifySignature(body, sig, "wrong-secret") {
		t.Fatal("expected VerifySignature to return false for wrong secret")
	}
}

func TestWebhookHandler_TransferSuccess_AdvancesWorkflow(t *testing.T) {
	now := time.Now().UTC()
	ref := "ref-wh-ok"
	orderID := "order-wh-ok"
	secret := "test-secret"

	intentStore := newMockIntentStore()
	_ = intentStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-wh",
		ChatID:            42,
		OrderID:           orderID,
		PaystackReference: ref,
		AmountKobo:        100000,
		Currency:          "NGN",
		Status:            service.PaymentIntentTransferPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	queue := &mockProviderMarkQueue{}
	verifier := &mockTransferVerifier{resp: &service.TransferResponse{BasePaystackResponse: service.BasePaystackResponse{Status: true}, Data: service.TransferData{Reference: ref, Amount: 100000, Currency: "NGN", Status: "success"}}}
	handler := NewPaystackWebhookHandler(secret, intentStore, verifier, queue, nil)

	body := makeEventBody("transfer.success", ref)
	req := httptest.NewRequest(http.MethodPost, "/webhook/paystack", bytes.NewReader(body))
	req.Header.Set("x-paystack-signature", makeSignature(body, secret))
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	intent, found, _ := intentStore.GetByReference(context.Background(), ref)
	if !found {
		t.Fatal("intent not found after webhook")
	}
	if intent.Status != service.PaymentIntentTransferSuccess {
		t.Fatalf("intent status = %s, want TRANSFER_SUCCESS", intent.Status)
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(queue.jobs))
	}
	if queue.jobs[0].OrderID != orderID {
		t.Fatalf("queued orderID = %s, want %s", queue.jobs[0].OrderID, orderID)
	}
	if verifier.calls != 1 {
		t.Fatalf("verifier calls = %d, want 1", verifier.calls)
	}
}

func TestWebhookHandler_TransferSuccess_ProviderFailureLeavesWorkflowPending(t *testing.T) {
	now := time.Now().UTC()
	ref := "ref-wh-provider-fail"
	orderID := "order-wh-provider-fail"
	secret := "test-secret"

	intentStore := newMockIntentStore()
	_ = intentStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-wh-fail",
		ChatID:            42,
		OrderID:           orderID,
		PaystackReference: ref,
		AmountKobo:        100000,
		Currency:          "NGN",
		Status:            service.PaymentIntentTransferPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	queue := &mockProviderMarkQueue{enqErr: fmt.Errorf("queue unavailable")}
	verifier := &mockTransferVerifier{resp: &service.TransferResponse{BasePaystackResponse: service.BasePaystackResponse{Status: true}, Data: service.TransferData{Reference: ref, Amount: 100000, Currency: "NGN", Status: "success"}}}
	handler := NewPaystackWebhookHandler(secret, intentStore, verifier, queue, nil)

	body := makeEventBody("transfer.success", ref)
	req := httptest.NewRequest(http.MethodPost, "/webhook/paystack", bytes.NewReader(body))
	req.Header.Set("x-paystack-signature", makeSignature(body, secret))
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	intent, found, _ := intentStore.GetByReference(context.Background(), ref)
	if !found {
		t.Fatal("intent not found after webhook")
	}
	if intent.Status != service.PaymentIntentProviderFailed {
		t.Fatalf("intent status = %s, want PROVIDER_MARK_FAILED", intent.Status)
	}
	if intent.RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1", intent.RetryCount)
	}
	if intent.NextRetryAt.IsZero() {
		t.Fatal("next retry should be set")
	}
}

func TestWebhookHandler_DuplicateEvent_IsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	ref := "ref-dup"
	orderID := "order-dup"
	secret := "dup-secret"

	intentStore := newMockIntentStore()
	_ = intentStore.Create(context.Background(), &service.PaymentIntentRecord{
		PaymentID:         "pi-dup",
		ChatID:            77,
		OrderID:           orderID,
		PaystackReference: ref,
		AmountKobo:        50000,
		Currency:          "NGN",
		Status:            service.PaymentIntentTransferPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	queue := &mockProviderMarkQueue{}
	verifier := &mockTransferVerifier{resp: &service.TransferResponse{BasePaystackResponse: service.BasePaystackResponse{Status: true}, Data: service.TransferData{Reference: ref, Amount: 50000, Currency: "NGN", Status: "success"}}}
	handler := NewPaystackWebhookHandler(secret, intentStore, verifier, queue, nil)

	body := makeEventBody("transfer.success", ref)
	sig := makeSignature(body, secret)

	req1 := httptest.NewRequest(http.MethodPost, "/webhook/paystack", bytes.NewReader(body))
	req1.Header.Set("x-paystack-signature", sig)
	rr1 := httptest.NewRecorder()
	handler(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rr1.Code)
	}

	savesAfterFirst := intentStore.saveCalls

	req2 := httptest.NewRequest(http.MethodPost, "/webhook/paystack", bytes.NewReader(body))
	req2.Header.Set("x-paystack-signature", sig)
	rr2 := httptest.NewRecorder()
	handler(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second request status = %d, want 200", rr2.Code)
	}

	if intentStore.saveCalls != savesAfterFirst {
		t.Fatalf("intent Save called %d extra time(s) on duplicate, want 0", intentStore.saveCalls-savesAfterFirst)
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(queue.jobs))
	}
}

func TestWebhookHandler_TransferSuccess_VerifyAmountMismatchBlocksCompletion(t *testing.T) {
	now := time.Now().UTC()
	ref := "ref-verify-amount"
	orderID := "order-verify-amount"
	secret := "verify-secret"

	intentStore := newMockIntentStore()
	_ = intentStore.Create(context.Background(), &service.PaymentIntentRecord{PaymentID: "pi-verify-amount", ChatID: 99, OrderID: orderID, PaystackReference: ref, AmountKobo: 100000, Currency: "NGN", Status: service.PaymentIntentTransferPending, CreatedAt: now, UpdatedAt: now})
	queue := &mockProviderMarkQueue{}
	verifier := &mockTransferVerifier{resp: &service.TransferResponse{BasePaystackResponse: service.BasePaystackResponse{Status: true}, Data: service.TransferData{Reference: ref, Amount: 90000, Currency: "NGN", Status: "success"}}}
	handler := NewPaystackWebhookHandler(secret, intentStore, verifier, queue, nil)

	body := makeEventBody("transfer.success", ref)
	req := httptest.NewRequest(http.MethodPost, "/webhook/paystack", bytes.NewReader(body))
	req.Header.Set("x-paystack-signature", makeSignature(body, secret))
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	intent, found, _ := intentStore.GetByReference(context.Background(), ref)
	if !found {
		t.Fatal("intent not found after webhook")
	}
	if intent.Status != service.PaymentIntentTransferPending {
		t.Fatalf("intent status = %s, want TRANSFER_PENDING", intent.Status)
	}
	if len(queue.jobs) != 0 {
		t.Fatalf("queued jobs = %d, want 0", len(queue.jobs))
	}
}

func TestWebhookHandler_TransferSuccess_VerifyCurrencyMismatchBlocksCompletion(t *testing.T) {
	now := time.Now().UTC()
	ref := "ref-verify-currency"
	orderID := "order-verify-currency"
	secret := "verify-secret"

	intentStore := newMockIntentStore()
	_ = intentStore.Create(context.Background(), &service.PaymentIntentRecord{PaymentID: "pi-verify-currency", ChatID: 99, OrderID: orderID, PaystackReference: ref, AmountKobo: 100000, Currency: "NGN", Status: service.PaymentIntentTransferPending, CreatedAt: now, UpdatedAt: now})
	queue := &mockProviderMarkQueue{}
	verifier := &mockTransferVerifier{resp: &service.TransferResponse{BasePaystackResponse: service.BasePaystackResponse{Status: true}, Data: service.TransferData{Reference: ref, Amount: 100000, Currency: "USD", Status: "success"}}}
	handler := NewPaystackWebhookHandler(secret, intentStore, verifier, queue, nil)

	body := makeEventBody("transfer.success", ref)
	req := httptest.NewRequest(http.MethodPost, "/webhook/paystack", bytes.NewReader(body))
	req.Header.Set("x-paystack-signature", makeSignature(body, secret))
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	intent, found, _ := intentStore.GetByReference(context.Background(), ref)
	if !found {
		t.Fatal("intent not found after webhook")
	}
	if intent.Status != service.PaymentIntentTransferPending {
		t.Fatalf("intent status = %s, want TRANSFER_PENDING", intent.Status)
	}
	if len(queue.jobs) != 0 {
		t.Fatalf("queued jobs = %d, want 0", len(queue.jobs))
	}
}
