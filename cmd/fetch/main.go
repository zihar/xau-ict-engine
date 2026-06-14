// Command fetch: verifikasi token OANDA lalu download & cache candle historis
// XAU_USD multi-timeframe ke disk. Read-only — tidak ada eksekusi order.
//
// Env wajib:
//
//	OANDA_TOKEN       personal access token v20
//	OANDA_ACCOUNT_ID  (opsional) account id untuk dicocokkan saat smoke-test
//	OANDA_ENV         "practice" (default) atau "live"
//
// Contoh:
//
//	go run ./cmd/fetch -from 2022-01-01   // full re-download (overwrite)
//	go run ./cmd/fetch -append            // incremental: hanya candle baru sejak cache terakhir
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/oanda"
)

// dailyAnchorHourNY = jam anchor candle harian (18:00 NY), sesuai
// daily_candle_anchor_hour_ny di config rules.
const dailyAnchorHourNY = 18

// granularities yang di-download untuk Phase 1.
var granularities = []string{"W", "D", "H4", "H1", "M15", "M5"}

func main() {
	var (
		fromStr    = flag.String("from", "2022-01-01", "tanggal mulai (YYYY-MM-DD)")
		toStr      = flag.String("to", "", "tanggal akhir (YYYY-MM-DD); kosong = sekarang")
		instrument = flag.String("instrument", "XAU_USD", "instrumen OANDA")
		outDir     = flag.String("out", "data", "direktori cache output")
		appendMode = flag.Bool("append", false, "incremental: tarik HANYA candle baru sejak candle terakhir di cache (AppendNew, anti-dup) — cepat & history aman. Default false = full re-download dari -from (overwrite). Cache kosong → bootstrap dari -from.")
	)
	flag.Parse()

	token := os.Getenv("OANDA_TOKEN")
	if token == "" {
		log.Fatal("OANDA_TOKEN belum di-set. export OANDA_TOKEN=... dulu.")
	}
	env := os.Getenv("OANDA_ENV")
	if env == "" {
		env = "practice"
	}

	from, err := time.Parse("2006-01-02", *fromStr)
	if err != nil {
		log.Fatalf("flag -from tidak valid: %v", err)
	}
	to := time.Now().UTC()
	if *toStr != "" {
		to, err = time.Parse("2006-01-02", *toStr)
		if err != nil {
			log.Fatalf("flag -to tidak valid: %v", err)
		}
	}

	client := oanda.New(token, env)

	// 1. Smoke-test token.
	accounts, err := client.GetAccounts()
	if err != nil {
		log.Fatalf("smoke-test gagal (token salah / VPN / env?): %v", err)
	}
	fmt.Printf("✓ Token valid (env=%s). Akun yang terlihat:\n", env)
	for _, a := range accounts {
		fmt.Printf("   - %s\n", a.ID)
	}
	if want := os.Getenv("OANDA_ACCOUNT_ID"); want != "" {
		found := false
		for _, a := range accounts {
			if a.ID == want {
				found = true
			}
		}
		if !found {
			log.Printf("⚠ OANDA_ACCOUNT_ID=%s tidak ada di daftar akun token ini.", want)
		}
	}

	// 2. Download tiap granularity. Dua mode:
	//   - default (overwrite): tarik full -from→to lalu WriteCSV (tulis ulang file).
	//   - -append (incremental): tarik HANYA dari candle terakhir di cache → AppendNew
	//     (anti-dup, history aman) — pola sama dgn alertd.refreshCache.
	if *appendMode {
		fmt.Printf("\nAppend incremental %s s/d %s — tarik hanya candle baru:\n", *instrument, to.Format("2006-01-02"))
	} else {
		fmt.Printf("\nDownload %s, %s → %s (full overwrite)\n", *instrument, from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
	for _, g := range granularities {
		t0 := time.Now()

		// Mode append: mulai dari candle terakhir + 1 detik (atau -from kalau cache kosong).
		gFrom := from
		if *appendMode {
			last, ok, err := data.LastCandleTime(*outDir, *instrument, g)
			if err != nil {
				log.Fatalf("baca last-candle %s: %v", g, err)
			}
			if ok {
				gFrom = last.Add(time.Second)
				if !gFrom.Before(to) {
					fmt.Printf("   %-3s sudah mutakhir (last %s)\n", g, last.UTC().Format("2006-01-02 15:04"))
					continue
				}
			}
		}

		candles, err := client.FetchCandles(*instrument, g, gFrom, to, dailyAnchorHourNY)
		if err != nil {
			log.Fatalf("fetch %s: %v", g, err)
		}
		path := data.CSVPath(*outDir, *instrument, g)

		if *appendMode {
			if err := data.AppendNew(*outDir, *instrument, g, candles); err != nil {
				log.Fatalf("append %s: %v", g, err)
			}
			fmt.Printf("   %-3s +%d candle baru → %s  (%s)\n", g, len(candles), path, time.Since(t0).Round(time.Millisecond))
			continue
		}

		if err := data.WriteCSV(path, candles); err != nil {
			log.Fatalf("simpan %s: %v", g, err)
		}
		rng := ""
		if n := len(candles); n > 0 {
			rng = fmt.Sprintf("  [%s .. %s]",
				candles[0].Time.Format("2006-01-02"),
				candles[n-1].Time.Format("2006-01-02"))
		}
		fmt.Printf("   %-3s %6d candle → %s%s  (%s)\n", g, len(candles), path, rng, time.Since(t0).Round(time.Millisecond))
	}
	fmt.Println("\nSelesai.")
}
