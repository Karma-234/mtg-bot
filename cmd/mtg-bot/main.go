package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/telebot.v4"
)

func main() {
	apiKey := os.Getenv("TG_BOT_API_KEY")
	if apiKey == "" {
		log.Fatal("TG_BOT_API_KEY is not set")
	}

	pref := telebot.Settings{
		Token:  apiKey,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}
	b, _ := telebot.NewBot(pref)
	me := b.Me
	log.Printf("Bot username: %s", me.Username)
	cmdsn := []telebot.Command{
		{Text: "start", Description: "Says Hello to the user"},
	}
	err := b.SetCommands(cmdsn)
	if err != nil {
		log.Printf("Error setting commands: %v", err)
	}
	startButton := &telebot.ReplyMarkup{}
	fetcherBtn := startButton.Data("Fetch latest MTG news", "fetch-news")
	startButton.Inline(startButton.Row(fetcherBtn))

	b.Handle("/start", func(ctx telebot.Context) error {
		sender := ctx.Sender()

		log.Printf("Received /start command from user %s (ID: %d) in chat %d", sender.Username, sender.ID, sender.ID)
		return ctx.Send("Hello, "+sender.Username+"!\n Here are the available services:\n\n", startButton)
	})
	b.Handle(&fetcherBtn, func(ctx telebot.Context) error {
		log.Printf("Received fetch news request from user %s (ID: %d) in chat %d", ctx.Sender().Username, ctx.Sender().ID, ctx.Chat().ID)

		ctx.Edit("Fetching latest MTG news...", &telebot.SendOptions{ReplyMarkup: &telebot.ReplyMarkup{}})
		time.Sleep(3 * time.Second)
		return ctx.Send("Here is the  latest MTG news...", &telebot.SendOptions{ReplyMarkup: startButton})
	})

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Bot started with username: %s", me.Username)
		b.Start()
	}()
	<-done
	b.Stop()
	log.Println("Bot stopped")
}
