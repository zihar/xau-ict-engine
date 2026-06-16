package detectors

import (
	"time"

	"xau-ict-engine/internal/data"
)

// SwingKind membedakan swing high dan swing low.
type SwingKind int

const (
	SwingLow SwingKind = iota
	SwingHigh
)

func (k SwingKind) String() string {
	if k == SwingHigh {
		return "high"
	}
	return "low"
}

// Swing adalah satu titik swing basic 3-bar (Section A.1 / C.1):
// STH = high > tetangga kiri & kanan; STL = low < tetangga kiri & kanan.
// Index = posisi candle di slice sumber; Price = High (SwingHigh) / Low (SwingLow).
type Swing struct {
	Index int
	Time  time.Time
	Price float64
	Kind  SwingKind
}

// DetectSwings mendeteksi semua swing basic 3-bar pada deret candle
// (`swing_basic_pattern: 3_bar`, Section M). TANPA filter retracement
// (itu khusus swing-valid anchor Fibonacci, lihat FindValidImpulse) dan TANPA
// filter shape candle manipulasi (Section A.1 catatan). Di-reuse Layer C (STL/STH).
func DetectSwings(candles []data.Candle) []Swing {
	var out []Swing
	for i := 1; i < len(candles)-1; i++ {
		c := candles[i]
		if c.High > candles[i-1].High && c.High > candles[i+1].High {
			out = append(out, Swing{Index: i, Time: c.Time, Price: c.High, Kind: SwingHigh})
		}
		if c.Low < candles[i-1].Low && c.Low < candles[i+1].Low {
			out = append(out, Swing{Index: i, Time: c.Time, Price: c.Low, Kind: SwingLow})
		}
	}
	return out
}

// Zigzag mereduksi deret swing basic jadi urutan turning point berselang-seling
// high/low — inilah segmentasi "impulsive move" (Section A.1). Kalau dua swing
// sejenis berurutan, ambil yang lebih ekstrem (high tertinggi / low terendah).
func Zigzag(swings []Swing) []Swing { return ZigzagMin(swings, 0) }

// ZigzagMin = Zigzag dengan filter magnitude: turning point lawan-arah baru
// hanya diterima kalau pergerakan harga dari pivot terakhir >= minLegPrice.
// Tujuan: meredam swing 3-bar yang terlalu rapat (noise) — `magnitude filter`
// tunable yang diantisipasi sejak Section A (catatan A.1/C). minLegPrice <= 0 →
// identik Zigzag murni (tanpa filter). Swing kecil yang ditolak tidak menggeser
// pivot; swing sejenis yang lebih ekstrem tetap meng-update pivot seperti biasa.
func ZigzagMin(swings []Swing, minLegPrice float64) []Swing {
	var z []Swing
	for _, s := range swings {
		if len(z) == 0 {
			z = append(z, s)
			continue
		}
		last := &z[len(z)-1]
		if s.Kind == last.Kind {
			if (s.Kind == SwingHigh && s.Price > last.Price) ||
				(s.Kind == SwingLow && s.Price < last.Price) {
				*last = s
			}
			continue
		}
		// turning point: terima hanya kalau leg cukup besar (filter noise).
		if minLegPrice <= 0 || absLeg(s.Price-last.Price) >= minLegPrice {
			z = append(z, s)
		}
	}
	return z
}

func absLeg(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
