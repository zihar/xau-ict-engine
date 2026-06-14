package detectors

import "forex-backtest/internal/data"

// ITKind membedakan Intermediary High vs Low (Section C.2/C.3).
type ITKind int

const (
	ITLow ITKind = iota
	ITHigh
)

func (k ITKind) String() string {
	if k == ITHigh {
		return "ITH"
	}
	return "ITL"
}

// ITType: standard (3 syarat lengkap, ada swing kanan) vs fast_early (shortcut,
// langsung break swing terakhir kiri tanpa swing kanan). Section C.2/C.3 tagging.
type ITType int

const (
	ITStandard ITType = iota
	ITFastEarly
)

func (t ITType) String() string {
	if t == ITFastEarly {
		return "fast_early"
	}
	return "standard"
}

// BreakType: konfirmasi break ITH/ITL pakai wick (high/low) atau body (close).
// Section M `itl_ith_break_type` (default wick; tune [wick, body]).
type BreakType int

const (
	BreakWick BreakType = iota
	BreakBody
)

// DefaultBreakType = wick (Section M itl_ith_break_type).
const DefaultBreakType = BreakWick

// Intermediate = ITH/ITL ter-konfirmasi (C.2/C.3).
type Intermediate struct {
	Kind         ITKind
	Type         ITType
	Pivot        Swing // trough (ITL) / peak (ITH)
	BrokenSwing  Swing // STH yg di-break ke ATAS (ITL) / STL ke BAWAH (ITH)
	ConfirmIndex int   // index candle saat break terkonfirmasi
}

// DetectIntermediates mendeteksi semua ITH & ITL ter-konfirmasi pada deret candle
// (Section C.2/C.3). breakType menentukan konfirmasi break (wick=high/low,
// body=close). Fast/Early di-tag terpisah — engine yang gate Fast dengan
// TDA+DB align (C.2 catatan); detektor ini tidak punya konteks OF, jadi
// emit semua dan biarkan layer atas memilih.
//
// Catatan C.5 (stop hunt) & C.4 (fokus arah mingguan) = tanggung jawab
// engine/state (butuh konteks OF), bukan detektor ini. Lihat BreakHeld.
func DetectIntermediates(candles []data.Candle, breakType BreakType) []Intermediate {
	return DetectIntermediatesMin(candles, breakType, 0)
}

// DetectIntermediatesMin = DetectIntermediates dengan filter magnitude swing
// (minLegPrice, lihat ZigzagMin) untuk meredam ITH/ITL dari swing 3-bar rapat.
// minLegPrice <= 0 → identik DetectIntermediates. Memakai jalur LEGACY (cuma cek
// syarat 1 + konfirmasi STH/STL pra-pivot). Untuk jalur STRICT (syarat 2 + konfirmasi
// STH/STL terbaru, sesuai indikator TradingView) lihat DetectIntermediatesStrictMin.
func DetectIntermediatesMin(candles []data.Candle, breakType BreakType, minLegPrice float64) []Intermediate {
	return detectIntermediates(candles, breakType, minLegPrice, false)
}

// DetectIntermediatesStrictMin = varian STRICT (C.2/C.3 syarat lengkap, "standard-only"):
// kandidat ITL/ITH WAJIB punya STL/STH KANAN di sisi benar (syarat 2 — C lebih ekstrem
// dari swing kanan z[p+2]), dan konfirmasi = break STL/STH TERBARU (ratchet), bukan
// pra-pivot. Replikasi persis indikator "Luna Trade AMS Detection". minLegPrice<=0 →
// tanpa filter magnitude. Dipakai engine saat cfg.AMSStrictStructure=true.
func DetectIntermediatesStrictMin(candles []data.Candle, breakType BreakType, minLegPrice float64) []Intermediate {
	return detectIntermediates(candles, breakType, minLegPrice, true)
}

