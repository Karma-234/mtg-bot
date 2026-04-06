package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

type TaskManager struct {
	tasks   map[int64]context.CancelFunc
	tasksMu sync.RWMutex
}

func main() {
	apiKey := os.Getenv("TG_BOT_API_KEY")
	if apiKey == "" {
		log.Fatal("TG_BOT_API_KEY is not set")
	}
	var prod, dev bool
	flag.BoolVar(&prod, "prod", false, "Use production environment")
	flag.BoolVar(&dev, "dev", false, "Use development/testnet environment")

	flag.Parse()
	var merchantConfig *service.MerchantServiceConfig
	switch {
	case prod && dev:
		log.Fatal("Cannot use both --prod and --dev flags at the same time")
	case prod:
		log.Println("Using production environment")
		merchantConfig = service.NewMerchantServiceConfig("prod")
	case dev:
		log.Println("Using development/testnet environment")
		merchantConfig = service.NewMerchantServiceConfig("dev")
	default:
		log.Println("No environment flag provided, defaulting to development/testnet environment")
		merchantConfig = service.NewMerchantServiceConfig("dev")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &service.RequestInterceptor{
			Base:          http.DefaultTransport,
			ServiceConfig: *merchantConfig,
		}}
	_ = client
	merchantService := &service.MerchantService{Config: *merchantConfig, Client: *client}
	pref := telebot.Settings{
		Token:  apiKey,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}
	b, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	taskManager := &TaskManager{tasks: make(map[int64]context.CancelFunc)}
	me := b.Me
	log.Printf("Bot username: %s", me.Username)
	commands := []telebot.Command{
		{Text: "start", Description: "Says Hello to the user"},
	}
	err = b.SetCommands(commands)
	if err != nil {
		log.Printf("Error setting commands: %v", err)
	}
	startButtonMarkup := &telebot.ReplyMarkup{}
	fetcherBtn := startButtonMarkup.Data("Bybit Agent", "bybit_fetcher")
	startButtonMarkup.Inline(startButtonMarkup.Row(fetcherBtn))

	taskDurationMarkup := &telebot.ReplyMarkup{}
	setTask1hDurationBtn := taskDurationMarkup.Data("1 hour", "dur_1h")
	setTask3hDurationBtn := taskDurationMarkup.Data("3 hours", "dur_3h")
	setTask6hDurationBtn := taskDurationMarkup.Data("6 hours", "dur_6h")
	taskDurationMarkup.Inline(taskDurationMarkup.Row(setTask1hDurationBtn, setTask3hDurationBtn, setTask6hDurationBtn))

	b.Handle("/start", func(ctx telebot.Context) error {
		sender := ctx.Sender()

		log.Printf("Received /start command from user %s  ", sender.Username)
		return ctx.Send("Hello, "+sender.Username+"!\n Here are the available services:\n\n", startButtonMarkup)
	})
	b.Handle(&fetcherBtn, func(ctx telebot.Context) error {
		log.Printf("Received Bybit Agent request from user %s ", ctx.Sender().Username)

		if err := ctx.Edit("Fetching latest MTG news...", &telebot.SendOptions{ReplyMarkup: &telebot.ReplyMarkup{}}); err != nil {
			log.Printf("Failed to edit message for user %s: %v", ctx.Sender().Username, err)
		}
		return ctx.Send("Here is the  latest MTG news...", &telebot.SendOptions{ReplyMarkup: taskDurationMarkup})
	})

	durationHandler := func(duration time.Duration) telebot.HandlerFunc {
		return func(ctx telebot.Context) error {
			log.Printf("Received task duration selection '%s' from user %s", duration.String(), ctx.Sender().Username)

			if err := ctx.Edit("You selected Bybit Agent for duration: "+duration.String(), &telebot.SendOptions{ReplyMarkup: &telebot.ReplyMarkup{}}); err != nil {
				log.Printf("Failed to edit duration selection for user %s: %v", ctx.Sender().Username, err)
			}
			taskScheduler(b, duration, ctx.Chat(), taskManager, merchantService)

			if err := ctx.Respond(&telebot.CallbackResponse{Text: "Task duration set to " + duration.String()}); err != nil {
				log.Printf("Failed to send callback response to user %s: %v", ctx.Sender().Username, err)
				return err
			}
			return nil
		}
	}
	b.Handle(&setTask1hDurationBtn, durationHandler(1*time.Hour))
	b.Handle(&setTask3hDurationBtn, durationHandler(3*time.Hour))
	b.Handle(&setTask6hDurationBtn, durationHandler(6*time.Hour))

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Bot started with username: %s", me.Username)
		b.Start()
	}()
	<-done
	b.Stop()
	for chatID, cancel := range taskManager.tasks {
		cancel()
		log.Printf("Cancelled task for chat %d", chatID)
	}
	log.Println("Bot stopped")
}

func taskScheduler(b *telebot.Bot, duration time.Duration, chat *telebot.Chat, taskManager *TaskManager, srv *service.MerchantService) {
	taskManager.tasksMu.Lock()
	if cancel, exists := taskManager.tasks[chat.ID]; exists {
		cancel()
		log.Printf("Existing task for chat %d cancelled", chat.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	taskManager.tasks[chat.ID] = cancel
	taskManager.tasksMu.Unlock()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer func() {
			ticker.Stop()
			taskManager.tasksMu.Lock()
			delete(taskManager.tasks, chat.ID)
			taskManager.tasksMu.Unlock()
			log.Printf("Task for chat %d completed or cancelled", chat.ID)
		}()

		messageCount := 1
		for {
			select {
			case t := <-ticker.C:
				log.Printf("Executing scheduled task for chat %s", chat.Username)
				resp, err := srv.GetLatestOrders(nil)
				if err != nil {
					log.Printf("Failed to get Orders to : %v", err)
					if _, sendErr := b.Send(chat, "Failed to fetch orders\n"+"TimeStamp"+t.Format("15:04:05")+"\n"+"Message count:"+fmt.Sprint(messageCount)); sendErr != nil {
						log.Printf("Error sending fetch failure message to chat %d: %v", chat.ID, sendErr)
					}
					continue
				}
				if !resp.OK() {
					log.Printf("Error from merchant: %v", resp.Error())
				}
				msg := service.FormatOrdersMessage(resp)
				if _, sendErr := b.Send(chat, "Here is the latest MTG news...\n"+"TimeStamp"+t.Format("15:04:05")+"\n"+"Message count:"+fmt.Sprint(messageCount)+"\n\n"+msg); sendErr != nil {
					log.Printf("Error sending periodic update to chat %d: %v", chat.ID, sendErr)
				}
			case <-ctx.Done():
				log.Printf("Task for chat %v Completed", chat.Username)
				if _, err := b.Send(chat, "Task for user "+chat.Username+" completed"); err != nil {
					log.Printf("Error sending completion message to chat %d: %v", chat.ID, err)
				}
				return
			}
			messageCount++
		}
	}()
}
