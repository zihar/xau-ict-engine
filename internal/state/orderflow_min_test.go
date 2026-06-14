package state

import (
	"testing"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
)

// TestWeeklyOFMinFilter: micro swing-low yang ke-wick HARUS membalik OF di jalur
// unfiltered (perilaku lama), tapi TIDAK di jalur filtered (fix #5) karena
// leg-nya < minLeg → struktur low tetap di swing besar.
func TestWeeklyOFMinFilter(t *testing.T) {
	mk := func(h, l float64) data.Candle {
		return data.Candle{High: h, Low: l, Open: (h + l) / 2, Close: (h + l) / 2}
	}
	weekly := []data.Candle{
		mk(120, 105),
		mk(115, 100), // swing low besar 100
		mk(140, 110),
		mk(160, 130),
		mk(200, 180), // swing high besar 200
		mk(190, 174), // micro swing low 174 (leg 26 dari 200)
		mk(205, 178), // swing high 205
		mk(202, 173), // wick 173 < 174 → unfiltered flip bearish
		mk(200, 180),
		mk(198, 182),
		mk(199, 183),
	}
	for i := range weekly {
		weekly[i].Time = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*i)
	}

	dirU, _, okU := WeeklyOF(weekly)
	if !okU || dirU != detectors.Bearish {
		t.Fatalf("unfiltered: mau bearish (flip di micro LTL), dapat %v ok=%v", dirU, okU)
	}
	dirF, _, okF := WeeklyOFMin(weekly, 50)
	if !okF || dirF != detectors.Bullish {
		t.Fatalf("filtered minLeg=50: mau tetap bullish (micro low diabaikan), dapat %v ok=%v", dirF, okF)
	}
}

// TestWeeklyOFBearConfirm: trigger wick-break bearish HARUS dikonfirmasi close
// minggu berikutnya (OFBearConfirmWeeks=1). Reclaim (close naik) → trigger
// hangus, tetap bullish; close lanjut turun → flip bearish sah.
func TestWeeklyOFBearConfirm(t *testing.T) {
	mk := func(h, l float64) data.Candle {
		return data.Candle{High: h, Low: l, Open: (h + l) / 2, Close: (h + l) / 2}
	}
	base := []data.Candle{
		mk(120, 105),
		mk(115, 100), // swing low besar 100
		mk(140, 110),
		mk(160, 130),
		mk(200, 180), // swing high 200
		mk(190, 174), // swing low 174
		mk(205, 178),
		mk(202, 173), // wick 173 < 174 → trigger bearish (close 187.5)
	}
	reclaim := append(append([]data.Candle{}, base...),
		mk(200, 180), // close 190 > 187.5 → konfirmasi GAGAL (reclaim)
		mk(198, 182),
		mk(199, 183),
	)
	lanjut := append(append([]data.Candle{}, base...),
		mk(190, 170), // close 180 < 187.5 → konfirmasi SAH → bearish
		mk(185, 168),
		mk(184, 167),
	)
	for _, c := range [][]data.Candle{reclaim, lanjut} {
		for i := range c {
			c[i].Time = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*i)
		}
	}

	// Tanpa konfirmasi (perilaku lama): dua-duanya flip bearish di candle trigger.
	if dir, _, ok := WeeklyOF(reclaim); !ok || dir != detectors.Bearish {
		t.Fatalf("tanpa confirm: mau bearish, dapat %v ok=%v", dir, ok)
	}
	// Dengan confirm=1: reclaim → tetap bullish; lanjut turun → bearish.
	if dir, _, _, ok := WeeklyOFFull(reclaim, 0, 1); !ok || dir != detectors.Bullish {
		t.Fatalf("confirm=1 reclaim: mau bullish (trigger hangus), dapat %v ok=%v", dir, ok)
	}
	if dir, _, _, ok := WeeklyOFFull(lanjut, 0, 1); !ok || dir != detectors.Bearish {
		t.Fatalf("confirm=1 lanjut-turun: mau bearish (terkonfirmasi), dapat %v ok=%v", dir, ok)
	}
}

// TestDailyBiasMinFilter: close-break micro swing high TIDAK membalik bias di
// jalur filtered kalau leg pivot < minLeg.
func TestDailyBiasMinFilter(t *testing.T) {
	mk := func(h, l, c float64) data.Candle {
		return data.Candle{High: h, Low: l, Open: (h + l) / 2, Close: c}
	}
	daily := []data.Candle{
		mk(120, 105, 110),
		mk(115, 100, 112), // swing low besar 100
		mk(130, 110, 128),
		mk(150, 125, 148), // swing high besar 150 (leg 50 dari low 100)
		mk(145, 130, 140),
		mk(160, 140, 158), // close 158 > 150 → bias bullish (sah di kedua jalur)
		mk(200, 180, 198), // swing high 200 (ganti pivot high, same-kind)
		mk(190, 174, 176), // micro swing low 174 (leg 26 dari 200 → ke-filter)
		mk(196, 178, 180),
		mk(186, 170, 172), // close 172 < micro low 174 → unfiltered jadi bearish
		mk(184, 171, 175),
		mk(185, 172, 176),
	}
	for i := range daily {
		daily[i].Time = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i)
	}

	dirU, okU := DailyBias(daily)
	if !okU || dirU != detectors.Bearish {
		t.Fatalf("unfiltered: mau bearish (close-break micro low), dapat %v ok=%v", dirU, okU)
	}
	dirF, okF := DailyBiasMin(daily, 50)
	if !okF || dirF != detectors.Bullish {
		t.Fatalf("filtered minLeg=50: mau tetap bullish, dapat %v ok=%v", dirF, okF)
	}
}