func detectIntermediates(candles []data.Candle, breakType BreakType, minLegPrice float64, strict bool) []Intermediate {
	z := ZigzagMin(DetectSwings(candles), minLegPrice)
	var out []Intermediate
	for p := 2; p < len(z); p++ {
		switch z[p].Kind {
		case SwingLow: // calon ITL (trough)
			if z[p-1].Kind != SwingHigh || z[p-2].Kind != SwingLow {
				continue
			}
			if z[p].Price >= z[p-2].Price { // bukan lower-low dari STL kiri (syarat 1)
				continue
			}
			var ref Swing
			var ci int
			var ok bool
			if strict {
				// syarat 2: STL kanan z[p+2] wajib ada & LEBIH TINGGI dari trough C.
				if p+2 >= len(z) || z[p+2].Kind != SwingLow || z[p+2].Price <= z[p].Price {
					continue
				}
				ref = z[p+1] // STH antara trough C dan STL kanan (di-break ke atas, ratchet)
				ci, ok = scanBreakUpStrict(candles, z, p, breakType)
			} else {
				ref = z[p-1] // STH terakhir sebelum trough (legacy: pra-pivot)
				ci, ok = scanBreakUp(candles, z[p].Index+1, z[p].Price, ref.Price, breakType)
			}
			if !ok {
				continue
			}
			out = append(out, Intermediate{
				Kind: ITLow, Type: classifyITType(z, p, ci),
				Pivot: z[p], BrokenSwing: ref, ConfirmIndex: ci,
			})
		case SwingHigh: // calon ITH (peak)
			if z[p-1].Kind != SwingLow || z[p-2].Kind != SwingHigh {
				continue
			}
			if z[p].Price <= z[p-2].Price { // bukan higher-high dari STH kiri (syarat 1)
				continue
			}
			var ref Swing
			var ci int
			var ok bool
			if strict {
				// syarat 2: STH kanan z[p+2] wajib ada & LEBIH RENDAH dari peak C.
				if p+2 >= len(z) || z[p+2].Kind != SwingHigh || z[p+2].Price >= z[p].Price {
					continue
				}
				ref = z[p+1] // STL antara peak C dan STH kanan (di-break ke bawah, ratchet)
				ci, ok = scanBreakDownStrict(candles, z, p, breakType)
			} else {
				ref = z[p-1] // STL terakhir sebelum peak (legacy: pra-pivot)
				ci, ok = scanBreakDown(candles, z[p].Index+1, z[p].Price, ref.Price, breakType)
			}
			if !ok {
				continue
			}
			out = append(out, Intermediate{
				Kind: ITHigh, Type: classifyITType(z, p, ci),
				Pivot: z[p], BrokenSwing: ref, ConfirmIndex: ci,
			})
		}
	}
	return out
}

// classifyITType: STANDARD kalau swing sejenis di kanan pivot (z[p+2]) sudah
// terbentuk SEBELUM break terkonfirmasi; FAST_EARLY kalau break terjadi di
// rally/drop pertama tanpa swing kanan (Section C.2/C.3).
func classifyITType(z []Swing, p, confirmIdx int) ITType {
	right := p + 2
	if right < len(z) && z[right].Index < confirmIdx {
		return ITStandard
	}
	return ITFastEarly
}

// scanBreakUp mencari candle pertama (mulai from) yang break `level` ke ATAS
// (konfirmasi ITL). Abort kalau harga bikin lower-low di bawah troughPrice dulu
// (trough invalid sebelum break).
func scanBreakUp(candles []data.Candle, from int, troughPrice, level float64, bt BreakType) (int, bool) {
	for i := from; i < len(candles); i++ {
		if candles[i].Low < troughPrice {
			return 0, false
		}
		if broke(candles[i], level, true, bt) {
			return i, true
		}
	}
	return 0, false
}

// scanBreakDown mirror: break `level` ke BAWAH (konfirmasi ITH). Abort kalau
// harga bikin higher-high di atas peakPrice dulu.
func scanBreakDown(candles []data.Candle, from int, peakPrice, level float64, bt BreakType) (int, bool) {
	for i := from; i < len(candles); i++ {
		if candles[i].High > peakPrice {
			return 0, false
		}
		if broke(candles[i], level, false, bt) {
			return i, true
		}
	}
	return 0, false
}

