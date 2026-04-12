package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubBankLookup struct {
	banks []BankEntry
}

func (s stubBankLookup) GetBanks(ctx context.Context, country string) ([]BankEntry, bool, error) {
	return s.banks, true, nil
}

type memoryRecipientCodeStore struct {
	mu    sync.Mutex
	store map[string]string
	sets  int
}

func (m *memoryRecipientCodeStore) GetRecipientCode(ctx context.Context, country, bankCode, accountNumber string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.store[country+":"+bankCode+":"+accountNumber]
	return value, ok, nil
}

func (m *memoryRecipientCodeStore) SetRecipientCode(ctx context.Context, country, bankCode, accountNumber, recipientCode string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		m.store = make(map[string]string)
	}
	m.store[country+":"+bankCode+":"+accountNumber] = recipientCode
	m.sets++
	return nil
}

func TestAutoTransferToOrder_RecipientCacheHitSkipsCreate(t *testing.T) {
	var createCalls int
	var transferRecipients []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, PaystackResolveAccount):
			writeJSON(t, w, ResolveAccountResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: ResolveAccountData{AccountName: "Test User"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackCreateRecipient:
			createCalls++
			writeJSON(t, w, CreateRecipientResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: RecipientData{RecipientCode: "RCP_created"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackTransfer:
			var req InitiateTransferRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode transfer request: %v", err)
			}
			transferRecipients = append(transferRecipients, req.Recipient)
			writeJSON(t, w, TransferResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: TransferData{Reference: req.Reference, TransferCode: "TRF_123", Status: "pending"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	store := &memoryRecipientCodeStore{store: map[string]string{"NG:011:0001234567": "RCP_cached"}}
	service := &PaystackService{Client: *server.Client(), BaseURL: server.URL, RecipientCodes: store}

	result, err := service.AutoTransferToOrder(context.Background(), stubBankLookup{banks: []BankEntry{{Name: "First Bank", Code: "011"}}}, AutoTransferRequest{
		AccountNumber: "0001234567",
		BankName:      "First Bank",
		AmountKobo:    150000,
		Currency:      "NGN",
		Reference:     "ref-1",
		Country:       "NG",
	})
	if err != nil {
		t.Fatalf("AutoTransferToOrder returned error: %v", err)
	}
	if result.TransferCode != "TRF_123" {
		t.Fatalf("transfer code = %s, want TRF_123", result.TransferCode)
	}
	if createCalls != 0 {
		t.Fatalf("CreateTransferRecipient calls = %d, want 0", createCalls)
	}
	if len(transferRecipients) != 1 || transferRecipients[0] != "RCP_cached" {
		t.Fatalf("transfer recipients = %v, want [RCP_cached]", transferRecipients)
	}
	if store.sets != 0 {
		t.Fatalf("cache set count = %d, want 0", store.sets)
	}
}

func TestAutoTransferToOrder_RecipientCacheMissCreatesAndStores(t *testing.T) {
	var createCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, PaystackResolveAccount):
			writeJSON(t, w, ResolveAccountResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: ResolveAccountData{AccountName: "Test User"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackCreateRecipient:
			createCalls++
			writeJSON(t, w, CreateRecipientResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: RecipientData{RecipientCode: "RCP_created"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackTransfer:
			var req InitiateTransferRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode transfer request: %v", err)
			}
			if req.Recipient != "RCP_created" {
				t.Fatalf("transfer recipient = %s, want RCP_created", req.Recipient)
			}
			writeJSON(t, w, TransferResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: TransferData{Reference: req.Reference, TransferCode: "TRF_123", Status: "pending"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	store := &memoryRecipientCodeStore{}
	service := &PaystackService{Client: *server.Client(), BaseURL: server.URL, RecipientCodes: store}

	_, err := service.AutoTransferToOrder(context.Background(), stubBankLookup{banks: []BankEntry{{Name: "First Bank", Code: "011"}}}, AutoTransferRequest{
		AccountNumber: "0001234567",
		BankName:      "First Bank",
		AmountKobo:    150000,
		Currency:      "NGN",
		Reference:     "ref-1",
		Country:       "NG",
	})
	if err != nil {
		t.Fatalf("AutoTransferToOrder returned error: %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("CreateTransferRecipient calls = %d, want 1", createCalls)
	}
	if got, ok := store.store["NG:011:0001234567"]; !ok || got != "RCP_created" {
		t.Fatalf("stored recipient code = %q, found=%v; want RCP_created", got, ok)
	}
}

func TestAutoTransferToOrder_StaleCachedRecipientRefreshesOnce(t *testing.T) {
	var createCalls int
	var transferCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, PaystackResolveAccount):
			writeJSON(t, w, ResolveAccountResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: ResolveAccountData{AccountName: "Test User"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackCreateRecipient:
			createCalls++
			writeJSON(t, w, CreateRecipientResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: RecipientData{RecipientCode: "RCP_fresh"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackTransfer:
			transferCalls++
			var req InitiateTransferRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode transfer request: %v", err)
			}
			if transferCalls == 1 {
				writeJSON(t, w, TransferResponse{BasePaystackResponse: BasePaystackResponse{Status: false, Message: "Invalid transfer recipient"}})
				return
			}
			if req.Recipient != "RCP_fresh" {
				t.Fatalf("second transfer recipient = %s, want RCP_fresh", req.Recipient)
			}
			writeJSON(t, w, TransferResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: TransferData{Reference: req.Reference, TransferCode: "TRF_123", Status: "pending"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	store := &memoryRecipientCodeStore{store: map[string]string{"NG:011:0001234567": "RCP_stale"}}
	service := &PaystackService{Client: *server.Client(), BaseURL: server.URL, RecipientCodes: store}

	result, err := service.AutoTransferToOrder(context.Background(), stubBankLookup{banks: []BankEntry{{Name: "First Bank", Code: "011"}}}, AutoTransferRequest{
		AccountNumber: "0001234567",
		BankName:      "First Bank",
		AmountKobo:    150000,
		Currency:      "NGN",
		Reference:     "ref-1",
		Country:       "NG",
	})
	if err != nil {
		t.Fatalf("AutoTransferToOrder returned error: %v", err)
	}
	if result.TransferCode != "TRF_123" {
		t.Fatalf("transfer code = %s, want TRF_123", result.TransferCode)
	}
	if createCalls != 1 {
		t.Fatalf("CreateTransferRecipient calls = %d, want 1", createCalls)
	}
	if transferCalls != 2 {
		t.Fatalf("transfer calls = %d, want 2", transferCalls)
	}
	if got := store.store["NG:011:0001234567"]; got != "RCP_fresh" {
		t.Fatalf("stored recipient code = %s, want RCP_fresh", got)
	}
	if store.sets != 1 {
		t.Fatalf("cache set count = %d, want 1", store.sets)
	}
}

func TestAutoTransferToOrder_TransferInsufficientBalanceMapsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, PaystackResolveAccount):
			writeJSON(t, w, ResolveAccountResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: ResolveAccountData{AccountName: "Test User"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackCreateRecipient:
			writeJSON(t, w, CreateRecipientResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: RecipientData{RecipientCode: "RCP_created"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackTransfer:
			writeJSON(t, w, TransferResponse{BasePaystackResponse: BasePaystackResponse{Status: false, Message: "Balance is not enough for this transfer"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	service := &PaystackService{Client: *server.Client(), BaseURL: server.URL}

	_, err := service.AutoTransferToOrder(context.Background(), stubBankLookup{banks: []BankEntry{{Name: "First Bank", Code: "011"}}}, AutoTransferRequest{
		AccountNumber: "0001234567",
		BankName:      "First Bank",
		AmountKobo:    150000,
		Currency:      "NGN",
		Reference:     "ref-1",
		Country:       "NG",
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("error = %v, want wrapped ErrInsufficientBalance", err)
	}
}

func TestAutoTransferToOrder_TransferGenericFailureRemainsGeneric(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, PaystackResolveAccount):
			writeJSON(t, w, ResolveAccountResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: ResolveAccountData{AccountName: "Test User"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackCreateRecipient:
			writeJSON(t, w, CreateRecipientResponse{BasePaystackResponse: BasePaystackResponse{Status: true}, Data: RecipientData{RecipientCode: "RCP_created"}})
		case r.Method == http.MethodPost && r.URL.Path == PaystackTransfer:
			writeJSON(t, w, TransferResponse{BasePaystackResponse: BasePaystackResponse{Status: false, Message: "Transfer OTP required"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	service := &PaystackService{Client: *server.Client(), BaseURL: server.URL}

	_, err := service.AutoTransferToOrder(context.Background(), stubBankLookup{banks: []BankEntry{{Name: "First Bank", Code: "011"}}}, AutoTransferRequest{
		AccountNumber: "0001234567",
		BankName:      "First Bank",
		AmountKobo:    150000,
		Currency:      "NGN",
		Reference:     "ref-1",
		Country:       "NG",
	})
	if err == nil {
		t.Fatalf("expected transfer error")
	}
	if errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("error = %v, did not want ErrInsufficientBalance", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
}
