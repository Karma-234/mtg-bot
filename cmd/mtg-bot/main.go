package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/karma-234/mtg-bot/internal/bothandlers"
	"github.com/karma-234/mtg-bot/internal/botruntime"
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
	go func() {
		banks, err := paystackPaymentService.ListBanks("NG")
		if err != nil {
			log.Printf("Failed to pre-populate bank cache: %v", err)
			return
		}
		if err := bankCache.SetBanks(context.Background(), "NG", banks.Data, 24*time.Hour); err != nil {
			log.Printf("Failed to cache bank list: %v", err)
			return
		}
		log.Printf("Bank list cached: %d banks", len(banks.Data))
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
	me := b.Me
	log.Printf("Bot username: %s", me.Username)
	commands := []telebot.Command{
		{Text: "start", Description: "Start the bot and see available services"},
	}
	err = b.SetCommands(commands)
	if err != nil {
		log.Printf("Error setting commands: %v", err)
	}
	bothandlers.RegisterHandlers(b, taskManager, merchantService, userStateCache, ordersCache)

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Bot started with username: %s", me.Username)
		b.Start()
	}()
	<-done
	b.Stop()
	taskManager.StopAll()
	log.Println("Bot stopped")
}
