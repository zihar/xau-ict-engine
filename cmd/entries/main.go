// Command entries menjalankan backtest lalu melaporkan SETIAP entry beserta
// ALASAN-nya (konteks piramida yang memicu) + outcome (exit & R). Opsi -svgdir
// menulis 1 chart SVG ber-anotasi per entry sehingga bisa ditelusuri dari waktu
// ke waktu. Opsi -detail N mencetak narasi langkah-demi-langkah entry ke-N.
//
// Contoh:
//
//	go run ./cmd/entries                          # tabel + alasan tiap entry
//	go run ./cmd/entries -svgdir /tmp/entries      # + 1 SVG per entry
//	go run ./cmd/entries -detail 3                 # narasi penuh entry #3
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"forex-backtest/internal/chartann"
	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
	"forex-backtest/internal/engine"
	"forex-backtest/internal/report"
	"forex-backtest/internal/viz"
)

// nyLoc = America/New_York; semua waktu output dalam waktu NY.
var nyLoc = func() *time.Location {
	l, err := detectors.NYLocation()
	if err != nil {
		return time.UTC
	}
	return l
}()

func nyf(t time.Time) string { return t.In(nyLoc).Format("2006-01-02 15:04") }
func nyz(t time.Time) string { return t.In(nyLoc).Format("2006-01-02 15:04 MST") }

func main() {
	var (
		dir        = flag.String("data", "data", "direktori cache CSV")
		instrument = flag.String("instrument", "XAU_USD", "instrumen")
		balance    = flag.Float64("balance", 25000, "saldo (sizing)")
		gap        = flag.Float64("gap", -1, "override MinGapPips (<0 = default)")
		conf       = flag.Int("conf", -1, "override ConfluenceMin (<0 = default)")
		m5         = flag.Float64("m5", -1, "override MinSwingPipsM5 (<0 = default)")
		svgDir     = flag.String("svgdir", "", "tulis 1 chart SVG per entry ke direktori ini (kosong = tidak)")
		detail     = flag.Int("detail", 0, "cetak narasi langkah-demi-langkah entry ke-N (1-based; 0 = off)")
		fibLevels  = flag.Bool("fiblevels", true, "gambar level Fib 0/1 + leg makro di chart per entry")
	)
	flag.Parse()

	load := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(*dir, *instrument, g))
		if err != nil {
			log.Fatalf("baca %s: %v (jalankan `go run ./cmd/fetch` dulu)", g, err)
		}
		return c
	}
	tf := engine.TFData{Weekly: load("W"), Daily: load("D"), H4: load("H4"), H1: load("H1"), M5: load("M5")}

	cfg := engine.DefaultConfig()
	cfg.StartBalance = *balance
	if *gap >= 0 {
		cfg.MinGapPips = *gap
	}
	if *conf >= 0 {
		cfg.ConfluenceMin = *conf
	}
	if *m5 >= 0 {
		cfg.MinSwingPipsM5 = *m5
	}

	res, err := engine.Run(tf, cfg)
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	if len(res.Trades) == 0 {
		fmt.Println("Tidak ada entry dengan parameter ini.")
		return
	}

	// Mode -detail: narasi penuh satu entry.
	if *detail > 0 {
		if *detail > len(res.Trades) {
			log.Fatalf("entry #%d tidak ada (cuma %d entry)", *detail, len(res.Trades))
		}
		t := res.Trades[*detail-1]
		fmt.Printf("Narasi entry #%d (%s NY):\n\n", *detail, nyf(t.Time))
		n := engine.Narrate(tf, cfg, t.Time)
		printNarrative(n, *instrument)
		fmt.Printf("\nOutcome: exit %s @ %.2f, %+.2fR ($%+.2f)\n", t.ExitReason, t.ExitPrice, t.RRealized, t.PnLUSD)
		return
	}

	// Tabel ringkas.
	fmt.Printf("%d entry (signal layer) — %s\n\n", len(res.Trades), *instrument)
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "#\tWAKTU(NY)\tDIR\tREGIME\tIT-TYPE\tSESI\tSCN\tPOI\tTF\tRR\tEXIT\tR")
	var totR float64
	wins := 0
	for i, t := range res.Trades {
		if t.RRealized > 0 {
			wins++
		}
		totR += t.RRealized
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\tT%d/c%d\t%s\t1:%g\t%s\t%+.2f\n",
			i+1, nyf(t.Time), t.Dir, t.Regime, t.ITHITLType,
			t.Session, t.Scenario, t.POITier, t.POIConfluence, t.POITF, t.RR, t.ExitReason, t.RRealized)
	}
	w.Flush()
	fmt.Printf("\nTOTAL: %d entry, win %.1f%%, TotalR %+.2f\n", len(res.Trades),
		float64(wins)/float64(len(res.Trades))*100, totR)

	// Alasan tiap entry.
	fmt.Println("\n=== ALASAN TIAP ENTRY ===")
	for i, t := range res.Trades {
		fmt.Printf("#%d  %s\n   %s\n", i+1, nyz(t.Time), report.TradeReason(t))
	}

	// SVG per entry.
	if *svgDir != "" {
		if err := os.MkdirAll(*svgDir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", *svgDir, err)
		}
		for i, t := range res.Trades {
			n := engine.Narrate(tf, cfg, t.Time)
			name := fmt.Sprintf("entry_%02d_%s_%s.svg", i+1, t.Time.In(nyLoc).Format("20060102-1504"), t.Dir)
			path := filepath.Join(*svgDir, name)
			f, err := os.Create(path)
			if err != nil {
				log.Fatalf("buat %s: %v", path, err)
			}
			err = viz.RenderSVG(f, n.Plot, annotationsFor(n, *instrument, t, *fibLevels))
			f.Close()
			if err != nil {
				log.Fatalf("render %s: %v", path, err)
			}
		}
		abs, _ := filepath.Abs(*svgDir)
		fmt.Printf("\n%d chart SVG → %s/  (buka di browser; urut kronologis sesuai nama file)\n", len(res.Trades), abs)
	}
}

