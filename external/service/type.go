package service

type OrdersResponse struct {
	RetCode int    `json:"ret_code"`
	RetMsg  string `json:"ret_msg"`
	Result  struct {
		Count int     `json:"count"`
		Items []Order `json:"items"`
	} `json:"result"`
	ExtCode string                 `json:"ext_code"`
	ExtInfo map[string]interface{} `json:"ext_info"`
	TimeNow string                 `json:"time_now"`
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
