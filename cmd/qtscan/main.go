// Command qtscan mendiagnosa klasifikasi skenario QT harian (AMDX vs XAMD) per
// trading day. XAMD = sesi Asia berperan "X" (D.2/D.4). Tujuannya membantu
// analisa manual: lihat tanggal mana saja yang Asia-nya dianggap X dan kenapa.
//
// Dua detektor directional close (lihat detectors.ClassifyDailyEx):
//   - atr   (D.2, default engine): |close−open| >= MinAtrMult × ATR_1H
//   - ratio (pertemuan 10):        |close−open| / range_sesi >= asia_range_ratio
//
// Setia ke engine: mereplikasi asiaSlice (18:00->00:00 NY) + ClassifyDailyEx,
// pakai DefaultConfig (MinAtrMult, MinGapPips, ATRPeriod). Read-only, offline.
//
//	go run ./cmd/qtscan                 # mode=both: bandingkan atr vs ratio + tabel hari beda
//	go run ./cmd/qtscan -mode atr       # satu detektor + contoh XAMD
//	go run ./cmd/qtscan -mode ratio -ratio 0.4
//	go run ./cmd/qtscan -mode both -all # daftar SEMUA hari beda
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/engine"
)

type day struct {
	start time.Time
	asia  []data.Candle
	last  int // index di h1 untuk candle Asia terakhir (≈00:00 NY)
	atr1h float64
}

func main() {
	def := engine.DefaultConfig() // basis param + default flag mirror engine
	dir := flag.String("data", "data", "direktori cache CSV")
	instrument := flag.String("instrument", "XAU_USD", "instrumen")
	mode := flag.String("mode", "both", "detektor A/X: atr | ratio | both")
	ratio := flag.Float64("ratio", 0, "ambang ratio (0 = pakai default config asia_range_ratio)")
	fvg := flag.Bool("fvg", def.AsiaRequireFVG, "syarat FVG: X wajib disertai FVG Asia (false = penentu murni true-move; default = engine)")
	n := flag.Int("n", 10, "jumlah contoh yang ditampilkan")
	all := flag.Bool("all", false, "tampilkan semua baris")
	flag.Parse()

	switch *mode {
	case "atr", "ratio", "both":
	default:
		log.Fatalf("mode %q tidak dikenal (atr|ratio|both)", *mode)
	}

	h1, err := data.ReadCSV(data.CSVPath(*dir, *instrument, "H1"))
	if err != nil {
		log.Fatalf("baca H1: %v (jalankan `go run ./cmd/fetch` dulu)", err)
	}
	loc, err := detectors.NYLocation()
	if err != nil {
		log.Fatalf("lokasi NY: %v", err)
	}
	cfg := def
	rangeRatio := cfg.AsiaRangeRatio
	if *ratio > 0 {
		rangeRatio = *ratio
	}

	// Kelompokkan candle per trading day (anchor 18:00 NY), urut.
	var days []*day
	idxByStart := map[time.Time]*day{}
	for i, c := range h1 {
		ds := detectors.TradingDayStart(c.Time, loc)
		asiaEnd := ds.Add(6 * time.Hour) // 00:00 NY
		if c.Time.Before(ds) || !c.Time.Before(asiaEnd) {
			continue // hanya candle sesi Asia [18:00, 00:00)
		}
		d := idxByStart[ds]
		if d == nil {
			d = &day{start: ds}
			idxByStart[ds] = d
			days = append(days, d)
		}
		d.asia = append(d.asia, c)
		d.last = i
	}

	// Saring hari yang bisa diklasifikasi (>=2 candle Asia + ATR siap).
	var valid []*day
	skipped := 0
	for _, d := range days {
		if len(d.asia) < 2 {
			skipped++
			continue
		}
		atr1h, ok := detectors.ATRAt(h1, d.last, cfg.ATRPeriod)
		if !ok {
			skipped++
			continue
		}
		d.atr1h = atr1h
		valid = append(valid, d)
	}

	fmt.Printf("Trading day terklasifikasi: %d  (skip %d: <2 candle/ATR belum siap)\n", len(valid), skipped)
	fmt.Printf("Param: MinAtrMult=%.2f  asia_range_ratio=%.2f  MinGapPips=%.1f  requireFVG=%v\n\n", cfg.MinAtrMult, rangeRatio, cfg.MinGapPips, *fvg)

	classify := func(d *day, m string) detectors.Scenario {
		return detectors.ClassifyDailyEx(d.asia, d.atr1h, cfg.MinAtrMult, cfg.MinGapPips, rangeRatio, m, *fvg)
	}

	if *mode != "both" {
		var xamd []*day
		for _, d := range valid {
			if classify(d, *mode) == detectors.XAMD {
				xamd = append(xamd, d)
			}
		}
		printDist(*mode, len(valid), len(xamd))
		limit := clamp(*n, *all, len(xamd))
		fmt.Printf("\nContoh tanggal XAMD (Asia=X) mode=%s — %d dari %d:\n", *mode, limit, len(xamd))
		for _, d := range xamd[:limit] {
			printRow(d, loc, cfg, rangeRatio)
		}
		return
	}

	// mode=both: distribusi tiap detektor + hari yang BERBEDA klasifikasinya.
	var xatr, xratio int
	var diff []*day
	for _, d := range valid {
		sa := classify(d, "atr")
		sr := classify(d, "ratio")
		if sa == detectors.XAMD {
			xatr++
		}
		if sr == detectors.XAMD {
			xratio++
		}
		if sa != sr {
			diff = append(diff, d)
		}
	}
	printDist("atr", len(valid), xatr)
	printDist("ratio", len(valid), xratio)
	agree := len(valid) - len(diff)
	fmt.Printf("\nSepakat: %d/%d (%.1f%%)  | BEDA: %d (%.1f%%)\n",
		agree, len(valid), pct(agree, len(valid)), len(diff), pct(len(diff), len(valid)))

	limit := clamp(*n, *all, len(diff))
	fmt.Printf("\nTanggal di mana atr vs ratio BEDA — %d dari %d (atr→ratio):\n", limit, len(diff))
	for _, d := range diff[:limit] {
		sa := classify(d, "atr")
		sr := classify(d, "ratio")
		fmt.Printf("  [%s→%s] ", scen(sa), scen(sr))
		printRow(d, loc, cfg, rangeRatio)
	}
}

