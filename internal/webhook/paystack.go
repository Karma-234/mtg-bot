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
	"slices"
	"strings"
	"time"

	"github.com/karma-234/mtg-bot/internal/cache"
	"github.com/karma-234/mtg-bot/internal/observability"
	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

const (
	paystackIP1 = "152.31.139.75"
	paystackIP2 = "252.49.173.169"
	paystackIP3 = "352.214.14.220"
)

func isValidPaystackIP(remoteAddr string) bool {
	ip := strings.Split(remoteAddr, ":")[0]

	validIPs := []string{paystackIP1, paystackIP2, paystackIP3}
	return slices.Contains(validIPs, ip)
}

type TransferVerifier interface {
	VerifyTransfer(reference string) (*service.TransferResponse, error)
}

type paystackEvent struct {
	Event string `json:"event"`
	Data  struct {
		Reference    string `json:"reference"`
		TransferCode string `json:"transfer_code"`
		Status       string `json:"status"`
		Currency     string `json:"currency"`
		Amount       int64  `json:"amount"`
		ID           int64  `json:"id"`
	} `json:"data"`
}

func webhookEventID(evt paystackEvent) string {
	if evt.Data.ID > 0 {
		return fmt.Sprintf("%s:%d", evt.Event, evt.Data.ID)
	}
	return fmt.Sprintf("%s:%s", evt.Event, evt.Data.Reference)
}

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
		start := time.Now()
		observability.Global().WebhookCount.Inc()

		// Validate request and extract payload
		evt, statusCode, err := validateAndParsWebhookRequest(r, secret)
		if err != nil {
			observability.Global().WebhookErrors.Inc()
			http.Error(w, err.Error(), statusCode)
			observability.Global().WebhookLatencyMS.RecordDuration(start)
			return
		}

		ctx := r.Context()
		ref := evt.Data.Reference
		eventID := webhookEventID(evt)

		processed, err := intentStore.MarkWebhookProcessed(ctx, eventID, 48*time.Hour)
		if err != nil {
			observability.Global().WebhookErrors.Inc()
			observability.Error("webhook_idempotency_check_failed", observability.LogFields{
				Component: "webhook",
				Extra: map[string]interface{}{
					"event_id": eventID,
				},
				Error: err,
			})
			http.Error(w, "internal error", http.StatusInternalServerError)
			observability.Global().WebhookLatencyMS.RecordDuration(start)
			return
		}
		if !processed {
			// Already processed this event (idempotent); skip and return OK
			observability.Global().WebhookLatencyMS.RecordDuration(start)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Dispatch event to appropriate handler
		switch evt.Event {
		case "transfer.success":
			handleTransferSuccess(ctx, ref, intentStore, verifier, providerQueue, bot)
		case "transfer.failed", "transfer.reversed":
			handleTransferFailed(ctx, ref, evt.Event, intentStore, bot)
		}

		observability.Global().WebhookLatencyMS.RecordDuration(start)
		w.WriteHeader(http.StatusOK)
	}
}

// validateAndParsWebhookRequest validates HTTP method, IP, signature, and parses JSON payload.
// Returns parsed event, HTTP status code, and error (if any).
func validateAndParsWebhookRequest(r *http.Request, secret string) (paystackEvent, int, error) {
	// Method validation
	if r.Method != http.MethodPost {
		return paystackEvent{}, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed")
	}

	// IP whitelist validation
	if !isValidPaystackIP(r.RemoteAddr) {
		return paystackEvent{}, http.StatusForbidden, fmt.Errorf("forbidden")
	}

	// Body read and signature verification
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return paystackEvent{}, http.StatusBadRequest, fmt.Errorf("failed to read body")
	}
	defer r.Body.Close()

	if secret != "" && !VerifySignature(body, r.Header.Get("x-paystack-signature"), secret) {
		return paystackEvent{}, http.StatusUnauthorized, fmt.Errorf("invalid signature")
	}

	// JSON parsing
	var evt paystackEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		return paystackEvent{}, http.StatusBadRequest, fmt.Errorf("invalid payload")
	}

	return evt, http.StatusOK, nil
}

// handleTransferSuccess processes transfer.success webhooks.
// Updates payment intent status, enqueues provider mark job for async completion.
// On verification failure, records error and stores state (no retry by polling).
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

// handleTransferFailed processes transfer.failed and transfer.reversed webhooks.
// Updates payment intent to failed state and notifies user.
// Guards against late transfer.failed events arriving after transfer.success (e.g., network delay).
// Out-of-order events are ignored if intent is already settled (TRANSFER_SUCCESS or PROVIDER_PAID).
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
	if eventType == "transfer.failed" && (intent.Status == service.PaymentIntentTransferSuccess || intent.Status == service.PaymentIntentProviderPaid) {
		log.Printf("webhook: ignored late transfer.failed for settled intent ref=%s status=%s", ref, intent.Status)
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
