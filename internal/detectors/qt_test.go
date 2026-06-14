package detectors

import (
	"testing"
	"time"
)

func TestSessionClassify(t *testing.T) {
	loc, err := NYLocation()
	if err != nil {
		t.Skipf("tzdata NY tidak tersedia: %v", err)
	}
	cases := []struct {
		hourNY int
		want   SessionKind
	}{
		{18, Asia}, {23, Asia}, {0, London}, {5, London},
		{6, NYAM}, {11, NYAM}, {12, PM}, {17, PM},
	}
	for _, c := range cases {
		tm := time.Date(2026, 5, 4, c.hourNY, 30, 0, 0, loc) // 2026-05-04 = Senin
		if got := Session(tm, loc); got != c.want {
			t.Errorf("Session(%02d:30 NY) = %s, mau %s", c.hourNY, got, c.want)
		}
	}
}

func TestTradingDayAnchor(t *testing.T) {
	loc, err := NYLocation()
	if err != nil {
		t.Skip("no tz")
	}
	// Senin 20:00 NY → trading day mulai Senin 18:00 → weekday Senin
	tm := time.Date(2026, 5, 4, 20, 0, 0, 0, loc)
	if wd := TradingWeekday(tm, loc); wd != time.Monday {
		t.Errorf("weekday(Senin 20:00) = %s, mau Monday", wd)
	}
	// Selasa 03:00 NY → masih trading day Senin (mulai Senin 18:00) → Senin
	tm2 := time.Date(2026, 5, 5, 3, 0, 0, 0, loc)
	if wd := TradingWeekday(tm2, loc); wd != time.Monday {
		t.Errorf("weekday(Selasa 03:00) = %s, mau Monday (trading day Senin)", wd)
	}
}

func TestPhaseTables(t *testing.T) {
	// AMDX daily: Asia=A, London=M, NYAM=D, PM=X
	if DailyPhase(AMDX, London) != PhaseM || DailyPhase(AMDX, NYAM) != PhaseD ||
		DailyPhase(AMDX, Asia) != PhaseA || DailyPhase(AMDX, PM) != PhaseX {
		t.Error("AMDX daily phase table salah")
	}
	// XAMD daily: Asia=X, London=A, NYAM=M, PM=D
	if DailyPhase(XAMD, NYAM) != PhaseM || DailyPhase(XAMD, PM) != PhaseD ||
		DailyPhase(XAMD, Asia) != PhaseX || DailyPhase(XAMD, London) != PhaseA {
		t.Error("XAMD daily phase table salah")
	}
	// Weekly AMDX: Sen=A, Sel=M, Rab=D, Kam=X, Jum=Special
	if WeeklyPhase(AMDX, time.Tuesday) != PhaseM || WeeklyPhase(AMDX, time.Wednesday) != PhaseD ||
		WeeklyPhase(AMDX, time.Friday) != PhaseSpecial {
		t.Error("AMDX weekly phase table salah")
	}
	// Kombinasi: Selasa(M) + NYAM AMDX(D) → tradeable; Senin(A) + NYAM(D) → tidak
	if !PhaseTradeable(WeeklyPhase(AMDX, time.Tuesday), DailyPhase(AMDX, NYAM)) {
		t.Error("Selasa+NYAM harusnya tradeable")
	}
	if PhaseTradeable(WeeklyPhase(AMDX, time.Monday), DailyPhase(AMDX, NYAM)) {
		t.Error("Senin(A)+NYAM harusnya TIDAK tradeable (weekly A override)")
	}
}

func TestClassifyDaily(t *testing.T) {
	// Asia trending + ada FVG → XAMD. 6 candle H1 naik kuat dgn gap.
	asia := candlesFromHL([][2]float64{
		{100.5, 100.0}, // open 100.25
		{101.0, 100.6},
		{102.5, 101.8}, // gap dari candle[0].high 100.5 → FVG bullish (low 101.8>100.5)
		{103.0, 102.2},
		{104.0, 103.0},
		{105.0, 104.0}, // close 104.5 ; net move ~4.25
	})
	atr1h := 1.0
	if got := ClassifyDaily(asia, atr1h, 1.5, 5); got != XAMD {
		t.Errorf("Asia trending+FVG → %s, mau xamd", got)
	}
	// Asia choppy, close balik ke open, no strong move → AMDX
	chop := candlesFromHL([][2]float64{
		{101, 100}, {101, 99}, {100.5, 99.5}, {101, 100}, {100.8, 99.8}, {100.4, 99.6},
	})
	if got := ClassifyDaily(chop, atr1h, 1.5, 5); got != AMDX {
		t.Errorf("Asia choppy → %s, mau amdx", got)
	}
}

func TestClassifyDailyRatioMode(t *testing.T) {
	// Asia trending kuat: open 100.25 → close 104.5 (move 4.25), range 5.0,
	// ratio 0.85 ≥ 0.5 + FVG → XAMD juga di mode ratio.
	trending := candlesFromHL([][2]float64{
		{100.5, 100.0}, {101.0, 100.6}, {102.5, 101.8},
		{103.0, 102.2}, {104.0, 103.0}, {105.0, 104.0},
	})
	if got := ClassifyDailyEx(trending, 1.0, 1.5, 5, 0.5, "ratio", true); got != XAMD {
		t.Errorf("ratio: Asia trending kuat → %s, mau xamd", got)
	}

	// Kasus PEMBEDA (poin pertemuan 10): range besar tapi true-move kecil =
	// AKUMULASI. open 100 → close 103 (move 3), range 9.5 → ratio 0.32 < 0.5.
	// Ada FVG (i=1: low 102 > high 100.5). atr1h=1.0 → thr 1.5; move 3 ≥ 1.5.
	bigRangeSmallMove := candlesFromHL([][2]float64{
		{100.5, 99.5}, {108.0, 107.0}, {109.0, 102.0},
		{106.0, 104.0}, {104.0, 102.0}, {103.5, 102.5},
	})
	if got := ClassifyDailyEx(bigRangeSmallMove, 1.0, 1.5, 5, 0.5, "atr", true); got != XAMD {
		t.Errorf("atr: move>thr → %s, mau xamd (sanity detektor lama tetap X)", got)
	}
	if got := ClassifyDailyEx(bigRangeSmallMove, 1.0, 1.5, 5, 0.5, "ratio", true); got != AMDX {
		t.Errorf("ratio: range besar+move kecil → %s, mau amdx (true-move kecil = akumulasi)", got)
	}
}

