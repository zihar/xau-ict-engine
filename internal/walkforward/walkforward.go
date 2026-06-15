// Package walkforward mengimplementasikan harness anchored walk-forward split
// untuk memvalidasi bahwa parameter terbaik hasil sweep tidak overfit.
//
// Idenya: bagi rentang waktu data menjadi beberapa blok kontigu. Tiap fold
// melatih (in-sample / IS) pada satu blok untuk memilih kombinasi parameter
// terbaik, lalu menguji pilihan itu pada blok berikutnya (out-of-sample / OOS)
// yang BELUM pernah dilihat saat memilih. Estimasi OOS gabungan adalah ukuran
// robustness yang lebih jujur dibanding metrik IS sweep.
//
// Penting: engine butuh SELURUH sejarah untuk warmup (mis. weekly order flow),
// jadi kita TIDAK pernah meng-slice input candle per fold. Sebagai gantinya tiap
// kombinasi dijalankan SEKALI atas seluruh data, hasil trade-nya di-cache, lalu
// trade difilter berdasarkan timestamp ke window IS/OOS yang relevan.
package walkforward

import (
	"fmt"
	"time"

	"forex-backtest/internal/engine"
	"forex-backtest/internal/report"
)

// Combo adalah satu kombinasi parameter pada grid sweep.
type Combo struct {
	Gap  float64 // MinGapPips (FVG)
	Conf int     // ConfluenceMin (POI)
	M5   float64 // MinSwingPipsM5 (filter trigger 5m)
}

// Label mengembalikan representasi ringkas kombinasi untuk dicetak.
func (c Combo) Label() string {
	return fmt.Sprintf("gap=%g conf=%d m5=%g", c.Gap, c.Conf, c.M5)
}

// applyTo mengisi field grid ke salinan config (field lain dibiarkan default).
func (c Combo) applyTo(cfg engine.Config) engine.Config {
	cfg.MinGapPips = c.Gap
	cfg.ConfluenceMin = c.Conf
	cfg.MinSwingPipsM5 = c.M5
	return cfg
}

// Grid menghasilkan produk kartesian dari ketiga sumbu param (urutan: gap, conf, m5).
func Grid(gaps []float64, confs []int, m5s []float64) []Combo {
	var out []Combo
	for _, gap := range gaps {
		for _, conf := range confs {
			for _, m5 := range m5s {
				out = append(out, Combo{Gap: gap, Conf: conf, M5: m5})
			}
		}
	}
	return out
}

// DefaultGrid = grid default sweep: gap∈{5,10} conf∈{2,3} m5∈{0,20,40} → 12 kombinasi.
func DefaultGrid() []Combo {
	return Grid([]float64{5, 10}, []int{2, 3}, []float64{0, 20, 40})
}

// Window adalah rentang waktu setengah-terbuka [Start, End).
type Window struct {
	Start time.Time
	End   time.Time
}

// contains menyatakan apakah t berada di dalam [Start, End).
func (w Window) contains(t time.Time) bool {
	return !t.Before(w.Start) && t.Before(w.End)
}

// FoldResult merangkum satu fold walk-forward.
type FoldResult struct {
	Index    int
	IS       Window
	OOS      Window
	Chosen   Combo
	Fallback bool // true jika tak ada kombinasi lolos guard minTrades → pakai baseline

	ISMetrics  report.Metrics
	OOSMetrics report.Metrics
	OOSTrades  []engine.Trade // trade OOS dari kombinasi terpilih (untuk agregasi)
}

// Outcome adalah keluaran lengkap satu run walk-forward.
type Outcome struct {
	DataStart time.Time
	DataEnd   time.Time
	Combos    []Combo
	Folds     []FoldResult

	// AggregateOOS = report.Compute atas gabungan SEMUA trade OOS dari kombinasi
	// terpilih tiap fold — estimasi walk-forward jujur.
	AggregateOOS report.Metrics
	// BaselineOOS = baseline DefaultConfig, trade difilter ke union semua window OOS.
	BaselineOOS report.Metrics
}

// Params mengatur eksekusi walk-forward.
type Params struct {
	Combos       []Combo
	Folds        int
	StartBalance float64
	MinTradesIS  int // guard: kombinasi wajib punya >= ini trade di IS untuk dipilih
}

// filterTrades mengembalikan subset trade yang fill-time-nya jatuh di window.
func filterTrades(trades []engine.Trade, w Window) []engine.Trade {
	var out []engine.Trade
	for _, tr := range trades {
		if w.contains(tr.Time) {
			out = append(out, tr)
		}
	}
	return out
}

