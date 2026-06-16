package engine

import (
	"testing"
	"time"

	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/news"
)

// TestBuildNewsSkipSet: hanya rilis USD high-impact yang jam-NY-nya = kandidat
// (SkipEntryHoursNY="8") yang masuk set, di-key pada awal-jam UTC candle H1.
func TestBuildNewsSkipSet(t *testing.T) {
	loc, err := detectors.NYLocation()
	if err != nil {
		t.Fatalf("NYLocation: %v", err)
	}
	// Semua di Juni 2026 = EDT (UTC-4): 12:30Z = 08:30 NY, 14:00Z = 10:00 NY.
	events := []news.Event{
		{Title: "CPI m/m", Country: "USD", Impact: "High", Time: time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)}, // → jam-NY 8, MASUK
		{Title: "ISM", Country: "USD", Impact: "High", Time: time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)},      // → jam-NY 10, TIDAK
		{Title: "ECB", Country: "EUR", Impact: "High", Time: time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)},     // bukan USD, TIDAK
		{Title: "Misc", Country: "USD", Impact: "Low", Time: time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)},     // bukan High, TIDAK
	}
	set := BuildNewsSkipSet(events, "8", loc)
	if len(set) != 1 {
		t.Fatalf("set ukuran %d, mau 1 (cuma CPI 08:30 USD/High)", len(set))
	}
	// Key harus awal-jam UTC candle H1 (12:00Z) — sama persis dgn tf.H1[i].Time.
	want := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	if !set[want] {
		t.Errorf("set tak memuat key %s (key candle H1 08:00 NY); isi=%v", want, set)
	}
	// Sanity: jam non-kandidat & non-USD/High tak boleh ada.
	if set[time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)] {
		t.Errorf("set keliru memuat 14:00Z (jam-NY 10, di luar kandidat \"8\")")
	}
}

// TestBuildNewsSkipSet_EST: di musim dingin (EST, UTC-5) rilis 08:30 NY = 13:30Z
// → candle 13:00Z; pemetaan jam-NY tetap benar via loc.
func TestBuildNewsSkipSet_EST(t *testing.T) {
	loc, err := detectors.NYLocation()
	if err != nil {
		t.Fatalf("NYLocation: %v", err)
	}
	events := []news.Event{
		{Title: "NFP", Country: "USD", Impact: "High", Time: time.Date(2026, 1, 9, 13, 30, 0, 0, time.UTC)}, // 08:30 EST
	}
	set := BuildNewsSkipSet(events, "8", loc)
	want := time.Date(2026, 1, 9, 13, 0, 0, 0, time.UTC)
	if len(set) != 1 || !set[want] {
		t.Errorf("EST: set=%v, mau key %s", set, want)
	}
}

// TestBuildNewsSkipSet_Empty: tanpa event relevan → set kosong (bukan nil-panic).
func TestBuildNewsSkipSet_Empty(t *testing.T) {
	loc, _ := detectors.NYLocation()
	set := BuildNewsSkipSet(nil, "8", loc)
	if len(set) != 0 {
		t.Errorf("set harus kosong, dapat %v", set)
	}
}
