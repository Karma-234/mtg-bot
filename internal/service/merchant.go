package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type MerchantServiceConfig struct {
	APIKey    string
	APISecret string
	BaseURL   string
}

func NewMerchantServiceConfig(e string) (*MerchantServiceConfig, error) {
	apiKey := os.Getenv("BBT_KEY")
	apiSecret := os.Getenv("BBT_SECRET")
	baseURLProd := os.Getenv("BBT_BASE_URL_PROD")
	if apiKey == "" || apiSecret == "" || os.Getenv("BBT_BASE_URL") == "" || baseURLProd == "" {
		return nil, fmt.Errorf("BBT_KEY and BBT_SECRET must be set in environment variables")
	}
	if e == "prod" {

		return &MerchantServiceConfig{
			APIKey:    apiKey,
			APISecret: apiSecret,
			BaseURL:   baseURLProd,
		}, nil
	}
	baseURL := os.Getenv("BBT_BASE_URL")
	return &MerchantServiceConfig{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   baseURL,
	}, nil
}

type MerchantService struct {
	ExternalService
	Config MerchantServiceConfig
	Client http.Client
}

func (s *MerchantService) GetLatestOrders(opts *OrderQueryRequest) (*OrdersResponse, error) {
	url := s.Config.BaseURL + GETALLORDERS
	if opts == nil {
		opts = &OrderQueryRequest{
			Page:     1,
			PageSize: 10,
		}
	}

	res, err := PostJSON(&s.Client, url, opts)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var result OrdersResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, err
}

func (s *MerchantService) GetPendingOrders(opts *OrderQueryRequest) (*OrdersResponse, error) {
	url := s.Config.BaseURL + GETPENDINGORDERS
	orderSide := 0
	if opts == nil {
		status := 10
		opts = &OrderQueryRequest{
			Page:     1,
			PageSize: 30,
			Status:   &status,
			Side:     &orderSide,
		}
	}

	res, err := PostJSON(&s.Client, url, opts)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	var result OrdersResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (s *MerchantService) GetOrderDetail(opts SingleOrderQueryRequest) (*http.Response, error) {
	url := s.Config.BaseURL + GETORDERDETAIL
	res, err := PostJSON(&s.Client, url, opts)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return res, err
}

func (s *MerchantService) MarkOrderPaid(opts MarkOrderPaidRequest) (*http.Response, error) {
	url := s.Config.BaseURL + MARKORDERASPAID
	res, err := PostJSON(&s.Client, url, opts)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return res, err
}
