package bothandlers

import (
	"context"
	"log"
	"time"

	"github.com/karma-234/mtg-bot/internal/botruntime"
	"github.com/karma-234/mtg-bot/internal/cache"
	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

type DurationButtons struct {
	OneHour    telebot.Btn
	ThreeHours telebot.Btn
	SixHours   telebot.Btn
}

func BuildStartMarkup() (*telebot.ReplyMarkup, telebot.Btn) {
	markup := &telebot.ReplyMarkup{}
	fetcherBtn := markup.Data("Bybit Agent", "bybit_fetcher")
	markup.Inline(markup.Row(fetcherBtn))
	return markup, fetcherBtn
}

func BuildDurationMarkup() (*telebot.ReplyMarkup, DurationButtons) {
	markup := &telebot.ReplyMarkup{}
	buttons := DurationButtons{
		OneHour:    markup.Data("1 hour", "dur_1h"),
		ThreeHours: markup.Data("3 hours", "dur_3h"),
		SixHours:   markup.Data("6 hours", "dur_6h"),
	}
	markup.Inline(markup.Row(buttons.OneHour, buttons.ThreeHours, buttons.SixHours))
	return markup, buttons
}

func RegisterHandlers(
	b *telebot.Bot,
	taskManager *botruntime.TaskManager,
	merchantService *service.MerchantService,
	userStateCache cache.UserStateCache,
	ordersCache cache.OrdersCache,
) {
	startMarkup, fetcherBtn := BuildStartMarkup()
	durationMarkup, durationButtons := BuildDurationMarkup()

	b.Handle("/start", func(ctx telebot.Context) error {
		sender := ctx.Sender()
		log.Printf("Received /start command from user %s  ", sender.Username)
		return ctx.Send("Hello, "+sender.Username+"!\n Here are the available services:\n\n", startMarkup)
	})

	b.Handle(&fetcherBtn, func(ctx telebot.Context) error {
		log.Printf("Received Bybit Agent request from user %s ", ctx.Sender().Username)
		if err := ctx.Edit("Fetching latest MTG news...", &telebot.SendOptions{ReplyMarkup: &telebot.ReplyMarkup{}}); err != nil {
			log.Printf("Failed to edit message for user %s: %v", ctx.Sender().Username, err)
		}
		return ctx.Send("Here is the  latest MTG news...", &telebot.SendOptions{ReplyMarkup: durationMarkup})
	})

	durationHandler := func(duration time.Duration) telebot.HandlerFunc {
		return func(ctx telebot.Context) error {
			log.Printf("Received task duration selection '%s' from user %s", duration.String(), ctx.Sender().Username)
			if userStateCache != nil {
				if err := userStateCache.SetSelectedDuration(context.Background(), ctx.Chat().ID, duration, 24*time.Hour); err != nil {
					log.Printf("Failed to persist duration for chat %d: %v", ctx.Chat().ID, err)
				}
			}
			if err := ctx.Edit("You selected Bybit Agent for duration: "+duration.String(), &telebot.SendOptions{ReplyMarkup: &telebot.ReplyMarkup{}}); err != nil {
				log.Printf("Failed to edit duration selection for user %s: %v", ctx.Sender().Username, err)
			}
			taskManager.Schedule(b, duration, ctx.Chat(), merchantService, ordersCache)
			if err := ctx.Respond(&telebot.CallbackResponse{Text: "Task duration set to " + duration.String()}); err != nil {
				log.Printf("Failed to send callback response to user %s: %v", ctx.Sender().Username, err)
				return err
			}
			return nil
		}
	}

	b.Handle(&durationButtons.OneHour, durationHandler(1*time.Minute))
	b.Handle(&durationButtons.ThreeHours, durationHandler(3*time.Minute))
	b.Handle(&durationButtons.SixHours, durationHandler(6*time.Minute))
}
