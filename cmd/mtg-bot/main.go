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
		{Text: "start", Description: "Starts the bot"},
	}
	err := b.SetCommands(cmdsn)
	if err != nil {
		log.Printf("Error setting commands: %v", err)
	}

	b.Handle("/start", func(ctx telebot.Context) error {
		sender := ctx.Sender()
		chat_id := ctx.Chat().ID
		log.Printf("Received /start command from user %s (ID: %d) in chat %d", sender.Username, sender.ID, chat_id)
		return ctx.Send("Hello, " + sender.Username + "!")
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
