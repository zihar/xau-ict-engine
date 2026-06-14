package detectors

import "forex-backtest/internal/data"

// PipSizeGold = nilai 1 pip XAUUSD dalam harga ($0.10). Konsisten:
// fvg_min_gap_pips_gold 5 = $0.50, SL 130 pip = $13 (Section H/I/M).
const PipSizeGold = 0.10

// PipsToPrice mengubah jarak pip gold ke jarak harga.
func PipsToPrice(pips float64) float64 { return pips * PipSizeGold }

// TrueRange candle i (butuh i>=1 untuk prevClose; i==0 pakai high-low).
func TrueRange(candles []data.Candle, i int) float64 {
	hl := candles[i].High - candles[i].Low
	if i == 0 {
		return hl
	}
	pc := candles[i-1].Close
	hc := abs(candles[i].High - pc)
	lc := abs(candles[i].Low - pc)
	return max3(hl, hc, lc)
}

// ATRAt = ATR (SMA TrueRange, default period 14) yang berakhir di index i.
// SMA dipilih (bukan Wilder) demi determinisme & kemudahan tuning. ok=false
// kalau data sebelum i kurang dari period.
func ATRAt(candles []data.Candle, i, period int) (float64, bool) {
	if period <= 0 || i-period+1 < 0 {
		return 0, false
	}
	var sum float64
	for k := i - period + 1; k <= i; k++ {
		sum += TrueRange(candles, k)
	}
	return sum / float64(period), true
}

// FVG = Fair Value Gap 3-candle (Section F Tier 4). Index = candle tengah.
// Bullish: low candle kanan > high candle kiri (gap di antara). Bearish mirror.
type FVG struct {
	Index  int       // candle tengah (i)
	Dir    Direction // Bullish / Bearish
	Top    float64   // batas atas gap
	Bottom float64   // batas bawah gap
}

// Size = lebar gap (harga).
func (f FVG) Size() float64 { return f.Top - f.Bottom }

// DetectFVGs mendeteksi semua FVG dengan gap >= minGapPips (Section M
// fvg_min_gap_pips_gold). minGapPips dalam pip gold.
func DetectFVGs(candles []data.Candle, minGapPips float64) []FVG {
	minGap := PipsToPrice(minGapPips)
	var out []FVG
	for i := 1; i < len(candles)-1; i++ {
		left, right := candles[i-1], candles[i+1]
		if right.Low > left.High && right.Low-left.High >= minGap { // bullish FVG
			out = append(out, FVG{Index: i, Dir: Bullish, Top: right.Low, Bottom: left.High})
		}
		if right.High < left.Low && left.Low-right.High >= minGap { // bearish FVG
			out = append(out, FVG{Index: i, Dir: Bearish, Top: left.Low, Bottom: right.High})
		}
	}
	return out
}

// FVGFilledAt: true kalau FVG sudah terisi (harga balik masuk gap) pada candle idx.
// Bullish FVG terisi kalau low <= Top (harga turun masuk gap); bearish kalau high >= Bottom.
func (f FVG) FilledAt(c data.Candle) bool {
	if f.Dir == Bullish {
		return c.Low <= f.Top
	}
	return c.High >= f.Bottom
}

// VolumeImbalance = gap close-to-open antar candle berurutan (Kunci #4, Tier 1).
// Bullish: open candle i > close candle i-1 (gap body ke atas) saat keduanya searah.
type VolumeImbalance struct {
	Index  int
	Dir    Direction
	Top    float64
	Bottom float64
}

// DetectVolumeImbalances mendeteksi VI (gap close→open antar candle berurutan,
// Kunci #4 Tier 1). Spec F.1: VI = gap close-to-open KEDUA candle berurutan saat
// keduanya SEARAH (Bug#6 — sebelumnya tak cek arah candle, jadi gap di pembalikan
// arah ikut ke-emit). viMinGapPips = threshold KHUSUS VI, di-set lebih besar dari
// fvg_min_gap_pips supaya VI tidak murah & banjir (Bug#5: VI murah bikin cluster
// auto-Tier1). <=0 → fallback pakai minGapPips fvg (perilaku lama).
func DetectVolumeImbalances(candles []data.Candle, viMinGapPips float64) []VolumeImbalance {
	minGap := PipsToPrice(viMinGapPips)
	var out []VolumeImbalance
	for i := 1; i < len(candles); i++ {
		prev, cur := candles[i-1], candles[i]
		prevBull := prev.Close > prev.Open
		prevBear := prev.Close < prev.Open
		curBull := cur.Close > cur.Open
		curBear := cur.Close < cur.Open
		// Bug#6: VI butuh KEDUA candle searah (gap impulsif searah), bukan gap di titik balik.
		if curBull && prevBull && cur.Open > prev.Close && cur.Open-prev.Close >= minGap {
			out = append(out, VolumeImbalance{Index: i, Dir: Bullish, Top: cur.Open, Bottom: prev.Close})
		}
		if curBear && prevBear && cur.Open < prev.Close && prev.Close-cur.Open >= minGap {
			out = append(out, VolumeImbalance{Index: i, Dir: Bearish, Top: prev.Close, Bottom: cur.Open})
		}
	}
	return out
}

