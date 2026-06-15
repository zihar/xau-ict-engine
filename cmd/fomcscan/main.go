// Command fomcscan menguji hipotesis: apakah hari SENIN di minggu rapat FOMC lebih
// sering ter-klasifikasi X (ekspansi/displacement) ketimbang A (akumulasi)?
// Pengumuman FOMC umumnya Rabu (statement 14:00 ET) → Senin = ~2 hari sebelum, indikasi
// positioning/volatilitas pra-FOMC.
//
// Dua metrik (keduanya pakai classifier yang sudah ada — mirror engine):
//
//	(a) Sesi HARIAN Senin (Asia H1, anchor 18:00 NY) → ClassifyDailyG, X = XAMD.
//	    Pakai default engine: gap-anchor ON, FVG-off (penentu murni true-move).
//	(b) QT MINGGUAN Senin (candle Senin di 4H, D.4) → ClassifyWeekly, X = XAMD.
//	    Mirror engine weekly: FVG-4H wajib, mode "atr", tanpa gap-anchor.
//
// "Minggu-FOMC" = minggu kalender (Senin) yang memuat tanggal pengumuman FOMC. Tiap
// tanggal FOMC dipetakan ke Senin minggunya; tiap Senin trading-day dicek keanggotaan.
//
// Read-only, offline. TIDAK mengubah engine/strategi/config. Murni diagnostik.
//
// CAVEAT: dataset ~2022→kini = 1 rezim BULL dominan. FOMC ~8/tahun → N kecil
// (indikatif, bukan definitif). Tanggal FOMC 2022–2026 di-hardcode dari jadwal resmi Fed
// (hari pengumuman = hari ke-2 rapat).
//
//	go run ./cmd/fomcscan                 # tabel %X kedua metrik, FOMC vs non-FOMC
//	go run ./cmd/fomcscan -all            # + dump tiap Senin (tanggal, FOMC?, label)
//	go run ./cmd/fomcscan -from 2024-01-01 -to 2024-12-31
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
	"forex-backtest/internal/engine"
)

// tally = jumlah Senin & jumlah-X, dipisah grup FOMC vs non-FOMC.
type tally struct{ nF, xF, nN, xN int }

// fomcAnnouncements = tanggal pengumuman FOMC (hari ke-2 rapat, statement 14:00 ET),
// 2022–2026, dari jadwal resmi Federal Reserve. Cukup tanggal kalender (zona NY).
var fomcAnnouncements = []string{
	// 2022
	"2022-01-26", "2022-03-16", "2022-05-04", "2022-06-15",
	"2022-07-27", "2022-09-21", "2022-11-02", "2022-12-14",
	// 2023
	"2023-02-01", "2023-03-22", "2023-05-03", "2023-06-14",
	"2023-07-26", "2023-09-20", "2023-11-01", "2023-12-13",
	// 2024
	"2024-01-31", "2024-03-20", "2024-05-01", "2024-06-12",
	"2024-07-31", "2024-09-18", "2024-11-07", "2024-12-18",
	// 2025
	"2025-01-29", "2025-03-19", "2025-05-07", "2025-06-18",
	"2025-07-30", "2025-09-17", "2025-10-29", "2025-12-10",
	// 2026
	"2026-01-28", "2026-03-18", "2026-04-29", "2026-06-17",
	"2026-07-29", "2026-09-16", "2026-10-28", "2026-12-09",
}

