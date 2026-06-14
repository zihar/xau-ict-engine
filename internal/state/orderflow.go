package state

import (
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
)

// Anchors = LTL/LTH aktif (unified formulation B.1):
//   - LTL = swing low valid paling recent yang masih DI BAWAH harga
//   - LTH = swing high valid paling recent yang masih DI ATAS harga
//
// Role (invalidation vs target liquidity) di-derive dari OF state oleh caller.
type Anchors struct {
	LTL    detectors.Swing
	HasLTL bool
	LTH    detectors.Swing
	HasLTH bool
}

// ComputeAnchors menerapkan unified formulation B.1 pada deret weekly + harga
// acuan (biasanya close terkini). Swing signifikan = turning point Zigzag.
// Wrapper kompat minLeg=0 (tanpa filter magnitude).
func ComputeAnchors(weekly []data.Candle, price float64) Anchors {
	return ComputeAnchorsMin(weekly, price, 0)
}

// ComputeAnchorsMin = ComputeAnchors dengan filter magnitude zigzag (minLeg
// dalam harga, biasanya N×ATR weekly). 0 = tanpa filter (perilaku lama).
func ComputeAnchorsMin(weekly []data.Candle, price, minLeg float64) Anchors {
	z := detectors.ZigzagMin(detectors.DetectSwings(weekly), minLeg)
	var a Anchors
	for i := len(z) - 1; i >= 0; i-- {
		s := z[i]
		if !a.HasLTL && s.Kind == detectors.SwingLow && s.Price < price {
			a.LTL, a.HasLTL = s, true
		}
		if !a.HasLTH && s.Kind == detectors.SwingHigh && s.Price > price {
			a.LTH, a.HasLTH = s, true
		}
		if a.HasLTL && a.HasLTH {
			break
		}
	}
	return a
}

// WeeklyOF mereplay deret weekly dan mengembalikan OF DEFINITIF terkini
// (Section B.1/B.3): flip HANYA saat LTL/LTH di-break (wick cukup, hard
// invalidation). Seed arah dari leg impulse pertama. Anchors = LTL/LTH aktif
// terhadap close terakhir.
//
// Catatan tier: ini OF MASTER (weekly, definitif). Sinyal EARLY (Skenario B,
// break swing intermediate candle-close di daily/1H) di-handle engine via
// Layer C (DetectIntermediates) — bukan di sini, supaya 2-tier tidak conflate.
// Wrapper kompat minLeg=0 (tanpa filter magnitude — perilaku lama).
func WeeklyOF(weekly []data.Candle) (dir detectors.Direction, anchors Anchors, ok bool) {
	return WeeklyOFMin(weekly, 0)
}

// WeeklyOFMin = WeeklyOF dengan filter magnitude swing (fix diagnosa #5
// 2026-06-02: zigzag weekly TANPA filter bikin OF flip bearish di micro
// swing-low $20-50 di tengah bull run → short counter-trend PF0.70/−18R).
//
// minLeg > 0: struktur = zigzag TER-FILTER (leg >= minLeg, biasanya N×ATR
// weekly), di-replay candle-per-candle ANTI-LOOKAHEAD — di tiap candle, pivot
// zigzag dibangun inkremental HANYA dari swing yang sudah terkonfirmasi
// (Index+1 <= i; algoritma streaming identik dengan detectors.ZigzagMin yang
// memang single forward pass). Flip OF tetap wick-break, tapi hanya pada
// LTL/LTH struktur signifikan — bukan tiap swing 3-bar.
//
// minLeg <= 0: jalur lama persis (replay SEMUA swing 3-bar) — test lama valid.
func WeeklyOFMin(weekly []data.Candle, minLeg float64) (dir detectors.Direction, anchors Anchors, ok bool) {
	dir, anchors, _, ok = WeeklyOFFull(weekly, minLeg, 0)
	return dir, anchors, ok
}

