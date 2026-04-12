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
	PaymentID         string              `json:"paymentId"`
	ChatID            int64               `json:"chatId"`
	OrderID           string              `json:"orderId"`
	Provider          string              `json:"provider"`
	PaystackReference string              `json:"paystackReference"`
	TransferCode      string              `json:"transferCode,omitempty"`
	AmountKobo        int64               `json:"amountKobo"`
	Currency          string              `json:"currency"`
	Status            PaymentIntentStatus `json:"status"`
	LastError         string              `json:"lastError,omitempty"`
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
	NextRetryAt       time.Time           `json:"nextRetryAt,omitempty"`
}
