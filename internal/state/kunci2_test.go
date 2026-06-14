package state

import (
	"testing"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
)

func c(h, l float64) data.Candle {
	return data.Candle{Open: (h + l) / 2, Close: (h + l) / 2, High: h, Low: l}
}

func TestRetraceFVGTarget(t *testing.T) {
	// Bullish FVG (support): c0.High=100, c2.Low=105 → gap [100,105].
	weekly := []data.Candle{c(100, 90), c(101, 95), c(110, 105)}
	f, ok := retraceFVGTarget(weekly, detectors.Bullish, 200, 5)
	if !ok {
		t.Fatal("harus ketemu FVG bullish di bawah ref 200")
	}
	if f.Dir != detectors.Bullish || f.Top != 105 || f.Bottom != 100 {
		t.Errorf("FVG salah: dir=%v top=%g bottom=%g (mau bullish 105/100)", f.Dir, f.Top, f.Bottom)
	}
	// ref di BAWAH FVG → tak ada target di bawah ref.
	if _, ok := retraceFVGTarget(weekly, detectors.Bullish, 50, 5); ok {
		t.Error("ref 50 di bawah FVG → harusnya tak ada target")
	}

	// Bearish FVG (resistance): c0.Low=200, c2.High=190 → gap [190,200].
	wb := []data.Candle{c(210, 200), c(205, 195), c(190, 185)}
	fb, ok := retraceFVGTarget(wb, detectors.Bearish, 100, 5)
	if !ok || fb.Dir != detectors.Bearish || fb.Top != 200 || fb.Bottom != 190 {
		t.Errorf("FVG bearish salah: ok=%v %+v (mau bearish 200/190)", ok, fb)
	}
}

func TestComputeRegimeNoGateNoTarget(t *testing.T) {
	// Deret naik monoton tanpa swing-high-sweep terakhir → IMPULSE (gate tak buka).
	var up []data.Candle
	for i := 0; i < 10; i++ {
		base := 100 + float64(i)*5
		up = append(up, c(base+2, base))
	}
	if r := ComputeRegime(up, detectors.Bullish, true, 5); r != RegimeImpulse {
		t.Errorf("tren naik tanpa gate → mau IMPULSE, dapat %v", r)
	}
	// fvgGate=false tetap IMPULSE juga kalau gate tak buka.
	if r := ComputeRegime(up, detectors.Bullish, false, 5); r != RegimeImpulse {
		t.Errorf("gate tak buka → IMPULSE apa pun fvgGate, dapat %v", r)
	}
	// Data terlalu pendek → IMPULSE.
	if r := ComputeRegime(up[:2], detectors.Bullish, true, 5); r != RegimeImpulse {
		t.Errorf("data pendek → IMPULSE, dapat %v", r)
	}
}