// annotationsFor membangun anotasi chart dari narasi + outcome trade
// (POI/Fib/ITH-ITL/trigger via chartann, plus garis EXIT aktual).
func annotationsFor(n engine.ScanNarrative, instrument string, t engine.Trade, fibLevels bool) viz.Annotations {
	ann := chartann.Build(n, fibLevels)
	ann.Title = fmt.Sprintf("%s — %s @ %.2f (exit %s %+.2fR)", instrument, strings.ToUpper(t.Dir.String()), t.Entry, t.ExitReason, t.RRealized)
	ann.Subtitle = fmt.Sprintf("%s NY · regime %s · POI T%d/c%d · RR 1:%g", nyf(t.Time), t.Regime, t.POITier, t.POIConfluence, t.RR)
	ann.Levels = append(ann.Levels, viz.Level{Price: t.ExitPrice, Label: "EXIT", Color: "#ff9800", Dash: true})
	return ann
}

// printNarrative mencetak narasi langkah-demi-langkah (untuk -detail).
func printNarrative(n engine.ScanNarrative, instrument string) {
	fmt.Printf("┌─ SCAN %s @ %s ─ harga %.2f\n│\n", instrument, nyz(n.At), n.Price)
	for _, s := range n.Steps {
		fmt.Printf("│ %s %s\n", icon(s.Status), s.Name)
		for _, line := range wrap(s.Text, 88) {
			fmt.Printf("│     %s\n", line)
		}
	}
	if n.HasSetup {
		fmt.Printf("│\n└─ ✅ SETUP VALID — %s\n", n.Decision)
	} else {
		fmt.Printf("│\n└─ ❌ %s\n", n.Decision)
	}
}

func icon(s engine.StepStatus) string {
	switch s {
	case engine.StepPass:
		return "✓"
	case engine.StepFail:
		return "✗"
	default:
		return "•"
	}
}

func wrap(s string, width int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line == "" {
			line = word
		} else if len(line)+1+len(word) > width {
			out = append(out, line)
			line = word
		} else {
			line += " " + word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
