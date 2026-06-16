// Command ithfreq menganalisa KAPAN ITH/ITL (AMS layer C, di H1) paling sering
// TERBENTUK — diukur pada waktu KONFIRMASI break (candles[ConfirmIndex].Time),
// momen ITH/ITL resmi terbentuk di AMS (bukan waktu pivot high/low aktual).
//
// Tiap kejadian diklasifikasi ke dimensi waktu/fase QT (Section D) lalu
// diagregasi jadi tabel frekuensi:
//   - Sesi harian (Asia/London/NY-AM/PM)            → "quarter berapa dalam hari"
//   - Sub-quarter sesi (Q1 akumulasi … Q4 distribusi) @1.5 jam
//   - Matriks Sesi × Sub-quarter
//   - Hari dalam minggu (trading weekday)           → "di minggu itu hari mana"
//   - DailyPhase A/M/D/X (manipulasi/distribusi tingkat sesi)
//   - WeeklyPhase A/M/D/X/Special (tingkat hari)
//   - DayType (normal/suspected_accum/heavy_accum/heavy_expanding)
//
// Setia ke engine (0-drift): pakai detectors.DetectIntermediates + DefaultBreakType,
// mereplikasi asiaSlice/classifyDayType/ClassifyDaily memakai DefaultConfig.
// Read-only, offline (tidak menyentuh OANDA, tidak menulis state apa pun).
//
//	go run ./cmd/ithfreq                       # seluruh cache
//	go run ./cmd/ithfreq -from 2024-01-01      # filter rentang (waktu konfirmasi)
//	go run ./cmd/ithfreq -type standard        # hanya ITH/ITL standard (buang fast_early)
//	go run ./cmd/ithfreq -csv > rows.csv       # dump baris mentah utk olah lanjut
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
)

