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
	return fmt.Errorf("Backend error ret_code=%d ret_msg=%s ext_code=%s", b.RetCode, b.RetMsg, b.ExtCode)
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
		fmt.Fprintf(&b, "💼 Tokens: %s %s\n", o.NotifyTokenQuantity, o.NotifyTokenID)
		fmt.Fprintf(&b, "💰 Amount: %.2f %s\n", amount, o.TokenID)
		fmt.Fprintf(&b, "💱 Rate: %.3f %s\n", price, o.CurrencyID)
		fmt.Fprintf(&b, "📍 Status: %s\n", formatStatus(o.Status))
		fmt.Fprintf(&b, "⏱ Time: %s\n", t.Format("15:04:05"))

		fmt.Fprint(&b, "\n----------------------\n\n")
	}

	return b.String()
}

type OrderDetailResponse struct {
	BaseResponse
	Result OrderDetail `json:"result"`
}

type OrderDetail struct {
	ID                             string         `json:"id"`
	Side                           int            `json:"side"`
	ItemID                         string         `json:"itemId"`
	AccountID                      string         `json:"accountId"`
	UserID                         string         `json:"userId"`
	NickName                       string         `json:"nickName"`
	MakerUserID                    string         `json:"makerUserId"`
	TargetAccountID                string         `json:"targetAccountId"`
	TargetUserID                   string         `json:"targetUserId"`
	TargetNickName                 string         `json:"targetNickName"`
	TargetFirstName                string         `json:"targetFirstName"`
	TargetSecondName               string         `json:"targetSecondName"`
	TargetUserAuthStatus           int            `json:"targetUserAuthStatus"`
	TargetConnectInformation       string         `json:"targetConnectInformation"`
	PayerRealName                  string         `json:"payerRealName"`
	SellerRealName                 string         `json:"sellerRealName"`
	BuyerRealName                  string         `json:"buyerRealName"`
	TokenID                        string         `json:"tokenId"`
	TokenName                      string         `json:"tokenName"`
	CurrencyID                     string         `json:"currencyId"`
	Price                          string         `json:"price"`
	Quantity                       string         `json:"quantity"`
	Amount                         string         `json:"amount"`
	PayCode                        string         `json:"payCode"`
	PaymentType                    int            `json:"paymentType"`
	TransferDate                   string         `json:"transferDate"`
	Status                         int            `json:"status"`
	CreateDate                     string         `json:"createDate"`
	PaymentTermList                []PaymentTerm  `json:"paymentTermList"`
	Remark                         string         `json:"remark"`
	TransferLastSeconds            string         `json:"transferLastSeconds"`
	RecentOrderNum                 int            `json:"recentOrderNum"`
	RecentExecuteRate              int            `json:"recentExecuteRate"`
	AppealLastSeconds              string         `json:"appealLastSeconds"`
	AppealContent                  string         `json:"appealContent"`
	AppealType                     int            `json:"appealType"`
	AppealNickName                 string         `json:"appealNickName"`
	CanAppeal                      string         `json:"canAppeal"`
	TotalAppealTimes               string         `json:"totalAppealTimes"`
	AppealedTimes                  string         `json:"appealedTimes"`
	PaymentTermResult              PaymentTerm    `json:"paymentTermResult"`
	OrderFinishMinute              int            `json:"orderFinishMinute"`
	ConfirmedPayTerm               PaymentTerm    `json:"confirmedPayTerm"`
	MakerFee                       string         `json:"makerFee"`
	TakerFee                       string         `json:"takerFee"`
	Fee                            string         `json:"fee"`
	ShowContact                    bool           `json:"showContact"`
	ContactInfo                    []any          `json:"contactInfo"`
	TokenBalance                   string         `json:"tokenBalance"`
	FiatBalance                    string         `json:"fiatBalance"`
	UnreadMsgCount                 string         `json:"unreadMsgCount"`
	UpdateDate                     string         `json:"updateDate"`
	Extension                      OrderExtension `json:"extension"`
	SelfUnreadMsgCount             string         `json:"selfUnreadMsgCount"`
	JudgeType                      string         `json:"judgeType"`
	CanReport                      bool           `json:"canReport"`
	CanReportDisagree              bool           `json:"canReportDisagree"`
	CanReportType                  []string       `json:"canReportType"`
	CanReportDisagreeType          []any          `json:"canReportDisagreeType"`
	AppraiseStatus                 string         `json:"appraiseStatus"`
	AppraiseInfo                   AppraiseInfo   `json:"appraiseInfo"`
	CanReportDisagreeTypes         []any          `json:"canReportDisagreeTypes"`
	CanReportTypes                 []string       `json:"canReportTypes"`
	OrderType                      string         `json:"orderType"`
	MiddleToken                    string         `json:"middleToken"`
	BeforePrice                    string         `json:"beforePrice"`
	BeforeQuantity                 string         `json:"beforeQuantity"`
	BeforeToken                    string         `json:"beforeToken"`
	Alternative                    string         `json:"alternative"`
	AppealUserID                   string         `json:"appealUserId"`
	NotifyTokenID                  string         `json:"notifyTokenId"`
	NotifyTokenQuantity            string         `json:"notifyTokenQuantity"`
	CancelResponsible              string         `json:"cancelResponsible"`
	ChainType                      string         `json:"chainType"`
	ChainAddress                   string         `json:"chainAddress"`
	TradeHashCode                  string         `json:"tradeHashCode"`
	EstimatedGasFee                string         `json:"estimatedGasFee"`
	GasFeeTokenID                  string         `json:"gasFeeTokenId"`
	TradingFeeTokenID              string         `json:"tradingFeeTokenId"`
	OnChainInfo                    string         `json:"onChainInfo"`
	TransactionID                  string         `json:"transactionId"`
	DisplayRefund                  string         `json:"displayRefund"`
	ChainWithdrawLastSeconds       string         `json:"chainWithdrawLastSeconds"`
	ChainTransferLastSeconds       string         `json:"chainTransferLastSeconds"`
	OrderSource                    string         `json:"orderSource"`
	CancelReason                   string         `json:"cancelReason"`
	SellerCancelExamineRemainTime  string         `json:"sellerCancelExamineRemainTime"`
	NeedSellerExamineCancel        bool           `json:"needSellerExamineCancel"`
	CouponCurrencyAmount           string         `json:"couponCurrencyAmount"`
	TotalCurrencyAmount            string         `json:"totalCurrencyAmount"`
	UsedCoupon                     bool           `json:"usedCoupon"`
	CouponTokenID                  string         `json:"couponTokenId"`
	CouponQuantity                 string         `json:"couponQuantity"`
	CompletedOrderAppealCount      int            `json:"completedOrderAppealCount"`
	TotalCompletedOrderAppealCount int            `json:"totalCompletedOrderAppealCount"`
	RealOrderStatus                int            `json:"realOrderStatus"`
	AppealVersion                  int            `json:"appealVersion"`
	JudgeInfo                      JudgeInfo      `json:"judgeInfo"`
	HelpType                       string         `json:"helpType"`
	AppealFlowStatus               string         `json:"appealFlowStatus"`
	AppealSubStatus                string         `json:"appealSubStatus"`
	BulkOrderFlag                  bool           `json:"bulkOrderFlag"`
	TargetUserType                 string         `json:"targetUserType"`
	TargetUserDisplays             []string       `json:"targetUserDisplays"`
	AppealProcessChangeFlag        bool           `json:"appealProcessChangeFlag"`
	AppealNegotiationNode          int            `json:"appealNegotiationNode"`
}

