package detectors

import "testing"

func filterIT(its []Intermediate, kind ITKind) []Intermediate {
	var out []Intermediate
	for _, it := range its {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

// ITL fast/early (narasi user): STL_kiri(100) → STH_between(120) → STL_ITL(90,
// lebih rendah) → rally pertama LANGSUNG break 120 ke atas tanpa STL kanan.
var itlFastHL = [][2]float64{
	{105, 101}, // 0
	{104, 100}, // 1  STL_kiri 100
	{115, 106}, // 2
	{120, 112}, // 3  STH_between 120 (ref)
	{110, 95},  // 4
	{98, 90},   // 5  STL_ITL 90
	{125, 100}, // 6  rally break 120 → konfirmasi (fast)
	{122, 118}, // 7
	{120, 110}, // 8
}

func TestDetectITL_FastEarly(t *testing.T) {
	its := filterIT(DetectIntermediates(candlesFromHL(itlFastHL), BreakWick), ITLow)
	if len(its) != 1 {
		t.Fatalf("mau 1 ITL, dapat %d (%+v)", len(its), its)
	}
	it := its[0]
	if it.Type != ITFastEarly {
		t.Errorf("type = %s, mau fast_early", it.Type)
	}
	if it.Pivot.Price != 90 {
		t.Errorf("pivot (trough) = %g, mau 90", it.Pivot.Price)
	}
	if it.BrokenSwing.Price != 120 {
		t.Errorf("broken STH = %g, mau 120", it.BrokenSwing.Price)
	}
	if it.ConfirmIndex != 6 {
		t.Errorf("confirm index = %d, mau 6", it.ConfirmIndex)
	}
}

// ITL standard: rally pertama (115) GAGAL break 120, terbentuk STL kanan (95),
// baru rally kedua break 120.
var itlStdHL = [][2]float64{
	{105, 101}, // 0
	{104, 100}, // 1  STL_kiri 100
	{115, 106}, // 2
	{120, 112}, // 3  STH_ref 120
	{110, 95},  // 4
	{98, 90},   // 5  STL_ITL 90
	{115, 100}, // 6  rally pertama 115 (<120, gagal)
	{112, 95},  // 7  STL kanan 95 (higher low)
	{125, 100}, // 8  rally kedua break 120 → konfirmasi (standard)
	{122, 118}, // 9
}

func TestDetectITL_Standard(t *testing.T) {
	its := filterIT(DetectIntermediates(candlesFromHL(itlStdHL), BreakWick), ITLow)
	if len(its) != 1 {
		t.Fatalf("mau 1 ITL, dapat %d (%+v)", len(its), its)
	}
	it := its[0]
	if it.Type != ITStandard {
		t.Errorf("type = %s, mau standard", it.Type)
	}
	if it.Pivot.Price != 90 || it.BrokenSwing.Price != 120 {
		t.Errorf("pivot/broken = %g/%g, mau 90/120", it.Pivot.Price, it.BrokenSwing.Price)
	}
	if it.ConfirmIndex != 8 {
		t.Errorf("confirm index = %d, mau 8", it.ConfirmIndex)
	}
}

// ITH fast/early (mirror): STH_kiri(100) → STL_a(88) → STH_peak(110, lebih
// tinggi) → drop LANGSUNG break STL_a(88) ke bawah tanpa STH kanan.
var ithFastHL = [][2]float64{
	{96, 92},   // 0
	{100, 94},  // 1  STH_kiri 100
	{97, 88},   // 2  STL_a 88 (ref)
	{110, 100}, // 3  STH_peak 110
	{105, 80},  // 4  drop break 88 → konfirmasi (fast)
	{90, 82},   // 5
}

func TestDetectITH_FastEarly(t *testing.T) {
	its := filterIT(DetectIntermediates(candlesFromHL(ithFastHL), BreakWick), ITHigh)
	if len(its) != 1 {
		t.Fatalf("mau 1 ITH, dapat %d (%+v)", len(its), its)
	}
	it := its[0]
	if it.Type != ITFastEarly {
		t.Errorf("type = %s, mau fast_early", it.Type)
	}
	if it.Pivot.Price != 110 {
		t.Errorf("pivot (peak) = %g, mau 110", it.Pivot.Price)
	}
	if it.BrokenSwing.Price != 88 {
		t.Errorf("broken STL = %g, mau 88", it.BrokenSwing.Price)
	}
	if it.ConfirmIndex != 4 {
		t.Errorf("confirm index = %d, mau 4", it.ConfirmIndex)
	}
}

// Body break: pakai itlFastHL tapi BreakBody. Candle idx6 close = mid(125,100)=112.5
// < 120 → TIDAK break by body di idx6. Konfirmasi mundur / tidak ada.
func TestDetectITL_BodyBreak(t *testing.T) {
	wick := filterIT(DetectIntermediates(candlesFromHL(itlFastHL), BreakWick), ITLow)
	body := filterIT(DetectIntermediates(candlesFromHL(itlFastHL), BreakBody), ITLow)
	if len(wick) != 1 {
		t.Fatalf("wick: mau 1 ITL")
	}
	// Body lebih strict: idx6 close 112.5 < 120, candle berikutnya juga tak close >120 →
	// tidak ada konfirmasi body untuk dataset ini.
	if len(body) != 0 {
		t.Errorf("body: mau 0 ITL (tak ada close>120), dapat %d (%+v)", len(body), body)
	}
}

func TestRealData_H1_Intermediates_Smoke(t *testing.T) {
	candles := loadH1OrSkip(t)
	its := DetectIntermediates(candles, DefaultBreakType)
	if len(its) == 0 {
		t.Fatal("harusnya ada ITH/ITL di data nyata")
	}
	var nITH, nITL, nFast int
	for _, it := range its {
		if it.Kind == ITHigh {
			nITH++
		} else {
			nITL++
		}
		if it.Type == ITFastEarly {
			nFast++
		}
		// invariant: ITL break STH ke atas (broken > pivot); ITH break STL ke bawah (broken < pivot)
		if it.Kind == ITLow && it.BrokenSwing.Price <= it.Pivot.Price {
			t.Fatalf("ITL invalid: broken %.2f <= pivot %.2f", it.BrokenSwing.Price, it.Pivot.Price)
		}
		if it.Kind == ITHigh && it.BrokenSwing.Price >= it.Pivot.Price {
			t.Fatalf("ITH invalid: broken %.2f >= pivot %.2f", it.BrokenSwing.Price, it.Pivot.Price)
		}
	}
	t.Logf("H1: %d intermediate (ITH=%d ITL=%d), fast_early=%d (%.0f%%)",
		len(its), nITH, nITL, nFast, 100*float64(nFast)/float64(len(its)))
}