func TestClassifyDailyGapAnchor(t *testing.T) {
	atr1h := 1.0 // threshold = 1.5×1.0 = 1.5 ; requireFVG=false → X iff true-move ≥ thr
	// Sesi DATAR (open≈close, true-move ~0) tapi BUKA dgn gap besar dari close
	// sebelumnya = Volume Imbalance. Anchor-open → A; anchor-prevClose → X.
	flat := candlesFromHL([][2]float64{
		{110.5, 109.5}, {110.4, 109.6}, {110.6, 109.4},
		{110.5, 109.5}, {110.3, 109.7}, {110.5, 109.5}, // open=close=110
	})
	if got := ClassifyDailyG(flat, 0, atr1h, 1.5, 5, 0, "atr", false); got != AMDX {
		t.Errorf("anchor-open sesi datar (move 0) → %s, mau amdx", got)
	}
	if got := ClassifyDailyG(flat, 108, atr1h, 1.5, 5, 0, "atr", false); got != XAMD {
		t.Errorf("gap-anchor: gap +2 di pembukaan (|110−108|=2≥1.5) → %s, mau xamd", got)
	}
	// Gap LAWAN-ARAH net move → saling mengurangi (net displacement kecil = akumulasi).
	// Sesi naik 110→112 (move 2), tapi buka gap-turun dari prevClose 113 → |112−113|=1.
	rising := candlesFromHL([][2]float64{
		{110.5, 109.5}, {110.8, 110.0}, {111.2, 110.5},
		{111.6, 111.0}, {112.0, 111.4}, {112.5, 111.5}, // open=110, close=112
	})
	if got := ClassifyDailyG(rising, 0, atr1h, 1.5, 5, 0, "atr", false); got != XAMD {
		t.Errorf("anchor-open sesi naik (move 2≥1.5) → %s, mau xamd", got)
	}
	if got := ClassifyDailyG(rising, 113, atr1h, 1.5, 5, 0, "atr", false); got != AMDX {
		t.Errorf("gap lawan-arah (|112−113|=1<1.5) → %s, mau amdx", got)
	}
	// prevClose ≤ 0 = anchor open (parity ClassifyDailyEx, 0-drift).
	if ClassifyDailyG(rising, 0, atr1h, 1.5, 5, 0, "atr", false) !=
		ClassifyDailyEx(rising, atr1h, 1.5, 5, 0, "atr", false) {
		t.Error("ClassifyDailyG(prevClose=0) harus identik ClassifyDailyEx")
	}
}

func TestClassifyDailyRequireFVG(t *testing.T) {
	// Asia bergerak kuat (move>thr & ratio tinggi) TAPI tanpa FVG: candle naik
	// merangkak dgn body lebar yg saling tumpang-tindih → tak ada gap/imbalance.
	// open 101 → close 103.5 (move 2.5 ≥ thr 1.5); range 4.5 → ratio 0.56 ≥ 0.5.
	// requireFVG=true → A (syarat-1 gagal); requireFVG=false → X (murni true-move).
	noFVG := candlesFromHL([][2]float64{
		{102, 100}, {102.5, 100.5}, {103, 101}, {103.5, 101.5}, {104, 102}, {104.5, 102.5},
	})
	// Sanity: tak ada FVG di slice ini.
	if len(DetectFVGs(noFVG, 5)) != 0 {
		t.Fatalf("fixture noFVG harusnya 0 FVG, dapat %d", len(DetectFVGs(noFVG, 5)))
	}
	if got := ClassifyDailyEx(noFVG, 1.0, 1.5, 5, 0.5, "atr", true); got != AMDX {
		t.Errorf("requireFVG=true tanpa FVG → %s, mau amdx", got)
	}
	if got := ClassifyDailyEx(noFVG, 1.0, 1.5, 5, 0.5, "atr", false); got != XAMD {
		t.Errorf("requireFVG=false + move kuat → %s, mau xamd (penentu murni true-move)", got)
	}
}

func TestDayTypeHelpers(t *testing.T) {
	atr := 10.0
	if !HeavyAccumStage1(3, 3, atr, 0.4) { // 3 < 4 dua-duanya
		t.Error("Asia&London kecil → suspected accum")
	}
	if HeavyAccumStage1(5, 3, atr, 0.4) { // 5 >= 4
		t.Error("salah satu >=40% → bukan accum")
	}
	if !HeavyAccumConfirm(2, atr, 0.4, false) { // NY kecil + no FVG → confirm
		t.Error("NY kecil+no FVG → confirm accum")
	}
	if HeavyAccumConfirm(2, atr, 0.4, true) { // ada FVG → batal (displacement)
		t.Error("NY ada FVG → batal accum")
	}
	if !HeavyExpanding(14, atr, 1.3) { // 14 > 13
		t.Error("net 14 > 130% ATR → expanding")
	}
}