// splitWindows membagi [start, end] menjadi n blok kontigu ~sama lebar.
// Blok terakhir di-extend sedikit (End eksklusif) agar trade tepat di end
// tetap tercakup.
func splitWindows(start, end time.Time, n int) []Window {
	if n < 1 {
		n = 1
	}
	total := end.Sub(start)
	step := total / time.Duration(n)
	out := make([]Window, n)
	for i := 0; i < n; i++ {
		ws := start.Add(step * time.Duration(i))
		we := start.Add(step * time.Duration(i+1))
		if i == n-1 {
			// pastikan inklusif terhadap timestamp end (tambah 1ns).
			we = end.Add(time.Nanosecond)
		}
		out[i] = Window{Start: ws, End: we}
	}
	return out
}

// Run mengeksekusi walk-forward. fullTF adalah seluruh data multi-timeframe;
// engine.Run dipanggil sekali per kombinasi (warmup memakai seluruh sejarah),
// lalu trade difilter per window. Mengembalikan Outcome berisi metrik per-fold
// serta agregat OOS vs baseline.
func Run(fullTF engine.TFData, base engine.Config, p Params) (Outcome, error) {
	if len(p.Combos) == 0 {
		p.Combos = DefaultGrid()
	}
	if p.Folds < 1 {
		p.Folds = 4
	}
	if p.StartBalance <= 0 {
		p.StartBalance = base.StartBalance
	}
	base.StartBalance = p.StartBalance

	// 1. Jalankan tiap kombinasi sekali atas seluruh data, cache trade-nya.
	cache := make(map[Combo][]engine.Trade, len(p.Combos))
	var dataStart, dataEnd time.Time
	for i, c := range p.Combos {
		res, err := engine.Run(fullTF, c.applyTo(base))
		if err != nil {
			return Outcome{}, fmt.Errorf("run kombinasi %s: %w", c.Label(), err)
		}
		cache[c] = res.Trades
		if i == 0 {
			dataStart, dataEnd = res.Start, res.End
		}
	}

	// 2. Bagi rentang waktu jadi folds+1 blok kontigu.
	blocks := splitWindows(dataStart, dataEnd, p.Folds+1)

	out := Outcome{
		DataStart: dataStart,
		DataEnd:   dataEnd,
		Combos:    p.Combos,
	}

	baseCombo := Combo{Gap: base.MinGapPips, Conf: base.ConfluenceMin, M5: base.MinSwingPipsM5}
	// Pastikan baseline ada di cache walau bukan bagian dari grid (untuk fallback &
	// perbandingan baseline OOS).
	if _, ok := cache[baseCombo]; !ok {
		res, err := engine.Run(fullTF, baseCombo.applyTo(base))
		if err != nil {
			return Outcome{}, fmt.Errorf("run baseline %s: %w", baseCombo.Label(), err)
		}
		cache[baseCombo] = res.Trades
	}

	var aggOOS []engine.Trade
	var unionStart, unionEnd time.Time

	// 3. Untuk tiap fold: IS = blok[i], OOS = blok[i+1].
	for i := 0; i < p.Folds; i++ {
		isWin := blocks[i]
		oosWin := blocks[i+1]

		// Pilih kombinasi terbaik di IS: TotalR tertinggi dengan guard minTrades.
		var (
			best      Combo
			bestM     report.Metrics
			haveBest  bool
			bestTotal float64
		)
		for _, c := range p.Combos {
			isTrades := filterTrades(cache[c], isWin)
			m := report.Compute(isTrades, p.StartBalance)
			if m.Trades < p.MinTradesIS {
				continue
			}
			if !haveBest || m.TotalR > bestTotal {
				haveBest = true
				best = c
				bestM = m
				bestTotal = m.TotalR
			}
		}

		fallback := false
		if !haveBest {
			// Tak ada yang lolos guard → fallback ke baseline DefaultConfig.
			fallback = true
			best = baseCombo
			bestM = report.Compute(filterTrades(cache[best], isWin), p.StartBalance)
		}

		oosTrades := filterTrades(cache[best], oosWin)
		oosM := report.Compute(oosTrades, p.StartBalance)

		out.Folds = append(out.Folds, FoldResult{
			Index:      i,
			IS:         isWin,
			OOS:        oosWin,
			Chosen:     best,
			Fallback:   fallback,
			ISMetrics:  bestM,
			OOSMetrics: oosM,
			OOSTrades:  oosTrades,
		})

		aggOOS = append(aggOOS, oosTrades...)
		if unionStart.IsZero() || oosWin.Start.Before(unionStart) {
			unionStart = oosWin.Start
		}
		if unionEnd.IsZero() || oosWin.End.After(unionEnd) {
			unionEnd = oosWin.End
		}
	}

	// 4. Agregat OOS jujur.
	out.AggregateOOS = report.Compute(aggOOS, p.StartBalance)

	// 5. Baseline OOS: DefaultConfig params, trade difilter ke UNION semua window OOS.
	baseUnion := filterTrades(cache[baseCombo], Window{Start: unionStart, End: unionEnd})
	out.BaselineOOS = report.Compute(baseUnion, p.StartBalance)

	return out, nil
}
