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

type ChatMessageRequest struct {
	SessionID   int64  `json:"sessionId"`
	Message     string `json:"message"`
	ContentType string `json:"contentType"`
}

type ChatSessionQueryRequest struct {
	PageSize   int     `json:"size"`
	SessionID  *string `json:"sessionId,omitempty"`
	UserMaskID *string `json:"userMaskId,omitempty"`
	LastID     *string `json:"lastId,omitempty"`
}