// ---- (A) Kunci #1: FVG @ swing-break (Tier-2) ----

// detectFVGSwingBreaks mendeteksi FVG yang lolos "Kunci #1" (Section F.1 Tier-2):
// FVG yang adjacent dengan swing high/low yang baru di-break (BOS). DUA varian
// (pilih via param geometric = Config.FVGBreakGeometric):
//
//   - geometric=false (DEFAULT, OOS-locked): swing = SEMUA pivot 3-bar (DetectSwings),
//     adjacency |abs| dua arah, BOS = flag-broken (ada candle SETELAH swing yg menembus
//     level-nya — brokenUp/brokenDown). Longgar tapi P&L OOS terbaik di sampel ini.
//   - geometric=true (opt-in): mirror indikator Pine — STH/STL struktural + BOS geometris
//     (level swing di dalam zona FVG). Lihat detectFVGSwingBreaksGeometric (OOS-dominated).
//
// Hasil di-emit sebagai KindFVGBreak (tierOf=2), range = range FVG itu sendiri. FVG
// asli tetap juga di-emit sebagai KindFVG (Tier-4) di DetectPDRs (tier lain tak rusak).
func detectFVGSwingBreaks(candles []data.Candle, fvgs []FVG, adj float64, geometric bool) []PDR {
	adjN := int(adj)
	if adjN <= 0 {
		adjN = 12
	}
	if geometric {
		return detectFVGSwingBreaksGeometric(candles, fvgs, adjN)
	}
	// ── Versi LAMA (default, OOS-locked) — broken-flag + semua pivot 3-bar ──
	swings := DetectSwings(candles)
	var out []PDR
	for _, f := range fvgs {
		for _, sw := range swings {
			// adjacency index (jarak candle FVG-tengah ke swing).
			d := f.Index - sw.Index
			if d < 0 {
				d = -d
			}
			if d > adjN {
				continue
			}
			if f.Dir == Bullish && sw.Kind == SwingHigh {
				// swing-high harus di-break KE ATAS oleh candle setelah swing.
				if brokenUp(candles, sw.Index, sw.Price) {
					out = append(out, PDR{Kind: KindFVGBreak, Dir: Bullish, Top: f.Top, Bottom: f.Bottom, Index: f.Index})
					break
				}
			}
			if f.Dir == Bearish && sw.Kind == SwingLow {
				if brokenDown(candles, sw.Index, sw.Price) {
					out = append(out, PDR{Kind: KindFVGBreak, Dir: Bearish, Top: f.Top, Bottom: f.Bottom, Index: f.Index})
					break
				}
			}
		}
	}
	return out
}

