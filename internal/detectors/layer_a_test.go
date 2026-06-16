package detectors

import (
	"os"
	"testing"
	"time"

	"xau-ict-engine/internal/data"
)

// hl membuat candle dari (high, low); open/close di tengah (tak dipakai deteksi swing).
func hl(high, low float64, i int) data.Candle {
	mid := (high + low) / 2
	return data.Candle{
		Time:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour),
		Open:   mid,
		High:   high,
		Low:    low,
		Close:  mid,
		Volume: 1,
	}
}

func candlesFromHL(pairs [][2]float64) []data.Candle {
	cs := make([]data.Candle, len(pairs))
	for i, p := range pairs {
		cs[i] = hl(p[0], p[1], i)
	}
	return cs
}

// canonHL = contoh kanonik user (memory Q1): impulse 4560->4640, retrace ke
// 4600 (=0.5 → discount, buy), lalu lanjut 4700.
var canonHL = [][2]float64{
	{4575, 4565}, // 0
	{4570, 4560}, // 1  STL 4560
	{4620, 4580}, // 2
	{4640, 4610}, // 3  STH 4640
	{4630, 4600}, // 4  STL 4600
	{4680, 4650}, // 5
	{4700, 4670}, // 6  STH 4700
	{4690, 4660}, // 7  STL 4660
	{4685, 4665}, // 8
}

func TestDetectSwings(t *testing.T) {
	sw := DetectSwings(candlesFromHL(canonHL))
	want := []Swing{
		{Index: 1, Price: 4560, Kind: SwingLow},
		{Index: 3, Price: 4640, Kind: SwingHigh},
		{Index: 4, Price: 4600, Kind: SwingLow},
		{Index: 6, Price: 4700, Kind: SwingHigh},
		{Index: 7, Price: 4660, Kind: SwingLow},
	}
	if len(sw) != len(want) {
		t.Fatalf("jumlah swing = %d, mau %d (%+v)", len(sw), len(want), sw)
	}
	for i, w := range want {
		if sw[i].Index != w.Index || sw[i].Price != w.Price || sw[i].Kind != w.Kind {
			t.Errorf("swing[%d] = %+v, mau idx=%d price=%g kind=%s", i, sw[i], w.Index, w.Price, w.Kind)
		}
	}
}

// Saat keputusan (price baru retrace ke 4600), last impulse = 4560->4640.
func TestFindLastImpulse_DecisionTime(t *testing.T) {
	z := Zigzag(DetectSwings(candlesFromHL(canonHL[:6]))) // s/d idx5 (STL4600 terkonfirmasi)
	imp, ok := FindLastImpulse(z, Bullish)
	if !ok {
		t.Fatal("tidak ada impulse bullish")
	}
	if imp.Start.Price != 4560 || imp.End.Price != 4640 {
		t.Fatalf("impulse = %g->%g, mau 4560->4640", imp.Start.Price, imp.End.Price)
	}
	if f := NewFib(imp); f.Equilibrium() != 4600 {
		t.Errorf("equilibrium = %g, mau 4600", f.Equilibrium())
	}
}

// Setelah lanjut ke 4700 + konfirmasi STL berikutnya, last impulse pindah ke 4600->4700.
func TestFindLastImpulse_Advanced(t *testing.T) {
	z := Zigzag(DetectSwings(candlesFromHL(canonHL)))
	imp, ok := FindLastImpulse(z, Bullish)
	if !ok {
		t.Fatal("tidak ada impulse")
	}
	if imp.Start.Price != 4600 || imp.End.Price != 4700 {
		t.Fatalf("impulse = %g->%g, mau 4600->4700", imp.Start.Price, imp.End.Price)
	}
}

func TestFibZone(t *testing.T) {
	f := Fib{Low: 4560, High: 4640} // eq 4600
	cases := []struct {
		price float64
		want  Zone
	}{
		{4590, ZoneDiscount},
		{4600, ZoneEquilibrium},
		{4610, ZonePremium},
	}
	for _, c := range cases {
		if got := f.Zone(c.price); got != c.want {
			t.Errorf("Zone(%g) = %s, mau %s", c.price, got, c.want)
		}
	}
}

func TestRuleOf05(t *testing.T) {
	f := Fib{Low: 4560, High: 4640} // eq 4600
	if !f.RetracementValid(4600, Bullish, DefaultMinRetracePct) {
		t.Error("retrace ke 4600 (=0.5) harusnya valid")
	}
	if f.RetracementValid(4610, Bullish, DefaultMinRetracePct) {
		t.Error("retrace ke 4610 (belum 0.5) harusnya invalid")
	}
}

func TestFindValidImpulse(t *testing.T) {
	imp, ok := FindValidImpulse(candlesFromHL(canonHL[:6]), Bullish, DefaultMinRetracePct, DefaultMaxCalibration)
	if !ok {
		t.Fatal("harusnya ada impulse valid")
	}
	if imp.Start.Price != 4560 || imp.End.Price != 4640 {
		t.Errorf("valid impulse = %g->%g, mau 4560->4640", imp.Start.Price, imp.End.Price)
	}
}

// loadH1OrSkip memuat cache H1 OANDA asli, atau skip kalau belum ada.
func loadH1OrSkip(t *testing.T) []data.Candle {
	t.Helper()
	path := data.CSVPath("../../data", "XAU_USD", "H1")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("cache H1 belum ada (%s) — jalankan `go run ./cmd/fetch`", path)
	}
	candles, err := data.ReadCSV(path)
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	if len(candles) < 100 {
		t.Fatalf("candle terlalu sedikit: %d", len(candles))
	}
	return candles
}

// Smoke-test pakai data OANDA asli (H1 cache). Skip kalau cache belum ada.
// Tujuan: pastikan deteksi tidak panic + hasil masuk akal di data nyata.
func TestRealData_H1_Smoke(t *testing.T) {
	candles := loadH1OrSkip(t)
	z := Zigzag(DetectSwings(candles))
	if len(z) < 2 {
		t.Fatalf("zigzag terlalu pendek: %d", len(z))
	}
	// Zigzag harus berselang-seling high/low.
	for i := 1; i < len(z); i++ {
		if z[i].Kind == z[i-1].Kind {
			t.Fatalf("zigzag tidak alternating di idx %d (%s,%s)", i, z[i-1].Kind, z[i].Kind)
		}
	}

	impBull, okBull := FindValidImpulse(candles, Bullish, DefaultMinRetracePct, DefaultMaxCalibration)
	impBear, okBear := FindValidImpulse(candles, Bearish, DefaultMinRetracePct, DefaultMaxCalibration)
	t.Logf("H1: %d candle, %d swing, %d zigzag", len(candles), len(DetectSwings(candles)), len(z))
	if okBull {
		f := NewFib(impBull)
		t.Logf("last valid BULL impulse: %.2f -> %.2f (eq %.2f)", impBull.Start.Price, impBull.End.Price, f.Equilibrium())
		if impBull.End.Price <= impBull.Start.Price {
			t.Errorf("bull impulse End harus > Start: %.2f -> %.2f", impBull.Start.Price, impBull.End.Price)
		}
	}
	if okBear {
		t.Logf("last valid BEAR impulse: %.2f -> %.2f", impBear.Start.Price, impBear.End.Price)
		if impBear.End.Price >= impBear.Start.Price {
			t.Errorf("bear impulse End harus < Start: %.2f -> %.2f", impBear.Start.Price, impBear.End.Price)
		}
	}
}
