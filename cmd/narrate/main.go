// Command narrate memindai piramida (TDA→DB→AMS→QT→PD Array→Entry) pada satu
// titik waktu lalu mencetak NARASI langkah-demi-langkah + menulis chart SVG
// ber-anotasi (POI, entry/SL/TP, equilibrium). Mengganti "screenshot chart"
// dalam workflow trader. Murni offline.
//
// Contoh:
//
//	go run ./cmd/narrate -at 2024-03-15T10:00:00Z
//	go run ./cmd/narrate -at "2024-03-15 10:00" -svg /tmp/scan.svg -gap 10 -m5 20
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"xau-ict-engine/internal/chartann"
	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/engine"
	"xau-ict-engine/internal/news"
	"xau-ict-engine/internal/viz"
)

// nyLoc = America/New_York; semua waktu ditampilkan dalam waktu NY (strategi
// ber-anchor ke jam NY). nyf = format ringkas, nyz = dengan zona (EST/EDT).
var nyLoc = mustNYLoc()

func mustNYLoc() *time.Location {
	l, err := detectors.NYLocation()
	if err != nil {
		return time.UTC
	}
	return l
}

func nyf(t time.Time) string { return t.In(nyLoc).Format("2006-01-02 15:04") }
func nyz(t time.Time) string { return t.In(nyLoc).Format("2006-01-02 15:04 MST") }

func main() {
	var (
		dir        = flag.String("data", "data", "direktori cache CSV")
		instrument = flag.String("instrument", "XAU_USD", "instrumen")
		atStr      = flag.String("at", "", "titik waktu scan (RFC3339, atau 'YYYY-MM-DD HH:MM' waktu NY); kosong = candle H1 terakhir")
		svgOut     = flag.String("svg", "narrative.svg", "path output chart SVG (kosong = tidak tulis)")
		balance    = flag.Float64("balance", 25000, "saldo (sizing)")
		gap        = flag.Float64("gap", -1, "override MinGapPips (default config kalau <0)")
		conf       = flag.Int("conf", -1, "override ConfluenceMin (<0 = default)")
		m5         = flag.Float64("m5", -1, "override MinSwingPipsM5 (<0 = default)")
		find       = flag.Int("find", 0, "scan dari AWAL data: laporkan N momen ber-SETUP VALID lalu narasikan yang pertama (0 = off)")
		recent     = flag.Int("recent", 0, "scan dari AKHIR data: laporkan N momen SETUP VALID TERBARU lalu narasikan yang paling baru (0 = off)")
		fibLevels  = flag.Bool("fiblevels", true, "gambar level Fib 0/1 + leg makro di chart (matikan kalau Fib makro jauh & bikin skala melebar)")
		bars       = flag.Int("bars", 0, "tampilkan N candle H1 TERAKHIR saja (price range auto-fit ke bar itu, anti-gepeng; 0 = semua ~120)")
		showPOI    = flag.Bool("poi", false, "cetak komponen PD Array penyusun POI terpilih (kind + waktu pembentukan NY)")
		newsSkip   = flag.Bool("news-skip", false, "skip-hour (08:00 NY) jadi KONDISIONAL: hanya skip bila ada rilis USD high-impact (kalender minggu BERJALAN; default OFF = blanket). Hanya bermakna utk scan minggu ini.")
	)
	flag.Parse()

	load := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(*dir, *instrument, g))
		if err != nil {
			log.Fatalf("baca %s: %v (jalankan `go run ./cmd/fetch` dulu)", g, err)
		}
		return c
	}
	tf := engine.TFData{Weekly: load("W"), Daily: load("D"), H4: load("H4"), H1: load("H1"), M5: load("M5"), M15: load("M15")}

	// Info kesegaran data — penting untuk "scan realtime": cache statis, candle
	// terbaru = batas data. Untuk scan "sekarang" beneran, refresh dulu via
	// `go run ./cmd/fetch` (butuh OANDA token + VPN SG), baru jalankan ini lagi.
	if len(tf.H1) > 0 {
		fmt.Printf("Data %s s/d candle H1 terakhir: %s NY. (refresh: `go run ./cmd/fetch`)\n\n",
			*instrument, nyz(tf.H1[len(tf.H1)-1].Time))
	}

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
	if *newsSkip {
		// Live-only: skip-hour kondisional vs kalender minggu berjalan. Feed FF cuma
		// minggu ini → hanya valid utk scan "sekarang"/minggu ini; scan historis akan
		// punya set kosong (→ jam-8 dibuka). Feed gagal → tetap blanket (protektif).
		events, fromCache, err := news.FetchCalendarCached(nil, filepath.Join(*dir, "news_feed_cache.json"))
		if err != nil {
			fmt.Printf("⚠️ news-skip: fetch kalender gagal (fallback blanket skip): %v\n\n", err)
		} else {
			if fromCache {
				fmt.Printf("⚠️ news-skip: feed live gagal — pakai cache (fallback blanket skip)\n\n")
			} else {
				cfg.NewsSkipHourStarts = engine.BuildNewsSkipSet(events, cfg.SkipEntryHoursNY, nyLoc)
				cfg.SkipEntryNewsOnly = true
				fmt.Printf("news-skip aktif — %d jam-news high-impact di set (skip-hour kondisional)\n\n", len(cfg.NewsSkipHourStarts))
			}
		}
	}

	at := lastH1Time(tf.H1)
	if *atStr != "" {
		at = parseTime(*atStr)
	}

	// Mode -find (dari awal) / -recent (dari akhir): pindai momen ber-SETUP VALID.
	switch {
	case *recent > 0:
		hits := findSetupsRecent(tf, cfg, *recent)
		if len(hits) == 0 {
			fmt.Println("Tidak ada SETUP VALID ditemukan dengan parameter ini.")
			return
		}
		fmt.Printf("%d momen SETUP VALID TERBARU:\n", len(hits))
		for _, h := range hits {
			fmt.Printf("  %s — %s\n", nyf(h.At), h.Decision)
		}
		at = hits[len(hits)-1].At // paling baru
		fmt.Printf("\nNarasi momen terbaru (%s NY):\n\n", nyf(at))
	case *find > 0:
		hits := findSetups(tf, cfg, *find)
		if len(hits) == 0 {
			fmt.Println("Tidak ada SETUP VALID ditemukan dengan parameter ini.")
			return
		}
		fmt.Printf("%d momen SETUP VALID pertama:\n", len(hits))
		for _, h := range hits {
			fmt.Printf("  %s — %s\n", nyf(h.At), h.Decision)
		}
		at = hits[0].At
		fmt.Printf("\nNarasi momen pertama (%s NY):\n\n", nyf(at))
	}

	n := engine.Narrate(tf, cfg, at)
	printNarrative(n, *instrument)
	printWatchlist(n, *instrument)

	if *showPOI {
		printPOIComponents(n)
	}

	if *svgOut != "" {
		if err := writeSVG(*svgOut, n, *instrument, *fibLevels, *bars); err != nil {
			log.Fatalf("tulis SVG: %v", err)
		}
		abs, _ := filepath.Abs(*svgOut)
		fmt.Printf("\nChart SVG → %s  (buka di browser)\n", abs)
	}
}

