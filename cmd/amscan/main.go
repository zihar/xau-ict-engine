// Command amscan (diagnostik AMS): dump semua ITL/ITH yang terdeteksi di window
// H1 sampai timestamp tertentu, lengkap status aktif/broken — untuk memverifikasi
// keputusan gate AMS (ActiveIntermediate) per kasus.
//
//	go run ./cmd/amscan -at "2026-01-15 14:00"
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
	"forex-backtest/internal/engine"
)

func main() {
	dataDir := flag.String("data", "data", "direktori cache CSV")
	instrument := flag.String("instrument", "XAU_USD", "instrumen")
	atStr := flag.String("at", "", "titik waktu (YYYY-MM-DD HH:MM UTC)")
	fvgTF := flag.String("fvg", "", "dump FVG di TF ini (W/D/H4/H1) dgn status fresh/taken — bukan ITL/ITH")
	flag.Parse()

	at, err := time.Parse("2006-01-02 15:04", *atStr)
	if err != nil {
		log.Fatalf("parse -at: %v (format: YYYY-MM-DD HH:MM)", err)
	}
	h1, err := data.ReadCSV(data.CSVPath(*dataDir, *instrument, "H1"))
	if err != nil {
		log.Fatalf("baca H1: %v", err)
	}
	loc, err := detectors.NYLocation()
	if err != nil {
		log.Fatalf("lokasi NY: %v", err)
	}
	ny := func(t time.Time) string { return t.In(loc).Format("2006-01-02 15:04 MST") }
	cfg := engine.DefaultConfig()

	// Mode dump FVG per TF (verifikasi POI fractal): list FVG searah + status
	// fresh/taken (body close menembus = taken) sampai `at`.
	if *fvgTF != "" {
		cs, err := data.ReadCSV(data.CSVPath(*dataDir, *instrument, *fvgTF))
		if err != nil {
			log.Fatalf("baca %s: %v", *fvgTF, err)
		}
		var upto []data.Candle
		for _, c := range cs {
			if !c.Time.After(at) {
				upto = append(upto, c)
			}
		}
		fvgs := detectors.DetectFVGs(upto, cfg.MinGapPips)
		fmt.Printf("FVG di TF %s s/d %s NY (harga %.2f):\n", *fvgTF, ny(at), upto[len(upto)-1].Close)
		fmt.Printf("  %-8s %-10s %-10s %-22s %s\n", "DIR", "BOTTOM", "TOP", "TERBENTUK (NY)", "STATUS")
		fmt.Println("  ──────────────────────────────────────────────────────────────────")
		for _, f := range fvgs {
			taken := false
			for k := f.Index + 1; k < len(upto); k++ {
				if f.Dir == detectors.Bullish && upto[k].Close < f.Bottom {
					taken = true
					break
				}
				if f.Dir == detectors.Bearish && upto[k].Close > f.Top {
					taken = true
					break
				}
			}
			status := "FRESH"
			if taken {
				status = "taken (body)"
			}
			fmt.Printf("  %-8s %-10.2f %-10.2f %-22s %s\n", f.Dir, f.Bottom, f.Top, ny(upto[f.Index].Time), status)
		}
		return
	}

	// Tiru engine: candle complete s/d `at`, lalu window H1Window terakhir.
	var closed []data.Candle
	for _, c := range h1 {
		if !c.Time.After(at) {
			closed = append(closed, c)
		}
	}
	w := closed
	if len(w) > cfg.H1Window {
		w = w[len(w)-cfg.H1Window:]
	}
	fmt.Printf("Window H1: %d candle, %s → %s NY (harga terakhir %.2f)\n",
		len(w), ny(w[0].Time), ny(w[len(w)-1].Time), w[len(w)-1].Close)
	fmt.Printf("(-at %s UTC = %s NY)\n\n", at.Format("2006-01-02 15:04"), ny(at))

	all := detectors.DetectIntermediatesMin(w, cfg.BreakType, 0)
	if cfg.AMSStrictStructure {
		all = detectors.DetectIntermediatesStrictMin(w, cfg.BreakType, 0)
	}
	fmt.Printf("%-4s %-10s %-10s %-18s %-18s %-9s\n", "KIND", "TYPE", "PIVOT", "PIVOT_TIME(NY)", "CONFIRM_TIME(NY)", "AKTIF?")
	fmt.Println("──────────────────────────────────────────────────────────────────────────────────────")
	for _, it := range all {
		active := !brokenAfter(w, it, cfg.BreakType)
		flag := "AKTIF"
		if !active {
			flag = "broken"
		}
		fmt.Printf("%-4s %-10s %-10.2f %-18s %-18s %-9s\n",
			it.Kind.String(), it.Type.String(), it.Pivot.Price,
			ny(it.Pivot.Time), ny(w[it.ConfirmIndex].Time), flag)
	}

	fmt.Println("\nKeputusan gate AMS (logika SAAT INI = ActiveIntermediate):")
	for _, k := range []detectors.ITKind{detectors.ITLow, detectors.ITHigh} {
		it, ok := detectors.ActiveIntermediate(w, k, cfg.BreakType, 0, cfg.AMSStrictStructure)
		if ok {
			fmt.Printf("  %s: AKTIF (pivot %.2f, %s) — entry %s diizinkan\n", k, it.Pivot.Price, it.Type, dir(k))
		} else {
			fmt.Printf("  %s: TIDAK aktif — entry %s DIBLOKIR\n", k, dir(k))
		}
	}
}

func dir(k detectors.ITKind) string {
	if k == detectors.ITHigh {
		return "sell"
	}
	return "buy"
}

// brokenAfter meniru intermediateBroken (unexported): ada candle setelah konfirmasi
// yang menembus pivot ke arah pembalik.
func brokenAfter(candles []data.Candle, it detectors.Intermediate, bt detectors.BreakType) bool {
	up := it.Kind == detectors.ITHigh
	for i := it.ConfirmIndex + 1; i < len(candles); i++ {
		c := candles[i]
		if up {
			if (bt == detectors.BreakBody && c.Close > it.Pivot.Price) || (bt == detectors.BreakWick && c.High > it.Pivot.Price) {
				return true
			}
		} else {
			if (bt == detectors.BreakBody && c.Close < it.Pivot.Price) || (bt == detectors.BreakWick && c.Low < it.Pivot.Price) {
				return true
			}
		}
	}
	return false
}
