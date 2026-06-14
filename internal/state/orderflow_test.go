package state

import (
	"os"
	"testing"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
)

func hl(high, low float64, i int) data.Candle {
	mid := (high + low) / 2
	return data.Candle{
		Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * 24 * time.Hour),
		Open: mid, High: high, Low: low, Close: mid, Volume: 1,
	}
}

func candles(pairs [][2]float64) []data.Candle {
	cs := make([]data.Candle, len(pairs))
	for i, p := range pairs {
		cs[i] = hl(p[0], p[1], i)
	}
	return cs
}

// Uptrend bersih (HH/HL) → OF bullish; LTL = swing low terakhir di bawah harga.
func TestWeeklyOF_Bullish(t *testing.T) {
	cs := candles([][2]float64{
		{105, 101}, // 0
		{104, 100}, // 1 STL 100
		{120, 106}, // 2
		{125, 118}, // 3 STH 125
		{122, 110}, // 4 STL 110 (HL)
		{140, 120}, // 5
		{145, 138}, // 6 STH 145
		{142, 130}, // 7 STL 130 (HL)
		{160, 140}, // 8 naik terus
	})
	dir, anc, ok := WeeklyOF(cs)
	if !ok {
		t.Fatal("WeeklyOF gagal")
	}
	if dir != detectors.Bullish {
		t.Errorf("dir = %s, mau bullish", dir)
	}
	if !anc.HasLTL {
		t.Fatal("LTL harus ada di uptrend")
	}
	if anc.LTL.Price >= cs[len(cs)-1].Close {
		t.Errorf("LTL %.1f harus di bawah close %.1f", anc.LTL.Price, cs[len(cs)-1].Close)
	}
}

// Setelah uptrend, LTL di-break ke bawah → flip bearish.
func TestWeeklyOF_FlipBearish(t *testing.T) {
	cs := candles([][2]float64{
		{105, 101}, // 0
		{104, 100}, // 1 STL 100
		{120, 106}, // 2
		{125, 118}, // 3 STH 125
		{122, 110}, // 4 STL 110
		{140, 120}, // 5
		{145, 138}, // 6 STH 145
		{142, 130}, // 7 STL 130 (LTL aktif)
		{135, 128}, // 8
		{132, 95},  // 9 break LTL 130 ke bawah → flip bearish
		{100, 90},  // 10
	})
	dir, _, ok := WeeklyOF(cs)
	if !ok {
		t.Fatal("WeeklyOF gagal")
	}
	if dir != detectors.Bearish {
		t.Errorf("dir = %s, mau bearish (LTL ke-break)", dir)
	}
}

// Daily bias: close break swing high terakhir → bullish.
func TestDailyBias_Bullish(t *testing.T) {
	cs := candles([][2]float64{
		{105, 101}, // 0
		{110, 100}, // 1 STH 110
		{108, 95},  // 2 STL 95
		{107, 98},  // 3
		{115, 100}, // 4 close 107.5 > STH 110? no. high115>110 tapi close mid=107.5<110
		{120, 112}, // 5 close 116 > 110 → bullish (close break)
	})
	dir, ok := DailyBias(cs)
	if !ok {
		t.Fatal("DailyBias gagal")
	}
	if dir != detectors.Bullish {
		t.Errorf("dir = %s, mau bullish", dir)
	}
}

func loadOrSkip(t *testing.T, gran string) []data.Candle {
	t.Helper()
	path := data.CSVPath("../../data", "XAU_USD", gran)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("cache %s belum ada — `go run ./cmd/fetch`", gran)
	}
	cs, err := data.ReadCSV(path)
	if err != nil {
		t.Fatalf("ReadCSV %s: %v", gran, err)
	}
	return cs
}

func TestRealData_OF_Smoke(t *testing.T) {
	weekly := loadOrSkip(t, "W")
	daily := loadOrSkip(t, "D")
	dir, anc, ok := WeeklyOF(weekly)
	if !ok {
		t.Fatal("WeeklyOF gagal di data nyata")
	}
	db, _ := DailyBias(daily)
	last := weekly[len(weekly)-1].Close
	t.Logf("Weekly OF=%s, Daily bias=%s, close=%.2f, LTL=%.2f(%v) LTH=%.2f(%v)",
		dir, db, last, anc.LTL.Price, anc.HasLTL, anc.LTH.Price, anc.HasLTH)
	if anc.HasLTL && anc.LTL.Price >= last {
		t.Errorf("LTL %.2f harus < close %.2f", anc.LTL.Price, last)
	}
	if anc.HasLTH && anc.LTH.Price <= last {
		t.Errorf("LTH %.2f harus > close %.2f", anc.LTH.Price, last)
	}
}