// printNarrative mencetak narasi piramida via engine.FormatNarrative —
// formatter bersama dengan alert Telegram alertd (tampilan identik).
func printNarrative(n engine.ScanNarrative, instrument string) {
	for _, line := range engine.FormatNarrative(n, instrument, 88) {
		fmt.Println(line)
	}
}

// printWatchlist mencetak pesan PANTAUAN ringkas via engine.FormatWatchlist —
// formatter bersama dengan alert Telegram alertd (tampilan identik, width 88 untuk
// terminal). Layout flat (revisi mobile 2026-06-08): TDA · Bias · AMS (ITL/ITH 1H) ·
// QT (Bulanan/Mingguan/Daily/Session) · Komponen Array per-TF · MO.
func printWatchlist(n engine.ScanNarrative, instrument string) {
	fmt.Println()
	for _, l := range engine.FormatWatchlist(n, instrument, 88) {
		fmt.Println(l)
	}
	fmt.Println("\n  Label: BISI=FVG bullish (buy-side) · SIBI=FVG bearish (sell-side) · VI=Volume")
	fmt.Println("  Imbalance · BB=Breaker · BPR=Balanced Price Range · FVGBreak=FVG@swing-break ·")
	fmt.Println("  IFVG=Inversion FVG · OB=Order Block · REH/REL=relative equal high/low (likuiditas).")
}