// WeeklyOFFull = WeeklyOFMin + dua tambahan (bedah 2026-06-02):
//
//   - flipTime: waktu COMPLETION candle weekly tempat arah OF terakhir berubah
//     (atau pertama kali terdefinisi). Dipakai engine utk gate MaxOFAgeDays —
//     temuan: OF berumur >60 hari = 60% sampel tapi PF cuma 1.29 (impulse
//     kehabisan tenaga).
//
//   - bearConfirmWeeks: konfirmasi-kelanjutan ASIMETRIS khusus flip bull→bear
//     (katalog 13 flip: bullish 6/6 benar, bearish 5/7 whipsaw wick-break
//     dangkal di koreksi bull). Trigger wick-break low TIDAK langsung flip;
//     flip sah hanya bila close candle ke-(trigger+N) tetap DI BAWAH close
//     candle trigger — kalau tidak, trigger hangus (tetap bullish).
//     Flip bear→bull tetap langsung (tanpa syarat). 0 = off (perilaku lama).
func WeeklyOFFull(weekly []data.Candle, minLeg float64, bearConfirmWeeks int) (dir detectors.Direction, anchors Anchors, flipTime time.Time, ok bool) {
	if minLeg > 0 {
		return weeklyOFFiltered(weekly, minLeg, bearConfirmWeeks)
	}
	swings := detectors.DetectSwings(weekly)
	z := detectors.Zigzag(swings)
	if len(z) < 2 || len(weekly) == 0 {
		return 0, Anchors{}, time.Time{}, false
	}

	// Seed arah dari leg zigzag pertama.
	dir = legDir(z[0], z[1])

	// Replay candle, jaga swing low/high terkonfirmasi terakhir (confirm = idx+1).
	type cs struct {
		s         detectors.Swing
		confirmAt int
	}
	cands := make([]cs, len(swings))
	for i, s := range swings {
		cands[i] = cs{s: s, confirmAt: s.Index + 1}
	}

	var lastLow, lastHigh detectors.Swing
	var hasLow, hasHigh bool
	ci := 0
	flipIdx := 0  // candle tempat dir terakhir berubah (0 = sejak seed)
	pending := -1 // index candle trigger bear yang menunggu konfirmasi close
	for i := 0; i < len(weekly); i++ {
		for ci < len(cands) && cands[ci].confirmAt == i {
			if cands[ci].s.Kind == detectors.SwingLow {
				lastLow, hasLow = cands[ci].s, true
			} else {
				lastHigh, hasHigh = cands[ci].s, true
			}
			ci++
		}
		switch dir {
		case detectors.Bullish:
			if hasLow && weekly[i].Low < lastLow.Price {
				if bearConfirmWeeks <= 0 {
					dir, flipIdx = detectors.Bearish, i // LTL break → flip (wick cukup)
				} else if pending < 0 {
					pending = i // tahan: tunggu konfirmasi kelanjutan
				}
			}
		case detectors.Bearish:
			if hasHigh && weekly[i].High > lastHigh.Price {
				dir, flipIdx = detectors.Bullish, i // LTH break → flip (langsung)
				pending = -1
			}
		}
		// Evaluasi konfirmasi bearish: close candle trigger+N harus < close trigger.
		if pending >= 0 && dir == detectors.Bullish && i >= pending+bearConfirmWeeks {
			if weekly[i].Close < weekly[pending].Close {
				dir, flipIdx = detectors.Bearish, i
			}
			pending = -1 // konfirmasi gagal → trigger hangus
		}
	}

	return dir, ComputeAnchors(weekly, weekly[len(weekly)-1].Close), weekly[flipIdx].Time.Add(7 * 24 * time.Hour), true
}