func main() {
	dir := flag.String("data", "data", "direktori cache CSV")
	instrument := flag.String("instrument", "XAU_USD", "instrumen")
	fromS := flag.String("from", "", "filter: waktu konfirmasi >= YYYY-MM-DD (UTC, opsional)")
	toS := flag.String("to", "", "filter: waktu konfirmasi <= YYYY-MM-DD (UTC, opsional)")
	typeFilter := flag.String("type", "all", "filter jenis: all|standard|fast_early")
	csvOut := flag.Bool("csv", false, "dump baris mentah CSV ke stdout (bukan tabel)")
	flag.Parse()

	h1, err := data.ReadCSV(data.CSVPath(*dir, *instrument, "H1"))
	if err != nil {
		log.Fatalf("baca H1: %v (jalankan `go run ./cmd/fetch` dulu)", err)
	}
	daily, err := data.ReadCSV(data.CSVPath(*dir, *instrument, "D"))
	if err != nil {
		log.Fatalf("baca D: %v", err)
	}
	loc, err := detectors.NYLocation()
	if err != nil {
		log.Fatalf("lokasi NY: %v", err)
	}
	cfg := engine.DefaultConfig()

	from, to := parseRange(*fromS, *toS)

	// Deteksi SEMUA ITH/ITL ter-konfirmasi di H1 (BreakWick = default engine).
	its := detectors.DetectIntermediates(h1, detectors.DefaultBreakType)

	// Precompute scenario (AMDX/XAMD) + DayType per trading day — di-cache supaya
	// tak dihitung ulang per kejadian.
	scen := map[time.Time]detectors.Scenario{}
	dtyp := map[time.Time]detectors.DayType{}
	getDay := func(t time.Time) (detectors.Scenario, detectors.DayType) {
		ds := detectors.TradingDayStart(t, loc)
		sc, ok := scen[ds]
		if !ok {
			sc = scenarioForDay(h1, loc, cfg, ds)
			scen[ds] = sc
			dtyp[ds] = dayTypeForDay(h1, daily, loc, cfg, ds)
		}
		return sc, dtyp[ds]
	}

	// Agregator.
	type cnt struct{ itl, ith int }
	bySession := map[string]*cnt{}
	byQuarter := map[int]*cnt{}
	byMatrix := map[string]*cnt{} // "session|q"
	byWeekday := map[time.Weekday]*cnt{}
	byDaily := map[string]*cnt{}
	byWeekly := map[string]*cnt{}
	byDayType := map[string]*cnt{}
	add := func(m map[string]*cnt, k string, isITH bool) {
		c := m[k]
		if c == nil {
			c = &cnt{}
			m[k] = c
		}
		if isITH {
			c.ith++
		} else {
			c.itl++
		}
	}

	var totalITL, totalITH int
	var minT, maxT time.Time

	if *csvOut {
		fmt.Println("confirm_time_ny,kind,type,session,quarter,weekday,daily_phase,weekly_phase,daytype")
	}

	for _, it := range its {
		if it.ConfirmIndex < 0 || it.ConfirmIndex >= len(h1) {
			continue
		}
		if *typeFilter == "standard" && it.Type != detectors.ITStandard {
			continue
		}
		if *typeFilter == "fast_early" && it.Type != detectors.ITFastEarly {
			continue
		}
		t := h1[it.ConfirmIndex].Time
		if !from.IsZero() && t.Before(from) {
			continue
		}
		if !to.IsZero() && t.After(to) {
			continue
		}

		sc, dt := getDay(t)
		session := detectors.Session(t, loc)
		quarter := detectors.SessionQuarter(t, loc)
		wd := detectors.TradingWeekday(t, loc)
		dphase := detectors.DailyPhase(sc, session)
		wphase := detectors.WeeklyPhase(sc, wd)
		isITH := it.Kind == detectors.ITHigh

		if *csvOut {
			fmt.Printf("%s,%s,%s,%s,%d,%s,%s,%s,%s\n",
				t.In(loc).Format("2006-01-02T15:04"), it.Kind, it.Type,
				session, quarter, wd, dphase, wphase, dt)
			continue
		}

		if isITH {
			totalITH++
		} else {
			totalITL++
		}
		if minT.IsZero() || t.Before(minT) {
			minT = t
		}
		if t.After(maxT) {
			maxT = t
		}
		add(bySession, session.String(), isITH)
		bq := byQuarter[quarter]
		if bq == nil {
			bq = &cnt{}
			byQuarter[quarter] = bq
		}
		if isITH {
			bq.ith++
		} else {
			bq.itl++
		}
		add(byMatrix, fmt.Sprintf("%s|%d", session, quarter), isITH)
		bw := byWeekday[wd]
		if bw == nil {
			bw = &cnt{}
			byWeekday[wd] = bw
		}
		if isITH {
			bw.ith++
		} else {
			bw.itl++
		}
		add(byDaily, dphase.String(), isITH)
		add(byWeekly, wphase.String(), isITH)
		add(byDayType, dt.String(), isITH)
	}

	if *csvOut {
		return
	}

	total := totalITL + totalITH
	if total == 0 {
		fmt.Println("Tidak ada ITH/ITL pada rentang/filter ini.")
		return
	}

	// ── Header ────────────────────────────────────────────────────────────────
	fmt.Println("══════════════════════════════════════════════════════════════════")
	fmt.Printf("ANALISA FREKUENSI PEMBENTUKAN ITH/ITL — %s\n", *instrument)
	fmt.Println("TF=H1 (AMS layer C) · acuan=waktu KONFIRMASI break · jam=NY")
	fmt.Printf("Rentang konfirmasi: %s → %s\n", minT.In(loc).Format("2006-01-02"), maxT.In(loc).Format("2006-01-02"))
	fmt.Printf("Total kejadian: %d  (ITL=%d %.1f%% · ITH=%d %.1f%%)  filter-jenis=%s\n",
		total, totalITL, pct(totalITL, total), totalITH, pct(totalITH, total), *typeFilter)
	fmt.Println("══════════════════════════════════════════════════════════════════")

	// helper cetak baris tabel
	type row struct {
		label     string
		itl, ith  int
	}
	printTable := func(title, col string, rows []row) {
		fmt.Printf("\n%s\n", title)
		fmt.Printf("  %-18s %8s %8s %8s %8s\n", col, "ITL", "ITH", "TOTAL", "%")
		fmt.Printf("  %s\n", "------------------------------------------------------------")
		for _, r := range rows {
			tt := r.itl + r.ith
			fmt.Printf("  %-18s %8d %8d %8d %7.1f%%\n", r.label, r.itl, r.ith, tt, pct(tt, total))
		}
	}

	// T1 — Sesi harian (urut Asia→London→NY-AM→PM)
	sessOrder := []string{"asia", "london", "ny_am", "pm"}
	var t1 []row
	for _, s := range sessOrder {
		if c := bySession[s]; c != nil {
			t1 = append(t1, row{sessLabel(s), c.itl, c.ith})
		}
	}
	printTable("T1 — SESI HARIAN (quarter 6-jam dalam satu hari)", "sesi", t1)

	// T2 — Sub-quarter sesi
	qLabel := map[int]string{1: "Q1 akumulasi", 2: "Q2 manipulasi", 3: "Q3 distribusi", 4: "Q4 distribusi/X"}
	var t2 []row
	for q := 1; q <= 4; q++ {
		if c := byQuarter[q]; c != nil {
			t2 = append(t2, row{qLabel[q], c.itl, c.ith})
		}
	}
	printTable("T2 — SUB-QUARTER SESI (@1.5 jam; pola QT akumulasi→manipulasi→distribusi)", "quarter", t2)

	// T3 — Matriks Sesi × Sub-quarter
	fmt.Printf("\nT3 — MATRIKS SESI × SUB-QUARTER (TOTAL kejadian; %% dari total)\n")
	fmt.Printf("  %-8s %10s %10s %10s %10s\n", "sesi\\q", "Q1", "Q2", "Q3", "Q4")
	fmt.Printf("  %s\n", "------------------------------------------------------------")
	for _, s := range sessOrder {
		line := fmt.Sprintf("  %-8s", sessShort(s))
		any := false
		for q := 1; q <= 4; q++ {
			c := byMatrix[fmt.Sprintf("%s|%d", s, q)]
			n := 0
			if c != nil {
				n = c.itl + c.ith
				any = true
			}
			line += fmt.Sprintf(" %5d %4.1f%%", n, pct(n, total))
		}
		if any {
			fmt.Println(line)
		}
	}

	// T4 — Hari dalam minggu
	wdOrder := []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
	var t4 []row
	for _, wd := range wdOrder {
		if c := byWeekday[wd]; c != nil {
			t4 = append(t4, row{wd.String(), c.itl, c.ith})
		}
	}
	printTable("T4 — HARI DALAM MINGGU (trading weekday, anchor 18:00 NY)", "hari", t4)

	// T5 — DailyPhase
	phaseOrder := []string{"A", "M", "D", "X", "Special"}
	phaseLabel := map[string]string{"A": "A akumulasi", "M": "M manipulasi", "D": "D distribusi", "X": "X ekspansi", "Special": "Special (Jumat)"}
	var t5 []row
	for _, p := range phaseOrder {
		if c := byDaily[p]; c != nil {
			t5 = append(t5, row{phaseLabel[p], c.itl, c.ith})
		}
	}
	printTable("T5 — DAILY PHASE (manipulasi/distribusi tingkat SESI, per scenario AMDX/XAMD)", "fase", t5)

	// T6 — WeeklyPhase
	var t6 []row
	for _, p := range phaseOrder {
		if c := byWeekly[p]; c != nil {
			t6 = append(t6, row{phaseLabel[p], c.itl, c.ith})
		}
	}
	printTable("T6 — WEEKLY PHASE (manipulasi/distribusi tingkat HARI dalam minggu)", "fase", t6)

	// T7 — DayType
	dtOrder := []string{"normal", "suspected_accum", "heavy_accum", "heavy_expanding"}
	var t7 []row
	for _, d := range dtOrder {
		if c := byDayType[d]; c != nil {
			t7 = append(t7, row{d, c.itl, c.ith})
		}
	}
	printTable("T7 — DAY TYPE (klasifikasi AMD harian D.7)", "daytype", t7)

	fmt.Println("\nCatatan: semua angka dari 1 rezim bull (data tercache); sampel per-segmen kecil.")
	fmt.Println("Acuan = waktu KONFIRMASI break (bukan pivot ekstrem). Lihat -csv utk baris mentah.")
}

