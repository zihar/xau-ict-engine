// Command q1sweep mendiagnosa seberapa sering sebuah RANGE sumber (R) tersapu
// likuiditasnya — di sisi BERLAWANAN daily-bias — selama sebuah WINDOW scan (W),
// lalu memetakan kuartal-W mana yang PERTAMA menyapu. Hasil di-tally per skenario
// harian AMDX vs XAMD (klasifikasi sesi Asia, mirror engine).
//
// Dua preset (flag -target), satu inti analisis fractal yang sama:
//
//	nyam-q1 (default) : R = Q1 NY-AM (06:00–07:30 NY), W = sisa NY-AM (07:30–12:00),
//	                    kuartal-pertama ∈ {Q2,Q3,Q4} NY-AM.
//	asia              : R = sesi Asia (18:00–00:00 NY), W = sesi London (00:00–06:00),
//	                    kuartal-pertama ∈ {Q1,Q2,Q3,Q4} London (manipulasi khas Q2).
//
// "Sweep berlawanan bias": bias Bullish → R-Low ditembus; Bearish → R-High ditembus
// (mirror konvensi engine.asiaSwept). Bias dihitung point-in-time (anti-lookahead):
// hanya candle daily yang sudah CLOSE sebelum trading-day berjalan.
//
// Read-only, offline. Tidak mengubah engine/strategi/config — murni observasional.
//
//	go run ./cmd/q1sweep                  # nyam-q1
//	go run ./cmd/q1sweep -target asia      # Asia di-sweep saat London (manipulasi)
//	go run ./cmd/q1sweep -all              # + dump per-hari
//	go run ./cmd/q1sweep -from 2023-01-01 -to 2024-12-31
package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"time"

	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/engine"
	"xau-ict-engine/internal/state"
)

// preset mendefinisikan window R (sumber) & W (scan) relatif terhadap awal
// trading-day (18:00 NY). W selalu 1 sesi = 6 jam = 4 kuartal × 90 menit.
type preset struct {
	name      string
	desc      string
	rStart    time.Duration // offset R-start dari trading-day start
	rEnd      time.Duration // offset R-end
	wStart    time.Duration // offset W-start (awal sesi scan)
	firstScan int           // kuartal-W pertama yang di-scan (2 untuk nyam-q1, 1 untuk asia)
}

func presetByName(name string) (preset, bool) {
	switch name {
	case "nyam-q1":
		return preset{
			name: "nyam-q1", desc: "range=NY-AM Q1 06:00–07:30, scan=NY-AM Q2..Q4",
			rStart: 12 * time.Hour, rEnd: 12*time.Hour + 90*time.Minute,
			wStart: 12 * time.Hour, firstScan: 2,
		}, true
	case "asia":
		return preset{
			name: "asia", desc: "range=Asia 18:00–00:00, scan=London Q1..Q4",
			rStart: 0, rEnd: 6 * time.Hour,
			wStart: 6 * time.Hour, firstScan: 1,
		}, true
	}
	return preset{}, false
}

type dayResult struct {
	start   time.Time
	scen    detectors.Scenario
	bias    detectors.Direction
	rLo     float64
	rHi     float64
	sweptQ  int // 0 = tidak tersapu dalam W; selain itu = kuartal-W pertama yang menyapu
}

type bucket struct {
	days  int
	swept int
	byQ   map[int]int // kuartal-pertama → jumlah hari
	never int
}

func newBucket() *bucket { return &bucket{byQ: map[int]int{}} }

func (b *bucket) add(r dayResult) {
	b.days++
	if r.sweptQ == 0 {
		b.never++
		return
	}
	b.swept++
	b.byQ[r.sweptQ]++
}

