package detectors

import (
	"time"

	"forex-backtest/internal/data"
)

// NYLocation = America/New_York (DST-aware). Section D pakai jam NY (UTC-4/-5).
func NYLocation() (*time.Location, error) { return time.LoadLocation("America/New_York") }

// SessionKind = quarter daily (D.1, jam NY).
type SessionKind int

const (
	Asia   SessionKind = iota // Q1 18:00-00:00
	London                    // Q2 00:00-06:00
	NYAM                      // Q3 06:00-12:00
	PM                        // Q4 12:00-18:00
)

func (s SessionKind) String() string {
	switch s {
	case Asia:
		return "asia"
	case London:
		return "london"
	case NYAM:
		return "ny_am"
	default:
		return "pm"
	}
}

// Session mengklasifikasi candle ke quarter daily berdasarkan jam NY (D.1).
func Session(t time.Time, loc *time.Location) SessionKind {
	h := t.In(loc).Hour()
	switch {
	case h >= 18:
		return Asia
	case h < 6:
		return London
	case h < 12:
		return NYAM
	default:
		return PM
	}
}

// LondonQuarter mengembalikan sub-kuartal QT per Session untuk sesi London
// (00:00-06:00 NY, dibagi 4 kuartal @1.5 jam — Phase 2). Pola manipulasi London:
//
//	Q1 00:00-01:30  Akumulasi
//	Q2 01:30-03:00  Manipulasi — sweep likuiditas Asia (swing low/high) BERLAWANAN OF
//	Q3 03:00-04:30  Distribusi searah OF (skenario AMDX)
//	Q4 04:30-06:00  Distribusi searah OF (skenario XAMD) / X
//
// Aman entry minimal Q3 (setelah manipulasi Q2 selesai). Return 1-4; 0 kalau t
// di luar sesi London (jam NY).
func LondonQuarter(t time.Time, loc *time.Location) int {
	nt := t.In(loc)
	mins := nt.Hour()*60 + nt.Minute()
	if mins >= 360 { // >= 06:00 NY (di luar London ke arah NY AM/PM)
		return 0
	}
	switch {
	case mins < 90:
		return 1
	case mins < 180:
		return 2
	case mins < 270:
		return 3
	default:
		return 4
	}
}

// SessionStart = jam NY pembuka SESI yang memuat t (18/00/06/12 NY). Tiap sesi
// 6 jam: Asia 18:00, London 00:00, NY-AM 06:00, PM 12:00. DST-aware (konstruksi
// di loc). Dipakai QT per-session (Phase 2): Q1 sesi = [SessionStart, +90m).
func SessionStart(t time.Time, loc *time.Location) time.Time {
	nt := t.In(loc)
	var sh int
	switch h := nt.Hour(); {
	case h >= 18:
		sh = 18
	case h < 6:
		sh = 0
	case h < 12:
		sh = 6
	default:
		sh = 12
	}
	y, m, d := nt.Date()
	return time.Date(y, m, d, sh, 0, 0, 0, loc)
}

// SessionQuarter = sub-kuartal QT 1-4 di dalam SESI yang memuat t (tiap quarter
// 1.5 jam). Generalisasi LondonQuarter ke semua sesi (Asia/London/NY-AM/PM).
// Q1 = akumulasi, Q2 = manipulasi (sweep), Q3/Q4 = distribusi/ekspansi.
func SessionQuarter(t time.Time, loc *time.Location) int {
	mins := int(t.In(loc).Sub(SessionStart(t, loc)).Minutes())
	q := mins/90 + 1
	if q < 1 {
		q = 1
	}
	if q > 4 {
		q = 4
	}
	return q
}

// TradingDayStart = 18:00 NY pembuka trading day yang memuat t
// (daily_candle_anchor_hour_ny: 18). Trading day = 18:00 NY → 18:00 berikutnya.
func TradingDayStart(t time.Time, loc *time.Location) time.Time {
	nt := t.In(loc)
	y, m, d := nt.Date()
	start := time.Date(y, m, d, 18, 0, 0, 0, loc)
	if nt.Hour() < 18 {
		start = start.AddDate(0, 0, -1)
	}
	return start
}

