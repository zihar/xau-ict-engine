package state

import (
	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
)

// Regime = saklar 2 mode dalam satu weekly OF (Section E).
type Regime int

const (
	RegimeImpulse Regime = iota // sebelum gate: trade searah weekly OF
	RegimeRetrace               // setelah gate: leg korektif ke weekly FVG
)

func (r Regime) String() string {
	if r == RegimeRetrace {
		return "retrace"
	}
	return "impulse"
}

// ComputeRegime menerapkan gate Kunci #2 (Section E). Gate sweep dibuka kalau,
// untuk OF aktif: (1) swing weekly searah OF = turning point TERAKHIR (impulse
// dianggap selesai) DAN (2) swing itu menyapu likuiditas (sweep old high/low,
// wick cukup) DAN (3) harga sudah mulai retrace dari swing itu.
//
// fvgGate=true (Section E penuh, default): retrace HANYA aktif kalau ada **weekly
// FVG target** searah retrace yang BELUM tercapai harga (bullish OF → FVG bullish
// di bawah; bearish OF → FVG bearish di atas). Begitu harga MENGISI FVG itu,
// regime kembali IMPULSE (retrace_end_eval: fvg_reaction) — ini menutup leak
// "retrace nyangkut counter-trend berbulan-bulan" (diagnosa 2026-05-31). Tanpa
// target FVG → tetap IMPULSE (jangan lawan tren tanpa tujuan).
//
// fvgGate=false: perilaku lama (retrace begitu gate sweep buka, tanpa batas).
// minGapPips = ambang deteksi FVG weekly.
// Wrapper kompat minLeg=0 (tanpa filter magnitude swing).
func ComputeRegime(weekly []data.Candle, ofDir detectors.Direction, fvgGate bool, minGapPips float64) Regime {
	return ComputeRegimeMin(weekly, ofDir, fvgGate, minGapPips, 0)
}

// ComputeRegimeMin = ComputeRegime dengan filter magnitude zigzag (minLeg dalam
// harga, biasanya N×ATR weekly; 0 = perilaku lama). Konsisten dengan WeeklyOFMin:
// turning point pemicu retrace harus swing SIGNIFIKAN, bukan micro 3-bar.
func ComputeRegimeMin(weekly []data.Candle, ofDir detectors.Direction, fvgGate bool, minGapPips, minLeg float64) Regime {
	z := detectors.ZigzagMin(detectors.DetectSwings(weekly), minLeg)
	if len(z) < 3 {
		return RegimeImpulse
	}
	last := z[len(z)-1]
	price := weekly[len(weekly)-1].Close

	gateOpen := false
	switch ofDir {
	case detectors.Bullish:
		if last.Kind == detectors.SwingHigh {
			if prevHigh, ok := prevSameKind(z, detectors.SwingHigh); ok && last.Price > prevHigh.Price && price < last.Price {
				gateOpen = true
			}
		}
	case detectors.Bearish:
		if last.Kind == detectors.SwingLow {
			if prevLow, ok := prevSameKind(z, detectors.SwingLow); ok && last.Price < prevLow.Price && price > last.Price {
				gateOpen = true
			}
		}
	}
	if !gateOpen {
		return RegimeImpulse
	}
	if !fvgGate {
		return RegimeRetrace // perilaku lama (unbounded)
	}

	// Section E penuh: target = weekly FVG di bawah swing high pemicu (bullish) /
	// di atas swing low pemicu (bearish) — level TETAP (relatif `last`, bukan
	// harga berjalan). Retrace aktif hanya selama harga masih DI ANTARA target &
	// swing pemicu (lagi menuju FVG); begitu harga mengisi FVG → balik IMPULSE.
	target, ok := retraceFVGTarget(weekly, ofDir, last.Price, minGapPips)
	if !ok {
		return RegimeImpulse // tak ada target → jangan lawan tren tanpa tujuan
	}
	switch ofDir {
	case detectors.Bullish:
		if price > target.Top { // belum turun mengisi FVG support → masih retrace
			return RegimeRetrace
		}
	case detectors.Bearish:
		if price < target.Bottom { // belum naik mengisi FVG resistance → masih retrace
			return RegimeRetrace
		}
	}
	return RegimeImpulse // FVG target sudah terisi → retrace selesai, balik impulse
}

// RetraceTargetMin mengembalikan weekly FVG target regime retrace SAAT INI —
// FVG yang sama yang dipakai ComputeRegimeMin (relatif swing pemicu = turning
// point zigzag terakhir). Dipakai engine untuk TP retrace = level FVG
// (keputusan user 2026-06-02: fill-rate 81% tapi dangkal — RR 1:3 tak selaras).
// ok=false kalau struktur/target tak ada.
func RetraceTargetMin(weekly []data.Candle, ofDir detectors.Direction, minGapPips, minLeg float64) (detectors.FVG, bool) {
	z := detectors.ZigzagMin(detectors.DetectSwings(weekly), minLeg)
	if len(z) < 1 {
		return detectors.FVG{}, false
	}
	return retraceFVGTarget(weekly, ofDir, z[len(z)-1].Price, minGapPips)
}

// retraceFVGTarget memilih weekly FVG target untuk leg retrace relatif level
// swing pemicu `ref`: bullish OF → FVG bullish (support) TERDEKAT di bawah ref;
// bearish OF → FVG bearish (resistance) terdekat di atas ref. ok=false kalau tak ada.
func retraceFVGTarget(weekly []data.Candle, ofDir detectors.Direction, ref, minGapPips float64) (detectors.FVG, bool) {
	var best detectors.FVG
	found := false
	for _, f := range detectors.DetectFVGs(weekly, minGapPips) {
		switch ofDir {
		case detectors.Bullish:
			if f.Dir == detectors.Bullish && f.Top < ref && (!found || f.Top > best.Top) {
				best, found = f, true
			}
		case detectors.Bearish:
			if f.Dir == detectors.Bearish && f.Bottom > ref && (!found || f.Bottom < best.Bottom) {
				best, found = f, true
			}
		}
	}
	return best, found
}

// TradeDirection menggabung OF + regime → arah trade aktual (Section E + N.2 STEP 1b):
// Impulse = searah OF; Retrace = leg korektif lawan arah OF (ke weekly FVG).
func TradeDirection(ofDir detectors.Direction, regime Regime) detectors.Direction {
	if regime == RegimeRetrace {
		if ofDir == detectors.Bullish {
			return detectors.Bearish
		}
		return detectors.Bullish
	}
	return ofDir
}

// prevSameKind mengembalikan swing sejenis sebelum turning point terakhir.
func prevSameKind(z []detectors.Swing, kind detectors.SwingKind) (detectors.Swing, bool) {
	seen := false
	for i := len(z) - 1; i >= 0; i-- {
		if z[i].Kind != kind {
			continue
		}
		if !seen { // ini yang terakhir, lewati
			seen = true
			continue
		}
		return z[i], true
	}
	return detectors.Swing{}, false
}
