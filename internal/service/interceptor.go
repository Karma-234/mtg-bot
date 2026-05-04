package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

type RequestInterceptor struct {
	ServiceConfig MerchantServiceConfig
	Base          http.RoundTripper
}

func (i *RequestInterceptor) RoundTrip(req *http.Request) (*http.Response, error) {

	log.Println("Sending request:", req.URL)
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	recvWindow := "5000"

	var bodyBytes []byte

	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	body := string(bodyBytes)

	payload := timestamp + i.ServiceConfig.APIKey + recvWindow + body

	h := hmac.New(sha256.New, []byte(i.ServiceConfig.APISecret))
	h.Write([]byte(payload))
	signature := hex.EncodeToString(h.Sum(nil))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BAPI-API-KEY", i.ServiceConfig.APIKey)
	req.Header.Set("X-BAPI-SIGN", signature)
	req.Header.Set("X-BAPI-TIMESTAMP", timestamp)
	req.Header.Set("X-BAPI-SIGN-TYPE", "2")
	req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)
	resp, err := i.Base.RoundTrip(req)
	return resp, err
}

type PaystackInterceptor struct {
	Base      http.RoundTripper
	SecretKey string
}

func (i *PaystackInterceptor) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+i.SecretKey)
	req.Header.Set("Content-Type", "application/json")
	return i.Base.RoundTrip(req)
}
