package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
)

func TestDropFVGBreak(t *testing.T) {
	in := []detectors.PDR{
		{Kind: detectors.KindFVG, Dir: detectors.Bullish, Top: 5, Bottom: 4},
		{Kind: detectors.KindFVGBreak, Dir: detectors.Bullish, Top: 5, Bottom: 4},
		{Kind: detectors.KindVI, Dir: detectors.Bullish, Top: 6, Bottom: 5},
		{Kind: detectors.KindFVGBreak, Dir: detectors.Bearish, Top: 9, Bottom: 8},
	}
	out := dropFVGBreak(in)
	for _, p := range out {
		if p.Kind == detectors.KindFVGBreak {
			t.Errorf("dropFVGBreak masih menyisakan KindFVGBreak: %+v", p)
		}
	}
	if len(out) != 2 { // KindFVG + KindVI tetap
		t.Errorf("dropFVGBreak buang yang salah: sisa %d, mau 2 (FVG+VI)", len(out))
	}
}

func TestFVGBreakAllowedTF(t *testing.T) {
	cases := []struct {
		csv  string
		tf   detectors.TFKind
		want bool
	}{
		{"H1", detectors.TFH1, true},
		{"H1", detectors.TFH4, false},
		{"H1,D", detectors.TFD, true},
		{"H1,D", detectors.TFH4, false},
		{" h1 , d ", detectors.TFD, true}, // spasi + case-insensitive
		{"H1,D,W", detectors.TFH4, false},
		{"H1,D,W", detectors.TFW, true},
	}
	for _, c := range cases {
		if got := fvgBreakAllowedTF(c.csv, c.tf); got != c.want {
			t.Errorf("fvgBreakAllowedTF(%q,%s)=%v, mau %v", c.csv, c.tf, got, c.want)
		}
	}
}

func TestClosedByDropsForming(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cs := []data.Candle{
		{Time: base},                    // 00:00
		{Time: base.Add(time.Hour)},     // 01:00
		{Time: base.Add(2 * time.Hour)}, // 02:00 (forming saat now=02:30)
	}
	now := base.Add(2*time.Hour + 30*time.Minute)
	got := closedBy(cs, now)
	// candle 02:00 masih forming (close 03:00 > now) → di-drop; sisa 2.
	if len(got) != 2 {
		t.Fatalf("closedBy len = %d, mau 2 (drop forming)", len(got))
	}
	if !got[len(got)-1].Time.Equal(base.Add(time.Hour)) {
		t.Errorf("candle terakhir = %v, mau 01:00", got[len(got)-1].Time)
	}
	// now sebelum semua → kosong
	if l := len(closedBy(cs, base.Add(-time.Hour))); l != 0 {
		t.Errorf("closedBy(before all) = %d, mau 0", l)
	}
}

func TestEntryFromIdx(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// M5 tiap 5 menit dari 00:00 sampai 02:55 (36 candle).
	var m5 []data.Candle
	for k := 0; k < 36; k++ {
		m5 = append(m5, data.Candle{Time: base.Add(time.Duration(k) * 5 * time.Minute)})
	}
	now := base.Add(3 * time.Hour) // 03:00 = close candle H1 02:00
	// EntryFreshBars=1 → fresh window mulai 02:00 → index candle pertama >= 02:00.
	idx := entryFromIdx(m5, now, Config{EntryFreshBars: 1})
	if !m5[idx].Time.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("fresh=1: fromIdx menunjuk %v, mau 02:00", m5[idx].Time)
	}
	// EntryFreshBars=0 → retrospektif (idx 0).
	if got := entryFromIdx(m5, now, Config{EntryFreshBars: 0}); got != 0 {
		t.Errorf("fresh=0: fromIdx = %d, mau 0 (retrospektif)", got)
	}
	// EntryFreshBars=2 → mulai 01:00.
	idx2 := entryFromIdx(m5, now, Config{EntryFreshBars: 2})
	if !m5[idx2].Time.Equal(base.Add(time.Hour)) {
		t.Errorf("fresh=2: fromIdx menunjuk %v, mau 01:00", m5[idx2].Time)
	}
}

func TestSLSaneAndRMultiple(t *testing.T) {
	if !slSane(detectors.Bullish, 100, 98) || slSane(detectors.Bullish, 100, 101) {
		t.Error("buy: SL harus di bawah entry")
	}
	if !slSane(detectors.Bearish, 100, 102) || slSane(detectors.Bearish, 100, 99) {
		t.Error("sell: SL harus di atas entry")
	}
	// long entry 100 SL 98 → 1R = $2 move. exit 106 → +3R.
	if r := rMultiple(Signal{Dir: detectors.Bullish, Entry: 100, SL: 98}, 106); r != 3 {
		t.Errorf("rMultiple long = %g, mau 3", r)
	}
	// short entry 100 SL 102 → 1R = 2. exit 94 → +3R.
	if r := rMultiple(Signal{Dir: detectors.Bearish, Entry: 100, SL: 102}, 94); r != 3 {
		t.Errorf("rMultiple short = %g, mau 3", r)
	}
	// kena SL persis → -1R.
	if r := rMultiple(Signal{Dir: detectors.Bullish, Entry: 100, SL: 98}, 98); r != -1 {
		t.Errorf("rMultiple SL = %g, mau -1", r)
	}
}

