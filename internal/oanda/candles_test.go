package oanda

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestFetchCandlesPaginationNoDuplicate mereplikasi perilaku OANDA asli pada
// paginasi: request berikutnya (from = lastTime+1s) mengembalikan LAGI candle
// yang MENGANDUNG timestamp itu (= candle terakhir halaman sebelumnya). Tanpa
// guard t.Before(cursor), candle itu masuk dobel tiap batas 5000-candle —
// persis duplikat yang ditemukan di cache (M5 62, H1 5, H4 1).
func TestFetchCandlesPaginationNoDuplicate(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	const step = 5 * time.Minute
	mk := func(i int, complete bool) rawCandle {
		return rawCandle{
			Time:     t0.Add(time.Duration(i) * step).Format(time.RFC3339),
			Volume:   1,
			Complete: complete,
			Mid:      ohlc{O: "1", H: "2", L: "0.5", C: "1.5"},
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
		if err != nil {
			http.Error(w, "from invalid", http.StatusBadRequest)
			return
		}
		var resp candlesResponse
		if !from.After(t0) {
			// Halaman 1: penuh maxCount candle complete (i = 0..maxCount-1).
			for i := 0; i < maxCount; i++ {
				resp.Candles = append(resp.Candles, mk(i, true))
			}
		} else {
			// Halaman 2 (from = lastTime+1s): OANDA mengikutkan candle yang
			// MENGANDUNG from → ulangi candle terakhir halaman 1, lalu 3 candle
			// baru complete + 1 candle berjalan (incomplete).
			for i := maxCount - 1; i < maxCount+3; i++ {
				resp.Candles = append(resp.Candles, mk(i, true))
			}
			resp.Candles = append(resp.Candles, mk(maxCount+3, false))
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &Client{token: "test", baseURL: srv.URL, http: srv.Client()}
	to := t0.Add(time.Duration(maxCount+10) * step)
	got, err := c.FetchCandles("XAU_USD", "M5", t0, to, 18)
	if err != nil {
		t.Fatalf("FetchCandles: %v", err)
	}

	want := maxCount + 3 // halaman 1 penuh + 3 baru (duplikat & incomplete dibuang)
	if len(got) != want {
		t.Fatalf("len = %d, want %d", len(got), want)
	}
	for i := 1; i < len(got); i++ {
		if !got[i].Time.After(got[i-1].Time) {
			t.Fatalf("waktu tidak strictly increasing di idx %d: %s lalu %s",
				i, got[i-1].Time.Format(time.RFC3339), got[i].Time.Format(time.RFC3339))
		}
	}
	if !got[0].Time.Equal(t0) {
		t.Errorf("candle pertama = %s, want %s (candle tepat di from tetap ikut)",
			got[0].Time.Format(time.RFC3339), t0.Format(time.RFC3339))
	}
	if last, wantLast := got[len(got)-1].Time, t0.Add(time.Duration(maxCount+2)*step); !last.Equal(wantLast) {
		t.Errorf("candle terakhir = %s, want %s", last.Format(time.RFC3339), wantLast.Format(time.RFC3339))
	}
}