func printDist(mode string, total, xamd int) {
	amdx := total - xamd
	fmt.Printf("mode=%-5s  AMDX(A): %d (%.1f%%)   XAMD(X): %d (%.1f%%)\n",
		mode, amdx, pct(amdx, total), xamd, pct(xamd, total))
}

func printRow(d *day, loc *time.Location, cfg engine.Config, rangeRatio float64) {
	open := d.asia[0].Open
	close := d.asia[len(d.asia)-1].Close
	move := abs(close - open)
	rng := candleRange(d.asia)
	fvgs := len(detectors.DetectFVGs(d.asia, cfg.MinGapPips))
	thrATR := cfg.MinAtrMult * d.atr1h
	var r float64
	if rng > 0 {
		r = move / rng
	}
	fmt.Printf("%s | open=%.2f close=%.2f |move|=%.2f | range=%.2f ratio=%.2f(thr%.2f) | thrATR=%.2f | FVG=%d\n",
		d.start.In(loc).Format("2006-01-02 (Mon)"), open, close, move, rng, r, rangeRatio, thrATR, fvgs)
}

func scen(s detectors.Scenario) string {
	if s == detectors.XAMD {
		return "X"
	}
	return "A"
}

func clamp(n int, all bool, max int) int {
	if all || n > max {
		return max
	}
	return n
}

func pct(a, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(a) / float64(total)
}

// candleRange = max High − min Low atas slice (mirror detectors.candleRange).
func candleRange(cs []data.Candle) float64 {
	if len(cs) == 0 {
		return 0
	}
	hi, lo := cs[0].High, cs[0].Low
	for _, c := range cs[1:] {
		if c.High > hi {
			hi = c.High
		}
		if c.Low < lo {
			lo = c.Low
		}
	}
	return hi - lo
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