// weeklyOFFiltered = jalur WeeklyOFFull minLeg>0: replay candle-per-candle dgn
// zigzag inkremental ter-filter (streaming ZigzagMin — anti-lookahead).
// bearConfirmWeeks & flipTime: semantik sama dengan WeeklyOFFull.
func weeklyOFFiltered(weekly []data.Candle, minLeg float64, bearConfirmWeeks int) (dir detectors.Direction, anchors Anchors, flipTime time.Time, ok bool) {
	swings := detectors.DetectSwings(weekly)
	if len(weekly) == 0 || len(swings) < 2 {
		return 0, Anchors{}, time.Time{}, false
	}

	// piv = pivot zigzag terkonfirmasi sejauh replay (aturan = ZigzagMin persis:
	// same-kind → ganti kalau lebih ekstrem; beda kind → terima kalau leg >= minLeg).
	var piv []detectors.Swing
	push := func(s detectors.Swing) {
		if len(piv) == 0 {
			piv = append(piv, s)
			return
		}
		last := &piv[len(piv)-1]
		if s.Kind == last.Kind {
			if (s.Kind == detectors.SwingHigh && s.Price > last.Price) ||
				(s.Kind == detectors.SwingLow && s.Price < last.Price) {
				*last = s
			}
			return
		}
		leg := s.Price - last.Price
		if leg < 0 {
			leg = -leg
		}
		if leg >= minLeg {
			piv = append(piv, s)
		}
	}
	lastPivot := func(kind detectors.SwingKind) (detectors.Swing, bool) {
		for i := len(piv) - 1; i >= 0; i-- {
			if piv[i].Kind == kind {
				return piv[i], true
			}
		}
		return detectors.Swing{}, false
	}

	si := 0
	dirSet := false
	flipIdx := 0
	pending := -1
	for i := 0; i < len(weekly); i++ {
		for si < len(swings) && swings[si].Index+1 == i {
			push(swings[si])
			si++
		}
		if !dirSet {
			if len(piv) >= 2 {
				dir = legDir(piv[0], piv[1])
				dirSet = true
				flipIdx = i
			}
			continue // belum ada struktur → belum bisa flip
		}
		switch dir {
		case detectors.Bullish:
			if low, has := lastPivot(detectors.SwingLow); has && weekly[i].Low < low.Price {
				if bearConfirmWeeks <= 0 {
					dir, flipIdx = detectors.Bearish, i // LTL signifikan break (wick) → flip
				} else if pending < 0 {
					pending = i // tahan: tunggu konfirmasi kelanjutan
				}
			}
		case detectors.Bearish:
			if high, has := lastPivot(detectors.SwingHigh); has && weekly[i].High > high.Price {
				dir, flipIdx = detectors.Bullish, i // LTH signifikan break → flip (langsung)
				pending = -1
			}
		}
		if pending >= 0 && dir == detectors.Bullish && i >= pending+bearConfirmWeeks {
			if weekly[i].Close < weekly[pending].Close {
				dir, flipIdx = detectors.Bearish, i
			}
			pending = -1
		}
	}
	if !dirSet {
		return 0, Anchors{}, time.Time{}, false // struktur ter-filter belum terbentuk (leg semua < minLeg)
	}
	return dir, ComputeAnchorsMin(weekly, weekly[len(weekly)-1].Close, minLeg), weekly[flipIdx].Time.Add(7 * 24 * time.Hour), true
}

// legDir arah leg dari a ke b (low→high = bullish).
func legDir(a, b detectors.Swing) detectors.Direction {
	if a.Kind == detectors.SwingLow && b.Kind == detectors.SwingHigh {
		return detectors.Bullish
	}
	if a.Kind == detectors.SwingHigh && b.Kind == detectors.SwingLow {
		return detectors.Bearish
	}
	// fallback by price
	if b.Price >= a.Price {
		return detectors.Bullish
	}
	return detectors.Bearish
}

// DailyBias menentukan bias daily (Section B.2): break swing terakhir dengan
// CANDLE CLOSE (bukan wick). Bullish = close break swing high terakhir; bearish
// = close break swing low terakhir. Layer KONFIRMASI di bawah weekly OF.
// Wrapper kompat minLeg=0 (tanpa filter magnitude — perilaku lama).
func DailyBias(daily []data.Candle) (dir detectors.Direction, ok bool) {
	return DailyBiasMin(daily, 0)
}

