package service

import (
	"encoding/json"
	"fmt"
	"net/http"
)

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
