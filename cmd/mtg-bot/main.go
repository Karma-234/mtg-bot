package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	pref := buildBotSettings(apiKey)
	b, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	taskManager := botruntime.NewTaskManager()
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
			taskManager.Schedule(b, duration, ctx.Chat(), merchantService)

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
	taskManager.StopAll()
	log.Println("Bot stopped")
}
