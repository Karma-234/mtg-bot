package providerqueue

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/karma-234/mtg-bot/internal/botruntime"
	"github.com/karma-234/mtg-bot/internal/cache"
	"github.com/karma-234/mtg-bot/internal/observability"
	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

type ProviderPaidMarker interface {
	MarkOrderPaid(opts service.MarkOrderPaidRequest) (*http.Response, error)
}

type providerMarkDeadLetterQueue interface {
	DeadLetter(ctx context.Context, job cache.ProviderMarkJob, reason string) error
}

type Worker struct {
	queue         cache.ProviderMarkQueue
	intentStore   cache.PaymentIntentStore
	workflowStore cache.WorkflowStore
	orderMarker   ProviderPaidMarker
	retryPolicy   botruntime.RetryPolicy
	bot           *telebot.Bot
	consumerName  string
	blockFor      time.Duration
}

func NewWorker(
	queue cache.ProviderMarkQueue,
	intentStore cache.PaymentIntentStore,
	workflowStore cache.WorkflowStore,
	orderMarker ProviderPaidMarker,
	retryPolicy botruntime.RetryPolicy,
	bot *telebot.Bot,
	consumerName string,
) *Worker {
	if consumerName == "" {
		consumerName = fmt.Sprintf("provider-worker-%d", time.Now().UnixNano())
	}
	return &Worker{
		queue:         queue,
		intentStore:   intentStore,
		workflowStore: workflowStore,
		orderMarker:   orderMarker,
		retryPolicy:   retryPolicy,
		bot:           bot,
		consumerName:  consumerName,
		blockFor:      2 * time.Second,
	}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.queue == nil || w.intentStore == nil || w.workflowStore == nil || w.orderMarker == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := w.queue.Dequeue(ctx, w.consumerName, w.blockFor)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("providerqueue: dequeue failed: %v", err)
			continue
		}
		if msg == nil {
			continue
		}

		if err := w.processMessage(ctx, msg); err != nil {
			log.Printf("providerqueue: process failed for message %s: %v", msg.ID, err)
		}
	}
}

func (w *Worker) processMessage(ctx context.Context, msg *cache.ProviderMarkMessage) error {
	intent, found, err := w.intentStore.GetByReference(ctx, msg.Job.PaymentReference)
	if err != nil {
		return err
	}
	if !found {
		intent, found, err = w.intentStore.GetByOrderID(ctx, msg.Job.OrderID)
		if err != nil {
			return err
		}
	}
	if !found {
		return w.queue.Ack(ctx, msg.ID)
	}

	if intent.Status == service.PaymentIntentProviderPaid {
		return w.queue.Ack(ctx, msg.ID)
	}
	if intent.Status != service.PaymentIntentTransferSuccess && intent.Status != service.PaymentIntentProviderFailed {
		return w.queue.Ack(ctx, msg.ID)
	}

	resp, markErr := w.orderMarker.MarkOrderPaid(service.MarkOrderPaidRequest{
		OrderID:     intent.OrderID,
		PaymentType: "transfer",
		PaymentID:   intent.PaystackReference,
	})
	if resp != nil {
		resp.Body.Close()
	}

	now := time.Now().UTC()
	if markErr != nil {
		intent.Status = service.PaymentIntentProviderFailed
		intent.LastError = markErr.Error()
		intent.RetryCount++
		observability.Global().RetryCount.Inc()
		if w.retryPolicy.IsExhausted(intent.RetryCount) {
			observability.Global().RetryExhausted.Inc()
			observability.Warn("provider_mark_retry_exhausted", observability.LogFields{
				Component: "providerqueue",
				OrderID:   intent.OrderID,
				ChatID:    intent.ChatID,
				Intent:    intent.PaymentID,
				Extra: map[string]any{
					"retry_count":  intent.RetryCount,
					"max_attempts": w.retryPolicy.MaxAttempts,
				},
				Error: markErr,
			})
			intent.NextRetryAt = time.Time{}
			intent.UpdatedAt = now
			intent.LastError = fmt.Sprintf("provider mark exhausted after %d attempts: %v", intent.RetryCount, markErr)
			if err := w.intentStore.Save(ctx, intent); err != nil {
				return err
			}
			if dlq, ok := w.queue.(providerMarkDeadLetterQueue); ok {
				if err := dlq.DeadLetter(ctx, msg.Job, intent.LastError); err != nil {
					return err
				}
			}
			return w.queue.Ack(ctx, msg.ID)
		}

		delay := w.retryPolicy.NextDelay(intent.RetryCount)
		intent.NextRetryAt = now.Add(delay)
		intent.UpdatedAt = now
		observability.Debug("provider_mark_retry_scheduled", observability.LogFields{
			Component: "providerqueue",
			OrderID:   intent.OrderID,
			ChatID:    intent.ChatID,
			Intent:    intent.PaymentID,
			Extra: map[string]any{
				"retry_count": intent.RetryCount,
				"delay_ms":    delay.Milliseconds(),
			},
		})
		if err := w.intentStore.Save(ctx, intent); err != nil {
			return err
		}
		msg.Job.Attempt = intent.RetryCount
		msg.Job.EarliestProcessAt = intent.NextRetryAt
		msg.Job.EnqueuedAt = now
		if err := w.queue.Requeue(ctx, msg.Job, delay); err != nil {
			return err
		}
		return w.queue.Ack(ctx, msg.ID)
	}

	intent.Status = service.PaymentIntentProviderPaid
	intent.LastError = ""
	intent.RetryCount = 0
	intent.NextRetryAt = time.Time{}
	intent.UpdatedAt = now
	if err := w.intentStore.Save(ctx, intent); err != nil {
		return err
	}

	record, found, err := w.workflowStore.Get(ctx, intent.OrderID)
	if err != nil {
		return err
	}
	if found && record.State == service.StatePaymentPendingExternal {
		nextState, applyErr := service.ApplyOrderEvent(record.State, service.EventPaymentConfirmed)
		if applyErr == nil {
			record.State = nextState
			record.UpdatedAt = now
			if _, saveErr := w.workflowStore.SaveIfState(ctx, record, service.StatePaymentPendingExternal); saveErr != nil {
				return saveErr
			}
		}
	}

	if w.bot != nil {
		chat := &telebot.Chat{ID: intent.ChatID}
		msgText := fmt.Sprintf("Payment confirmed for order %s\nRef: %s", intent.OrderID, intent.PaystackReference)
		if _, err := w.bot.Send(chat, msgText); err != nil {
			log.Printf("providerqueue: notify failed for chat %d: %v", intent.ChatID, err)
		}
	}

	return w.queue.Ack(ctx, msg.ID)
}