// scenarioForDay mereplikasi asiaSlice+ClassifyDaily (engine.go) untuk trading
// day ber-anchor ds (18:00 NY). Default AMDX bila sesi Asia kurang dari 2 candle.
func scenarioForDay(h1 []data.Candle, loc *time.Location, cfg engine.Config, ds time.Time) detectors.Scenario {
	asiaEnd := ds.Add(6 * time.Hour) // 00:00 NY
	var asia []data.Candle
	lastIdx := -1
	for i, c := range h1 {
		if !c.Time.Before(ds) && c.Time.Before(asiaEnd) {
			asia = append(asia, c)
			lastIdx = i
		}
	}
	if len(asia) < 2 {
		return detectors.AMDX
	}
	atr1h, ok := detectors.ATRAt(h1, lastIdx, cfg.ATRPeriod)
	if !ok {
		return detectors.AMDX
	}
	return detectors.ClassifyDaily(asia, atr1h, cfg.MinAtrMult, cfg.MinGapPips)
}

// dayTypeForDay mereplikasi classifyDayType (engine.go:1572) untuk trading day
// ber-anchor ds. ATR daily diambil dari candle daily ter-CLOSED sebelum ds
// (deterministik, mengikuti semangat closedBy engine).
func dayTypeForDay(h1, daily []data.Candle, loc *time.Location, cfg engine.Config, ds time.Time) detectors.DayType {
	// index daily terakhir dengan Time < ds (candle hari sebelumnya yg sudah close).
	di := sort.Search(len(daily), func(i int) bool { return !daily[i].Time.Before(ds) }) - 1
	if di < cfg.ATRPeriod {
		return detectors.DayNormal
	}
	atrDaily, ok := detectors.ATRAt(daily, di, cfg.ATRPeriod)
	if !ok || atrDaily <= 0 {
		return detectors.DayNormal
	}
	asiaR := sessionRange(h1, ds, ds.Add(6*time.Hour))
	londonR := sessionRange(h1, ds.Add(6*time.Hour), ds.Add(12*time.Hour))
	asiaNet := sessionNet(h1, ds, ds.Add(12*time.Hour))
	if londonR > 0 && detectors.HeavyAccumStage1(asiaR, londonR, atrDaily, cfg.HeavyAccumMaxRangePct) {
		return detectors.DayHeavyAccum
	}
	if detectors.HeavyExpanding(asiaNet, atrDaily, 1.3) { // 1.3 = hardcoded di engine
		return detectors.DayHeavyExpanding
	}
	return detectors.DayNormal
}