// printPOIComponents mencetak komponen PD Array penyusun POI terpilih
// (InPOI=true) beserta jenis & waktu candle pembentuk (NY).
func printPOIComponents(n engine.ScanNarrative) {
	if !n.HasPOI {
		fmt.Println("\n(Tidak ada POI terpilih pada scan ini.)")
		return
	}
	fmt.Printf("\nPOI terpilih [TF %s]: %.2f–%.2f — komponen penyusun (confluence):\n", n.POITF.String(), n.POIBottom, n.POITop)
	fmt.Printf("  %-5s %-8s %-10s %-10s %s\n", "KIND", "DIR", "BOTTOM", "TOP", "TERBENTUK (NY)")
	fmt.Println("  ────────────────────────────────────────────────────────────")
	for _, p := range n.POIComponents {
		fmt.Printf("  %-5s %-8s %-10.2f %-10.2f %s\n", p.Kind, p.Dir, p.Bottom, p.Top, nyz(p.Time))
	}
	fmt.Println("\n  Keterangan kind: VI=Volume Imbalance(T1) · FVGBreak=FVG@swing-break(T2) ·")
	fmt.Println("  BB=Breaker/BPR(T3) · FVG/IFVG(T4) · OB=Order Block(T5). Tier POI = komponen terbaik.")
}

func writeSVG(path string, n engine.ScanNarrative, instrument string, fibLevels bool, bars int) error {
	ann := chartann.Build(n, fibLevels)
	ann.Title = fmt.Sprintf("%s — %s", instrument, decisionShort(n))
	ann.Subtitle = fmt.Sprintf("scan %s NY · bias %s · harga %.2f", nyf(n.At), n.Bias, n.Price)

	plot := n.Plot
	// Crop bar: tampilkan N candle terakhir + fit sumbu harga ke low/high bar itu
	// saja (+pad 4%). Level/marker jauh (mis. ITH/REH di luar window) di-clamp ke
	// tepi oleh viz, jadi aksi harga terbaru tak digepengkan swing lama.
	if bars > 0 && bars < len(plot) {
		plot = plot[len(plot)-bars:]
		lo, hi := plot[0].Low, plot[0].High
		for _, c := range plot {
			if c.Low < lo {
				lo = c.Low
			}
			if c.High > hi {
				hi = c.High
			}
		}
		pad := (hi - lo) * 0.04
		ann.PriceLo, ann.PriceHi = lo-pad, hi+pad
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return viz.RenderSVG(f, plot, ann)
}

func decisionShort(n engine.ScanNarrative) string {
	if n.HasSetup {
		return n.Decision
	}
	return "NO SETUP"
}

// findSetups memindai tiap candle H1 dari AWAL dan mengembalikan maksimal n
// momen ber-SETUP VALID (semua gate lolos + entry). Untuk menemukan titik entry
// yang bisa dinarasikan/divisualisasikan.
func findSetups(tf engine.TFData, cfg engine.Config, n int) []engine.ScanNarrative {
	var out []engine.ScanNarrative
	for _, c := range tf.H1 {
		sc := engine.Narrate(tf, cfg, c.Time.Add(time.Hour))
		if sc.HasSetup {
			out = append(out, sc)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}

// findSetupsRecent memindai dari AKHIR data mundur, mengumpulkan n momen
// ber-SETUP VALID terbaru, lalu dikembalikan urut kronologis (lama→baru).
func findSetupsRecent(tf engine.TFData, cfg engine.Config, n int) []engine.ScanNarrative {
	var out []engine.ScanNarrative
	for i := len(tf.H1) - 1; i >= 0; i-- {
		sc := engine.Narrate(tf, cfg, tf.H1[i].Time.Add(time.Hour))
		if sc.HasSetup {
			out = append(out, sc)
			if len(out) >= n {
				break
			}
		}
	}
	// balik jadi kronologis
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

// lastH1Time = waktu candle H1 terakhir (default scan kalau -at kosong).
func lastH1Time(h1 []data.Candle) time.Time {
	if len(h1) == 0 {
		return time.Time{}
	}
	return h1[len(h1)-1].Time
}

func parseTime(s string) time.Time {
	// RFC3339 punya zona eksplisit → hormati apa adanya.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	// Format tanpa zona → diartikan waktu NY (strategi ber-anchor NY).
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, nyLoc); err == nil {
			return t.UTC()
		}
	}
	log.Fatalf("format -at tidak dikenali: %q (pakai RFC3339, atau 'YYYY-MM-DD HH:MM' waktu NY)", s)
	return time.Time{}
}
