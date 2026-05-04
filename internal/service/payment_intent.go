package service

import "time"

type PaymentIntentStatus string

const (
	PaymentIntentInitiated        PaymentIntentStatus = "INITIATED"
	PaymentIntentTransferPending  PaymentIntentStatus = "TRANSFER_PENDING"
	PaymentIntentTransferSuccess  PaymentIntentStatus = "TRANSFER_SUCCESS"
	PaymentIntentTransferFailed   PaymentIntentStatus = "TRANSFER_FAILED"
	PaymentIntentInsufficientFund PaymentIntentStatus = "INSUFFICIENT_FUNDS"
	PaymentIntentProviderPaid     PaymentIntentStatus = "PROVIDER_MARKED_PAID"
	PaymentIntentProviderFailed   PaymentIntentStatus = "PROVIDER_MARK_FAILED"
)

type PaymentIntentRecord struct {
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
	NextRetryAt       time.Time           `json:"nextRetryAt,omitempty"`
	PaymentID         string              `json:"paymentId"`
	OrderID           string              `json:"orderId"`
	Provider          string              `json:"provider"`
	PaystackReference string              `json:"paystackReference"`
	TransferCode      string              `json:"transferCode,omitempty"`
	Currency          string              `json:"currency"`
	LastError         string              `json:"lastError,omitempty"`
	Status            PaymentIntentStatus `json:"status"`
	ChatID            int64               `json:"chatId"`
	AmountKobo        int64               `json:"amountKobo"`
	RetryCount        int                 `json:"retryCount,omitempty"`
}