// sessionRange/sessionNet = mirror engine.go:1593-1637 (0-drift DayType).
func sessionRange(c []data.Candle, from, to time.Time) float64 {
	hi, lo := 0.0, 0.0
	seen := false
	for _, x := range c {
		if x.Time.Before(from) || !x.Time.Before(to) {
			continue
		}
		if !seen {
			hi, lo, seen = x.High, x.Low, true
			continue
		}
		if x.High > hi {
			hi = x.High
		}
		if x.Low < lo {
			lo = x.Low
		}
	}
	if !seen {
		return 0
	}
	return hi - lo
}

func sessionNet(c []data.Candle, from, to time.Time) float64 {
	var first, last data.Candle
	seen := false
	for _, x := range c {
		if x.Time.Before(from) || !x.Time.Before(to) {
			continue
		}
		if !seen {
			first, seen = x, true
		}
		last = x
	}
	if !seen {
		return 0
	}
	d := last.Close - first.Open
	if d < 0 {
		d = -d
	}
	return d
}

func parseRange(fromS, toS string) (from, to time.Time) {
	if fromS != "" {
		t, err := time.Parse("2006-01-02", fromS)
		if err != nil {
			log.Fatalf("-from: %v", err)
		}
		from = t
	}
	if toS != "" {
		t, err := time.Parse("2006-01-02", toS)
		if err != nil {
			log.Fatalf("-to: %v", err)
		}
		to = t.Add(24 * time.Hour) // inklusif hari -to
	}
	return from, to
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

func sessLabel(s string) string {
	switch s {
	case "asia":
		return "Asia 18-00"
	case "london":
		return "London 00-06"
	case "ny_am":
		return "NY-AM 06-12"
	default:
		return "PM 12-18"
	}
}

func sessShort(s string) string {
	switch s {
	case "ny_am":
		return "NY-AM"
	default:
		return s
	}
}
