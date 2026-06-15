// cmd/nfpqt: analisa QT BULANAN di sekitar NFP — untuk tiap NFP (Jumat pertama
// tiap bulan) tarik fase bulanan (skenario AMDX/XAMD + fase minggu) di minggu-NFP
// dan minggu-sesudah, hubungkan dengan return minggu-sesudah. Throwaway diagnosa.
package main

import (
	"fmt"
	"log"
	"sort"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/engine"
)

func main() {
	ny, _ := time.LoadLocation("America/New_York")
	load := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath("data", "XAU_USD", g))
		if err != nil {
			log.Fatalf("baca %s: %v", g, err)
		}
		return c
	}
	tf := engine.TFData{Weekly: load("W"), Daily: load("D"), H4: load("H4"), H1: load("H1"), M5: load("M5"), M15: load("M15")}
	cfg := engine.DefaultConfig()
	d := tf.Daily

	// candle harian yang MEMUAT timestamp ts (anchor 18:00 NY → window 24h).
	findD := func(ts time.Time) int {
		for i, c := range d {
			if !ts.Before(c.Time) && ts.Before(c.Time.Add(24*time.Hour)) {
				return i
			}
		}
		return -1
	}
	// fase bulanan (skenario + fase-minggu) di scan Rabu 12:00 NY minggu yg memuat anchor.
	monPhase := func(at time.Time) (string, string) {
		n := engine.Narrate(tf, cfg, at)
		return up(n.MonthlyScenario), n.MonthlyPhase
	}

	type row struct {
		date            string
		nfpRet, wkRet   float64
		mNfpSc, mNfpPh  string
		mAftSc, mAftPh  string
	}
	var rows []row

	for y := 2022; y <= 2026; y++ {
		for m := 1; m <= 12; m++ {
			if y == 2026 && m > 6 {
				break
			}
			fri := time.Date(y, time.Month(m), 1, 13, 30, 0, 0, time.UTC)
			for fri.Weekday() != time.Friday {
				fri = fri.AddDate(0, 0, 1)
			}
			i := findD(fri)
			if i < 0 || i+5 >= len(d) {
				continue
			}
			nfpRet := (d[i].Close - d[i].Open) / d[i].Open * 100
			wkRet := (d[i+5].Close - d[i].Close) / d[i].Close * 100

			fl, _ := time.ParseInLocation("2006-01-02", fri.Format("2006-01-02"), ny)
			// Scan DI HARI NFP (Jumat pertama, selalu dalam bulan yg benar) — bukan
			// Rabu-sebelumnya yg utk NFP tgl 1-2 nyebrang ke bulan lalu (artefak "D").
			nfpScan := time.Date(fl.Year(), fl.Month(), fl.Day(), 11, 0, 0, 0, ny)
			aftScan := nfpScan.AddDate(0, 0, 7) // Jumat minggu berikutnya (tetap se-bulan, Q2)
			sc1, ph1 := monPhase(nfpScan)
			sc2, ph2 := monPhase(aftScan)
			rows = append(rows, row{fri.Format("2006-01-02"), nfpRet, wkRet, sc1, ph1, sc2, ph2})
		}
	}

	fmt.Printf("%-12s %8s %9s | %-12s | %-12s\n", "NFP", "NFPday%", "wkAfter%", "BULANAN-NFP", "BULANAN-sesdh")
	fmt.Println("---------------------------------------------------------------------------")
	for _, r := range rows {
		fmt.Printf("%-12s %+7.2f%% %+8.2f%% | %-5s ph=%-3s | %-5s ph=%-3s\n",
			r.date, r.nfpRet, r.wkRet, r.mNfpSc, r.mNfpPh, r.mAftSc, r.mAftPh)
	}

	// Agregat: return minggu-sesudah dikelompokkan per FASE BULANAN minggu-NFP & minggu-sesudah.
	agg := func(title string, key func(row) string) {
		type st struct {
			sum  float64
			n, g int
		}
		m := map[string]*st{}
		var order []string
		for _, r := range rows {
			k := key(r)
			if k == "" {
				k = "(kosong)"
			}
			if m[k] == nil {
				m[k] = &st{}
				order = append(order, k)
			}
			m[k].sum += r.wkRet
			m[k].n++
			if r.wkRet > 0 {
				m[k].g++
			}
		}
		sort.Strings(order)
		fmt.Printf("\n== %s ==\n", title)
		fmt.Printf("%-10s %4s %10s %8s\n", "fase", "N", "mean-wk%", "%hijau")
		for _, k := range order {
			s := m[k]
			fmt.Printf("%-10s %4d %+9.2f%% %6.0f%%\n", k, s.n, s.sum/float64(s.n), float64(s.g)/float64(s.n)*100)
		}
	}
	agg("Return minggu-sesudah per FASE BULANAN di minggu-NFP", func(r row) string { return r.mNfpPh })
	agg("Return minggu-sesudah per FASE BULANAN di minggu-SESUDAH", func(r row) string { return r.mAftPh })
	agg("...subset hanya NFP-day JEBLOK (<-0.5%) per fase bulanan minggu-NFP", func(r row) string {
		if r.nfpRet < -0.5 {
			return r.mNfpPh
		}
		return ""
	})
}

func up(s string) string {
	if s == "" {
		return "-"
	}
	r := []byte(s)
	for i := range r {
		if r[i] >= 'a' && r[i] <= 'z' {
			r[i] -= 32
		}
	}
	return string(r)
}
