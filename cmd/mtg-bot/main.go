package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/karma-234/mtg-bot/internal/bothandlers"
	"github.com/karma-234/mtg-bot/internal/botruntime"
	"github.com/karma-234/mtg-bot/internal/providerqueue"
	"github.com/karma-234/mtg-bot/internal/webhook"
	"gopkg.in/telebot.v4"
)

func main() {
	apiKey := os.Getenv("TG_BOT_API_KEY")
	if apiKey == "" {
		log.Fatal("TG_BOT_API_KEY is not set")
	}
	var prod, dev bool
	flag.BoolVar(&prod, "prod", false, "Use production environment")
	flag.BoolVar(&dev, "dev", false, "Use development/testnet environment")

	flag.Parse()
	merchantConfig := selectEnvironment(prod, dev)
	client := buildHTTPClient(*merchantConfig)
	merchantService := buildMerchantService(*merchantConfig, client)
	paystackPaymentService := buildPaystackService()

	redisConfig := buildRedisConfigFromEnv()
	rdb := buildRedisClient(redisConfig)
	bankCache := buildBankCache(rdb)
	recipientCodeCache := buildRecipientCodeCache(rdb)
	paystackPaymentService.RecipientCodes = recipientCodeCache
	cacheCtx, cancelCache := context.WithCancel(context.Background())
	defer cancelCache()
	refreshBanks := func(ctx context.Context) {
		banks, err := paystackPaymentService.ListBanks("NG")
		if err != nil {
			log.Printf("Failed to refresh bank cache: %v", err)
			return
		}
		if err := bankCache.SetBanks(ctx, "NG", banks.Data, 24*time.Hour); err != nil {
			log.Printf("Failed to cache bank list: %v", err)
			return
		}
		log.Printf("Bank list cache refreshed: %d banks", len(banks.Data))
	}

	refreshBanks(cacheCtx)

	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				refreshBanks(cacheCtx)
			case <-cacheCtx.Done():
				return
			}
		}
	}()

	ordersCache := buildOrdersCache(rdb)
	workflowStore := buildWorkflowStore(rdb)
	userStateCache := buildUserStateCache(rdb)
	retryPolicy := buildRetryPolicy()
	pref := buildBotSettings(apiKey)
	b, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	taskManager := botruntime.NewTaskManager(workflowStore, retryPolicy)
	paymentIntentStore := buildPaymentIntentStore(rdb)
	providerMarkQueue := buildProviderMarkQueue(rdb)
	taskManager.SetPaymentDeps(paystackPaymentService, paymentIntentStore, bankCache)
	me := b.Me
	log.Printf("Bot username: %s", me.Username)
	commands := []telebot.Command{
		{Text: "start", Description: "Start the bot and see available services"},
		{Text: "balance", Description: "Check Paystack balance"},
		{Text: "payments", Description: "View recent payment history"},
		{Text: "fund", Description: "Instructions to top up Paystack balance"},
	}
	err = b.SetCommands(commands)
	if err != nil {
		log.Printf("Error setting commands: %v", err)
	}
	bothandlers.RegisterHandlers(b, taskManager, merchantService, userStateCache, ordersCache)
	bothandlers.RegisterPaymentHandlers(b, paystackPaymentService, paymentIntentStore)

	webhookSecret := os.Getenv("PAYSTACK_WEBHOOK_SECRET")
	webhookPort := os.Getenv("WEBHOOK_PORT")
	if webhookPort == "" {
		webhookPort = "8080"
	}
	webhookMux := http.NewServeMux()
	webhookMux.Handle("/webhook/paystack", webhook.NewPaystackWebhookHandler(
		webhookSecret, paymentIntentStore, paystackPaymentService, providerMarkQueue, b,
	))

	workerCtx, cancelWorker := context.WithCancel(context.Background())
	providerMarkWorker := providerqueue.NewWorker(
		providerMarkQueue,
		paymentIntentStore,
		workflowStore,
		merchantService,
		retryPolicy,
		b,
		"provider-mark-main",
	)
	go providerMarkWorker.Run(workerCtx)
	go func() {
		log.Printf("Webhook server listening on :%s", webhookPort)
		if err := http.ListenAndServe(":"+webhookPort, webhookMux); err != nil && err != http.ErrServerClosed {
			log.Printf("Webhook server error: %v", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Bot started with username: %s", me.Username)
		b.Start()
	}()
	<-done
	cancelCache()
	cancelWorker()
	b.Stop()
	taskManager.StopAll()
	log.Println("Bot stopped")
}