func main() {
	def := engine.DefaultConfig()
	target := flag.String("target", "nyam-q1", "analisis: nyam-q1 | asia")
	dir := flag.String("data", "data", "direktori cache CSV")
	instrument := flag.String("instrument", "XAU_USD", "instrumen")
	scenMode := flag.String("scenario-mode", def.AsiaAXMode, "detektor AMDX/XAMD: atr | ratio | range")
	fvg := flag.Bool("fvg", def.AsiaRequireFVG, "syarat FVG untuk XAMD (default = engine)")
	biasMult := flag.Float64("bias-atr-mult", def.BiasSwingATRMult, "filter magnitude swing daily-bias (× ATR; 0 = unfiltered)")
	fromStr := flag.String("from", "", "filter mulai tanggal (YYYY-MM-DD, opsional)")
	toStr := flag.String("to", "", "filter sampai tanggal (YYYY-MM-DD, opsional)")
	all := flag.Bool("all", false, "dump per-hari")
	flag.Parse()

	switch *scenMode {
	case "atr", "ratio", "range":
	default:
		log.Fatalf("scenario-mode %q tidak dikenal (atr|ratio|range)", *scenMode)
	}
	pre, ok := presetByName(*target)
	if !ok {
		log.Fatalf("target %q tidak dikenal (nyam-q1|asia)", *target)
	}

	loc, err := detectors.NYLocation()
	if err != nil {
		log.Fatalf("lokasi NY: %v", err)
	}
	from, to := parseBound(*fromStr, false), parseBound(*toStr, true)

	read := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(*dir, *instrument, g))
		if err != nil {
			log.Fatalf("baca %s: %v (jalankan `go run ./cmd/fetch` dulu)", g, err)
		}
		return c
	}
	h1 := read("H1")
	m5 := read("M5")
	daily := read("D")

	cfg := def
	cfg.BiasSwingATRMult = *biasMult

	// Bucket H1 sesi Asia per trading-day (untuk skenario AMDX/XAMD) + index ATR.
	type asiaDay struct {
		candles []data.Candle
		last    int
	}
	asiaByDay := map[time.Time]*asiaDay{}
	for i, c := range h1 {
		ds := detectors.TradingDayStart(c.Time, loc)
		asiaEnd := ds.Add(6 * time.Hour)
		if c.Time.Before(ds) || !c.Time.Before(asiaEnd) {
			continue
		}
		a := asiaByDay[ds]
		if a == nil {
			a = &asiaDay{}
			asiaByDay[ds] = a
		}
		a.candles = append(a.candles, c)
		a.last = i
	}

	// Bucket M5 per trading-day (untuk window R & W).
	m5ByDay := map[time.Time][]data.Candle{}
	var dayStarts []time.Time
	for _, c := range m5 {
		ds := detectors.TradingDayStart(c.Time, loc)
		if _, seen := m5ByDay[ds]; !seen {
			dayStarts = append(dayStarts, ds)
		}
		m5ByDay[ds] = append(m5ByDay[ds], c)
	}
	sort.Slice(dayStarts, func(i, j int) bool { return dayStarts[i].Before(dayStarts[j]) })

	rangeRatio := cfg.AsiaRangeRatio

	var results []dayResult
	skip := map[string]int{}
	for _, ds := range dayStarts {
		if !from.IsZero() && ds.Before(from) {
			continue
		}
		if !to.IsZero() && !ds.Before(to) {
			continue
		}
		// Skenario harian dari sesi Asia.
		a := asiaByDay[ds]
		if a == nil || len(a.candles) < 2 {
			skip["asia<2"]++
			continue
		}
		atr1h, ok := detectors.ATRAt(h1, a.last, cfg.ATRPeriod)
		if !ok {
			skip["atr"]++
			continue
		}
		scen := detectors.ClassifyDailyEx(a.candles, atr1h, cfg.MinAtrMult, cfg.MinGapPips, rangeRatio, *scenMode, *fvg)

		// Daily bias point-in-time: candle daily yang sudah CLOSE sebelum trading-day ini.
		cut := sort.Search(len(daily), func(i int) bool { return !daily[i].Time.Before(ds) })
		dUpTo := daily[:cut]
		bias, _, _, biasOK := state.DailyBiasRef(dUpTo, biasMinLeg(dUpTo, cfg))
		if !biasOK {
			skip["bias"]++
			continue
		}

		// Range R dari M5.
		m5d := m5ByDay[ds]
		rCandles := windowCandles(m5d, ds.Add(pre.rStart), ds.Add(pre.rEnd))
		rLo, rHi, rok := rangeOf(rCandles)
		if !rok {
			skip["range"]++
			continue
		}

		// Scan kuartal W (90m) untuk sweep sisi berlawanan bias.
		sweptQ := 0
		for q := pre.firstScan; q <= 4; q++ {
			qs := ds.Add(pre.wStart + time.Duration(q-1)*90*time.Minute)
			qe := qs.Add(90 * time.Minute)
			if swept(windowCandles(m5d, qs, qe), bias, rLo, rHi) {
				sweptQ = q
				break
			}
		}
		results = append(results, dayResult{start: ds, scen: scen, bias: bias, rLo: rLo, rHi: rHi, sweptQ: sweptQ})
	}

	report(pre, *scenMode, *fvg, *biasMult, from, to, results, skip, loc, *all)
}

// swept melaporkan apakah ada candle yang menembus R di sisi berlawanan bias.
func swept(cs []data.Candle, bias detectors.Direction, rLo, rHi float64) bool {
	for _, c := range cs {
		if bias == detectors.Bullish && c.Low < rLo {
			return true
		}
		if bias == detectors.Bearish && c.High > rHi {
			return true
		}
	}
	return false
}

// windowCandles = candle dengan Time di [from, to) (mirror engine.m15InWindow).
func windowCandles(cs []data.Candle, from, to time.Time) []data.Candle {
	var out []data.Candle
	for _, c := range cs {
		if !c.Time.Before(from) && c.Time.Before(to) {
			out = append(out, c)
		}
	}
	return out
}

// rangeOf = min Low / max High atas slice (ok=false kalau kosong).
func rangeOf(cs []data.Candle) (lo, hi float64, ok bool) {
	if len(cs) == 0 {
		return 0, 0, false
	}
	lo, hi = cs[0].Low, cs[0].High
	for _, c := range cs[1:] {
		if c.Low < lo {
			lo = c.Low
		}
		if c.High > hi {
			hi = c.High
		}
	}
	return lo, hi, true
}

