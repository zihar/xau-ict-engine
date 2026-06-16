// Command walkforward menjalankan validasi anchored walk-forward atas grid
// parameter sweep. Tujuannya menilai apakah kombinasi parameter "terbaik" pada
// in-sample (IS) masih bertahan pada out-of-sample (OOS) — indikasi overfit bila
// agregat OOS jauh lebih buruk dari metrik IS.
//
// Data di-load sekali; tiap kombinasi dijalankan sekali atas seluruh sejarah
// (warmup utuh), lalu trade difilter per window. Murni offline (tidak menyentuh
// OANDA). ~1 menit untuk 12 kombinasi.
//
//	go run ./cmd/walkforward
//	go run ./cmd/walkforward -folds 5 -gap 5,10 -conf 2,3 -m5 0,20,40 -mintrades 5
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"xau-ict-engine/internal/config"
	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/engine"
	"xau-ict-engine/internal/report"
	"xau-ict-engine/internal/walkforward"
)

func main() {
	var (
		dir        = flag.String("data", "data", "direktori cache CSV")
		instrument = flag.String("instrument", "XAU_USD", "instrumen")
		balance    = flag.Float64("balance", 25000, "saldo awal")
		folds      = flag.Int("folds", 4, "jumlah fold (blok kontigu = folds+1)")
		gapList    = flag.String("gap", "", "grid MinGapPips FVG (CSV pip), kosong=default 5,10")
		confList   = flag.String("conf", "", "grid ConfluenceMin POI (CSV), kosong=default 2,3")
		m5List     = flag.String("m5", "", "grid MinSwingPipsM5 (CSV pip), kosong=default 0,20,40")
		minTrades  = flag.Int("mintrades", 5, "minimum trade IS agar kombinasi boleh dipilih")
		amsGate    = flag.Bool("ams", true, "gate AMS struktur intermediate 1H (false = OFF, untuk A/B OOS)")
		poiWick    = flag.Bool("poiwick", true, "invalidasi PDR pakai wick (false = body close)")
		fractal    = flag.Bool("fractal", true, "POI fractal multi-TF (false = H1 saja)")
		agenda     = flag.Bool("agenda", true, "agenda gate (false = OFF)")
		agendaNear = flag.Bool("agendanearest", false, "agenda nearest (true) vs farthest")
		pmGate     = flag.Bool("pmgate", true, "gate sesi PM Q4 (false = OFF, izinkan entry PM)")
		moGate     = flag.Bool("mogate", false, "gate Midnight Open (pertemuan 8; untuk A/B OOS)")
		releqSweep = flag.Bool("releqsweep", false, "gate wait-for-sweep REH/REL (untuk A/B OOS)")
		cfgPath    = flag.String("config", "", "path config.yaml (kosong = engine.DefaultConfig); flag lain tetap override di atasnya")
	)
	flag.Parse()
	flagSet := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { flagSet[f.Name] = true })

	load := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(*dir, *instrument, g))
		if err != nil {
			log.Fatalf("baca %s: %v (jalankan `go run ./cmd/fetch` dulu)", g, err)
		}
		return c
	}
	tf := engine.TFData{Weekly: load("W"), Daily: load("D"), H4: load("H4"), H1: load("H1"), M5: load("M5")}

	// Grid: pakai default kecuali ada override CSV. Override parsial diperbolehkan.
	gaps := []float64{5, 10}
	confs := []int{2, 3}
	m5s := []float64{0, 20, 40}
	if *gapList != "" {
		gaps = parseFloats(*gapList)
	}
	if *confList != "" {
		confs = parseInts(*confList)
	}
	if *m5List != "" {
		m5s = parseFloats(*m5List)
	}
	combos := walkforward.Grid(gaps, confs, m5s)

	base := engine.DefaultConfig()
	if *cfgPath != "" {
		loaded, err := config.Load(*cfgPath)
		if err != nil {
			log.Fatalf("muat config: %v", err)
		}
		base = loaded
	}
	base.StartBalance = *balance
	if flagSet["ams"] {
		base.AMSGate = *amsGate
	}
	if flagSet["poiwick"] {
		base.POIBreakWick = *poiWick
	}
	if flagSet["fractal"] {
		base.FractalPOI = *fractal
	}
	if flagSet["agenda"] {
		base.AgendaGate = *agenda
	}
	if flagSet["agendanearest"] {
		base.AgendaNearest = *agendaNear
	}
	if flagSet["pmgate"] {
		base.SessionPMGate = *pmGate
	}
	if flagSet["mogate"] {
		base.MOGate = *moGate
	}
	if flagSet["releqsweep"] {
		base.RelEqSweepGate = *releqSweep
	}

	fmt.Printf("Loaded %s: H1=%d M5=%d. Menjalankan %d kombinasi atas seluruh data...\n",
		*instrument, len(tf.H1), len(tf.M5), len(combos))

	out, err := walkforward.Run(tf, base, walkforward.Params{
		Combos:       combos,
		Folds:        *folds,
		StartBalance: *balance,
		MinTradesIS:  *minTrades,
	})
	if err != nil {
		log.Fatalf("walk-forward: %v", err)
	}

	const dateFmt = "2006-01-02"
	fmt.Printf("\nRentang data : %s → %s\n", out.DataStart.Format(dateFmt), out.DataEnd.Format(dateFmt))
	fmt.Printf("Kombinasi    : %d   Fold: %d (blok kontigu: %d)\n\n", len(out.Combos), len(out.Folds), len(out.Folds)+1)

	// Tabel per-fold.
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "FOLD\tIS_WINDOW\tCHOSEN_PARAMS\tIS_TRADES\tIS_TOTR\tOOS_WINDOW\tOOS_TRADES\tOOS_TOTR\tOOS_WIN%\tOOS_PF")
	for _, f := range out.Folds {
		chosen := f.Chosen.Label()
		if f.Fallback {
			chosen += " (fallback)"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%+.2f\t%s\t%d\t%+.2f\t%.1f\t%s\n",
			f.Index+1,
			win(f.IS, dateFmt),
			chosen,
			f.ISMetrics.Trades, f.ISMetrics.TotalR,
			win(f.OOS, dateFmt),
			f.OOSMetrics.Trades, f.OOSMetrics.TotalR,
			f.OOSMetrics.WinRate, pf(f.OOSMetrics.ProfitFactor),
		)
	}
	w.Flush()

	// Ringkasan agregat vs baseline.
	fmt.Println()
	sw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(sw, "SUMMARY\tTRADES\tWIN%\tTOTAL_R\tAVG_R\tPF\tMAXDD%")
	printSummary(sw, "AGGREGATE OOS", out.AggregateOOS)
	printSummary(sw, "BASELINE OOS", out.BaselineOOS)
	sw.Flush()

	// Catatan jujur.
	fmt.Println()
	fmt.Println("Catatan: sampel kecil — ini INDIKASI robustness, bukan jaminan profit.")
	fmt.Println("AGGREGATE OOS = gabungan trade OOS dari param terpilih tiap fold (estimasi jujur).")
	fmt.Println("BASELINE OOS  = DefaultConfig (gap=5 conf=2 m5=0) atas union window OOS yang sama.")
	verdict(out.AggregateOOS, out.BaselineOOS, out.Folds)
}

