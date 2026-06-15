package engine

import (
	"testing"
	"time"

	"forex-backtest/internal/data"
)

// TestClassifyWeeklyQT_MondayClosesMonday mengunci definisi candle Senin (D.4,
// konvensi NY-close/ICT): candle Senin = BUKA Minggu 18:00 → TUTUP Senin 18:00 NY.
// Regresi guard untuk off-by-one yang sempat ada (def keliru "tutup Selasa 18:00",
// dibatalkan 2026-06-09): pada Senin 19:00 NY (setelah Senin 18:00) weekly QT harus
// SUDAH final (ok=true); sebelum Senin 18:00 masih pending (ok=false).
func TestClassifyWeeklyQT_MondayClosesMonday(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load loc: %v", err)
	}
	// 4H candles tiap 4 jam (UTC), dari minggu sebelumnya s/d Senin 14:00 EDT (candle
	// terakhir yang close SEBELUM Senin 18:00). EDT = UTC-4. Cukup banyak utk ATR(14).
	var h4 []data.Candle
	start := time.Date(2026, 5, 31, 22, 0, 0, 0, time.UTC) // ~Minggu 18:00 EDT minggu lalu
	end := time.Date(2026, 6, 8, 18, 0, 0, 0, time.UTC)    // Senin 8 Jun 14:00 EDT
	px := 4300.0
	for tcur := start; !tcur.After(end); tcur = tcur.Add(4 * time.Hour) {
		h4 = append(h4, data.Candle{Time: tcur, Open: px, High: px + 5, Low: px - 5, Close: px + 2, Volume: 1000})
		px += 1
	}

	// now = Senin 8 Jun 19:00 EDT (23:00 UTC) — candle Senin (tutup 18:00 EDT) SUDAH close.
	nowFinal := time.Date(2026, 6, 8, 23, 0, 0, 0, time.UTC)
	if _, ok := classifyWeeklyQT(h4, nowFinal, loc, 14, 1.5, 5); !ok {
		t.Errorf("Senin 19:00 NY: weekly QT harus final (ok=true), dapat ok=false — off-by-one candle Senin?")
	}

	// now = Senin 8 Jun 12:00 EDT (16:00 UTC) — candle Senin BELUM close (< Senin 18:00).
	nowPending := time.Date(2026, 6, 8, 16, 0, 0, 0, time.UTC)
	if _, ok := classifyWeeklyQT(h4, nowPending, loc, 14, 1.5, 5); ok {
		t.Errorf("Senin 12:00 NY: weekly QT harus pending (ok=false), dapat ok=true")
	}
}
