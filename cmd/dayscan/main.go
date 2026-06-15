// Command dayscan mendiagnosa KARAKTER fase Manipulasi dalam model AMD
// (Accumulation-Manipulation-Distribution) XAUUSD, di DUA frame:
//
//	intraday : sesi dalam 1 trading-day (anchor 18:00 NY). AMDX → Asia=A,
//	           London=M, NY-AM=D, PM=X.  XAMD → Asia=X, London=A, NY-AM=M, PM=D.
//	weekly   : hari sebagai quarter (D.4). AMDX → Sen=A,Sel=M,Rab=D,Kam=X.
//	           XAMD → Sen=X,Sel=A,Rab=M,Kam=D. (A/M/D-day ditentukan via WeeklyPhase,
//	           mirror engine — robust thd anchor 18:00 NY.)
//
// Tiga pertanyaan yang dijawab (observasional, bukan rule):
//
//  1. Apakah sesi/hari Manipulasi SELALU men-sweep range Akumulasi sebelumnya?
//     → % sweptOF (sisi yang diharapkan OF) + % sweptAny (sisi mana pun).
//  2. Sampai mana kedalaman sentuhnya? DUA ukuran:
//     OVERSHOOT  = seberapa jauh wick MELEWATI extreme akumulasi (dalam ATR & %range);
//     bermakna pada subset SWEPT.
//     PENETRASI  = seberapa dalam harga MASUK ke dalam range akumulasi sebelum balik
//     (0% = tepi awal, 100% = tepi sweep); bermakna pada subset NOT-swept.
//  3. Sesi Distribusi (NY pada AMDX) — searah Order Flow weekly, atau berlawanan-OF
//     dulu (judas) baru searah? → % close searah-OF + % poke-kontra-dulu-lalu-searah.
//
// Arah sweep tergantung OF (mirror konvensi engine/q1sweep): OF Bullish → manipulasi
// diharapkan sweep LOW akumulasi (sell-side); Bearish → sweep HIGH (buy-side). OF & ATR
// dihitung POINT-IN-TIME (anti-lookahead): hanya candle yang sudah close.
//
// Read-only, offline. Tidak mengubah engine/strategi/config.
//
// CAVEAT: dataset ~2022→kini = 1 rezim BULL dominan. Sampel OF-Bearish tipis →
// baris "sweep aHi"/bearish kecil-n (indikatif, bukan definitif). Poke-first weekly =
// proxy upper-bound (urutan intra-hari tak teramati di granularitas Daily).
//
//	go run ./cmd/dayscan                            # kedua frame, semua scenario
//	go run ./cmd/dayscan -frame intraday -scenario amdx
//	go run ./cmd/dayscan -frame weekly -all
//	go run ./cmd/dayscan -from 2024-01-01 -to 2024-12-31
package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
	"forex-backtest/internal/engine"
	"forex-backtest/internal/state"
)

