package service

type OrderQueryRequest struct {
	Page      int     `json:"page"`
	PageSize  int     `json:"size"`
	Status    *int    `json:"status,omitempty"`
	BeginTime *string `json:"beginTime,omitempty"`
	EndTime   *string `json:"endTime,omitempty"`
	TokenID   *string `json:"tokenId,omitempty"`
	Side      *int    `json:"side,omitempty"`
}

type SingleOrderQueryRequest struct {
	OrderID string `json:"orderId"`
}

type MarkOrderPaidRequest struct {
	OrderID     string `json:"orderId"`
	PaymentType string `json:"paymentType"`
	PaymentID   string `json:"paymentId"`
}