// scanBreakUpStrict = konfirmasi ITL STRICT: mulai SETELAH STL kanan z[p+2] terbentuk,
// break STH TERBARU (ratchet: ref di-update ke high zigzag berikutnya begitu indeksnya
// terlewati), abort kalau harga bikin low < trough z[p] dulu. Mirror indikator TV.
func scanBreakUpStrict(candles []data.Candle, z []Swing, p int, bt BreakType) (int, bool) {
	trough := z[p].Price
	from := z[p+2].Index + 1
	ref := z[p+1].Price // STH antara trough C dan STL kanan
	nextHigh := p + 3   // high zigzag berikutnya (alternating: p+1, p+3, ...)
	for i := from; i < len(candles); i++ {
		for nextHigh < len(z) && z[nextHigh].Index <= i {
			if z[nextHigh].Kind == SwingHigh {
				ref = z[nextHigh].Price // ratchet ke STH terbaru
			}
			nextHigh += 2
		}
		if candles[i].Low < trough { // trough invalid sebelum break
			return 0, false
		}
		if broke(candles[i], ref, true, bt) {
			return i, true
		}
	}
	return 0, false
}

// scanBreakDownStrict = mirror untuk ITH: break STL TERBARU (ratchet) ke bawah,
// abort kalau harga bikin high > peak z[p] dulu.
func scanBreakDownStrict(candles []data.Candle, z []Swing, p int, bt BreakType) (int, bool) {
	peak := z[p].Price
	from := z[p+2].Index + 1
	ref := z[p+1].Price // STL antara peak C dan STH kanan
	nextLow := p + 3
	for i := from; i < len(candles); i++ {
		for nextLow < len(z) && z[nextLow].Index <= i {
			if z[nextLow].Kind == SwingLow {
				ref = z[nextLow].Price // ratchet ke STL terbaru
			}
			nextLow += 2
		}
		if candles[i].High > peak { // peak invalid sebelum break
			return 0, false
		}
		if broke(candles[i], ref, false, bt) {
			return i, true
		}
	}
	return 0, false
}

// broke true kalau candle menembus level (up=ke atas, !up=ke bawah) sesuai BreakType.
func broke(c data.Candle, level float64, up bool, bt BreakType) bool {
	if up {
		if bt == BreakBody {
			return c.Close > level
		}
		return c.High > level
	}
	if bt == BreakBody {
		return c.Close < level
	}
	return c.Low < level
}

// LastIntermediate mengembalikan ITH/ITL ter-konfirmasi terbaru dengan kind tertentu.
func LastIntermediate(candles []data.Candle, kind ITKind, breakType BreakType) (Intermediate, bool) {
	all := DetectIntermediates(candles, breakType)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Kind == kind {
			return all[i], true
		}
	}
	return Intermediate{}, false
}

// ActiveIntermediate mengembalikan ITH/ITL ter-konfirmasi TERBARU dari `kind`
// yang masih AKTIF (belum di-break ke arah pembalik) — fungsi "arah mingguan"
// C.4 (Layer 3 AMS). ITL aktif = pivot trough belum ditembus ke BAWAH setelah
// konfirmasi; ITH aktif = pivot peak belum ditembus ke ATAS. Kalau yang terbaru
// dari `kind` itu sudah di-break, dikembalikan ok=false (fokus arah sudah shift,
// C.4 — bukan jatuh ke ITL/ITH lama). minLegPrice = filter magnitude swing
// (0 = semua, sesuai C.1: STL/STH AMS pakai basic 3-bar tanpa syarat retracement).
func ActiveIntermediate(candles []data.Candle, kind ITKind, breakType BreakType, minLegPrice float64, strict bool) (Intermediate, bool) {
	all := detectIntermediates(candles, breakType, minLegPrice, strict)
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Kind != kind {
			continue
		}
		// Hanya pertimbangkan yang TERBARU dari kind ini (C.4).
		return all[i], !intermediateBroken(candles, all[i], breakType)
	}
	return Intermediate{}, false
}

// intermediateBroken true kalau ada candle SETELAH konfirmasi yang menembus
// pivot ke arah pembalik (ITL → break ke BAWAH; ITH → break ke ATAS), sesuai
// BreakType. Dipakai ActiveIntermediate untuk menilai "masih aktif?".
func intermediateBroken(candles []data.Candle, it Intermediate, bt BreakType) bool {
	up := it.Kind == ITHigh // ITH di-break ke ATAS, ITL ke BAWAH
	for i := it.ConfirmIndex + 1; i < len(candles); i++ {
		if broke(candles[i], it.Pivot.Price, up, bt) {
			return true
		}
	}
	return false
}