// DailyBiasMin = DailyBias dengan filter magnitude swing (audit 2026-06-02:
// DailyBias unfiltered flip 17×/tahun, sisi bearish agreement 44% < koin —
// penyakit sama dengan WeeklyOF). minLeg>0: hanya swing pivot zigzag ter-filter
// (leg >= minLeg, biasanya N×ATR daily) yang dipakai sebagai acuan close-break,
// di-replay streaming anti-lookahead. 0 = perilaku lama.
func DailyBiasMin(daily []data.Candle, minLeg float64) (dir detectors.Direction, ok bool) {
	dir, _, ok = DailyBiasFull(daily, minLeg)
	return dir, ok
}

// DailyBiasFull = DailyBiasMin + flipTime = waktu COMPLETION candle daily tempat
// nilai bias terakhir BERUBAH ARAH (bukan re-konfirmasi arah sama). Dipakai
// engine utk gate MinBiasAgeDays — temuan bedah 2026-06-02: bias umur <=5 hari
// (baru flip) = whipsaw belum matang (PF0.77, bearish-fresh win 14.5%/−25R).
// Wrapper compat (drop refLevel) — pakai DailyBiasRef bila butuh refLevel.
func DailyBiasFull(daily []data.Candle, minLeg float64) (dir detectors.Direction, flipTime time.Time, ok bool) {
	dir, flipTime, _, ok = DailyBiasRef(daily, minLeg)
	return
}

// DailyBiasRef = DailyBiasFull + refLevel = level swing high/low yang di-break
// candle close saat bias flip terakhir. Dipakai engine untuk DetectSkenarioB
// (B.3 Skenario B: apakah intermediate swing atau macro LTH/LTL).
func DailyBiasRef(daily []data.Candle, minLeg float64) (dir detectors.Direction, flipTime time.Time, refLevel float64, ok bool) {
	if minLeg > 0 {
		return dailyBiasFilteredRef(daily, minLeg)
	}
	return dailyBias0Ref(daily)
}

// dailyBias0Ref = implementasi DailyBiasRef jalur unfiltered (minLeg=0).
func dailyBias0Ref(daily []data.Candle) (dir detectors.Direction, flipTime time.Time, refLevel float64, ok bool) {
	swings := detectors.DetectSwings(daily)
	if len(swings) == 0 {
		return 0, time.Time{}, 0, false
	}
	type cs struct {
		s         detectors.Swing
		confirmAt int
	}
	cands := make([]cs, len(swings))
	for i, s := range swings {
		cands[i] = cs{s: s, confirmAt: s.Index + 1}
	}
	var lastLow, lastHigh detectors.Swing
	var hasLow, hasHigh, set bool
	ci := 0
	flipIdx := 0
	for i := 0; i < len(daily); i++ {
		for ci < len(cands) && cands[ci].confirmAt == i {
			if cands[ci].s.Kind == detectors.SwingLow {
				lastLow, hasLow = cands[ci].s, true
			} else {
				lastHigh, hasHigh = cands[ci].s, true
			}
			ci++
		}
		if hasHigh && daily[i].Close > lastHigh.Price {
			if !set || dir != detectors.Bullish {
				flipIdx = i
				refLevel = lastHigh.Price
			}
			dir, set = detectors.Bullish, true
		}
		if hasLow && daily[i].Close < lastLow.Price {
			if !set || dir != detectors.Bearish {
				flipIdx = i
				refLevel = lastLow.Price
			}
			dir, set = detectors.Bearish, true
		}
	}
	if !set {
		return dir, time.Time{}, 0, false
	}
	return dir, daily[flipIdx].Time.Add(24 * time.Hour), refLevel, true
}

