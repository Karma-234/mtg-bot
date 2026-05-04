package service

const (
	GETALLORDERS         = "/v5/p2p/order/simplifyList"
	GETORDERDETAIL       = "/v5/p2p/order/info"
	GETPENDINGORDERS     = "/v5/p2p/order/pending/simplifyList"
	MARKORDERASPAID      = "/v5/p2p/order/pay"
	SENDCHATMESSAGE      = "/v5/p2p/chat/message/send"
	GETCHATSESSION       = "/v5/p2p/chat/session/list"
	GETORDERCHATMESSAGE  = "/v5/p2p/order/message/listpage"
	SENDORDERCHATMESSAGE = "/v5/p2p/order/message/send"
)

const (
	PaystackBaseURL         = "https://api.paystack.co"
	PaystackResolveAccount  = "/bank/resolve"
	PaystackListBanks       = "/bank"
	PaystackBalance         = "/balance"
	PaystackCreateRecipient = "/transferrecipient"
	PaystackTransfer        = "/transfer"
	PaystackVerifyTransfer  = "/transfer/verify"
	PaystackInitTransaction = "/transaction/initialize"
)