func main() {
	def := engine.DefaultConfig()
	dir := flag.String("data", "data", "direktori cache CSV")
	instrument := flag.String("instrument", "XAU_USD", "instrumen")
	frame := flag.String("frame", "both", "frame analisis: intraday | weekly | both")
	scenFilter := flag.String("scenario", "all", "filter scenario: amdx | xamd | all")
	scenMode := flag.String("scenario-mode", def.AsiaAXMode, "detektor AMDX/XAMD: atr | ratio | range")
	fvg := flag.Bool("fvg", def.AsiaRequireFVG, "syarat FVG untuk XAMD (default = engine)")
	ofMult := flag.Float64("of-atr-mult", def.OFSwingATRMult, "filter magnitude swing weekly-OF (× ATR; 0 = unfiltered)")
	nyPokeEps := flag.Float64("ny-poke-eps", 0, "ambang minimal poke kontra-OF lewat open distribusi (× ATR; 0 = sentuhan apa pun)")
	fromStr := flag.String("from", "", "filter mulai tanggal (YYYY-MM-DD, opsional)")
	toStr := flag.String("to", "", "filter sampai tanggal (YYYY-MM-DD, opsional)")
	all := flag.Bool("all", false, "dump per-hari")
	flag.Parse()

	switch *scenMode {
	case "atr", "ratio", "range":
	default:
		log.Fatalf("scenario-mode %q tidak dikenal (atr|ratio|range)", *scenMode)
	}
	switch *frame {
	case "intraday", "weekly", "both":
	default:
		log.Fatalf("frame %q tidak dikenal (intraday|weekly|both)", *frame)
	}
	switch *scenFilter {
	case "amdx", "xamd", "all":
	default:
		log.Fatalf("scenario %q tidak dikenal (amdx|xamd|all)", *scenFilter)
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

	cfg := def
	cfg.OFSwingATRMult = *ofMult

	p := params{cfg: cfg, scenMode: *scenMode, fvg: *fvg, scenFilter: *scenFilter,
		nyPokeEps: *nyPokeEps, from: from, to: to, all: *all, loc: loc}

	if *frame == "intraday" || *frame == "both" {
		weekly := read("W")
		runIntraday(read("H1"), read("M5"), weekly, p)
	}
	if *frame == "weekly" || *frame == "both" {
		if *frame == "both" {
			fmt.Println()
		}
		runWeekly(read("D"), read("H4"), read("W"), p)
	}
}

type params struct {
	cfg        engine.Config
	scenMode   string
	fvg        bool
	scenFilter string
	nyPokeEps  float64
	from, to   time.Time
	all        bool
	loc        *time.Location
}

// scenAllowed = filter scenario (-scenario flag).
func (p params) scenAllowed(s detectors.Scenario) bool {
	switch p.scenFilter {
	case "amdx":
		return s == detectors.AMDX
	case "xamd":
		return s == detectors.XAMD
	default:
		return true
	}
}

func (p params) inRange(t time.Time) bool {
	if !p.from.IsZero() && t.Before(p.from) {
		return false
	}
	if !p.to.IsZero() && !t.Before(p.to) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Frame INTRADAY: sesi A/M/D dalam 1 trading-day.
// ---------------------------------------------------------------------------

func runIntraday(h1, m5, weekly []data.Candle, p params) {
	cfg := p.cfg

	// Bucket H1 sesi Asia per trading-day (untuk klasifikasi AMDX/XAMD + index ATR).
	type sess struct {
		candles []data.Candle
		last    int
	}
	asiaByDay := map[time.Time]*sess{}
	for i, c := range h1 {
		ds := detectors.TradingDayStart(c.Time, p.loc)
		if c.Time.Before(ds) || !c.Time.Before(ds.Add(6*time.Hour)) {
			continue
		}
		a := asiaByDay[ds]
		if a == nil {
			a = &sess{}
			asiaByDay[ds] = a
		}
		a.candles = append(a.candles, c)
		a.last = i
	}

	// Bucket M5 per trading-day (untuk window sesi A/M/D).
	m5ByDay := map[time.Time][]data.Candle{}
	var dayStarts []time.Time
	for _, c := range m5 {
		ds := detectors.TradingDayStart(c.Time, p.loc)
		if _, seen := m5ByDay[ds]; !seen {
			dayStarts = append(dayStarts, ds)
		}
		m5ByDay[ds] = append(m5ByDay[ds], c)
	}
	sort.Slice(dayStarts, func(i, j int) bool { return dayStarts[i].Before(dayStarts[j]) })

	var recs []rec
	skip := map[string]int{}
	for _, ds := range dayStarts {
		if !p.inRange(ds) {
			continue
		}
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
		scen := detectors.ClassifyDailyEx(a.candles, atr1h, cfg.MinAtrMult, cfg.MinGapPips, cfg.AsiaRangeRatio, p.scenMode, p.fvg)
		if !p.scenAllowed(scen) {
			skip["scenario-filter"]++
			continue
		}

		// Weekly OF point-in-time (candle weekly yang sudah close sebelum hari ini).
		cut := sort.Search(len(weekly), func(i int) bool { return !weekly[i].Time.Before(ds) })
		of, _, ofOK := state.WeeklyOFMin(weekly[:cut], ofMinLeg(weekly[:cut], cfg))
		if !ofOK {
			skip["of"]++
			continue
		}

		// Offset sesi A/M/D dari awal trading-day (jam NY: Asia 0-6h, London 6-12h,
		// NY-AM 12-18h, PM 18-24h). XAMD geser 1 sesi.
		var accOff, manipOff, distOff time.Duration
		if scen == detectors.XAMD {
			accOff, manipOff, distOff = 6*time.Hour, 12*time.Hour, 18*time.Hour
		} else { // AMDX
			accOff, manipOff, distOff = 0, 6*time.Hour, 12*time.Hour
		}
		m5d := m5ByDay[ds]
		aLo, aHi, aok := rangeOf(windowCandles(m5d, ds.Add(accOff), ds.Add(accOff+6*time.Hour)))
		if !aok || aHi-aLo <= 0 {
			skip["acc-range"]++
			continue
		}
		mLo, mHi, mok := rangeOf(windowCandles(m5d, ds.Add(manipOff), ds.Add(manipOff+6*time.Hour)))
		if !mok {
			skip["manip-empty"]++
			continue
		}

		d := computeDepth(of, aLo, aHi, mLo, mHi, atr1h)
		distC := windowCandles(m5d, ds.Add(distOff), ds.Add(distOff+6*time.Hour))
		hasDist, aligned, poke := distMetrics(distC, of, p.nyPokeEps*atr1h)

		recs = append(recs, rec{
			start: ds, label: ds.In(p.loc).Format("2006-01-02 (Mon)"), scen: scen, of: of,
			aLo: aLo, aHi: aHi, depth: d, hasDist: hasDist, aligned: aligned, poke: poke,
		})
	}

	report("INTRADAY", "sesi London/NY-AM/PM men-sweep sesi Asia/London (akumulasi)", p, recs, skip)
}

// ---------------------------------------------------------------------------
// Frame WEEKLY: hari sebagai quarter (D.4). Unit = candle Daily.
// ---------------------------------------------------------------------------

func runWeekly(daily, h4, weekly []data.Candle, p params) {
	cfg := p.cfg

	// Bucket H4 per trading-day (untuk klasifikasi scenario weekly dari hari Senin).
	type sess struct {
		candles []data.Candle
		last    int
	}
	h4ByDay := map[time.Time]*sess{}
	for i, c := range h4 {
		ds := detectors.TradingDayStart(c.Time, p.loc)
		g := h4ByDay[ds]
		if g == nil {
			g = &sess{}
			h4ByDay[ds] = g
		}
		g.candles = append(g.candles, c)
		g.last = i
	}

	// Bucket Daily ke minggu (key = anchor Senin). Simpan per weekday + index daily.
	type weekBucket struct {
		byWd  map[time.Weekday]data.Candle
		idxWd map[time.Weekday]int
	}
	weeks := map[time.Time]*weekBucket{}
	var weekKeys []time.Time
	for i, c := range daily {
		anchor := detectors.TradingDayStart(c.Time, p.loc)
		wd := anchor.Weekday()
		if wd < time.Monday || wd > time.Thursday {
			continue // hanya Sen-Kam relevan untuk A/M/D
		}
		key := mondayAnchor(anchor)
		w := weeks[key]
		if w == nil {
			w = &weekBucket{byWd: map[time.Weekday]data.Candle{}, idxWd: map[time.Weekday]int{}}
			weeks[key] = w
			weekKeys = append(weekKeys, key)
		}
		w.byWd[wd] = c
		w.idxWd[wd] = i
	}
	sort.Slice(weekKeys, func(i, j int) bool { return weekKeys[i].Before(weekKeys[j]) })

	var recs []rec
	skip := map[string]int{}
	for _, key := range weekKeys {
		w := weeks[key]
		// Klasifikasi scenario weekly dari hari Senin (4H view, D.4).
		g := h4ByDay[key]
		if g == nil || len(g.candles) < 2 {
			skip["monday-4h<2"]++
			continue
		}
		atr4h, ok := detectors.ATRAt(h4, g.last, cfg.ATRPeriod)
		if !ok {
			skip["atr4h"]++
			continue
		}
		scen := detectors.ClassifyWeeklyEx(g.candles, atr4h, cfg.MinAtrMult, cfg.MinGapPips, cfg.AsiaRangeRatio, p.scenMode, p.fvg)
		if !p.scenAllowed(scen) {
			skip["scenario-filter"]++
			continue
		}

		// A/M/D-day via WeeklyPhase (mirror engine) — robust thd anchor 18:00 NY.
		aWd, mWd, dWd := amdWeekdays(scen)
		aC, aOK := w.byWd[aWd]
		mC, mOK := w.byWd[mWd]
		dC, dOK := w.byWd[dWd]
		if !aOK || !mOK || !dOK {
			skip["week-incomplete"]++
			continue
		}
		aDayStart := detectors.TradingDayStart(aC.Time, p.loc)
		if !p.inRange(aDayStart) {
			continue
		}
		aLo, aHi := aC.Low, aC.High
		if aHi-aLo <= 0 {
			skip["acc-range"]++
			continue
		}
		atrD, _ := detectors.ATRAt(daily, w.idxWd[aWd], cfg.ATRPeriod)

		// Weekly OF point-in-time (sebelum A-day mulai).
		cut := sort.Search(len(weekly), func(i int) bool { return !weekly[i].Time.Before(aDayStart) })
		of, _, ofOK := state.WeeklyOFMin(weekly[:cut], ofMinLeg(weekly[:cut], cfg))
		if !ofOK {
			skip["of"]++
			continue
		}

		d := computeDepth(of, aLo, aHi, mC.Low, mC.High, atrD)
		hasDist, aligned, poke := distMetricsCandle(dC, of, p.nyPokeEps*atrD)

		recs = append(recs, rec{
			start: aDayStart, label: aDayStart.In(p.loc).Format("2006-01-02 (Mon)"), scen: scen, of: of,
			aLo: aLo, aHi: aHi, depth: d, hasDist: hasDist, aligned: aligned, poke: poke,
		})
	}

	report("WEEKLY", "hari M men-sweep range hari A; distribusi = hari D (proxy 1-candle)", p, recs, skip)
}

// amdWeekdays = weekday hari A/M/D per scenario (D.4).
func amdWeekdays(s detectors.Scenario) (a, m, d time.Weekday) {
	if s == detectors.XAMD {
		return time.Tuesday, time.Wednesday, time.Thursday
	}
	return time.Monday, time.Tuesday, time.Wednesday
}

// mondayAnchor = anchor trading-day Senin di minggu yang sama dgn anchor.
func mondayAnchor(anchor time.Time) time.Time {
	off := (int(anchor.Weekday()) - int(time.Monday) + 7) % 7
	return anchor.AddDate(0, 0, -off)
}

// ---------------------------------------------------------------------------
// Metrik inti
// ---------------------------------------------------------------------------

type depthRec struct {
	sweptOF, sweptAny bool
	overATR, overPct  float64 // valid hanya bila sweptOF
	penPct            float64 // kedalaman masuk range [0,100]
}

// computeDepth: sweep + overshoot + penetrasi, arah ditentukan OF.
// OF Bullish → harap sweep aLo (manipulasi poke turun). Bearish → sweep aHi.
func computeDepth(of detectors.Direction, aLo, aHi, mLo, mHi, atr float64) depthRec {
	w := aHi - aLo
	var d depthRec
	d.sweptAny = mLo < aLo || mHi > aHi
	if of == detectors.Bullish {
		d.sweptOF = mLo < aLo
		if d.sweptOF {
			if atr > 0 {
				d.overATR = (aLo - mLo) / atr
			}
			d.overPct = (aLo - mLo) / w * 100
		}
		// kedalaman turun dari tepi atas (aHi) menuju tepi sweep (aLo)
		d.penPct = clamp((aHi-mLo)/w, 0, 1) * 100
	} else {
		d.sweptOF = mHi > aHi
		if d.sweptOF {
			if atr > 0 {
				d.overATR = (mHi - aHi) / atr
			}
			d.overPct = (mHi - aHi) / w * 100
		}
		d.penPct = clamp((mHi-aLo)/w, 0, 1) * 100
	}
	return d
}

// distMetrics (intraday, multi-candle M5): close searah-OF? poke kontra-OF dulu?
func distMetrics(cs []data.Candle, of detectors.Direction, eps float64) (hasDist, aligned, poke bool) {
	if len(cs) < 2 {
		return false, false, false
	}
	open, cl := cs[0].Open, cs[len(cs)-1].Close
	if of == detectors.Bullish {
		aligned = cl > open
	} else {
		aligned = cl < open
	}
	contraIdx, ofIdx := -1, -1
	for i, c := range cs {
		if of == detectors.Bullish {
			if contraIdx < 0 && c.Low < open-eps {
				contraIdx = i
			}
			if ofIdx < 0 && c.High > open {
				ofIdx = i
			}
		} else {
			if contraIdx < 0 && c.High > open+eps {
				contraIdx = i
			}
			if ofIdx < 0 && c.Low < open {
				ofIdx = i
			}
		}
	}
	poke = contraIdx >= 0 && (ofIdx < 0 || contraIdx <= ofIdx) && aligned
	return true, aligned, poke
}

// distMetricsCandle (weekly, 1 candle Daily): poke = wick kontra-OF lewat open ADA
// & close searah-OF (PROXY upper-bound — urutan intra-hari tak teramati).
func distMetricsCandle(c data.Candle, of detectors.Direction, eps float64) (hasDist, aligned, poke bool) {
	if of == detectors.Bullish {
		aligned = c.Close > c.Open
		poke = c.Low < c.Open-eps && aligned
	} else {
		aligned = c.Close < c.Open
		poke = c.High > c.Open+eps && aligned
	}
	return true, aligned, poke
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// ---------------------------------------------------------------------------
// Agregasi & report
// ---------------------------------------------------------------------------

type rec struct {
	start   time.Time
	label   string
	scen    detectors.Scenario
	of      detectors.Direction
	aLo     float64
	aHi     float64
	depth   depthRec
	hasDist bool
	aligned bool
	poke    bool
}

type stats struct{ v []float64 }

func (s *stats) add(x float64) { s.v = append(s.v, x) }
func (s *stats) summary() (n int, mean, med, p25, p75 float64) {
	n = len(s.v)
	if n == 0 {
		return
	}
	cp := append([]float64(nil), s.v...)
	sort.Float64s(cp)
	var sum float64
	for _, x := range cp {
		sum += x
	}
	return n, sum / float64(n), quant(cp, 0.5), quant(cp, 0.25), quant(cp, 0.75)
}

func quant(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q*float64(len(sorted)-1) + 0.5)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

type agg struct {
	days, sweptOF, sweptAny  int
	over, overP, penNotSwept stats // overshoot (subset swept) & penetrasi (subset not-swept)
	penAll                   stats // penetrasi semua hari
	dist, aligned, poke      int
}

func (a *agg) add(r rec) {
	a.days++
	if r.depth.sweptOF {
		a.sweptOF++
		a.over.add(r.depth.overATR)
		a.overP.add(r.depth.overPct)
	} else {
		a.penNotSwept.add(r.depth.penPct)
	}
	if r.depth.sweptAny {
		a.sweptAny++
	}
	a.penAll.add(r.depth.penPct)
	if r.hasDist {
		a.dist++
		if r.aligned {
			a.aligned++
		}
		if r.poke {
			a.poke++
		}
	}
}

func report(frame, desc string, p params, recs []rec, skip map[string]int) {
	fmt.Printf("════ FRAME: %s ════  (%s)\n", frame, desc)
	fmt.Printf("Param: scenario=%s  mode=%s  requireFVG=%v  of-atr-mult=%.2f  ny-poke-eps=%.2f×ATR  rentang=%s\n",
		p.scenFilter, p.scenMode, p.fvg, p.cfg.OFSwingATRMult, p.nyPokeEps, rngStr(p.from, p.to))
	fmt.Printf("Hari terklasifikasi: %d  (skip: %s)\n\n", len(recs), fmtSkip(skip))

	if len(recs) == 0 {
		return
	}

	total, amdx, xamd := &agg{}, &agg{}, &agg{}
	bull, bear := &agg{}, &agg{}
	for _, r := range recs {
		total.add(r)
		if r.scen == detectors.XAMD {
			xamd.add(r)
		} else {
			amdx.add(r)
		}
		if r.of == detectors.Bullish {
			bull.add(r)
		} else {
			bear.add(r)
		}
	}

	// (1) Sweep frekuensi.
	fmt.Println("① SWEEP akumulasi oleh manipulasi:")
	fmt.Printf("  %-7s | %5s | %8s | %7s | %8s | %7s\n", "grup", "hari", "sweptOF", "%OF", "sweptAny", "%Any")
	fmt.Println("  ------------------------------------------------------------")
	printSweep("AMDX", amdx)
	printSweep("XAMD", xamd)
	fmt.Println("  ------------------------------------------------------------")
	printSweep("TOTAL", total)
	fmt.Println("  -- per arah OF --")
	fmt.Printf("  %-22s | %5d hari | %d sweptOF (%.1f%%)\n", "bullish (harap sweep aLo)", bull.days, bull.sweptOF, pct(bull.sweptOF, bull.days))
	fmt.Printf("  %-22s | %5d hari | %d sweptOF (%.1f%%)\n", "bearish (harap sweep aHi)", bear.days, bear.sweptOF, pct(bear.sweptOF, bear.days))

	// (2) Kedalaman.
	fmt.Println("\n② KEDALAMAN sentuh akumulasi (TOTAL):")
	no, omean, omed, op25, op75 := total.over.summary()
	fmt.Printf("  OVERSHOOT (subset swept, n=%d) — seberapa jauh wick LEWAT extreme akumulasi:\n", no)
	fmt.Printf("    dalam ATR : mean %.2f | median %.2f | p25 %.2f | p75 %.2f\n", omean, omed, op25, op75)
	pno, pmean, pmed, pp25, pp75 := total.overP.summary()
	_ = pno
	fmt.Printf("    %% range   : mean %.0f%% | median %.0f%% | p25 %.0f%% | p75 %.0f%%\n", pmean, pmed, pp25, pp75)
	nn, nmean, nmed, np25, np75 := total.penNotSwept.summary()
	fmt.Printf("  PENETRASI (subset NOT-swept, n=%d) — kedalaman masuk range sebelum balik (0%%=tepi awal, 100%%=tepi sweep):\n", nn)
	fmt.Printf("    %% kedalaman: mean %.0f%% | median %.0f%% | p25 %.0f%% | p75 %.0f%%\n", nmean, nmed, np25, np75)
	an, amean, amed, ap25, ap75 := total.penAll.summary()
	_ = an
	fmt.Printf("  PENETRASI (semua hari) : mean %.0f%% | median %.0f%% | p25 %.0f%% | p75 %.0f%%\n", amean, amed, ap25, ap75)

	// (3) Distribusi.
	headline := "AMDX"
	hagg := amdx
	if p.scenFilter == "xamd" {
		headline, hagg = "XAMD", xamd
	}
	fmt.Printf("\n③ DISTRIBUSI vs OF (headline %s, n=%d sesi):\n", headline, hagg.dist)
	fmt.Printf("  %% close searah OF            : %.1f%%\n", pct(hagg.aligned, hagg.dist))
	fmt.Printf("  %% poke KONTRA-OF dulu, lalu searah : %.1f%%\n", pct(hagg.poke, hagg.dist))
	fmt.Printf("  (semua scenario, n=%d) close-searah %.1f%% | poke-dulu %.1f%%\n",
		total.dist, pct(total.aligned, total.dist), pct(total.poke, total.dist))

	if p.all {
		fmt.Println("\nPer-hari (A-day | scen | OF | aLo | aHi | sweptOF | overATR | pen% | distAligned | poke):")
		for _, r := range recs {
			fmt.Printf("  %s | %-4s | %-7s | %.2f | %.2f | %-5v | %6.2f | %3.0f%% | %-5v | %v\n",
				r.label, scenStr(r.scen), r.of.String(), r.aLo, r.aHi, r.depth.sweptOF,
				r.depth.overATR, r.depth.penPct, r.aligned, r.poke)
		}
	}
}

func printSweep(name string, a *agg) {
	fmt.Printf("  %-7s | %5d | %8d | %6.1f%% | %8d | %6.1f%%\n",
		name, a.days, a.sweptOF, pct(a.sweptOF, a.days), a.sweptAny, pct(a.sweptAny, a.days))
}

// ---------------------------------------------------------------------------
// Helper (replika lokal — pola repo, sama spt cmd/q1sweep)
// ---------------------------------------------------------------------------

// windowCandles = candle dengan Time di [from, to).
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

// ofMinLeg = ambang magnitude swing weekly-OF (replika engine.ofMinLeg).
func ofMinLeg(weekly []data.Candle, cfg engine.Config) float64 {
	if cfg.OFSwingATRMult <= 0 || len(weekly) <= cfg.ATRPeriod {
		return 0
	}
	if atr, ok := detectors.ATRAt(weekly, len(weekly)-1, cfg.ATRPeriod); ok {
		return cfg.OFSwingATRMult * atr
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
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func rngStr(from, to time.Time) string {
	if from.IsZero() && to.IsZero() {
		return "semua"
	}
	f, t := "…", "…"
	if !from.IsZero() {
		f = from.Format("2006-01-02")
	}
	if !to.IsZero() {
		t = to.AddDate(0, 0, -1).Format("2006-01-02")
	}
	return f + ".." + t
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