// win memformat sebuah window jadi "start..end".
func win(w walkforward.Window, layout string) string {
	return w.Start.Format(layout) + ".." + w.End.Format(layout)
}

func printSummary(w *tabwriter.Writer, label string, m report.Metrics) {
	fmt.Fprintf(w, "%s\t%d\t%.1f\t%+.2f\t%+.3f\t%s\t%.1f\n",
		label, m.Trades, m.WinRate, m.TotalR, m.AvgR, pf(m.ProfitFactor), m.MaxDrawdownPc)
}

// verdict mencetak interpretasi ringkas agregat OOS vs baseline.
func verdict(agg, base report.Metrics, folds []walkforward.FoldResult) {
	fallbacks := 0
	for _, f := range folds {
		if f.Fallback {
			fallbacks++
		}
	}
	if fallbacks > 0 {
		fmt.Printf("Peringatan: %d/%d fold pakai fallback baseline (tak ada kombinasi lolos guard minTrades di IS).\n", fallbacks, len(folds))
	}
	switch {
	case agg.Trades == 0:
		fmt.Println("Verdict: tidak ada trade OOS — sampel/guard terlalu ketat untuk menyimpulkan apa pun.")
	case agg.TotalR > 0 && agg.TotalR >= base.TotalR:
		fmt.Println("Verdict: agregat OOS positif dan >= baseline → param terpilih tampak bertahan keluar-sample (robustness OK untuk sampel ini).")
	case agg.TotalR <= 0 && base.TotalR > agg.TotalR:
		fmt.Println("Verdict: agregat OOS negatif dan lebih buruk dari baseline → INDIKASI OVERFIT pada seleksi IS.")
	default:
		fmt.Println("Verdict: hasil OOS campuran — belum ada bukti kuat overfit maupun robustness. Perbesar sampel.")
	}
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
