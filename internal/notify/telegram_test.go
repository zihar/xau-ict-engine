package notify

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetUpdatesParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "7" {
			t.Errorf("offset = %s, want 7", r.URL.Query().Get("offset"))
		}
		fmt.Fprint(w, `{"ok":true,"result":[
			{"update_id":7,"message":{"text":"/watchlist","chat":{"id":12345}}},
			{"update_id":8,"message":{"text":"halo","chat":{"id":67890}}}
		]}`)
	}))
	defer srv.Close()

	tg := &Telegram{token: "tok", chatID: "12345", http: srv.Client(), api: srv.URL}
	tg.http.Timeout = 5 * time.Second
	ups, err := tg.GetUpdates(7, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(ups) != 2 {
		t.Fatalf("len = %d, want 2", len(ups))
	}
	if ups[0].ID != 7 || ups[0].Text != "/watchlist" || ups[0].ChatID != "12345" {
		t.Errorf("update[0] = %+v, want ID=7 text=/watchlist chat=12345", ups[0])
	}
	if ups[1].ChatID != "67890" {
		t.Errorf("update[1].ChatID = %s, want 67890", ups[1].ChatID)
	}
}

func TestGetUpdatesNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":false,"description":"Unauthorized"}`)
	}))
	defer srv.Close()
	tg := &Telegram{token: "tok", http: srv.Client(), api: srv.URL}
	if _, err := tg.GetUpdates(0, 0); err == nil {
		t.Fatal("ok=false harus jadi error")
	}
}
