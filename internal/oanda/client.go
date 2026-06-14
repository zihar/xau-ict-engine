// Package oanda adalah client read-only minimal untuk OANDA v20 REST API.
// Cuma dipakai untuk verifikasi token + tarik candle historis. Tidak ada
// eksekusi order.
package oanda

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Base URL per environment OANDA.
const (
	basePractice = "https://api-fxpractice.oanda.com"
	baseLive     = "https://api-fxtrade.oanda.com"
)

// Client memanggil OANDA v20 REST API.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// New membuat Client. env "live" memakai endpoint live; selain itu (termasuk
// "practice"/"demo"/kosong) memakai endpoint demo — default aman.
func New(token, env string) *Client {
	base := basePractice
	if env == "live" {
		base = baseLive
	}
	return &Client{
		token:   token,
		baseURL: base,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// get melakukan GET ke path relatif dengan query, mengembalikan body mentah.
// Error JSON OANDA (errorMessage) di-decode jadi error Go yang informatif.
func (c *Client) get(path string, query url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Datetime-Format", "RFC3339")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var ae apiError
		if json.Unmarshal(body, &ae) == nil && ae.ErrorMessage != "" {
			return nil, fmt.Errorf("OANDA %d: %s", resp.StatusCode, ae.ErrorMessage)
		}
		return nil, fmt.Errorf("OANDA %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}
