package service

import "fmt"

type BasePaystackResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
}

func (r *BasePaystackResponse) OK() bool {
	return r.Status
}
func (r *BasePaystackResponse) Error() error {
	if r.OK() {
		return nil
	}
	return fmt.Errorf("Paystack Error: %s", r.Message)
}

type BalanceEntry struct {
	Currency string `json:"currency"`
	Balance  int64  `json:"balance"`
}

type PaystackBalanceResponse struct {
	BasePaystackResponse
	Data []BalanceEntry `json:"data"`
}
type BankEntry struct {
	Name     string `json:"name"`
	Code     string `json:"code"`
	Country  string `json:"country"`
	Currency string `json:"currency"`
	Active   bool   `json:"active"`
}

type ListBanksResponse struct {
	BasePaystackResponse
	Data []BankEntry `json:"data"`
}

type ResolveAccountData struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
}
type ResolveAccountResponse struct {
	BasePaystackResponse
	Data ResolveAccountData `json:"data"`
}

type CreateRecipientRequest struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	AccountNumber string `json:"account_number"`
	BankCode      string `json:"bank_code"`
	Currency      string `json:"currency"`
}
type RecipientDetails struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	BankCode      string `json:"bank_code"`
	BankName      string `json:"bank_name"`
}

type RecipientData struct {
	RecipientCode string           `json:"recipient_code"`
	Name          string           `json:"name"`
	Type          string           `json:"type"`
	Currency      string           `json:"currency"`
	Details       RecipientDetails `json:"details"`
	Active        bool             `json:"active"`
	ID            int64            `json:"id"`
}

type CreateRecipientResponse struct {
	BasePaystackResponse
	Data RecipientData `json:"data"`
}

type InitiateTransferRequest struct {
	Source    string `json:"source"`    // always "balance"
	Amount    int64  `json:"amount"`    // kobo
	Recipient string `json:"recipient"` // recipient_code e.g. RCP_xxx
	Reference string `json:"reference"` // 16-50 chars, lowercase+digits+-_
	Reason    string `json:"reason,omitempty"`
	Currency  string `json:"currency,omitempty"` // defaults NGN
}

type TransferData struct {
	ID           int64  `json:"id"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
	TransferCode string `json:"transfer_code"`
	Reference    string `json:"reference"`
	Status       string `json:"status"` // "pending", "success", "failed", "otp"
	Reason       string `json:"reason"`
	CreatedAt    string `json:"createdAt"`
}

type TransferResponse struct {
	BasePaystackResponse
	Data TransferData `json:"data"`
}

type InitTransactionRequest struct {
	Email    string `json:"email"`
	Amount   int64  `json:"amount"` // kobo
	Callback string `json:"callback_url,omitempty"`
}

type InitTransactionData struct {
	AuthorizationURL string `json:"authorization_url"`
	AccessCode       string `json:"access_code"`
	Reference        string `json:"reference"`
}

type InitTransactionResponse struct {
	BasePaystackResponse
	Data InitTransactionData `json:"data"`
}
