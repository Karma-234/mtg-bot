package service

import (
	"bytes"
	"encoding/json"
	"net/http"
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
