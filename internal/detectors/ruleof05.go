package detectors

import "forex-backtest/internal/data"

// Default Section M (Layer A).
const (
	DefaultMinRetracePct  = 0.50 // swing_validity_min_retrace_pct (Rule of 0.5)
	DefaultMaxCalibration = 2    // max_swing_calibration
)

// RetracementValid menerapkan Rule of 0.5 (Section A.2): impulse dianggap valid
// kalau retracement-nya mencapai minimal level minRetracePct (0.5 = equilibrium).
// retraceExtreme = ekstrem retracement (low untuk Bullish, high untuk Bearish).
func (f Fib) RetracementValid(retraceExtreme float64, dir Direction, minRetracePct float64) bool {
	rng := f.High - f.Low
	if dir == Bullish {
		// retrace turun dari High; valid kalau low retrace <= level (1-min).
		return retraceExtreme <= f.Low+(1-minRetracePct)*rng
	}
	// retrace naik dari Low; valid kalau high retrace >= level min.
	return retraceExtreme >= f.Low+minRetracePct*rng
}

// RetraceExtremeAfter menghitung ekstrem retracement setelah imp.End:
// Bullish = low terendah; Bearish = high tertinggi — sampai (a) harga menembus
// balik ekstrem impulse (continuation) atau (b) habis data. ok=false kalau tidak
// ada candle setelah End.
func RetraceExtremeAfter(candles []data.Candle, imp Impulse) (extreme float64, ok bool) {
	start := imp.End.Index + 1
	if start >= len(candles) {
		return 0, false
	}
	if imp.Dir == Bullish {
		ext := candles[start].Low
		for i := start; i < len(candles); i++ {
			if candles[i].High > imp.End.Price {
				break // impulse high ditembus → retrace selesai (continuation)
			}
			if candles[i].Low < ext {
				ext = candles[i].Low
			}
		}
		return ext, true
	}
	ext := candles[start].High
	for i := start; i < len(candles); i++ {
		if candles[i].Low < imp.End.Price {
			break
		}
		if candles[i].High > ext {
			ext = candles[i].High
		}
	}
	return ext, true
}

// FindValidImpulse menggabung A.1 + A.2 + kalibrasi: pilih impulse ter-validasi
// paling baru searah dir yang retracement-nya lolos Rule of 0.5. Kalau gagal,
// mundur ke impulse lebih lama (kalibrasi), maksimal maxCalibration kali.
// Semantik penuh "geser ke swing newer / tunggu kalau belum ada" ada di engine/state.
func FindValidImpulse(candles []data.Candle, dir Direction, minRetracePct float64, maxCalibration int) (Impulse, bool) {
	return FindValidImpulseZ(Zigzag(DetectSwings(candles)), candles, dir, minRetracePct, maxCalibration)
}

// FindValidImpulseZ = inti A.1+A.2 atas zigzag `z` yang SUDAH dibentuk caller
// (mis. ZigzagMin ter-filter magnitude). `candles` = deret asli (untuk hitung
// retracement via Swing.Index). Memisahkan pembentukan zigzag dari validasi
// supaya filter magnitude (Bug#2) tetap dipakai saat anchor Fibo (Bug#1 fix):
// engine kirim zigzag ter-filter, bukan biarkan fungsi ini bikin zigzag mentah.
func FindValidImpulseZ(z []Swing, candles []data.Candle, dir Direction, minRetracePct float64, maxCalibration int) (Impulse, bool) {
	tries := 0
	for j := len(z) - 2; j >= 1 && tries <= maxCalibration; j-- {
		matched := (dir == Bullish && z[j].Kind == SwingHigh && z[j-1].Kind == SwingLow) ||
			(dir == Bearish && z[j].Kind == SwingLow && z[j-1].Kind == SwingHigh)
		if !matched {
			continue
		}
		imp := Impulse{Start: z[j-1], End: z[j], Dir: dir}
		ext, hasRetrace := RetraceExtremeAfter(candles, imp)
		if hasRetrace && NewFib(imp).RetracementValid(ext, dir, minRetracePct) {
			return imp, true
		}
		tries++
	}
	return Impulse{}, false
}
