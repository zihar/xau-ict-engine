// Command gatestats menghitung, untuk SETIAP candle H1 selama rentang data,
// gate PERTAMA yang menggagalkan scan (urut pyramid TDA→DB→QT→POI→Entry→SL)
// atau PASS = entry. Menjawab "gate mana yang paling banyak makan korban".
// Murni offline — memanggil engine.GateStats yang reuse evaluate() yang sama
// dengan Run (fidelitas penuh, bukan estimasi).
//
// Contoh:
//
//	go run ./cmd/gatestats
//	go run ./cmd/gatestats -config config.yaml -maxtier 0
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"xau-ict-engine/internal/config"
	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/engine"
)

func main() {
	var (
		dir         = flag.String("data", "data", "direktori cache CSV")
		instrument  = flag.String("instrument", "XAU_USD", "instrumen")
		cfgPath     = flag.String("config", "", "path config.yaml (kosong = engine.DefaultConfig)")
		maxTier     = flag.Int("maxtier", -1, "override MaxPOITier (0=tanpa batas; <0=default)")
		kunci3      = flag.Bool("kunci3", false, "aktifkan Kunci#3 fallback opposing-liquidity")
		asiaGate    = flag.Bool("asiagate", true, "gate Asia-close (false = OFF)")
		qtPhase     = flag.Bool("qtphase", true, "gate QT phase tradeable daily (false = OFF)")
		weeklyPhase = flag.Bool("weeklyphase", false, "gate weekly phase AND di atas daily (default OFF = weekly dimatikan)")
		dayType     = flag.Bool("daytype", true, "gate day-type heavy_accum (false = OFF)")
		londonQT    = flag.Bool("londonqt", true, "Phase2: London hanya Q3/Q4 (false = OFF)")
		amsGate     = flag.Bool("ams", true, "gate AMS struktur intermediate 1H (false = OFF)")
		fibGate     = flag.Bool("fibgate", true, "gate Fib zone (false = OFF, Fib jadi non-gating)")
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

	cfg := engine.DefaultConfig()
	if *cfgPath != "" {
		loaded, err := config.Load(*cfgPath)
		if err != nil {
			log.Fatalf("muat config: %v", err)
		}
		cfg = loaded
	}
	if *maxTier >= 0 {
		cfg.MaxPOITier = *maxTier
	}
	if flagSet["kunci3"] {
		cfg.Kunci3Fallback = *kunci3
	}
	if flagSet["asiagate"] {
		cfg.AsiaCloseGate = *asiaGate
	}
	if flagSet["qtphase"] {
		cfg.QTPhaseGate = *qtPhase
	}
	if flagSet["weeklyphase"] {
		cfg.WeeklyPhaseGate = *weeklyPhase
	}
	if flagSet["daytype"] {
		cfg.DayTypeGate = *dayType
	}
	if flagSet["londonqt"] {
		cfg.LondonQ34Only = *londonQT
	}
	if flagSet["ams"] {
		cfg.AMSGate = *amsGate
	}
	if flagSet["fibgate"] {
		cfg.FibZoneGate = *fibGate
	}

	stats, total, err := engine.GateStats(tf, cfg)
	if err != nil {
		log.Fatalf("gatestats: %v", err)
	}

	fmt.Printf("Gate diagnostik %s — %d candle H1 (%s → %s)\n",
		*instrument, total,
		tf.H1[0].Time.Format("2006-01-02"), tf.H1[len(tf.H1)-1].Time.Format("2006-01-02"))
	fmt.Printf("maxtier=%d kunci3=%v poiwindow=%d\n\n", cfg.MaxPOITier, cfg.Kunci3Fallback, cfg.POITouchWindowBars)

	fmt.Printf("%-40s %9s %8s  %s\n", "GATE (alasan skip pertama)", "candle", "%", "bar")
	fmt.Println(strings.Repeat("─", 78))

	// "Survivor" = candle yang LOLOS sampai gate ini (untuk lihat funnel).
	survivors := total
	for _, st := range stats {
		pct := 100 * float64(st.Count) / float64(total)
		bar := strings.Repeat("█", int(pct/2+0.5))
		fmt.Printf("%-40s %9d %7.1f%%  %s\n", st.Label, st.Count, pct, bar)
		_ = survivors
	}

	// Funnel: berapa candle yang LOLOS tiap tahap (sisa setelah gate ke-bawah).
	fmt.Println("\nFUNNEL (sisa candle yang lolos sampai tahap X):")
	remaining := total
	for _, st := range stats {
		if st.Gate == 0 { // gatePass = entry, bukan gate skip
			fmt.Printf("  → PASS/entry: %d\n", st.Count)
			continue
		}
		remaining -= st.Count
		fmt.Printf("  setelah %-38s sisa %6d (%.1f%%)\n", st.Label, remaining, 100*float64(remaining)/float64(total))
	}
}
