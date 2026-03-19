package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func PostJSON(client *http.Client, url string, payload any) (*http.Response, error) {

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	return client.Do(req)
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
