// Command sweep menjalankan engine backtest atas GRID parameter (kalibrasi
// Section M / N.4) lalu cetak tabel komparatif ter-ranking. Data di-load sekali,
// dipakai ulang tiap kombinasi. Murni offline (tidak menyentuh OANDA).
//
// Default grid menyapu 3 sumbu paling berdampak ke noise/kualitas sinyal:
// magnitude filter swing 5m, confluence minimum POI, dan min gap FVG. Ubah
// daftar nilai via flag (CSV), mis: -m5 0,20,40 -conf 2,3 -gap 5,10.
//
//	go run ./cmd/sweep
//	go run ./cmd/sweep -m5 0,15,30,50 -conf 2,3 -gap 5 -sort pf
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"forex-backtest/internal/data"
	"forex-backtest/internal/engine"
	"forex-backtest/internal/report"
)

type row struct {
	label string
	cfg   engine.Config
	m     report.Metrics
}

func main() {
	var (
		dir        = flag.String("data", "data", "direktori cache CSV")
		instrument = flag.String("instrument", "XAU_USD", "instrumen")
		balance    = flag.Float64("balance", 25000, "saldo awal")
		m5List     = flag.String("m5", "0,20,40", "grid MinSwingPipsM5 (CSV pip)")
		confList   = flag.String("conf", "2,3", "grid ConfluenceMin POI (CSV)")
		gapList    = flag.String("gap", "5,10", "grid MinGapPips FVG (CSV pip)")
		moList     = flag.String("mogate", "false", "grid MOGate Midnight Open (CSV bool, mis. false,true)")
		releqList  = flag.String("releqsweep", "false", "grid RelEqSweepGate wait-for-sweep (CSV bool)")
		relTolPct  = flag.String("releqtolpct", "0.05", "grid RelEqTolerancePct (CSV %, mis. 0.05,0.1)")
		relTolPips = flag.String("releqtolpips", "0", "grid RelEqTolerancePips (CSV pip; >0 override pct)")
		sortBy     = flag.String("sort", "totr", "urut: totr|pf|winrate|avgr")
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
	fmt.Printf("Loaded %s: H1=%d M5=%d. Grid: m5=%s conf=%s gap=%s\n\n",
		*instrument, len(tf.H1), len(tf.M5), *m5List, *confList, *gapList)

	m5s := parseFloats(*m5List)
	confs := parseInts(*confList)
	gaps := parseFloats(*gapList)
	mos := parseBools(*moList)
	releqs := parseBools(*releqList)
	relPcts := parseFloats(*relTolPct)
	relPips := parseFloats(*relTolPips)

	var rows []row
	for _, gap := range gaps {
		for _, conf := range confs {
			for _, m5 := range m5s {
				for _, mo := range mos {
					for _, rq := range releqs {
						for _, rp := range relPcts {
							for _, rpip := range relPips {
								cfg := engine.DefaultConfig()
								cfg.StartBalance = *balance
								cfg.MinGapPips = gap
								cfg.ConfluenceMin = conf
								cfg.MinSwingPipsM5 = m5
								cfg.MOGate = mo
								cfg.RelEqSweepGate = rq
								cfg.RelEqTolerancePct = rp
								cfg.RelEqTolerancePips = rpip
								res, err := engine.Run(tf, cfg)
								if err != nil {
									log.Fatalf("run: %v", err)
								}
								label := fmt.Sprintf("gap=%g conf=%d m5=%g", gap, conf, m5)
								if len(mos) > 1 {
									label += fmt.Sprintf(" mo=%v", mo)
								}
								if len(releqs) > 1 {
									label += fmt.Sprintf(" rqsweep=%v", rq)
								}
								if len(relPcts) > 1 {
									label += fmt.Sprintf(" rqpct=%g", rp)
								}
								if len(relPips) > 1 {
									label += fmt.Sprintf(" rqpip=%g", rpip)
								}
								rows = append(rows, row{
									label: label,
									cfg:   cfg,
									m:     report.Compute(res.Trades, *balance),
								})
							}
						}
					}
				}
			}
		}
	}

	sortRows(rows, *sortBy)

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "RANK\tPARAMS\tTRADES\tWIN%\tTOTAL_R\tAVG_R\tPF\tMAXDD%\tCONSEC_L")
	for i, r := range rows {
		fmt.Fprintf(w, "%d\t%s\t%d\t%.1f\t%+.2f\t%+.3f\t%s\t%.1f\t%d\n",
			i+1, r.label, r.m.Trades, r.m.WinRate, r.m.TotalR, r.m.AvgR,
			pf(r.m.ProfitFactor), r.m.MaxDrawdownPc, r.m.MaxConsecLoss)
	}
	w.Flush()
	fmt.Printf("\n%d kombinasi dijalankan. Urut: %s (terbaik di atas).\n", len(rows), *sortBy)
	fmt.Println("Catatan: angka mentah belum dikalibrasi penuh — pakai untuk membandingkan ARAH efek tiap param, bukan klaim profit absolut.")
}

func sortRows(rows []row, by string) {
	less := func(a, b row) bool { return a.m.TotalR > b.m.TotalR }
	switch by {
	case "pf":
		less = func(a, b row) bool { return a.m.ProfitFactor > b.m.ProfitFactor }
	case "winrate":
		less = func(a, b row) bool { return a.m.WinRate > b.m.WinRate }
	case "avgr":
		less = func(a, b row) bool { return a.m.AvgR > b.m.AvgR }
	}
	sort.SliceStable(rows, func(i, j int) bool { return less(rows[i], rows[j]) })
}

func pf(x float64) string {
	if x > 1e9 || x != x {
		return "inf"
	}
	return fmt.Sprintf("%.2f", x)
}

func parseFloats(s string) []float64 {
	var out []float64
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			log.Fatalf("nilai float tidak valid %q: %v", p, err)
		}
		out = append(out, v)
	}
	return out
}

func parseInts(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			log.Fatalf("nilai int tidak valid %q: %v", p, err)
		}
		out = append(out, v)
	}
	return out
}

func parseBools(s string) []bool {
	var out []bool
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseBool(p)
		if err != nil {
			log.Fatalf("nilai bool tidak valid %q: %v", p, err)
		}
		out = append(out, v)
	}
	return out
}
