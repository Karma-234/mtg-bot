package service

import (
	"net/http"
	"os"
)

type MerchantServiceConfig struct {
	APIKey    string
	APISecret string
	BaseURL   string
}

func NewMerchantServiceConfig(e string) *MerchantServiceConfig {
	apiKey := os.Getenv("BBT_KEY")
	apiSecret := os.Getenv("BBT_SECRET")
	baseURLProd := os.Getenv("BBT_BASE_URL_PROD")
	if apiKey == "" || apiSecret == "" || os.Getenv("BBT_BASE_URL") == "" || baseURLProd == "" {
		panic("BBT_KEY and BBT_SECRET must be set in environment variables")
	}
	if e == "prod" {

		return &MerchantServiceConfig{
			APIKey:    apiKey,
			APISecret: apiSecret,
			BaseURL:   baseURLProd,
		}
	}
	baseURL := os.Getenv("BBT_BASE_URL")
	return &MerchantServiceConfig{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   baseURL,
	}
}

type MerchantService struct {
	ExternalService
	config MerchantServiceConfig
}

func (s *MerchantService) GetLatestOrders(req *http.Request) error {

	return nil
}

func (s *MerchantService) GetTradeDetails() {

}