// detectFVGSwingBreaksGeometric — versi BARU (opt-in via Config.FVGBreakGeometric;
// default OFF). Mirror indikator Pine (keputusan user 2026-06-11, correctness-driven):
//   (a) swing = STH/STL STRUKTURAL = Zigzag(DetectSwings) alternating, BUKAN semua pivot
//       3-bar mentah (pivot minor di-merge ambil ekstrem) → tak over-tag di tren;
//   (b) BOS GEOMETRIS: level swing JATUH DI DALAM zona FVG [Bottom,Top] (displacement
//       pembentuk FVG menembus swing itu), bukan flag-broken retrospektif;
//   (c) swing harus TERBENTUK <= adj candle SEBELUM FVG (0 <= f.Index-sw.Index <= adj).
// ⚠️ TERBUKTI OOS-DOMINATED (2026-06-11): IS/OOS ~ −34R/−29R, PF & DD lebih buruk di
// semua adj 3..100 → DEFAULT OFF. Disimpan utk re-eval saat sampel diperbesar (-from 2018).
func detectFVGSwingBreaksGeometric(candles []data.Candle, fvgs []FVG, adjN int) []PDR {
	swings := Zigzag(DetectSwings(candles))
	var out []PDR
	for _, f := range fvgs {
		for _, sw := range swings {
			d := f.Index - sw.Index
			if d < 0 || d > adjN {
				continue
			}
			if sw.Price < f.Bottom || sw.Price > f.Top {
				continue
			}
			if f.Dir == Bullish && sw.Kind == SwingHigh {
				out = append(out, PDR{Kind: KindFVGBreak, Dir: Bullish, Top: f.Top, Bottom: f.Bottom, Index: f.Index})
				break
			}
			if f.Dir == Bearish && sw.Kind == SwingLow {
				out = append(out, PDR{Kind: KindFVGBreak, Dir: Bearish, Top: f.Top, Bottom: f.Bottom, Index: f.Index})
				break
			}
		}
	}
	return out
}

// brokenUp: ada candle SETELAH swingIdx yang high-nya menembus price (BOS ke atas).
func brokenUp(candles []data.Candle, swingIdx int, price float64) bool {
	for k := swingIdx + 1; k < len(candles); k++ {
		if candles[k].High > price {
			return true
		}
	}
	return false
}

// brokenDown: ada candle SETELAH swingIdx yang low-nya menembus price (BOS ke bawah).
func brokenDown(candles []data.Candle, swingIdx int, price float64) bool {
	for k := swingIdx + 1; k < len(candles); k++ {
		if candles[k].Low < price {
			return true
		}
	}
	return false
}

// ---- (B) BPR — Balanced Price Range (Tier-3) ----

// detectBPRs mendeteksi BPR (Section F.1 Tier-3): "BISI + SIBI overlap dalam <=5
// candle". BISI = FVG bullish, SIBI = FVG bearish. BPR terbentuk kalau range harga
// FVG bullish & FVG bearish BERIRISAN dan keduanya lahir dalam <= maxDist candle.
// Zona BPR = IRISAN kedua FVG (overlap region).
//
// Arah (Dir) — dua mode (Config.BPRDirectional):
//   - false (lama, default): BPR diperlakukan NETRAL → di-emit DUA KALI (Bullish +
//     Bearish) di zona irisan yg sama, jadi bisa konfluen utk arah manapun & gate
//     zona di SelectPOI yg memutuskan relevansi. KELEMAHAN: BPR jadi confluence
//     padding (selalu +1 ke cluster arah manapun) — tak "menentukan arah".
//   - true (pertemuan 4): BPR itu DIRECTIONAL ("dari BPR kita bisa menentukan arah
//     market"). Emit SATU PDR dgn arah = FVG yg LEBIH BARU (Index lebih besar) —
//     imbalance terakhir yg "menyeimbangkan" prior imbalance menentukan arah zona
//     (konvensi ICT). Menghapus artifak padding + faithful ke materi. Mekanisme
//     HOLD/BREAK penuh (break→reverse entry) di luar scope (bentrok ImpulseOnly):
//     HOLD ≈ entry continuation searah OF yg sudah ada; BREAK ≈ FilterLivePDRs invalidate.
func detectBPRs(candles []data.Candle, fvgs []FVG, maxDist float64, directional bool) []PDR {
	maxN := int(maxDist)
	if maxN <= 0 {
		maxN = 5
	}
	var out []PDR
	for i := 0; i < len(fvgs); i++ {
		for j := i + 1; j < len(fvgs); j++ {
			a, b := fvgs[i], fvgs[j]
			if a.Dir == b.Dir { // butuh satu bullish (BISI) + satu bearish (SIBI)
				continue
			}
			d := a.Index - b.Index
			if d < 0 {
				d = -d
			}
			if d > maxN {
				continue
			}
			lo := maxf(a.Bottom, b.Bottom)
			hi := minf(a.Top, b.Top)
			if lo >= hi { // tidak beririsan
				continue
			}
			// recent = FVG yg lebih baru (imbalance TERAKHIR yg menyeimbangkan).
			recent := a
			if b.Index > a.Index {
				recent = b
			}
			idx := recent.Index
			if directional {
				// Arah = FVG lebih baru (imbalance terakhir yg menyeimbangkan).
				// ZONA = IRISAN (overlap). Zona-FVG-terakhir-penuh DIUJI 2026-06-04
				// (lebih faithful "zona aktif") tapi MERUGIKAN: BPR bucket PF1.16→0.58,
				// OOS 2.08→1.94 → user pilih pertahankan irisan (juga tafsir ICT sah:
				// overlap = "balanced region"). Lihat DECISIONS.md.
				out = append(out, PDR{Kind: KindBPR, Dir: recent.Dir, Top: hi, Bottom: lo, Index: idx})
			} else {
				out = append(out,
					PDR{Kind: KindBPR, Dir: Bullish, Top: hi, Bottom: lo, Index: idx},
					PDR{Kind: KindBPR, Dir: Bearish, Top: hi, Bottom: lo, Index: idx},
				)
			}
		}
	}
	return out
}

