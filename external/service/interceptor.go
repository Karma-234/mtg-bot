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
	base          http.RoundTripper
	serviceConfig MerchantServiceConfig
}

func (i *RequestInterceptor) InterceptRequest(req *http.Request) (*http.Response, error) {

	log.Println("Sending request:", req.URL)

	now := time.Now().UnixNano() / 1e6

	h := hmac.New(sha256.New, []byte(i.serviceConfig.APISecret))
	h.Write([]byte(strconv.FormatInt(now, 10) + i.serviceConfig.APIKey + "5000"))
	signature := hex.EncodeToString(h.Sum(nil))
	req.Header.Set("X-Service", "vaultmind")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BAPI-API-KEY", i.serviceConfig.APIKey)
	req.Header.Set("X-BAPI-SIGN", signature)
	resp, err := i.base.RoundTrip(req)
	return resp, err
}

func (i *RequestInterceptor) InterceptResponse() {

}
