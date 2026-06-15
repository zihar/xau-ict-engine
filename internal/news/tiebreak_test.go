package news

import (
	"strings"
	"testing"
	"time"
)

// cpiInline membangun Release CPI dengan headline m/m PAS forecast (NETRAL),
// core m/m = coreActual (untuk menguji tiebreaker).
func cpiInline(coreActual string) Release {
	return Release{
		Name: "CPI", Kind: KindInflation, Time: time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC),
		Events: []Event{
			{Title: "CPI m/m", Forecast: "0.5%", Actual: "0.5%"}, // headline NETRAL
			{Title: "Core CPI m/m", Forecast: "0.3%", Actual: coreActual},
		},
	}
}

func TestClassifyTiebreakCore(t *testing.T) {
	// Headline NETRAL + core LUNAK (0.2 < 0.3) → tilt BULLISH, bias formal tetap NETRAL.
	soft := cpiInline("0.2%").Classify()
	if soft.Bias != BiasNeutral {
		t.Errorf("bias formal harus NETRAL (dari headline), dapat %v", soft.Bias)
	}
	if soft.Tilt != BiasBullish || soft.Tiebreak == "" {
		t.Errorf("core lunak → tilt BULLISH+alasan, dapat tilt=%v tb=%q", soft.Tilt, soft.Tiebreak)
	}

	// Headline NETRAL + core PANAS (0.4 > 0.3) → tilt BEARISH.
	hot := cpiInline("0.4%").Classify()
	if hot.Tilt != BiasBearish {
		t.Errorf("core panas → tilt BEARISH, dapat %v", hot.Tilt)
	}

	// Headline NETRAL + core PAS (0.3 == 0.3) → tak ada tilt.
	none := cpiInline("0.3%").Classify()
	if none.Tilt != BiasNeutral || none.Tiebreak != "" {
		t.Errorf("core pas → tak ada tilt, dapat tilt=%v tb=%q", none.Tilt, none.Tiebreak)
	}

	// Pesan post harus memuat tilt di header + baris tiebreaker.
	msg := BuildPostMessage(cpiInline("0.2%"), "$4.350")
	if !strings.Contains(msg, "condong tipis BULLISH gold") || !strings.Contains(msg, "Tiebreaker:") {
		t.Errorf("pesan post tak memuat tilt/tiebreaker:\n%s", msg)
	}
}

func TestClassifyNoTiebreakWhenHeadlineDecisive(t *testing.T) {
	// Headline sudah PANAS → tiebreaker TIDAK aktif (headline dominan).
	r := Release{
		Name: "CPI", Kind: KindInflation, Time: time.Now(),
		Events: []Event{
			{Title: "CPI m/m", Forecast: "0.3%", Actual: "0.6%"},      // headline ABOVE
			{Title: "Core CPI m/m", Forecast: "0.3%", Actual: "0.1%"}, // core di bawah
		},
	}
	v := r.Classify()
	if v.Bias != BiasBearish || v.Tilt != BiasNeutral || v.Tiebreak != "" {
		t.Errorf("headline tegas → tanpa tiebreaker, dapat bias=%v tilt=%v tb=%q", v.Bias, v.Tilt, v.Tiebreak)
	}
}
