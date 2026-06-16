package engine

import (
	"strings"
	"testing"
	"time"

	"xau-ict-engine/internal/detectors"
)

// sampleWatchNarrative = ScanNarrative sintetis berisi semua field yang dipakai
// FormatWatchlist (TDA/DB/AMS/QT/Array/MO/Asia).
func sampleWatchNarrative() ScanNarrative {
	at := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	return ScanNarrative{
		At:    at,
		Price: 4446.88,
		Bias:  "sell",

		WeeklyOFDir:   "bearish",
		DailyBiasNote: "Bias sell (searah weekly OF bearish). Daily bias align (umur 18 hari).",
		AMSChecked:    true,
		AMSActive:     true,
		AMSKind:       "ITH",
		AMSPivot:      4496.89,
		AMSITH:        AMSStruct{Present: true, Pivot: 4496.89, PivotTime: at, Active: true, Type: "standard"},
		AMSITL:        AMSStruct{Present: true, Pivot: 4390.00, PivotTime: at.Add(-2 * time.Hour), Active: false, Type: "standard"},
		Swings: []SwingMark{
			{Price: 4350.00, Kind: "ITL"},
			{Price: 4496.89, Kind: "ITH"},
			{Price: 4390.00, Kind: "ITL"},
		},
		QTSession:     "london",
		QTLondonQ:     2,
		QTSkenario:    "amdx",
		QTWeeklyPhase: "M",
		QTDailyPhase:  "M",
		QTDayType:     "normal",
		QTWeekday:     "Tuesday",
		QTSessionQ:    2,
		QTSessName:    "London",
		QTSessQStartNY: "01:30",
		QTSessQEndNY:   "03:00",
		QTSessPhase:    "M",
		QTDailyScenario:  "AMDX",
		QTDailySessPhase: "M",
		QTQ1Scenario:     "A",

		MonthlyScenario: "amdx",
		MonthlyPhase:    "D",
		MonthlyWeekNum:  3,

		HasMO:        true,
		MOPrice:      4484.62,
		AsiaClosed:   true,
		AsiaScenario: detectors.AMDX,

		RelEquals: []RelEqualMark{
			{Kind: "REH", TF: detectors.TFH4, Level: 4536.82, Count: 3, Distance: 89.94},
			{Kind: "REL", TF: detectors.TFH1, Level: 4420.00, Count: 2, Distance: 26.88},
		},
		ArrayByTF: []TFArray{
			{TF: detectors.TFH1, PDRs: []PDRMark{
				{Kind: "FVG", Dir: "bullish", Bottom: 4439.19, Top: 4468.82}, // → BISI
				{Kind: "FVG", Dir: "bearish", Bottom: 4474.45, Top: 4481.86}, // → SIBI
				{Kind: "VI", Dir: "bearish", Bottom: 4450.00, Top: 4450.00},  // tipis → satu harga
				{Kind: "BB", Dir: "bearish", Bottom: 4418.26, Top: 4439.19},
				{Kind: "BB", Dir: "bearish", Bottom: 4418.26, Top: 4439.19}, // duplikat → dedup
			}},
			{TF: detectors.TFH4, PDRs: []PDRMark{
				{Kind: "FVGBreak", Dir: "bearish", Bottom: 4439.19, Top: 4494.77},
			}},
		},
	}
}

func TestFormatWatchlistSections(t *testing.T) {
	out := strings.Join(FormatWatchlist(sampleWatchNarrative(), "XAU_USD", 88), "\n")
	wants := []string{
		// Header — instrumen + harga, lalu waktu NY.
		"📍 Watchlist XAU_USD · 4446.88",
		// TDA + Bias ringkas (tanpa alasan panjang).
		"TDA: Bearish · Bias: Sell",
		// AMS dua arah, ◀ di sisi searah bias (ITH).
		"AMS ITL 4390.00 broken",
		"AMS ITH 4496.89 aktif ◀",
		// QT berurutan Bulanan → Mingguan → Daily → Session.
		"QT Bulanan  Q1=A → AMDX",
		"QT Mingguan Sen=A → AMDX",
		"QT Daily    London = M → AMDX",
		"QT Session  Q2 London 01:30–03:00 = M",
		// Komponen Array per-TF — daftar VERTIKAL grup-per-TF: header "<TF>:" lalu
		// "- <label>" per komponen. Non-FVG dapat tag Buy/Sell; FVG tetap BISI/SIBI.
		"1H:",
		"- BB Sell 4418.26–4439.19",
		"- BISI 4439.19–4468.82",
		"- VI Sell 4450.00",
		"- REL 4420.00 (2×)",
		"4H:",
		"- FVGBreak Sell 4439.19–4494.77",
		"- REH 4536.82 (3×)",
		// MO terpisah di paling bawah.
		"MO: 4484.62 — DI BAWAH (discount)",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("FormatWatchlist tidak memuat %q\n--- output ---\n%s", w, out)
		}
	}
	// MO harus jadi baris TERAKHIR (terpisah sendiri di bawah).
	lines := FormatWatchlist(sampleWatchNarrative(), "XAU_USD", 88)
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "MO:") {
		t.Errorf("baris terakhir = %q, mau berawalan \"MO:\"", last)
	}
	// Dedup: "- BB Sell 4418.26–4439.19" hanya sekali walau ada dua PDR identik.
	if n := strings.Count(out, "- BB Sell 4418.26–4439.19"); n != 1 {
		t.Errorf("dedup gagal: BB identik muncul %d kali (mau 1)", n)
	}
}

func TestFormatWatchlistNoWrap(t *testing.T) {
	// Width 0 (jalur Telegram teks-biasa): tak ada hard-wrap. Tiap seksi non-Array
	// jadi SATU baris logis; tiap komponen Array berdiri sendiri di baris "- ".
	lines := FormatWatchlist(sampleWatchNarrative(), "XAUUSD", 0)
	nQT, nDash, sawTFHeader := 0, 0, false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "QT "):
			nQT++
		case strings.HasPrefix(l, "- "):
			nDash++
		case l == "1H:" || l == "4H:" || l == "D:":
			sawTFHeader = true
		}
	}
	if nQT != 4 { // Bulanan/Mingguan/Daily/Session, masing-masing 1 baris (tak ke-wrap)
		t.Errorf("QT = %d baris (mau 4, tanpa hard-wrap)\n--- output ---\n%s", nQT, strings.Join(lines, "\n"))
	}
	if !sawTFHeader {
		t.Errorf("tidak ada header TF (1H:/4H:/D:)\n--- output ---\n%s", strings.Join(lines, "\n"))
	}
	if nDash < 5 { // 1H: BB/BISI/VI/REL + 4H: FVGBreak/REH
		t.Errorf("baris komponen \"- \" = %d (mau ≥5)\n--- output ---\n%s", nDash, strings.Join(lines, "\n"))
	}
}

func TestFormatWatchlistNarrowWrap(t *testing.T) {
	// Width 42 (terminal sempit): seksi non-Array tetap ≤ 42 rune (komponen Array
	// kini selalu satu baris pendek, jadi tak perlu wrap).
	lines := FormatWatchlist(sampleWatchNarrative(), "XAU_USD", 42)
	for _, l := range lines {
		if r := len([]rune(l)); r > 42 {
			t.Errorf("baris %q = %d rune > 42 (tidak ter-wrap)", l, r)
		}
	}
}