// dailyBiasFilteredRef = implementasi DailyBiasRef jalur filtered (minLeg>0):
// close-break terhadap pivot zigzag ter-filter (streaming ZigzagMin,
// anti-lookahead — semantik identik dengan weeklyOFFiltered).
func dailyBiasFilteredRef(daily []data.Candle, minLeg float64) (dir detectors.Direction, flipTime time.Time, refLevel float64, ok bool) {
	swings := detectors.DetectSwings(daily)
	if len(swings) == 0 {
		return 0, time.Time{}, 0, false
	}
	var piv []detectors.Swing
	push := func(s detectors.Swing) {
		if len(piv) == 0 {
			piv = append(piv, s)
			return
		}
		last := &piv[len(piv)-1]
		if s.Kind == last.Kind {
			if (s.Kind == detectors.SwingHigh && s.Price > last.Price) ||
				(s.Kind == detectors.SwingLow && s.Price < last.Price) {
				*last = s
			}
			return
		}
		leg := s.Price - last.Price
		if leg < 0 {
			leg = -leg
		}
		if leg >= minLeg {
			piv = append(piv, s)
		}
	}
	lastPivot := func(kind detectors.SwingKind) (detectors.Swing, bool) {
		for i := len(piv) - 1; i >= 0; i-- {
			if piv[i].Kind == kind {
				return piv[i], true
			}
		}
		return detectors.Swing{}, false
	}

	si := 0
	var set bool
	flipIdx := 0
	for i := 0; i < len(daily); i++ {
		for si < len(swings) && swings[si].Index+1 == i {
			push(swings[si])
			si++
		}
		if high, has := lastPivot(detectors.SwingHigh); has && daily[i].Close > high.Price {
			if !set || dir != detectors.Bullish {
				flipIdx = i
				refLevel = high.Price
			}
			dir, set = detectors.Bullish, true
		}
		if low, has := lastPivot(detectors.SwingLow); has && daily[i].Close < low.Price {
			if !set || dir != detectors.Bearish {
				flipIdx = i
				refLevel = low.Price
			}
			dir, set = detectors.Bearish, true
		}
	}
	if !set {
		return dir, time.Time{}, 0, false
	}
	return dir, daily[flipIdx].Time.Add(24 * time.Hour), refLevel, true
}

// DetectSkenarioB mendeteksi apakah kondisi saat ini = B.3 Skenario B (EARLY
// phase): daily bias sudah flip ke arah BERLAWANAN weekly OF definitif, TAPI
// dailyRefLevel (swing yang di-break di daily, dari DailyBiasRef) masih
// di bawah LTH / di atas LTL — artinya ini "intermediate swing break",
// bukan definitif flip (LTH/LTL macro belum tersentuh).
//
// Contoh BULLISH: weeklyOF=BEARISH, daily flip BULLISH close-break swing high
// 3100, LTH=3200 → 3100 < 3200 → intermediate → isEarly=true, earlyDir=BULLISH.
//
// Returns (isEarly, earlyDir). isEarly=false kalau searah, atau refLevel
// sudah >= LTH / <= LTL (macro level sudah tembus → WeeklyOF harusnya sudah flip).
func DetectSkenarioB(anchors Anchors, weeklyOFDir, dailyBiasDir detectors.Direction, dailyRefLevel float64) (isEarly bool, earlyDir detectors.Direction) {
	if weeklyOFDir == dailyBiasDir {
		return false, 0 // searah = OF berjalan normal, bukan flip
	}
	switch dailyBiasDir {
	case detectors.Bullish:
		// weekly bearish, daily bullish → potential bullish flip
		// Skenario B: refLevel (intermediate swing high) masih di bawah LTH
		if !anchors.HasLTH {
			return false, 0 // tidak ada LTH = edge case, skip
		}
		if dailyRefLevel < anchors.LTH.Price {
			return true, detectors.Bullish
		}
	case detectors.Bearish:
		// weekly bullish, daily bearish → potential bearish flip
		if !anchors.HasLTL {
			return false, 0
		}
		if dailyRefLevel > anchors.LTL.Price {
			return true, detectors.Bearish
		}
	}
	return false, 0
}
