package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/karma-234/mtg-bot/internal/cache"
	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

type TransferVerifier interface {
	VerifyTransfer(reference string) (*service.TransferResponse, error)
}

type paystackEvent struct {
	Event string `json:"event"`
	Data  struct {
		ID           int64  `json:"id"`
		Reference    string `json:"reference"`
		TransferCode string `json:"transfer_code"`
		Status       string `json:"status"`
		Amount       int64  `json:"amount"`
		Currency     string `json:"currency"`
	} `json:"data"`
}

// VerifySignature checks the Paystack webhook HMAC-SHA512 signature.
func VerifySignature(body []byte, signature, secret string) bool {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// NewPaystackWebhookHandler returns an http.HandlerFunc that processes Paystack webhook events.
func NewPaystackWebhookHandler(
	secret string,
	intentStore cache.PaymentIntentStore,
	verifier TransferVerifier,
	providerQueue cache.ProviderMarkQueue,
	bot *telebot.Bot,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if secret != "" && !VerifySignature(body, r.Header.Get("x-paystack-signature"), secret) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var evt paystackEvent
		if err := json.Unmarshal(body, &evt); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		ref := evt.Data.Reference
		eventID := fmt.Sprintf("%s:%s", evt.Event, ref)

		processed, err := intentStore.MarkWebhookProcessed(ctx, eventID, 48*time.Hour)
		if err != nil {
			log.Printf("webhook: idempotency check failed for %s: %v", eventID, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !processed {
			w.WriteHeader(http.StatusOK)
			return
		}

		switch evt.Event {
		case "transfer.success":
			handleTransferSuccess(ctx, ref, intentStore, verifier, providerQueue, bot)
		case "transfer.failed", "transfer.reversed":
			handleTransferFailed(ctx, ref, evt.Event, intentStore, bot)
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleTransferSuccess(
	ctx context.Context,
	ref string,
	intentStore cache.PaymentIntentStore,
	verifier TransferVerifier,
	providerQueue cache.ProviderMarkQueue,
	bot *telebot.Bot,
) {
	intent, found, err := intentStore.GetByReference(ctx, ref)
	if err != nil || !found {
		log.Printf("webhook: transfer.success - intent not found for ref %s (err=%v)", ref, err)
		return
	}

	if verifier != nil {
		verifyResp, verifyErr := verifier.VerifyTransfer(ref)
		if verifyErr != nil {
			intent.LastError = fmt.Sprintf("verify transfer failed: %v", verifyErr)
			intent.UpdatedAt = time.Now().UTC()
			_ = intentStore.Save(ctx, intent)
			log.Printf("webhook: verify transfer failed for ref %s: %v", ref, verifyErr)
			return
		}
		if verifyResp.Data.Reference != intent.PaystackReference {
			intent.LastError = fmt.Sprintf("verify mismatch: reference=%s", verifyResp.Data.Reference)
			intent.UpdatedAt = time.Now().UTC()
			_ = intentStore.Save(ctx, intent)
			log.Printf("webhook: verify reference mismatch for ref %s: got %s", ref, verifyResp.Data.Reference)
			return
		}
		if !strings.EqualFold(verifyResp.Data.Status, "success") {
			intent.LastError = fmt.Sprintf("verify mismatch: status=%s", verifyResp.Data.Status)
			intent.UpdatedAt = time.Now().UTC()
			_ = intentStore.Save(ctx, intent)
			log.Printf("webhook: verify status mismatch for ref %s: got %s", ref, verifyResp.Data.Status)
			return
		}
		if verifyResp.Data.Amount != intent.AmountKobo {
			intent.LastError = fmt.Sprintf("verify mismatch: amount=%d", verifyResp.Data.Amount)
			intent.UpdatedAt = time.Now().UTC()
			_ = intentStore.Save(ctx, intent)
			log.Printf("webhook: verify amount mismatch for ref %s: got %d want %d", ref, verifyResp.Data.Amount, intent.AmountKobo)
			return
		}
		if !strings.EqualFold(verifyResp.Data.Currency, intent.Currency) {
			intent.LastError = fmt.Sprintf("verify mismatch: currency=%s", verifyResp.Data.Currency)
			intent.UpdatedAt = time.Now().UTC()
			_ = intentStore.Save(ctx, intent)
			log.Printf("webhook: verify currency mismatch for ref %s: got %s want %s", ref, verifyResp.Data.Currency, intent.Currency)
			return
		}
	}

	intent.Status = service.PaymentIntentTransferSuccess
	intent.UpdatedAt = time.Now().UTC()
	if err := intentStore.Save(ctx, intent); err != nil {
		log.Printf("webhook: failed to save intent ref=%s: %v", ref, err)
		return
	}

	if providerQueue != nil {
		if err := providerQueue.Enqueue(ctx, cache.ProviderMarkJob{
			OrderID:          intent.OrderID,
			PaymentReference: intent.PaystackReference,
			ChatID:           intent.ChatID,
			Attempt:          0,
		}); err != nil {
			intent.Status = service.PaymentIntentProviderFailed
			intent.LastError = fmt.Sprintf("enqueue provider mark failed: %v", err)
			intent.RetryCount = 1
			intent.NextRetryAt = time.Now().UTC().Add(15 * time.Second)
			intent.UpdatedAt = time.Now().UTC()
			_ = intentStore.Save(ctx, intent)
			log.Printf("webhook: enqueue provider mark failed for order %s: %v", intent.OrderID, err)
			return
		}
	}

	if bot != nil {
		chat := &telebot.Chat{ID: intent.ChatID}
		msg := fmt.Sprintf("Transfer confirmed for order %s\nRef: %s\nAwaiting provider confirmation.", intent.OrderID, ref)
		if _, sendErr := bot.Send(chat, msg); sendErr != nil {
			log.Printf("webhook: transfer success notify failed for chat %d: %v", intent.ChatID, sendErr)
		}
	}
}

func handleTransferFailed(
	ctx context.Context,
	ref, eventType string,
	intentStore cache.PaymentIntentStore,
	bot *telebot.Bot,
) {
	intent, found, err := intentStore.GetByReference(ctx, ref)
	if err != nil || !found {
		log.Printf("webhook: %s - intent not found for ref %s (err=%v)", eventType, ref, err)
		return
	}

	intent.Status = service.PaymentIntentTransferFailed
	intent.LastError = eventType
	intent.UpdatedAt = time.Now().UTC()
	if err := intentStore.Save(ctx, intent); err != nil {
		log.Printf("webhook: failed to save failed intent ref=%s: %v", ref, err)
		return
	}

	if bot != nil {
		chat := &telebot.Chat{ID: intent.ChatID}
		if _, sendErr := bot.Send(chat, fmt.Sprintf("Transfer failed for order %s (ref: %s). Please retry.", intent.OrderID, ref)); sendErr != nil {
			log.Printf("webhook: notify failed for chat %d: %v", intent.ChatID, sendErr)
		}
	}
}
