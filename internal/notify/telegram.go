// Package notify mengirim alert (Telegram) + menyimpan state dedup antar-run.
// Std-lib only (net/http + encoding/json), tidak ada SDK eksternal.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Notifier = abstraksi pengirim pesan (memudahkan test / pengganti channel).
type Notifier interface {
	SendMessage(text string) error
}

// Telegram mengirim pesan ke Telegram Bot API (read-only ke market; di sini
// hanya kirim notifikasi + terima perintah bot). token & chatID dari env
// (jangan hard-code). api injectable untuk test (default api.telegram.org).
type Telegram struct {
	token  string
	chatID string
	http   *http.Client
	api    string
}

// NewTelegram membuat Telegram notifier dengan timeout HTTP ~20s.
func NewTelegram(token, chatID string) *Telegram {
	return &Telegram{
		token:  token,
		chatID: chatID,
		http:   &http.Client{Timeout: 20 * time.Second},
		api:    "https://api.telegram.org",
	}
}

// Update = satu pesan masuk dari getUpdates — cukup field yang dipakai
// perintah bot (mis. "/watchlist").
type Update struct {
	ID     int64  // update_id (offset berikutnya = ID+1)
	ChatID string // id chat pengirim (numerik, sebagai string)
	Text   string
}

// GetUpdates long-poll pesan masuk mulai `offset` (= update_id terakhir + 1;
// 0 = dari backlog paling lama). timeoutSec = long-poll sisi server — HARUS
// lebih kecil dari timeout http client (~20s), pakai <=15. Return urut update_id.
func (t *Telegram) GetUpdates(offset int64, timeoutSec int) ([]Update, error) {
	u := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=%d", t.api, t.token, offset, timeoutSec)
	resp, err := t.http.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram getUpdates %d: %s", resp.StatusCode, string(body))
	}
	var r struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  struct {
				Text string `json:"text"`
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if !r.OK {
		return nil, fmt.Errorf("telegram getUpdates: ok=false (%s)", string(body))
	}
	out := make([]Update, 0, len(r.Result))
	for _, u := range r.Result {
		out = append(out, Update{
			ID:     u.UpdateID,
			ChatID: strconv.FormatInt(u.Message.Chat.ID, 10),
			Text:   u.Message.Text,
		})
	}
	return out, nil
}

// SendMessage mem-POST teks (Markdown) ke endpoint sendMessage. Non-2xx atau
// error transport → return error.
func (t *Telegram) SendMessage(text string) error {
	payload := map[string]string{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	u := t.api + "/bot" + t.token + "/sendMessage"
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
