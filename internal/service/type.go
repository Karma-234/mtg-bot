package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type BaseResponse struct {
	RetCode int                    `json:"ret_code"`
	RetMsg  string                 `json:"ret_msg"`
	ExtCode string                 `json:"ext_code"`
	ExtInfo map[string]interface{} `json:"ext_info"`
	TimeNow string                 `json:"time_now"`
}

func (b BaseResponse) OK() bool {
	return b.RetCode == 0
}

func (b BaseResponse) Error() error {
	if b.OK() {
		return nil
	}
	return fmt.Errorf("backend error ret_code=%d ret_msg=%s ext_code=%s", b.RetCode, b.RetMsg, b.ExtCode)
}

type OrdersResponse struct {
	BaseResponse
	Result struct {
		Count int     `json:"count"`
		Items []Order `json:"items"`
	} `json:"result"`
}

type Order struct {
	ID                  string `json:"id"`
	Side                int    `json:"side"`
	TokenID             string `json:"tokenId"`
	OrderType           string `json:"orderType"`
	Amount              string `json:"amount"`
	CurrencyID          string `json:"currencyId"`
	Price               string `json:"price"`
	NotifyTokenQuantity string `json:"notifyTokenQuantity"`
	NotifyTokenID       string `json:"notifyTokenId"`
	Fee                 string `json:"fee"`
	TargetNickName      string `json:"targetNickName"`
	TargetUserID        string `json:"targetUserId"`
	Status              int    `json:"status"`
	SelfUnreadMsgCount  string `json:"selfUnreadMsgCount"`
	CreateDate          string `json:"createDate"`
	TransferLastSeconds string `json:"transferLastSeconds"`
	AppealLastSeconds   string `json:"appealLastSeconds"`
	UserID              string `json:"userId"`
	SellerRealName      string `json:"sellerRealName"`
	BuyerRealName       string `json:"buyerRealName"`
	JudgeInfo           struct {
		AutoJudgeUnlockTime string `json:"autoJudgeUnlockTime"`
		DissentResult       string `json:"dissentResult"`
		PreDissent          string `json:"preDissent"`
		PostDissent         string `json:"postDissent"`
	} `json:"judgeInfo"`
	UnreadMsgCount string `json:"unreadMsgCount"`
	Extension      struct {
		IsDelayWithdraw bool   `json:"isDelayWithdraw"`
		DelayTime       string `json:"delayTime"`
		StartTime       string `json:"startTime"`
	} `json:"extension"`
	BulkOrderFlag bool `json:"bulkOrderFlag"`
}

func formatSide(side int) string {
	if side == 0 {
		return "🟢 BUY"
	}
	return "🔴 SELL"
}

func formatStatus(status int) string {
	switch status {
	case 10:
		return "Waiting for Payment"
	case 20:
		return "Waiting for Release"
	case 50:
		return "Completed"
	case 70:
		return "Payment Failed"
	default:
		return fmt.Sprintf("Unknown (%d)", status)
	}
}

func shortID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[:6] + "..."
}

func FormatOrdersMessage(res *OrdersResponse) string {

	if len(res.Result.Items) == 0 {
		return "📭 No active orders found"
	}

	var b strings.Builder

	b.WriteString("📊 *Latest Orders*\n\n")

	for i, o := range res.Result.Items {

		amount, _ := strconv.ParseFloat(o.Amount, 64)
		price, _ := strconv.ParseFloat(o.Price, 64)

		timestamp, _ := strconv.ParseInt(o.CreateDate, 10, 64)
		t := time.UnixMilli(timestamp)

		fmt.Fprintf(&b, "*Order %d*\n", i+1)
		fmt.Fprintf(&b, "🆔 ID: `%s`\n", shortID(o.ID))
		fmt.Fprintf(&b, "📌 Side: %s\n", formatSide(o.Side))
		fmt.Fprintf(&b, "💰 Amount: %.2f %s\n", amount, o.TokenID)
		fmt.Fprintf(&b, "💱 Price: %.3f %s\n", price, o.CurrencyID)
		fmt.Fprintf(&b, "📍 Status: %s\n", formatStatus(o.Status))
		fmt.Fprintf(&b, "⏱ Time: %s\n", t.Format("15:04:05"))

		fmt.Fprint(&b, "\n----------------------\n\n")
	}

	return b.String()
}
