// Command maniplist men-dump dua list untuk backtest manual, MIRROR engine:
//
//	(1) MINGGU MANIPULASI (QT BULANAN) — per bulan, klasifikasi AMD bulanan dari
//	    H4 candle hari kalender 1–7 (week-1 proxy) via ClassifyDailyEx(mode atr,
//	    no-FVG) = mirror engine.classifyMonthlyQT. Peta minggu → quarter:
//	      AMDX → W1=A, W2=M, W3=D, W4=X   → minggu Manipulasi = MINGGU-2 (tgl 8–14)
//	      XAMD → W1=X, W2=A, W3=M, W4=D   → minggu Manipulasi = MINGGU-3 (tgl 15–21)
//
//	(2) HARI X (QT MINGGUAN) — per minggu (candle Senin 4H) klasifikasi QT
//	    Mingguan (D.4) via ClassifyWeekly (FVG-4H wajib, mode atr). Peta hari →
//	    quarter (WeeklyPhase):
//	      AMDX → Sen=A, Sel=M, Rab=D, Kam=X → hari X = KAMIS
//	      XAMD → Sen=X, Sel=A, Rab=M, Kam=D → hari X = SENIN
//
// Read-only, offline. Setia ke engine DefaultConfig.
//
//	go run ./cmd/maniplist                       # ringkasan + list ke stdout
//	go run ./cmd/maniplist -out-dir /tmp         # + tulis monthly_manip_week.csv & weekly_x_day.csv
//	go run ./cmd/maniplist -from 2026-01-01      # batasi rentang (filter berdasar tanggal acuan)
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
	"forex-backtest/internal/engine"
)