// TradingWeekday = hari (NY) dari trading day yang memuat t (untuk weekly QT).
func TradingWeekday(t time.Time, loc *time.Location) time.Weekday {
	return TradingDayStart(t, loc).Weekday()
}

// MidnightOpenTime = 00:00 NY dari trading day yang memuat t (= TradingDayStart
// + 6 jam; trading day ber-anchor 18:00 NY). Pertemuan 8: Midnight Open (MO) =
// open candle 00:00 NY, divider premium/discount untuk hari itu.
func MidnightOpenTime(t time.Time, loc *time.Location) time.Time {
	return TradingDayStart(t, loc).Add(6 * time.Hour)
}

// MidnightOpenPrice = harga open candle H1 tepat di MidnightOpenTime trading
// day yang memuat t. ok=false bila:
//   - t masih SEBELUM 00:00 NY (sesi Asia 18:00–00:00 — MO hari itu belum
//     terbentuk), atau
//   - candle 00:00 NY tidak ada di cache (toleransi gap data: pakai candle H1
//     pertama setelah moTime, maksimal +2 jam).
//
// Daily candle ber-anchor 18:00 NY, jadi MO TIDAK bisa diambil dari open daily
// — harus dari H1.
func MidnightOpenPrice(h1 []data.Candle, t time.Time, loc *time.Location) (price float64, moTime time.Time, ok bool) {
	moTime = MidnightOpenTime(t, loc)
	if t.Before(moTime) {
		return 0, moTime, false // masih Asia — MO trading day ini belum ada
	}
	// h1 terurut waktu; cari candle pertama dengan Time >= moTime (binary search
	// manual sederhana cukup — dipanggil per evaluasi, len ~26k).
	lo, hi := 0, len(h1)
	for lo < hi {
		mid := (lo + hi) / 2
		if h1[mid].Time.Before(moTime) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(h1) {
		return 0, moTime, false
	}
	c := h1[lo]
	if c.Time.After(moTime.Add(2 * time.Hour)) {
		return 0, moTime, false // gap data terlalu lebar — jangan pakai MO palsu
	}
	if c.Time.After(t) {
		return 0, moTime, false // candle MO belum termuat di window <= t
	}
	return c.Open, moTime, true
}

// Scenario = AMDX (default) vs XAMD (Q1 expensive). D.2/D.4.
type Scenario int

const (
	AMDX Scenario = iota
	XAMD
)

func (s Scenario) String() string {
	if s == XAMD {
		return "xamd"
	}
	return "amdx"
}

// Phase = A/M/D/X (+ Special untuk Jumat weekly). Tradeable kalau M atau D.
type Phase int

const (
	PhaseA Phase = iota
	PhaseM
	PhaseD
	PhaseX
	PhaseSpecial // Jumat weekly — Phase 1 tradeable normal (RR 1:2), filter news → Phase 2
)

func (p Phase) String() string {
	switch p {
	case PhaseA:
		return "A"
	case PhaseM:
		return "M"
	case PhaseD:
		return "D"
	case PhaseSpecial:
		return "Special"
	default:
		return "X"
	}
}

// Tradeable: M atau D → entry allowed. Special (Jumat) = tradeable di Phase 1
// (H-Q5c, news calendar → Phase 2). A/X → skip.
func (p Phase) Tradeable() bool { return p == PhaseM || p == PhaseD || p == PhaseSpecial }

// ClassifyDaily menentukan AMDX vs XAMD dari sesi Asia (D.2). XAMD kalau KEDUA
// syarat (AND): (1) Asia tinggalkan FVG 1H, (2) directional close
// |close_00:00 − open_18:00| >= minAtrMult × ATR_1H. asiaH1 = candle H1 sesi
// Asia (18:00→00:00). atr1h = ATR_1H @ 00:00.
//
// Thin-wrapper ke ClassifyDailyEx mode "atr" (FVG wajib) → caller lama 0-drift.
func ClassifyDaily(asiaH1 []data.Candle, atr1h, minAtrMult, minGapPips float64) Scenario {
	return ClassifyDailyEx(asiaH1, atr1h, minAtrMult, minGapPips, 0, "atr", true)
}

// ClassifyDailyEx = ClassifyDaily dengan pilihan normalisasi directional close +
// toggle syarat FVG.
//   - mode "atr" (default/lama, D.2): expansive kalau |close−open| >= minAtrMult × ATR.
//   - mode "ratio" (pertemuan 10): expansive kalau true-move / range-sesi >= rangeRatio.
//     Instruktur: |close−open|/range_sesi < 0.3 → akumulasi (A). Patokannya "apakah
//     pergerakan open→close meaningful", bukan candle individual besar. rangeRatio = ambang X.
//   - mode "range": expansive kalau high−low >= minAtrMult × ATR (total excursion).
//     Lebih sensitif dari "atr"; two-way sweep (range besar tapi close balik tengah) juga X.
//   - requireFVG (D.2, default true): X wajib disertai FVG. false = penentu murni move.
//
// Mode "atr" + requireFVG=true identik byte-for-byte dgn perilaku lama.
//
// Thin-wrapper ke ClassifyDailyG dgn prevClose=0 (anchor open) → caller lama 0-drift.
func ClassifyDailyEx(asiaH1 []data.Candle, atr1h, minAtrMult, minGapPips, rangeRatio float64, mode string, requireFVG bool) Scenario {
	return ClassifyDailyG(asiaH1, 0, atr1h, minAtrMult, minGapPips, rangeRatio, mode, requireFVG)
}

// ClassifyDailyG = ClassifyDailyEx dengan anchor true-move OPSIONAL ke close candle
// SEBELUM sesi (prevClose) alih-alih open candle pertama sesi. Tujuan: gap pembukaan
// sesi (Volume Imbalance — body candle terputus dari close sebelumnya) ikut terhitung
// sebagai displacement, bukan diabaikan. Secara aljabar identik dgn menambah gap ke
// move open→close:  (close−open) + (open−prevClose) = close − prevClose.
//
// prevClose <= 0 → anchor open candle pertama (perilaku lama, 0-drift). Hanya
// mempengaruhi `directional` (mode "atr" & "ratio"); mode "range" (high−low) tak
// terpengaruh. Gap berlawanan arah move → keduanya saling mengurangi (net displacement
// kecil = akumulasi A), sesuai logika VI.
func ClassifyDailyG(asiaH1 []data.Candle, prevClose, atr1h, minAtrMult, minGapPips, rangeRatio float64, mode string, requireFVG bool) Scenario {
	if len(asiaH1) < 2 {
		return AMDX
	}
	anchor := asiaH1[0].Open
	if prevClose > 0 {
		anchor = prevClose
	}
	directional := abs(asiaH1[len(asiaH1)-1].Close - anchor)
	var expansive bool
	switch mode {
	case "ratio":
		r := candleRange(asiaH1)
		expansive = r > 0 && directional/r >= rangeRatio
	case "range":
		expansive = candleRange(asiaH1) >= minAtrMult*atr1h
	default: // "atr"
		expansive = directional >= minAtrMult*atr1h
	}
	fvgOK := !requireFVG || len(DetectFVGs(asiaH1, minGapPips)) > 0
	if fvgOK && expansive {
		return XAMD
	}
	return AMDX
}

// candleRange = range sesi atas slice candle (max High − min Low). Dipakai mode
// "ratio" ClassifyDailyEx; konsep sama dgn engine.sessionRange tapi atas slice
// (bukan window waktu).
func candleRange(cs []data.Candle) float64 {
	if len(cs) == 0 {
		return 0
	}
	hi, lo := cs[0].High, cs[0].Low
	for _, c := range cs[1:] {
		if c.High > hi {
			hi = c.High
		}
		if c.Low < lo {
			lo = c.Low
		}
	}
	return hi - lo
}

// ClassifyWeekly mirror untuk weekly QT (D.4) — applied ke candle Senin (4H view).
// XAMD weekly kalau Senin tinggalkan FVG 4H AND |close−open| Senin >= minAtrMult×ATR_4H.
//
// Thin-wrapper ke ClassifyWeeklyEx mode "atr" (FVG wajib) → caller lama 0-drift.
func ClassifyWeekly(monday4h []data.Candle, atr4h, minAtrMult, minGapPips float64) Scenario {
	return ClassifyWeeklyEx(monday4h, atr4h, minAtrMult, minGapPips, 0, "atr", true)
}

// ClassifyWeeklyEx = ClassifyWeekly dengan pilihan normalisasi + toggle FVG (lihat ClassifyDailyEx).
// Mode "ratio" pakai true-move Senin / range candle Senin (4H view, fractal D.4).
func ClassifyWeeklyEx(monday4h []data.Candle, atr4h, minAtrMult, minGapPips, rangeRatio float64, mode string, requireFVG bool) Scenario {
	return ClassifyDailyEx(monday4h, atr4h, minAtrMult, minGapPips, rangeRatio, mode, requireFVG)
}

// DailyPhase: assignment phase per session sesuai scenario (D.3).
func DailyPhase(sc Scenario, s SessionKind) Phase {
	if sc == XAMD {
		switch s {
		case Asia:
			return PhaseX
		case London:
			return PhaseA
		case NYAM:
			return PhaseM
		default: // PM
			return PhaseD
		}
	}
	switch s { // AMDX
	case Asia:
		return PhaseA
	case London:
		return PhaseM
	case NYAM:
		return PhaseD
	default: // PM
		return PhaseX
	}
}

// WeeklyPhase: hari sebagai quarter (D.4). Jumat = Special.
func WeeklyPhase(sc Scenario, wd time.Weekday) Phase {
	if wd == time.Friday {
		return PhaseSpecial
	}
	if sc == XAMD {
		switch wd {
		case time.Monday:
			return PhaseX
		case time.Tuesday:
			return PhaseA
		case time.Wednesday:
			return PhaseM
		case time.Thursday:
			return PhaseD
		}
	} else {
		switch wd {
		case time.Monday:
			return PhaseA
		case time.Tuesday:
			return PhaseM
		case time.Wednesday:
			return PhaseD
		case time.Thursday:
			return PhaseX
		}
	}
	return PhaseX // weekend (jarang ada candle)
}

// PhaseTradeable: entry diperbolehkan hanya kalau weekly DAN daily dua-duanya
// tradeable (M/D, atau Jumat special) — D.5. PM override (session != PM)
// diterapkan terpisah oleh engine.
func PhaseTradeable(weekly, daily Phase) bool {
	return weekly.Tradeable() && daily.Tradeable()
}

// DayType = klasifikasi AMD day (D.7).
type DayType int

const (
	DayNormal DayType = iota
	DaySuspectedAccum
	DayHeavyAccum
	DayHeavyExpanding
)

func (d DayType) String() string {
	switch d {
	case DaySuspectedAccum:
		return "suspected_accum"
	case DayHeavyAccum:
		return "heavy_accum"
	case DayHeavyExpanding:
		return "heavy_expanding"
	default:
		return "normal"
	}
}

// HeavyAccumStage1 (D.7 @06:00 NY): Asia DAN London dua-duanya range < maxRangePct
// × ATR_daily → suspected. maxRangePct = heavy_accumulation_max_range_pct (0.4).
func HeavyAccumStage1(asiaRange, londonRange, atrDaily, maxRangePct float64) bool {
	thr := maxRangePct * atrDaily
	return asiaRange < thr && londonRange < thr
}

// HeavyAccumConfirm (D.7 stage3, hybrid Opsi C): confirm heavy accum HANYA kalau
// NY range < maxRangePct×ATR_daily DAN tidak ada FVG 1H baru di NY. Kalau salah
// satu gagal → NY displacement → batalkan (return false).
func HeavyAccumConfirm(nyRange, atrDaily, maxRangePct float64, nyHasNewFVG bool) bool {
	return nyRange < maxRangePct*atrDaily && !nyHasNewFVG
}

// HeavyExpanding (D.7): Asia+London directional searah > minPct×ATR_daily → caution.
// minPct = heavy_expanding_min_range_pct (1.3). asiaLondonNet = |net move Asia+London|.
func HeavyExpanding(asiaLondonNet, atrDaily, minPct float64) bool {
	return asiaLondonNet > minPct*atrDaily
}