// ---- (C) IFVG — Inversion FVG (Tier-4) ----

// detectIFVGs mendeteksi IFVG (Section F.1 Tier-4): FVG yang sudah DITEMBUS harga
// (gap-nya ter-mitigasi/di-close menembus) lalu BERBALIK peran jadi PD array arah
// BERLAWANAN (support<->resistance flip).
//
// Definisi paling defensible (MASTER_LESSONS video 4): FVG dianggap "gagal hold"
// kalau ada candle SETELAH FVG yang CLOSE-nya menembus sisi jauh gap:
//   - Bullish FVG gagal kalau ada close < Bottom (harga jebol ke bawah gap) →
//     zona itu flip jadi RESISTANCE → IFVG BEARISH (range = range FVG lama).
//   - Bearish FVG gagal kalau ada close > Top → flip jadi SUPPORT → IFVG BULLISH.
//
// Range IFVG = range FVG lama (zona yg sekarang dipakai dari sisi sebaliknya).
// Index = candle yg mengkonfirmasi penembusan (konteks flip terbaru).
//
// requireNoSameDirFVG (pertemuan 4 "IFVG Fallback Rule"): kalau true, IFVG arah D
// HANYA di-emit kalau saat momen flip (candle k) TIDAK ada FVG searah D yang masih
// live — "harga tidak menyediakan FVG searah" → baru pakai FVG gagal sbg pijakan.
// FVG-nya tetap di-consume (break) walau emisi ditekan: dia memang sudah flip, cuma
// IFVG-nya tak diperlukan karena ada FVG searah yg lebih utama.
func detectIFVGs(candles []data.Candle, fvgs []FVG, requireNoSameDirFVG bool) []PDR {
	var out []PDR
	for _, f := range fvgs {
		for k := f.Index + 2; k < len(candles); k++ { // mulai setelah kaki kanan FVG (i+1)
			c := candles[k]
			if f.Dir == Bullish && c.Close < f.Bottom {
				if !(requireNoSameDirFVG && liveSameDirFVGExists(candles, fvgs, Bearish, k)) {
					out = append(out, PDR{Kind: KindIFVG, Dir: Bearish, Top: f.Top, Bottom: f.Bottom, Index: k})
				}
				break
			}
			if f.Dir == Bearish && c.Close > f.Top {
				if !(requireNoSameDirFVG && liveSameDirFVGExists(candles, fvgs, Bullish, k)) {
					out = append(out, PDR{Kind: KindIFVG, Dir: Bullish, Top: f.Top, Bottom: f.Bottom, Index: k})
				}
				break
			}
		}
	}
	return out
}

// liveSameDirFVGExists: true kalau ADA FVG arah dir yang sudah terbentuk pada/atau
// sebelum candle k DAN masih live (belum gagal) per snapshot di k. "Gagal" pakai
// semantik close-tembus yang sama dengan pemicu IFVG (konsisten: FVG yg belum
// flip = masih tersedia sbg POI searah). Dipakai oleh IFVG Fallback Rule.
func liveSameDirFVGExists(candles []data.Candle, fvgs []FVG, dir Direction, k int) bool {
	for _, f := range fvgs {
		if f.Dir != dir || f.Index > k {
			continue
		}
		broken := false
		for i := f.Index + 1; i <= k && i < len(candles); i++ {
			c := candles[i]
			if dir == Bullish {
				if c.Close < f.Bottom { // bullish FVG gagal hold
					broken = true
					break
				}
			} else if c.Close > f.Top { // bearish FVG gagal hold
				broken = true
				break
			}
		}
		if !broken {
			return true
		}
	}
	return false
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func max3(a, b, c float64) float64 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}
