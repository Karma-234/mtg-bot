package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const defaultTransferFeeBufferKobo int64 = 10000

var ErrInsufficientBalance = errors.New("insufficient paystack balance")

type BankLookup interface {
	GetBanks(ctx context.Context, country string) ([]BankEntry, bool, error)
}

type AutoTransferRequest struct {
	OrderID       string
	ChatID        int64
	Provider      string
	Beneficiary   string
	AccountNumber string
	BankName      string
	AmountKobo    int64
	Currency      string
	Reference     string
	Reason        string
	Country       string
}

type AutoTransferResult struct {
	Reference    string
	TransferCode string
	Status       string
}

type PaystackService struct {
	Client  http.Client
	BaseURL string
}

func (s *PaystackService) GetBalance() (*PaystackBalanceResponse, error) {
	res, err := GetJSON(&s.Client, s.BaseURL+PaystackBalance)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result PaystackBalanceResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, result.Error()
}

func (s *PaystackService) ListBanks(country string) (*ListBanksResponse, error) {
	url := fmt.Sprintf("%s%s?country=%s&perPage=100", s.BaseURL, PaystackListBanks, country)
	res, err := GetJSON(&s.Client, url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result ListBanksResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, result.Error()
}

func (s *PaystackService) ResolveAccount(accountNumber, bankCode string) (*ResolveAccountResponse, error) {
	url := fmt.Sprintf("%s%s?account_number=%s&bank_code=%s",
		s.BaseURL, PaystackResolveAccount, accountNumber, bankCode)
	res, err := GetJSON(&s.Client, url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result ResolveAccountResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, result.Error()
}

func (s *PaystackService) CreateTransferRecipient(req CreateRecipientRequest) (*CreateRecipientResponse, error) {
	res, err := PostJSON(&s.Client, s.BaseURL+PaystackCreateRecipient, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result CreateRecipientResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, result.Error()
}

func (s *PaystackService) InitiateTransfer(req InitiateTransferRequest) (*TransferResponse, error) {
	res, err := PostJSON(&s.Client, s.BaseURL+PaystackTransfer, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result TransferResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, result.Error()
}

func (s *PaystackService) VerifyTransfer(reference string) (*TransferResponse, error) {
	url := fmt.Sprintf("%s%s/%s", s.BaseURL, PaystackVerifyTransfer, reference)
	res, err := GetJSON(&s.Client, url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result TransferResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, result.Error()
}

func (s *PaystackService) InitializeTransaction(req InitTransactionRequest) (*InitTransactionResponse, error) {
	res, err := PostJSON(&s.Client, s.BaseURL+PaystackInitTransaction, req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result InitTransactionResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, result.Error()
}

func normalizeBankName(value string) string {
	replacer := strings.NewReplacer("-", " ", "_", " ", ",", " ", ".", " ")
	parts := strings.Fields(strings.ToLower(replacer.Replace(value)))
	return strings.Join(parts, " ")
}

func (s *PaystackService) FindBankCodeByName(ctx context.Context, bankLookup BankLookup, country, bankName string) (string, error) {
	if bankLookup == nil {
		return "", fmt.Errorf("bank lookup is nil")
	}
	banks, found, err := bankLookup.GetBanks(ctx, country)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("bank cache miss for country %s", country)
	}

	normalizedTarget := normalizeBankName(bankName)
	for _, bank := range banks {
		if normalizeBankName(bank.Name) == normalizedTarget {
			return bank.Code, nil
		}
	}

	return "", fmt.Errorf("bank code not found for bank name %q", bankName)
}

func (s *PaystackService) EnsureSufficientBalance(requiredKobo int64, currency string) error {
	balanceResp, err := s.GetBalance()
	if err != nil {
		return err
	}

	balanceByCurrency := int64(0)
	targetCurrency := strings.ToUpper(currency)
	for _, entry := range balanceResp.Data {
		if strings.EqualFold(entry.Currency, targetCurrency) {
			balanceByCurrency = entry.Balance
			break
		}
	}

	requiredTotal := requiredKobo + defaultTransferFeeBufferKobo
	if balanceByCurrency < requiredTotal {
		return fmt.Errorf("%w: available=%d required=%d currency=%s", ErrInsufficientBalance, balanceByCurrency, requiredTotal, targetCurrency)
	}

	return nil
}

func (s *PaystackService) AutoTransferToOrder(ctx context.Context, bankLookup BankLookup, req AutoTransferRequest) (*AutoTransferResult, error) {
	if req.Reference == "" {
		return nil, fmt.Errorf("transfer reference is required")
	}
	if req.AccountNumber == "" {
		return nil, fmt.Errorf("account number is required")
	}
	if req.BankName == "" {
		return nil, fmt.Errorf("bank name is required")
	}
	if req.AmountKobo <= 0 {
		return nil, fmt.Errorf("amount must be greater than zero")
	}
	if req.Currency == "" {
		req.Currency = "NGN"
	}
	if req.Country == "" {
		req.Country = "NG"
	}

	bankCode, err := s.FindBankCodeByName(ctx, bankLookup, req.Country, req.BankName)
	if err != nil {
		return nil, err
	}

	resolvedAccount, err := s.ResolveAccount(req.AccountNumber, bankCode)
	if err != nil {
		return nil, err
	}

	recipientName := req.Beneficiary
	if recipientName == "" {
		recipientName = resolvedAccount.Data.AccountName
	}

	if err := s.EnsureSufficientBalance(req.AmountKobo, req.Currency); err != nil {
		return nil, err
	}

	recipientResp, err := s.CreateTransferRecipient(CreateRecipientRequest{
		Type:          "nuban",
		Name:          recipientName,
		AccountNumber: req.AccountNumber,
		BankCode:      bankCode,
		Currency:      strings.ToUpper(req.Currency),
	})
	if err != nil {
		return nil, err
	}

	transferResp, err := s.InitiateTransfer(InitiateTransferRequest{
		Source:    "balance",
		Amount:    req.AmountKobo,
		Recipient: recipientResp.Data.RecipientCode,
		Reference: req.Reference,
		Reason:    req.Reason,
		Currency:  strings.ToUpper(req.Currency),
	})
	if err != nil {
		return nil, err
	}

	return &AutoTransferResult{
		Reference:    transferResp.Data.Reference,
		TransferCode: transferResp.Data.TransferCode,
		Status:       transferResp.Data.Status,
	}, nil
}
