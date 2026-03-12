package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

	pref := telebot.Settings{
		Token:  apiKey,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}
	b, _ := telebot.NewBot(pref)
	taskManager := &TaskManager{tasks: make(map[int64]context.CancelFunc)}
	me := b.Me
	log.Printf("Bot username: %s", me.Username)
	cmdsn := []telebot.Command{
		{Text: "start", Description: "Says Hello to the user"},
	}
	err := b.SetCommands(cmdsn)
	if err != nil {
		log.Printf("Error setting commands: %v", err)
	}
	startButtonMarkup := &telebot.ReplyMarkup{}
	fetcherBtn := startButtonMarkup.Data("Fetch latest MTG news", "fetch-news")
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
		log.Printf("Received fetch news request from user %s ", ctx.Sender().Username)

		ctx.Edit("Fetching latest MTG news...", &telebot.SendOptions{ReplyMarkup: &telebot.ReplyMarkup{}})
		time.Sleep(3 * time.Second)
		return ctx.Send("Here is the  latest MTG news...", &telebot.SendOptions{ReplyMarkup: taskDurationMarkup})
	})

	durationHandler := func(duration time.Duration) telebot.HandlerFunc {
		return func(ctx telebot.Context) error {
			log.Printf("Received task duration selection '%s' from user %s", duration.String(), ctx.Sender().Username)

			ctx.Edit("You selected task duration: "+duration.String(), &telebot.SendOptions{ReplyMarkup: &telebot.ReplyMarkup{}})
			taskScheduler(b, duration, ctx.Chat(), taskManager)

			return ctx.Respond(&telebot.CallbackResponse{Text: "Task duration set to " + duration.String()})
		}
	}
	b.Handle(&setTask1hDurationBtn, durationHandler(1*time.Minute))
	b.Handle(&setTask3hDurationBtn, durationHandler(3*time.Minute))
	b.Handle(&setTask6hDurationBtn, durationHandler(6*time.Minute))

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

func taskScheduler(b *telebot.Bot, duration time.Duration, chat *telebot.Chat, taskManager *TaskManager) {
	taskManager.tasksMu.Lock()
	if cancel, exists := taskManager.tasks[chat.ID]; exists {
		cancel()
		log.Printf("Existing task for chat %d cancelled", chat.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	taskManager.tasks[chat.ID] = cancel
	taskManager.tasksMu.Unlock()

	go func() {
		ticker := time.NewTicker(20 * time.Second)
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
				b.Send(chat, "Here is the latest MTG news...\n"+"TimeStamp"+t.Format("15:04:05")+"\n"+"Message count:"+fmt.Sprint(messageCount))
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
