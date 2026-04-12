package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/karma-234/mtg-bot/internal/botruntime"
	"github.com/karma-234/mtg-bot/internal/cache"
	redisinfra "github.com/karma-234/mtg-bot/internal/redis"
	"github.com/karma-234/mtg-bot/internal/service"
	"github.com/redis/go-redis/v9"
	"gopkg.in/telebot.v4"
)

func selectEnvironment(prod, dev bool) *service.MerchantServiceConfig {
	switch {
	case prod && dev:
		log.Fatal("Cannot use both --prod and --dev flags at the same time")
	case prod:
		log.Println("Using production environment")
		cfg, err := service.NewMerchantServiceConfig("prod")
		if err != nil {
			log.Fatalf("Failed to load production merchant config: %v", err)
		}
		return cfg
	case dev:
		log.Println("Using development/testnet environment")
		cfg, err := service.NewMerchantServiceConfig("dev")
		if err != nil {
			log.Fatalf("Failed to load development merchant config: %v", err)
		}
		return cfg
	default:
		log.Println("No environment flag provided, defaulting to development/testnet environment")
		cfg, err := service.NewMerchantServiceConfig("dev")
		if err != nil {
			log.Fatalf("Failed to load default merchant config: %v", err)
		}
		return cfg
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

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func buildRedisConfigFromEnv() RedisConfig {
	db := 0
	if rawDB := os.Getenv("REDIS_DB"); rawDB != "" {
		parsedDB, err := strconv.Atoi(rawDB)
		if err != nil {
			log.Printf("Invalid REDIS_DB value %q; defaulting to 0", rawDB)
		} else {
			db = parsedDB
		}
	}

	return RedisConfig{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       db,
	}
}

func buildRedisClient(cfg RedisConfig) *redis.Client {
	client := redisinfra.NewClient(redisinfra.Config{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := redisinfra.Ping(context.Background(), client); err != nil {
		log.Printf("Redis ping failed: %v", err)
	}

	return client
}

func buildOrdersCache(rdb *redis.Client) cache.OrdersCache {
	return cache.NewRedisOrdersCache(rdb)
}

func buildWorkflowStore(rdb *redis.Client) cache.WorkflowStore {
	return cache.NewRedisWorkflowStore(rdb)
}

func buildUserStateCache(rdb *redis.Client) cache.UserStateCache {
	return cache.NewRedisUserStateCache(rdb)
}

func buildRetryPolicy() botruntime.RetryPolicy {
	return botruntime.DefaultRetryPolicy()
}

func buildBankCache(rdb *redis.Client) cache.BankCache {
	return cache.NewRedisBankCache(rdb)
}

func buildRecipientCodeCache(rdb *redis.Client) cache.RecipientCodeCache {
	return cache.NewRedisRecipientCodeCache(rdb)
}

func buildPaymentIntentStore(rdb *redis.Client) cache.PaymentIntentStore {
	return cache.NewRedisPaymentIntentStore(rdb)
}

func buildProviderMarkQueue(rdb *redis.Client) cache.ProviderMarkQueue {
	return cache.NewRedisProviderMarkQueue(rdb)
}

func buildPaystackService() *service.PaystackService {
	key := os.Getenv("PMNT_PRV_KEY")
	if key == "" {
		log.Fatal("PMNT_PRV_KEY is not set")
	}
	return &service.PaystackService{
		Client: http.Client{
			Timeout: 15 * time.Second,
			Transport: &service.PaystackInterceptor{
				Base:      http.DefaultTransport,
				SecretKey: key,
			},
		},
		BaseURL: service.PaystackBaseURL,
	}
}
