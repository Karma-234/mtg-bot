package bothandlers

import (
	"context"
	"fmt"
	"log"
	"strings"
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

		return ctx.Send("You selected Bybit Agent. Please choose a duration:", &telebot.SendOptions{ReplyMarkup: durationMarkup})
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
			taskManager.Schedule(b, duration, ctx.Chat(), "bybit", merchantService, ordersCache)
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

type PaymentInspector interface {
	GetBalance() (*service.PaystackBalanceResponse, error)
}

type PaymentHistoryStore interface {
	ListByChat(ctx context.Context, chatID int64, limit int) ([]*service.PaymentIntentRecord, error)
}

func RegisterPaymentHandlers(
	b *telebot.Bot,
	inspector PaymentInspector,
	store PaymentHistoryStore,
) {
	b.Handle("/balance", func(ctx telebot.Context) error {
		resp, err := inspector.GetBalance()
		if err != nil {
			return ctx.Send("Failed to fetch Paystack balance: " + err.Error())
		}
		msg := "Paystack Balance:\n"
		for _, entry := range resp.Data {
			msg += fmt.Sprintf("  %s: %.2f\n", entry.Currency, float64(entry.Balance)/100)
		}
		return ctx.Send(msg)
	})

	b.Handle("/payments", func(ctx telebot.Context) error {
		intents, err := store.ListByChat(context.Background(), ctx.Chat().ID, 10)
		if err != nil {
			return ctx.Send("Failed to fetch payment history: " + err.Error())
		}
		if len(intents) == 0 {
			return ctx.Send("No payment records found.")
		}
		var msg strings.Builder
		msg.WriteString("Recent payments:\n")
		for _, pi := range intents {
			fmt.Fprintf(&msg, "  Order: %s | Status: %s | Amount: %.2f NGN | Ref: %s\n",
				pi.OrderID, pi.Status, float64(pi.AmountKobo)/100, pi.PaystackReference)
		}
		return ctx.Send(msg.String())
	})

	b.Handle("/fund", func(ctx telebot.Context) error {
		return ctx.Send("To fund your Paystack balance, visit https://dashboard.paystack.com and top up your NGN balance.")
	})
}
