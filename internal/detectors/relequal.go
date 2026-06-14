// Relative Equal High/Low (REH/REL) — pertemuan 1-2 (konsep liquidity) +
// pertemuan 10 (aplikasi QT): dua+ swing sejenis yang levelnya hampir sama dan
// BELUM di-sweep = liquidity target yang "hampir pasti diserang market maker"
// (double top/bottom trap). Rule materi: tunggu level disapu dulu sebelum entry
// searah agenda MM (wait-for-sweep).

package detectors

import (
	"time"

	"forex-backtest/internal/data"
)

// RelEqualLevel = grup 2+ swing sejenis yang levelnya dalam toleransi & belum
// di-sweep oleh candle setelah swing terakhir grup.
type RelEqualLevel struct {
	Kind     SwingKind // SwingHigh = REH, SwingLow = REL
	Level    float64   // ekstrem grup (tertinggi utk REH / terendah utk REL) — sweep harus melewati SEMUA
	Count    int       // jumlah swing penyusun
	Swings   []Swing   // referensi penyusun (untuk marker chart)
	LastTime time.Time // waktu swing terakhir grup
}

// DetectRelEquals memindai swing basic 3-bar dalam `lookback` candle terakhir,
// meng-cluster swing sejenis yang berdekatan harga (selisih ke ekstrem grup <=
// tolerance) DAN berdekatan waktu (gap antar swing berurutan di grup <= maxGap
// candle — "double top" harus sejajar dekat, bukan dua puncak kebetulan selevel
// berhari-hari terpisah), lalu membuang grup yang SUDAH di-sweep: ada candle
// SETELAH swing terakhir grup dengan high > Level (REH) / low < Level (REL) —
// wick dihitung (stop hunt menyapu via wick).
//
// lookback <= 0 = seluruh candles. tolerance <= 0 = hanya level PERSIS sama.
// maxGap <= 0 = tanpa batas jarak (perilaku lama).
func DetectRelEquals(candles []data.Candle, tolerance float64, lookback, maxGap int) []RelEqualLevel {
	start := 0
	if lookback > 0 && len(candles) > lookback {
		start = len(candles) - lookback
	}
	window := candles[start:]
	swings := DetectSwings(window)

	var out []RelEqualLevel
	for _, kind := range []SwingKind{SwingHigh, SwingLow} {
		var ks []Swing
		for _, s := range swings {
			if s.Kind == kind {
				ks = append(ks, s)
			}
		}
		out = append(out, clusterRelEquals(window, ks, kind, tolerance, maxGap)...)
	}
	return out
}

// clusterRelEquals meng-cluster swing sejenis secara KRONOLOGIS: swing
// bergabung ke grup bila harganya dalam `tolerance` dari ekstrem grup saat itu
// DAN jaraknya <= maxGap candle dari anggota terakhir grup (grup terdekat
// harganya yang menang bila ada beberapa kandidat). Grup valid butuh >= 2 swing
// & belum di-sweep.
func clusterRelEquals(window []data.Candle, swings []Swing, kind SwingKind, tolerance float64, maxGap int) []RelEqualLevel {
	if len(swings) < 2 {
		return nil
	}
	type grp struct {
		swings  []Swing
		extreme float64
		lastIdx int
	}
	var groups []*grp
	for _, s := range swings { // swings urut index (kronologis dari DetectSwings)
		var best *grp
		bestDiff := tolerance + 1
		for _, g := range groups {
			if maxGap > 0 && s.Index-g.lastIdx > maxGap {
				continue
			}
			if d := absLeg(s.Price - g.extreme); d <= tolerance && d < bestDiff {
				best, bestDiff = g, d
			}
		}
		if best == nil {
			groups = append(groups, &grp{swings: []Swing{s}, extreme: s.Price, lastIdx: s.Index})
			continue
		}
		best.swings = append(best.swings, s)
		best.lastIdx = s.Index
		if (kind == SwingHigh && s.Price > best.extreme) ||
			(kind == SwingLow && s.Price < best.extreme) {
			best.extreme = s.Price
		}
	}

	var out []RelEqualLevel
	for _, g := range groups {
		if len(g.swings) < 2 {
			continue
		}
		lvl := RelEqualLevel{Kind: kind, Level: g.extreme, Count: len(g.swings), Swings: g.swings}
		for _, s := range g.swings {
			if s.Time.After(lvl.LastTime) {
				lvl.LastTime = s.Time
			}
		}
		if relEqSwept(window, g.extreme, kind, g.lastIdx) {
			continue
		}
		out = append(out, lvl)
	}
	return out
}

// relEqSwept = true bila ada candle SETELAH index `afterIdx` yang menembus
// level (wick dihitung — sweep stop-hunt khas lewat wick).
func relEqSwept(window []data.Candle, level float64, kind SwingKind, afterIdx int) bool {
	for i := afterIdx + 1; i < len(window); i++ {
		if kind == SwingHigh && window[i].High > level {
			return true
		}
		if kind == SwingLow && window[i].Low < level {
			return true
		}
	}
	return false
}