// biasMinLeg = ambang magnitude swing daily-bias (replika engine.biasMinLeg).
func biasMinLeg(daily []data.Candle, cfg engine.Config) float64 {
	if cfg.BiasSwingATRMult <= 0 || len(daily) <= cfg.ATRPeriod {
		return 0
	}
	if atr, ok := detectors.ATRAt(daily, len(daily)-1, cfg.ATRPeriod); ok {
		return cfg.BiasSwingATRMult * atr
	}
	return 0
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
		t = t.AddDate(0, 0, 1) // inklusif sampai akhir hari -to
	}
	return t
}

func report(pre preset, scenMode string, fvg bool, biasMult float64, from, to time.Time, res []dayResult, skip map[string]int, loc *time.Location, all bool) {
	rng := "semua"
	if !from.IsZero() || !to.IsZero() {
		f, t := "…", "…"
		if !from.IsZero() {
			f = from.Format("2006-01-02")
		}
		if !to.IsZero() {
			t = to.AddDate(0, 0, -1).Format("2006-01-02")
		}
		rng = f + ".." + t
	}
	fmt.Printf("Target: %s  (%s)\n", pre.name, pre.desc)
	fmt.Printf("Param: scenario-mode=%s  requireFVG=%v  bias-atr-mult=%.2f  rentang=%s\n", scenMode, fvg, biasMult, rng)
	fmt.Printf("Trading day terklasifikasi: %d  (skip: %s)\n\n", len(res), fmtSkip(skip))

	// Kuartal yang di-scan (header kolom dinamis).
	var qs []int
	for q := pre.firstScan; q <= 4; q++ {
		qs = append(qs, q)
	}

	amdx, xamd, total := newBucket(), newBucket(), newBucket()
	for _, r := range res {
		total.add(r)
		if r.scen == detectors.XAMD {
			xamd.add(r)
		} else {
			amdx.add(r)
		}
	}

	// Header.
	fmt.Printf("  %-7s | %5s | %7s | %5s | %6s |", "skenario", "hari", "tersapu", "tdk", "%sweep")
	for _, q := range qs {
		fmt.Printf(" %4s |", fmt.Sprintf("Q%d", q))
	}
	fmt.Printf(" %5s\n", "never")
	sep := "  --------------------------------------------------------"
	for range qs {
		sep += "-------"
	}
	fmt.Println(sep)
	printBucket("AMDX", amdx, qs)
	printBucket("XAMD", xamd, qs)
	fmt.Println(sep)
	printBucket("TOTAL", total, qs)

	// Breakdown asimetri arah bias.
	fmt.Println("\nPer arah bias (TOTAL):")
	bull, bear := newBucket(), newBucket()
	for _, r := range res {
		if r.bias == detectors.Bullish {
			bull.add(r)
		} else {
			bear.add(r)
		}
	}
	fmt.Printf("  bullish (sweep R-Low) : %d hari, %d tersapu (%.1f%%)\n", bull.days, bull.swept, pct(bull.swept, bull.days))
	fmt.Printf("  bearish (sweep R-High): %d hari, %d tersapu (%.1f%%)\n", bear.days, bear.swept, pct(bear.swept, bear.days))

	if all {
		fmt.Println("\nPer-hari — tanggal = SESI scan (NY-AM/London), trading-day anchor 18:00 NY")
		fmt.Println("(sesi-scan | trading-day | skenario | bias | R-Low | R-High | kuartal-sweep):")
		for _, r := range res {
			q := "never"
			if r.sweptQ != 0 {
				q = fmt.Sprintf("Q%d", r.sweptQ)
			}
			sess := r.start.Add(pre.wStart).In(loc).Format("2006-01-02 (Mon)")
			td := r.start.In(loc).Format("2006-01-02")
			fmt.Printf("  %s | td %s | %-4s | %-7s | %.2f | %.2f | %s\n",
				sess, td, scenStr(r.scen), r.bias.String(), r.rLo, r.rHi, q)
		}
	}
}

func printBucket(name string, b *bucket, qs []int) {
	fmt.Printf("  %-7s | %5d | %7d | %5d | %5.1f%% |", name, b.days, b.swept, b.never, pct(b.swept, b.days))
	for _, q := range qs {
		fmt.Printf(" %4d |", b.byQ[q])
	}
	fmt.Printf(" %5d\n", b.never)
}

func scenStr(s detectors.Scenario) string {
	if s == detectors.XAMD {
		return "XAMD"
	}
	return "AMDX"
}

func fmtSkip(skip map[string]int) string {
	if len(skip) == 0 {
		return "0"
	}
	keys := make([]string, 0, len(skip))
	for k := range skip {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := ""
	for i, k := range keys {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s=%d", k, skip[k])
	}
	return s
}

func pct(a, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(a) / float64(total)
}
