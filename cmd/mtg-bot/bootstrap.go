package main

import (
	"log"
	"net/http"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

func selectEnvironment(prod, dev bool) *service.MerchantServiceConfig {
	switch {
	case prod && dev:
		log.Fatal("Cannot use both --prod and --dev flags at the same time")
	case prod:
		log.Println("Using production environment")
		return service.NewMerchantServiceConfig("prod")
	case dev:
		log.Println("Using development/testnet environment")
		return service.NewMerchantServiceConfig("dev")
	default:
		log.Println("No environment flag provided, defaulting to development/testnet environment")
		return service.NewMerchantServiceConfig("dev")
	}

	return nil
}

func buildHTTPClient(cfg service.MerchantServiceConfig) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &service.RequestInterceptor{
			Base:          http.DefaultTransport,
			ServiceConfig: cfg,
		},
	}
}

func buildMerchantService(cfg service.MerchantServiceConfig, client *http.Client) *service.MerchantService {
	return &service.MerchantService{Config: cfg, Client: *client}
}

func buildBotSettings(apiKey string) telebot.Settings {
	return telebot.Settings{
		Token:  apiKey,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}
}
