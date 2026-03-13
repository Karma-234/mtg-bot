package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strconv"
	"time"
)

type RequestInterceptor struct {
	Base          http.RoundTripper
	ServiceConfig MerchantServiceConfig
}

func (i *RequestInterceptor) RoundTrip(req *http.Request) (*http.Response, error) {

	log.Println("Sending request:", req.URL)

	now := time.Now().UnixNano() / 1e6

	h := hmac.New(sha256.New, []byte(i.ServiceConfig.APISecret))
	h.Write([]byte(strconv.FormatInt(now, 10) + i.ServiceConfig.APIKey + "5000"))
	signature := hex.EncodeToString(h.Sum(nil))
	req.Header.Set("X-Service", "vaultmind")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BAPI-API-KEY", i.ServiceConfig.APIKey)
	req.Header.Set("X-BAPI-SIGN", signature)
	resp, err := i.Base.RoundTrip(req)
	return resp, err
}