func main() {
	def := engine.DefaultConfig()
	dir := flag.String("data", "data", "direktori cache CSV")
	instrument := flag.String("instrument", "XAU_USD", "instrumen")
	fromStr := flag.String("from", "", "filter mulai tanggal (YYYY-MM-DD, opsional)")
	toStr := flag.String("to", "", "filter sampai tanggal (YYYY-MM-DD, opsional)")
	all := flag.Bool("all", false, "dump per-Senin")
	flag.Parse()

	loc, err := detectors.NYLocation()
	if err != nil {
		log.Fatalf("lokasi NY: %v", err)
	}
	cfg := def
	from, to := parseBound(*fromStr, false), parseBound(*toStr, true)

	read := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(*dir, *instrument, g))
		if err != nil {
			log.Fatalf("baca %s: %v (jalankan `go run ./cmd/fetch` dulu)", g, err)
		}
		return c
	}
	h1 := read("H1")
	h4 := read("H4")

	// Set Senin minggu-FOMC (key = tanggal Senin "2006-01-02", zona NY).
	fomcWeeks := map[string]bool{}
	for _, s := range fomcAnnouncements {
		t, err := time.ParseInLocation("2006-01-02", s, loc)
		if err != nil {
			log.Fatalf("parse tanggal FOMC %q: %v", s, err)
		}
		// Mundur ke Senin minggu kalender itu (Monday=1).
		mon := t.AddDate(0, 0, -(int(t.Weekday())-int(time.Monday)+7)%7)
		fomcWeeks[mon.Format("2006-01-02")] = true
	}

	// Bucket H1 sesi Asia [ds, ds+6h) per trading-day; simpan index candle pertama &
	// terakhir (firstIdx → prevClose untuk gap-anchor; last → index ATR).
	type asiaBucket struct {
		candles  []data.Candle
		firstIdx int
		lastIdx  int
	}
	asiaByDay := map[time.Time]*asiaBucket{}
	for i, c := range h1 {
		ds := detectors.TradingDayStart(c.Time, loc)
		if c.Time.Before(ds) || !c.Time.Before(ds.Add(6*time.Hour)) {
			continue
		}
		a := asiaByDay[ds]
		if a == nil {
			a = &asiaBucket{firstIdx: i}
			asiaByDay[ds] = a
		}
		a.candles = append(a.candles, c)
		a.lastIdx = i
	}

	// Bucket H4 per trading-day (candle hari Senin di 4H view → QT Mingguan D.4).
	type h4Bucket struct {
		candles []data.Candle
		lastIdx int
	}
	h4ByDay := map[time.Time]*h4Bucket{}
	for i, c := range h4 {
		ds := detectors.TradingDayStart(c.Time, loc)
		g := h4ByDay[ds]
		if g == nil {
			g = &h4Bucket{}
			h4ByDay[ds] = g
		}
		g.candles = append(g.candles, c)
		g.lastIdx = i
	}

	// Kumpulkan trading-day Senin terurut.
	var mondays []time.Time
	for ds := range asiaByDay {
		if ds.Weekday() != time.Monday {
			continue
		}
		if !inRange(ds, from, to) {
			continue
		}
		mondays = append(mondays, ds)
	}
	sort.Slice(mondays, func(i, j int) bool { return mondays[i].Before(mondays[j]) })

	var daily, weekly tally
	skip := map[string]int{}

	type row struct {
		date   string
		fomc   bool
		dLabel string // "X" / "A" / "-" (skip)
		wLabel string
	}
	var rows []row

	for _, ds := range mondays {
		isFOMC := fomcWeeks[ds.Format("2006-01-02")]

		// (a) Metrik harian Senin (Asia H1) — gap-anchor ON, FVG-off (default engine).
		dLabel := "-"
		if a := asiaByDay[ds]; a != nil && len(a.candles) >= 2 {
			if atr1h, ok := detectors.ATRAt(h1, a.lastIdx, cfg.ATRPeriod); ok {
				prevClose := 0.0
				if a.firstIdx > 0 {
					prevClose = h1[a.firstIdx-1].Close
				}
				anchor := asiaAnchor(cfg.AsiaGapAnchor, prevClose)
				scen := detectors.ClassifyDailyG(a.candles, anchor, atr1h, cfg.MinAtrMult,
					cfg.MinGapPips, cfg.AsiaRangeRatio, cfg.AsiaAXMode, cfg.AsiaRequireFVG)
				x := scen == detectors.XAMD
				dLabel = label(x)
				tallyAdd(&daily, isFOMC, x)
			} else {
				skip["daily:atr"]++
			}
		} else {
			skip["daily:asia<2"]++
		}

		// (b) Metrik QT Mingguan (candle Senin 4H) — FVG wajib, tanpa gap-anchor (mirror engine).
		wLabel := "-"
		if g := h4ByDay[ds]; g != nil && len(g.candles) >= 2 {
			if atr4h, ok := detectors.ATRAt(h4, g.lastIdx, cfg.ATRPeriod); ok {
				scen := detectors.ClassifyWeekly(g.candles, atr4h, cfg.MinAtrMult, cfg.MinGapPips)
				x := scen == detectors.XAMD
				wLabel = label(x)
				tallyAdd(&weekly, isFOMC, x)
			} else {
				skip["weekly:atr"]++
			}
		} else {
			skip["weekly:h4<2"]++
		}

		rows = append(rows, row{ds.Format("2006-01-02"), isFOMC, dLabel, wLabel})
	}

	// ── Output ──────────────────────────────────────────────────────────────
	rng := "seluruh cache"
	if !from.IsZero() || !to.IsZero() {
		rng = fmt.Sprintf("%s → %s", fmtBound(from), fmtBound(to))
	}
	fmt.Printf("FOMC-Monday scan — %d Senin (%s)\n", len(mondays), rng)
	fmt.Printf("Hipotesis: Senin minggu-FOMC lebih sering X (ekspansi) drpd non-FOMC?\n\n")

	printMetric("(a) Sesi HARIAN Senin (Asia H1; gap-anchor ON, FVG-off)", daily)
	fmt.Println()
	printMetric("(b) QT MINGGUAN Senin (candle Senin 4H; FVG wajib, no gap-anchor)", weekly)

	if len(skip) > 0 {
		fmt.Printf("\nSenin di-skip (data kurang): ")
		var ks []string
		for k := range skip {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for i, k := range ks {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("%s=%d", k, skip[k])
		}
		fmt.Println()
	}

	fmt.Printf("\nCAVEAT: 1 rezim bull 2022–2026, N kecil → indikatif, bukan definitif.\n")
	fmt.Printf("Display-only; A/X bukan gate P&L (qt_phase_gate=false).\n")

	if *all {
		fmt.Printf("\n%-12s  %-5s  %-7s  %-8s\n", "Senin", "FOMC?", "harian", "mingguan")
		for _, r := range rows {
			f := "-"
			if r.fomc {
				f = "FOMC"
			}
			fmt.Printf("%-12s  %-5s  %-7s  %-8s\n", r.date, f, r.dLabel, r.wLabel)
		}
	}
}

