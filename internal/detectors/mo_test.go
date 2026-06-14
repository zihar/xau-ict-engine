package detectors

import (
	"testing"
	"time"

	"forex-backtest/internal/data"
)

// mkH1 membuat candle H1 sintetis berurutan mulai `start` (UTC) sebanyak n;
// Open di-encode dari indeks supaya gampang diverifikasi.
func mkH1(start time.Time, n int) []data.Candle {
	out := make([]data.Candle, 0, n)
	for i := 0; i < n; i++ {
		t := start.Add(time.Duration(i) * time.Hour)
		out = append(out, data.Candle{Time: t, Open: 1000 + float64(i)})
	}
	return out
}

func TestMidnightOpenTime(t *testing.T) {
	loc, err := NYLocation()
	if err != nil {
		t.Fatalf("NYLocation: %v", err)
	}
	cases := []struct {
		name string
		t    string // RFC3339 UTC
		want string // RFC3339 UTC dari 00:00 NY trading day yang memuat t
	}{
		// Musim panas (EDT, UTC-4): 00:00 NY = 04:00 UTC.
		{"summer london", "2024-07-10T08:00:00Z", "2024-07-10T04:00:00Z"},
		// Asia (22:00 NY 9 Jul) → trading day mulai 18:00 NY 9 Jul → MO = 00:00 NY 10 Jul.
		{"summer asia", "2024-07-10T02:00:00Z", "2024-07-10T04:00:00Z"},
		// Musim dingin (EST, UTC-5): 00:00 NY = 05:00 UTC.
		{"winter london", "2024-01-10T10:00:00Z", "2024-01-10T05:00:00Z"},
		// Tepat di 00:00 NY → MO trading day yang sama.
		{"exact midnight", "2024-07-10T04:00:00Z", "2024-07-10T04:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := time.Parse(time.RFC3339, tc.t)
			want, _ := time.Parse(time.RFC3339, tc.want)
			got := MidnightOpenTime(at, loc)
			if !got.Equal(want) {
				t.Errorf("MidnightOpenTime(%s) = %s, want %s", tc.t, got.UTC().Format(time.RFC3339), tc.want)
			}
		})
	}
}

func TestMidnightOpenPrice(t *testing.T) {
	loc, err := NYLocation()
	if err != nil {
		t.Fatalf("NYLocation: %v", err)
	}
	utc := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return ts
	}

	// Cache H1 musim panas: mulai 2024-07-09 18:00 UTC, 24 jam.
	// MO trading day (utk t di 10 Jul siang NY) = 2024-07-10T04:00:00Z = indeks 10 → Open 1010.
	h1 := mkH1(utc("2024-07-09T18:00:00Z"), 24)

	t.Run("normal: open candle 00:00 NY", func(t *testing.T) {
		price, moTime, ok := MidnightOpenPrice(h1, utc("2024-07-10T08:00:00Z"), loc)
		if !ok || price != 1010 {
			t.Fatalf("ok=%v price=%.0f, want ok=true price=1010 (moTime=%s)", ok, price, moTime.UTC())
		}
		if !moTime.Equal(utc("2024-07-10T04:00:00Z")) {
			t.Errorf("moTime = %s, want 2024-07-10T04:00:00Z", moTime.UTC().Format(time.RFC3339))
		}
	})

	t.Run("asia: MO belum terbentuk", func(t *testing.T) {
		// 02:00 UTC = 22:00 NY 9 Jul (Asia) — sebelum 00:00 NY 10 Jul.
		if _, _, ok := MidnightOpenPrice(h1, utc("2024-07-10T02:00:00Z"), loc); ok {
			t.Error("ok=true di sesi Asia, want false (MO trading day ini belum ada)")
		}
	})

	t.Run("gap kecil: fallback candle pertama setelah MO (≤2h)", func(t *testing.T) {
		// Buang candle 04:00 UTC (indeks 10) → kandidat berikut 05:00 (Open 1011).
		gap := append(append([]data.Candle{}, h1[:10]...), h1[11:]...)
		price, _, ok := MidnightOpenPrice(gap, utc("2024-07-10T08:00:00Z"), loc)
		if !ok || price != 1011 {
			t.Fatalf("ok=%v price=%.0f, want ok=true price=1011 (fallback +1h)", ok, price)
		}
	})

	t.Run("gap lebar: >2h dari MO → tidak dipakai", func(t *testing.T) {
		// Buang candle 04:00–06:00 UTC (indeks 10..12) → kandidat berikut 07:00 (>+2h).
		gap := append(append([]data.Candle{}, h1[:10]...), h1[13:]...)
		if _, _, ok := MidnightOpenPrice(gap, utc("2024-07-10T08:00:00Z"), loc); ok {
			t.Error("ok=true padahal gap >2h, want false (jangan pakai MO palsu)")
		}
	})

	t.Run("winter DST: 00:00 NY = 05:00 UTC", func(t *testing.T) {
		// Cache mulai 2024-01-09 19:00 UTC; MO = 2024-01-10T05:00:00Z = indeks 10 → 1010.
		h1w := mkH1(utc("2024-01-09T19:00:00Z"), 24)
		price, moTime, ok := MidnightOpenPrice(h1w, utc("2024-01-10T10:00:00Z"), loc)
		if !ok || price != 1010 {
			t.Fatalf("ok=%v price=%.0f, want ok=true price=1010", ok, price)
		}
		if !moTime.Equal(utc("2024-01-10T05:00:00Z")) {
			t.Errorf("moTime = %s, want 2024-01-10T05:00:00Z", moTime.UTC().Format(time.RFC3339))
		}
	})
}