type PaymentTerm struct {
	ID                     string          `json:"id"`
	RealName               string          `json:"realName"`
	PaymentType            int             `json:"paymentType"`
	BankName               string          `json:"bankName"`
	BranchName             string          `json:"branchName"`
	AccountNo              string          `json:"accountNo"`
	Qrcode                 string          `json:"qrcode"`
	Visible                int             `json:"visible"`
	PayMessage             string          `json:"payMessage"`
	FirstName              string          `json:"firstName"`
	LastName               string          `json:"lastName"`
	SecondLastName         string          `json:"secondLastName"`
	Clabe                  string          `json:"clabe"`
	DebitCardNumber        string          `json:"debitCardNumber"`
	Mobile                 string          `json:"mobile"`
	BusinessName           string          `json:"businessName"`
	Concept                string          `json:"concept"`
	Online                 string          `json:"online"`
	PaymentExt1            string          `json:"paymentExt1"`
	PaymentExt2            string          `json:"paymentExt2"`
	PaymentExt3            string          `json:"paymentExt3"`
	PaymentExt4            string          `json:"paymentExt4"`
	PaymentExt5            string          `json:"paymentExt5"`
	PaymentExt6            string          `json:"paymentExt6"`
	PaymentTemplateVersion int             `json:"paymentTemplateVersion"`
	PaymentConfigVo        PaymentConfigVo `json:"paymentConfigVo"`
	RuPaymentPrompt        bool            `json:"ruPaymentPrompt"`
}

type PaymentConfigVo struct {
	Items       []any  `json:"items"`
	PaymentType string `json:"paymentType"`
	PaymentName string `json:"paymentName"`
	AddTips     string `json:"addTips"`
	ItemTips    string `json:"itemTips"`
	Online      int    `json:"online"`
	CheckType   int    `json:"checkType"`
	Sort        int    `json:"sort"`
}

type OrderExtension struct {
	DelayTime       string `json:"delayTime"`
	StartTime       string `json:"startTime"`
	IsDelayWithdraw bool   `json:"isDelayWithdraw"`
}

type AppraiseInfo struct {
	Anonymous       string `json:"anonymous"`
	AppraiseContent string `json:"appraiseContent"`
	AppraiseID      string `json:"appraiseId"`
	AppraiseType    string `json:"appraiseType"`
	ModifyFlag      string `json:"modifyFlag"`
	UpdateDate      string `json:"updateDate"`
}

type JudgeInfo struct {
	AutoJudgeUnlockTime string `json:"autoJudgeUnlockTime"`
	DissentResult       string `json:"dissentResult"`
	PreDissent          string `json:"preDissent"`
	PostDissent         string `json:"postDissent"`
}

type ChatSessionListResponse struct {
	BaseResponse
	Result ChatSessionListResult `json:"result"`
}

type ChatSessionListResult struct {
	ChatSession []ChatSession `json:"chatSession"`
}

type ChatSession struct {
	SessionName string `json:"sessionName"`
	SessionID   string `json:"sessionId"`
	Type        string `json:"type"`
	ID          string `json:"id"`
}