func main() {
	cfg := engine.DefaultConfig()
	dir := flag.String("data", "data", "direktori cache CSV")
	instrument := flag.String("instrument", "XAU_USD", "instrumen")
	outDir := flag.String("out-dir", "", "kalau diisi, tulis monthly_manip_week.csv & weekly_x_day.csv ke sini")
	fromS := flag.String("from", "", "batas awal (YYYY-MM-DD, kosong = semua)")
	toS := flag.String("to", "", "batas akhir (YYYY-MM-DD, kosong = semua)")
	flag.Parse()

	from := parseDate(*fromS, time.Time{})
	to := parseDate(*toS, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))

	h4, err := data.ReadCSV(data.CSVPath(*dir, *instrument, "H4"))
	if err != nil {
		log.Fatalf("baca H4: %v (jalankan `go run ./cmd/fetch` dulu)", err)
	}
	loc, err := detectors.NYLocation()
	if err != nil {
		log.Fatalf("lokasi NY: %v", err)
	}

	// === (1) MINGGU MANIPULASI (QT BULANAN) ===
	// Kelompokkan H4 per (tahun,bulan) NY, ambil candle hari 1–7 (week-1 proxy).
	type monthB struct {
		year     int
		month    time.Month
		week1    []data.Candle
		week1End int // index di h4 utk candle week-1 terakhir (utk ATR)
	}
	monthByKey := map[string]*monthB{}
	for i, c := range h4 {
		cNY := c.Time.In(loc)
		key := fmt.Sprintf("%04d-%02d", cNY.Year(), int(cNY.Month()))
		m := monthByKey[key]
		if m == nil {
			m = &monthB{year: cNY.Year(), month: cNY.Month(), week1End: -1}
			monthByKey[key] = m
		}
		if cNY.Day() <= 7 {
			m.week1 = append(m.week1, c)
			m.week1End = i
		}
	}
	type moRow struct {
		year      int
		month     time.Month
		scen      detectors.Scenario
		manipWeek int       // 2 (AMDX) / 3 (XAMD)
		manipFrom time.Time // tgl 8 (AMDX) / 15 (XAMD)
		manipTo   time.Time // tgl 14 / 21
	}
	var months []*monthB
	for _, m := range monthByKey {
		months = append(months, m)
	}
	sort.Slice(months, func(i, j int) bool {
		if months[i].year != months[j].year {
			return months[i].year < months[j].year
		}
		return months[i].month < months[j].month
	})
	var monthly []moRow
	for _, m := range months {
		ref := time.Date(m.year, m.month, 1, 12, 0, 0, 0, loc) // acuan tgl-1 utk filter rentang
		if !inRange(ref, from, to) {
			continue
		}
		if len(m.week1) == 0 || m.week1End < 0 {
			continue
		}
		atr4h, _ := detectors.ATRAt(h4, m.week1End, cfg.ATRPeriod)
		// Mirror classifyMonthlyQT: minGapPips=0, rangeRatio=0, mode atr, requireFVG=false.
		scen := detectors.ClassifyDailyEx(m.week1, atr4h, cfg.MinAtrMult, 0, 0, "atr", false)
		manipWeek := 2
		startDay := 8
		if scen == detectors.XAMD {
			manipWeek, startDay = 3, 15
		}
		monthly = append(monthly, moRow{
			year: m.year, month: m.month, scen: scen, manipWeek: manipWeek,
			manipFrom: time.Date(m.year, m.month, startDay, 0, 0, 0, 0, loc),
			manipTo:   time.Date(m.year, m.month, startDay+6, 0, 0, 0, 0, loc),
		})
	}

	// === (2) HARI X (QT MINGGUAN) ===
	// Bucket H4 per trading-day; kumpulkan trading-day Senin → ClassifyWeekly.
	type h4B struct {
		candles []data.Candle
		lastIdx int
	}
	h4ByDay := map[time.Time]*h4B{}
	for i, c := range h4 {
		ds := detectors.TradingDayStart(c.Time, loc)
		g := h4ByDay[ds]
		if g == nil {
			g = &h4B{}
			h4ByDay[ds] = g
		}
		g.candles = append(g.candles, c)
		g.lastIdx = i
	}
	type wkRow struct {
		monday time.Time
		scen   detectors.Scenario
		xDay   time.Time // Kamis (AMDX) / Senin (XAMD)
	}
	var mondays []time.Time
	for ds := range h4ByDay {
		if ds.Weekday() == time.Monday && inRange(ds, from, to) {
			mondays = append(mondays, ds)
		}
	}
	sort.Slice(mondays, func(i, j int) bool { return mondays[i].Before(mondays[j]) })
	var weekly []wkRow
	for _, ds := range mondays {
		g := h4ByDay[ds]
		if len(g.candles) < 2 {
			continue
		}
		atr4h, ok := detectors.ATRAt(h4, g.lastIdx, cfg.ATRPeriod)
		if !ok {
			continue
		}
		scen := detectors.ClassifyWeekly(g.candles, atr4h, cfg.MinAtrMult, cfg.MinGapPips)
		x := ds // XAMD → hari X = Senin
		if scen != detectors.XAMD {
			x = ds.AddDate(0, 0, 3) // AMDX → hari X = Kamis
		}
		weekly = append(weekly, wkRow{monday: ds, scen: scen, xDay: x})
	}

	// === Output ===
	fmt.Printf("Rentang data H4: %s → %s\n", h4[0].Time.In(loc).Format("2006-01-02"), h4[len(h4)-1].Time.In(loc).Format("2006-01-02"))
	fmt.Printf("Param mirror engine: MinAtrMult=%.2f  ATRPeriod=%d\n\n", cfg.MinAtrMult, cfg.ATRPeriod)

	fmt.Printf("=== (1) MINGGU MANIPULASI (QT BULANAN) — %d bulan ===\n", len(monthly))
	fmt.Printf("Skenario bulanan dari H4 hari 1–7. Minggu Manipulasi: AMDX→Minggu-2 (tgl 8–14), XAMD→Minggu-3 (tgl 15–21).\n\n")
	fmt.Printf("%-9s  %-5s  %-7s  %-25s\n", "bulan", "QT", "minggu", "rentang_minggu_manipulasi")
	for _, m := range monthly {
		fmt.Printf("%04d-%02d   %-5s  W%-6d %s s/d %s\n", m.year, int(m.month), scenName(m.scen),
			m.manipWeek, m.manipFrom.Format("2006-01-02"), m.manipTo.Format("2006-01-02"))
	}

	fmt.Printf("\n=== (2) HARI X (QT MINGGUAN) — %d minggu ===\n", len(weekly))
	fmt.Printf("Skenario mingguan dari candle Senin (4H). Hari X: AMDX→Kamis, XAMD→Senin.\n\n")
	fmt.Printf("%-12s  %-5s  %-22s\n", "week(Senin)", "QT", "hari_X")
	for _, w := range weekly {
		fmt.Printf("%-12s  %-5s  %s\n", w.monday.In(loc).Format("2006-01-02"), scenName(w.scen),
			w.xDay.In(loc).Format("2006-01-02 (Mon)"))
	}

	// === CSV opsional ===
	if *outDir != "" {
		var b strings.Builder
		b.WriteString("month,monthly_scenario,manip_week_num,manip_week_from,manip_week_to\n")
		for _, m := range monthly {
			b.WriteString(fmt.Sprintf("%04d-%02d,%s,%d,%s,%s\n", m.year, int(m.month), scenName(m.scen),
				m.manipWeek, m.manipFrom.Format("2006-01-02"), m.manipTo.Format("2006-01-02")))
		}
		p1 := filepath.Join(*outDir, "monthly_manip_week.csv")
		writeFile(p1, b.String())

		b.Reset()
		b.WriteString("week_monday,weekly_scenario,x_day,x_weekday\n")
		for _, w := range weekly {
			xd := w.xDay.In(loc)
			b.WriteString(fmt.Sprintf("%s,%s,%s,%s\n", w.monday.In(loc).Format("2006-01-02"),
				scenName(w.scen), xd.Format("2006-01-02"), xd.Format("Mon")))
		}
		p2 := filepath.Join(*outDir, "weekly_x_day.csv")
		writeFile(p2, b.String())
		fmt.Printf("\nDitulis: %s (%d baris) & %s (%d baris)\n", p1, len(monthly), p2, len(weekly))
	}
}

func scenName(s detectors.Scenario) string {
	if s == detectors.XAMD {
		return "XAMD"
	}
	return "AMDX"
}

func parseDate(s string, def time.Time) time.Time {
	if s == "" {
		return def
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		log.Fatalf("tanggal %q tidak valid (YYYY-MM-DD)", s)
	}
	return t
}

func inRange(t, from, to time.Time) bool {
	return !t.Before(from) && t.Before(to)
}

func writeFile(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		log.Fatalf("tulis %s: %v", path, err)
	}
}