func TestTouched(t *testing.T) {
	c := data.Candle{High: 105, Low: 95}
	if !touched(detectors.Bullish, c, 104, true) { // TP 104 di bawah high → kena
		t.Error("buy TP 104 harusnya kena (high 105)")
	}
	if !touched(detectors.Bullish, c, 96, false) { // SL 96 di atas low → kena
		t.Error("buy SL 96 harusnya kena (low 95)")
	}
	if touched(detectors.Bullish, c, 106, true) {
		t.Error("buy TP 106 di atas high → belum kena")
	}
}

func TestFridayCloseDetect(t *testing.T) {
	loc, err := detectors.NYLocation()
	if err != nil {
		t.Skip("no tz")
	}
	fri := time.Date(2026, 5, 29, 17, 5, 0, 0, loc) // Jumat 17:05 NY
	if !isFridayClose(fri, loc) {
		t.Error("Jumat 17:05 NY harus force-close")
	}
	thu := time.Date(2026, 5, 28, 17, 5, 0, 0, loc)
	if isFridayClose(thu, loc) {
		t.Error("Kamis bukan friday-close")
	}
	friMorning := time.Date(2026, 5, 29, 9, 0, 0, 0, loc)
	if isFridayClose(friMorning, loc) {
		t.Error("Jumat 09:00 (sebelum 17:00) belum force-close")
	}
}

// dataDir mencari direktori cache (naik dari internal/engine ke root repo).
func dataDir() string {
	for _, p := range []string{"../../data", "data"} {
		if st, err := os.Stat(filepath.Join(p, "XAU_USD", "H1.csv")); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func TestRealData_RunSmoke(t *testing.T) {
	dir := dataDir()
	if dir == "" {
		t.Skip("cache data tidak ada (jalankan cmd/fetch)")
	}
	load := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(dir, "XAU_USD", g))
		if err != nil {
			t.Fatalf("baca %s: %v", g, err)
		}
		return c
	}
	tf := TFData{Weekly: load("W"), Daily: load("D"), H4: load("H4"), H1: load("H1"), M5: load("M5")}
	res, err := Run(tf, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Signals) != len(res.Trades) {
		t.Errorf("signal=%d trade=%d harus sama", len(res.Signals), len(res.Trades))
	}
	for i, tr := range res.Trades {
		if tr.ExitReason == "" {
			t.Errorf("trade %d tak punya exit_reason", i)
		}
		if tr.Lot <= 0 || tr.RiskUSD <= 0 || tr.BalanceAt <= 0 {
			t.Errorf("trade %d sizing belum terisi: lot=%g risk=%g bal=%g", i, tr.Lot, tr.RiskUSD, tr.BalanceAt)
		}
		if !slSane(tr.Dir, tr.Entry, tr.SL) {
			t.Errorf("trade %d SL tidak sane", i)
		}
	}
	t.Logf("smoke: %d sinyal, %d trade, end equity $%.0f", len(res.Signals), len(res.Trades), DefaultConfig().StartBalance+sumPnL(res.Trades))
}

// TestRealData_NarrateNextPOIsByTF: smoke daftar pantauan multi-TF — Narrate di
// candle H1 terakhir harus mengisi NextPOIsByTF (FractalPOI default ON) dengan
// entri yang konsisten (TF ∈ {H1,H4,D}, Top≥Bottom, Distance≥0).
func TestRealData_NarrateNextPOIsByTF(t *testing.T) {
	dir := dataDir()
	if dir == "" {
		t.Skip("cache data tidak ada (jalankan cmd/fetch)")
	}
	load := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(dir, "XAU_USD", g))
		if err != nil {
			t.Fatalf("baca %s: %v", g, err)
		}
		return c
	}
	tf := TFData{Weekly: load("W"), Daily: load("D"), H4: load("H4"), H1: load("H1"), M5: load("M5")}
	n := Narrate(tf, DefaultConfig(), tf.H1[len(tf.H1)-1].Time)
	for i, p := range n.NextPOIsByTF {
		if p.TF != detectors.TFH1 && p.TF != detectors.TFH4 && p.TF != detectors.TFD {
			t.Errorf("entri %d: TF tak terduga %v (harus H1/H4/D)", i, p.TF)
		}
		if p.Top < p.Bottom {
			t.Errorf("entri %d: Top %.2f < Bottom %.2f", i, p.Top, p.Bottom)
		}
		if p.Distance < 0 {
			t.Errorf("entri %d: Distance negatif %.2f", i, p.Distance)
		}
		if p.Confluence < 1 {
			t.Errorf("entri %d: Confluence %d < 1", i, p.Confluence)
		}
	}
	t.Logf("smoke: %d entri pantauan multi-TF", len(n.NextPOIsByTF))
}

func sumPnL(trades []Trade) float64 {
	var s float64
	for _, t := range trades {
		s += t.PnLUSD
	}
	return s
}