func tallyAdd(t *tally, isFOMC, x bool) {
	if isFOMC {
		t.nF++
		if x {
			t.xF++
		}
	} else {
		t.nN++
		if x {
			t.xN++
		}
	}
}

func printMetric(title string, t tally) {
	fmt.Printf("=== %s ===\n", title)
	fmt.Printf("  minggu-FOMC : %2d/%-3d X  (%5.1f%%)\n", t.xF, t.nF, pct(t.xF, t.nF))
	fmt.Printf("  non-FOMC    : %2d/%-3d X  (%5.1f%%)\n", t.xN, t.nN, pct(t.xN, t.nN))
	diff := pct(t.xF, t.nF) - pct(t.xN, t.nN)
	fmt.Printf("  selisih     : %+.1f pp", diff)
	if t.nF > 0 && t.nN > 0 {
		p := twoPropZ(t.xF, t.nF, t.xN, t.nN)
		fmt.Printf("   (z-test 2-sisi p≈%.3f, indikatif)", p)
	}
	fmt.Println()
}

// twoPropZ = p-value dua-sisi uji selisih proporsi (pooled), pakai erfc — std-lib.
// Indikatif saja (N kecil, asumsi normal longgar).
func twoPropZ(x1, n1, x2, n2 int) float64 {
	if n1 == 0 || n2 == 0 {
		return math.NaN()
	}
	p1 := float64(x1) / float64(n1)
	p2 := float64(x2) / float64(n2)
	pp := float64(x1+x2) / float64(n1+n2)
	se := math.Sqrt(pp * (1 - pp) * (1/float64(n1) + 1/float64(n2)))
	if se == 0 {
		return math.NaN()
	}
	z := (p1 - p2) / se
	return math.Erfc(math.Abs(z) / math.Sqrt2)
}

func pct(x, n int) float64 {
	if n == 0 {
		return 0
	}
	return 100 * float64(x) / float64(n)
}

func label(x bool) string {
	if x {
		return "X"
	}
	return "A"
}

// asiaAnchor = mirror engine.asiaAnchor: prevClose bila gap-anchor ON, else 0.
func asiaAnchor(on bool, prevClose float64) float64 {
	if on {
		return prevClose
	}
	return 0
}

func inRange(t, from, to time.Time) bool {
	if !from.IsZero() && t.Before(from) {
		return false
	}
	if !to.IsZero() && !t.Before(to) {
		return false
	}
	return true
}

func parseBound(s string, end bool) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		log.Fatalf("tanggal %q tidak valid (YYYY-MM-DD): %v", s, err)
	}
	if end {
		t = t.AddDate(0, 0, 1) // eksklusif → inklusif hari "to"
	}
	return t
}

func fmtBound(t time.Time) string {
	if t.IsZero() {
		return "…"
	}
	return t.Format("2006-01-02")
}
