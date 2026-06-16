// Implementasi metrik Section L (package doc ada di doc.go).

package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"xau-ict-engine/internal/engine"
	"xau-ict-engine/internal/xlsx"
)

// Metrics = ringkasan per-run (Section L "Per backtest run").
type Metrics struct {
	Trades        int
	Wins          int
	WinRate       float64
	TotalR        float64
	AvgR          float64
	ProfitFactor  float64 // gross win R / gross loss R
	MaxDrawdownR  float64
	MaxDrawdownPc float64 // % terhadap equity peak
	MaxConsecLoss int
	SharpeLike    float64 // mean(R)/stddev(R)
	NetPnLUSD     float64
	EndEquity     float64
}

// Compute menghitung Metrics dari sekumpulan trade (urut waktu fill).
func Compute(trades []engine.Trade, startBalance float64) Metrics {
	m := Metrics{}
	if len(trades) == 0 {
		m.EndEquity = startBalance
		return m
	}
	sorted := append([]engine.Trade(nil), trades...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	var grossWin, grossLoss, sumR, sumR2 float64
	var consec, maxConsec int
	equity := startBalance
	peak := startBalance
	var maxDDr, maxDDpc, cumR, peakR float64

	for _, t := range sorted {
		m.Trades++
		r := t.RRealized
		sumR += r
		sumR2 += r * r
		if t.Win() {
			m.Wins++
			grossWin += r
			consec = 0
		} else {
			grossLoss += -r
			consec++
			if consec > maxConsec {
				maxConsec = consec
			}
		}
		// drawdown dalam R
		cumR += r
		if cumR > peakR {
			peakR = cumR
		}
		if dd := peakR - cumR; dd > maxDDr {
			maxDDr = dd
		}
		// drawdown dalam % equity
		equity += t.PnLUSD
		if equity > peak {
			peak = equity
		}
		if peak > 0 {
			if dd := (peak - equity) / peak * 100; dd > maxDDpc {
				maxDDpc = dd
			}
		}
		m.NetPnLUSD += t.PnLUSD
	}

	m.TotalR = sumR
	m.AvgR = sumR / float64(m.Trades)
	m.WinRate = float64(m.Wins) / float64(m.Trades) * 100
	if grossLoss > 0 {
		m.ProfitFactor = grossWin / grossLoss
	} else if grossWin > 0 {
		m.ProfitFactor = math.Inf(1)
	}
	m.MaxDrawdownR = maxDDr
	m.MaxDrawdownPc = maxDDpc
	m.MaxConsecLoss = maxConsec
	if m.Trades > 1 {
		mean := m.AvgR
		variance := sumR2/float64(m.Trades) - mean*mean
		if variance > 0 {
			m.SharpeLike = mean / math.Sqrt(variance)
		}
	}
	m.EndEquity = equity
	return m
}

// executed mengembalikan trade yang tidak di-skip execution layer (J.2).
func executed(trades []engine.Trade) []engine.Trade {
	out := make([]engine.Trade, 0, len(trades))
	for _, t := range trades {
		if !t.WouldBeSkipped {
			out = append(out, t)
		}
	}
	return out
}

// Print menulis ringkasan + breakdown ke w (mis. os.Stdout).
func Print(w io.Writer, res engine.Result) {
	bal := res.Config.StartBalance
	all := Compute(res.Trades, bal)

	fmt.Fprintf(w, "=== BACKTEST XAU_USD  %s → %s ===\n",
		res.Start.Format("2006-01-02"), res.End.Format("2006-01-02"))
	fmt.Fprintf(w, "Sinyal di-fire (signal layer): %d\n", len(res.Signals))
	fmt.Fprintf(w, "Trade tersimulasi            : %d\n\n", len(res.Trades))

	printMetrics(w, "SIGNAL LAYER (semua sinyal)", all, bal)

	if res.Config.ExecLayerOn {
		ex := executed(res.Trades)
		skipped := len(res.Trades) - len(ex)
		fmt.Fprintf(w, "\n--- Execution layer ON (J.2) — %d would-be-skipped ---\n", skipped)
		printMetrics(w, "EXECUTED (setelah J.2)", Compute(ex, bal), bal)
	}

	fmt.Fprintln(w, "\n=== BREAKDOWN (signal layer) ===")
	printBreakdown(w, "Exit reason", res.Trades, bal, func(t engine.Trade) string { return t.ExitReason })
	printBreakdown(w, "ITH/ITL type", res.Trades, bal, func(t engine.Trade) string { return t.ITHITLType.String() })
	printBreakdown(w, "QT scenario", res.Trades, bal, func(t engine.Trade) string { return t.Scenario.String() })
	printBreakdown(w, "Daily phase", res.Trades, bal, func(t engine.Trade) string { return t.DailyPhase.String() })
	printBreakdown(w, "Regime", res.Trades, bal, func(t engine.Trade) string { return t.Regime.String() })
	printBreakdown(w, "RR target", res.Trades, bal, func(t engine.Trade) string {
		return fmt.Sprintf("1:%g", t.RR)
	})
	printBreakdown(w, "Day type", res.Trades, bal, func(t engine.Trade) string { return t.DayType.String() })
	printBreakdown(w, "Direction", res.Trades, bal, func(t engine.Trade) string { return t.Dir.String() })
	printBreakdown(w, "Session", res.Trades, bal, func(t engine.Trade) string { return t.Session.String() })
	printBreakdown(w, "POI tier", res.Trades, bal, func(t engine.Trade) string { return "T" + strconv.Itoa(t.POITier) })
	// Tier × TF asal POI: MaxPOITier hanya memfilter pool H1 (HTF bypass by
	// design) — breakdown gabungan menyesatkan tanpa pemisahan ini.
	printBreakdown(w, "POI tier×TF", res.Trades, bal, func(t engine.Trade) string {
		return t.POITF.String() + "-T" + strconv.Itoa(t.POITier)
	})
	printBreakdown(w, "MO relative", res.Trades, bal, func(t engine.Trade) string { return t.MORel })
	printBreakdown(w, "RelEq sweep", res.Trades, bal, func(t engine.Trade) string { return t.RelEqSwept })
	printBreakdown(w, "Flip timing", res.Trades, bal, func(t engine.Trade) string { return t.FlipTiming.String() })
}

func printMetrics(w io.Writer, title string, m Metrics, bal float64) {
	pf := "∞"
	if !math.IsInf(m.ProfitFactor, 1) {
		pf = fmt.Sprintf("%.2f", m.ProfitFactor)
	}
	fmt.Fprintf(w, "[%s]\n", title)
	fmt.Fprintf(w, "  Trades=%d  WinRate=%.1f%%  TotalR=%.2f  AvgR=%.3f\n",
		m.Trades, m.WinRate, m.TotalR, m.AvgR)
	fmt.Fprintf(w, "  ProfitFactor=%s  MaxConsecLoss=%d  Sharpe~=%.2f\n",
		pf, m.MaxConsecLoss, m.SharpeLike)
	fmt.Fprintf(w, "  MaxDD=%.2fR / %.1f%%  NetPnL=$%.2f  EndEquity=$%.2f (start $%.0f)\n",
		m.MaxDrawdownR, m.MaxDrawdownPc, m.NetPnLUSD, m.EndEquity, bal)
}

func printBreakdown(w io.Writer, dim string, trades []engine.Trade, bal float64, key func(engine.Trade) string) {
	groups := map[string][]engine.Trade{}
	var order []string
	for _, t := range trades {
		k := key(t)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], t)
	}
	sort.Strings(order)
	fmt.Fprintf(w, "\n%s:\n", dim)
	for _, k := range order {
		m := Compute(groups[k], bal)
		fmt.Fprintf(w, "  %-16s n=%-4d win=%5.1f%%  totR=%7.2f  avgR=%+.3f  PF=%s\n",
			k, m.Trades, m.WinRate, m.TotalR, m.AvgR, pfStr(m.ProfitFactor))
	}
}

func pfStr(pf float64) string {
	if math.IsInf(pf, 1) {
		return "∞"
	}
	return fmt.Sprintf("%.2f", pf)
}

// WriteCSV menulis per-trade record (skema Section L) ke path.
func WriteCSV(path string, res engine.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	defer cw.Flush()

	header := []string{
		"timestamp_ny", "direction", "regime", "session", "qt_scenario",
		"weekly_phase", "daily_phase", "day_type", "ith_itl_type",
		"entry", "sl", "tp", "rr_target", "poi_tier", "poi_tf", "poi_kinds", "poi_confluence",
		"lot", "risk_usd", "balance_basis",
		"exit_reason", "exit_price", "exit_timestamp_ny", "r_realized", "pnl_usd",
		"would_be_skipped", "skip_reason", "flip_timing",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, t := range res.Trades {
		rec := []string{
			t.Time.Format(time.RFC3339),
			t.Dir.String(),
			t.Regime.String(),
			t.Session.String(),
			t.Scenario.String(),
			t.WeeklyPhase.String(),
			t.DailyPhase.String(),
			t.DayType.String(),
			t.ITHITLType.String(),
			f64(t.Entry), f64(t.SL), f64(t.TP),
			fmt.Sprintf("1:%g", t.RR),
			strconv.Itoa(t.POITier),
			t.POITF.String(),
			t.POIKinds,
			strconv.Itoa(t.POIConfluence),
			f64(t.Lot), f64(t.RiskUSD), f64(t.BalanceAt),
			t.ExitReason, f64(t.ExitPrice), t.ExitTime.Format(time.RFC3339),
			f64(t.RRealized), f64(t.PnLUSD),
			strconv.FormatBool(t.WouldBeSkipped), t.SkipReason,
			t.FlipTiming.String(),
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	return cw.Error()
}

func f64(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }

// TradeReason menyusun alasan naratif satu paragraf (Bahasa Indonesia) dari
// tag konteks sebuah trade: arah, regime + alignment ke weekly OF, konteks QT
// (sesi/skenario/phase/day-type), POI, trigger entry, level harga, lalu hasil.
func TradeReason(t engine.Trade) string {
	// Alignment regime terhadap weekly order flow.
	align := "searah weekly OF"
	if t.Regime.String() == "retrace" {
		align = "LAWAN weekly OF (leg retrace)"
	}
	// Verdict hasil simulasi.
	verdict := "kalah"
	if t.RRealized > 0 {
		verdict = "menang"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s. ", t.Dir.String())
	fmt.Fprintf(&sb, "Regime %s (%s); daily bias align. ", t.Regime.String(), align)
	fmt.Fprintf(&sb, "QT: sesi %s, skenario %s, phase W=%s D=%s, day-type %s. ",
		t.Session.String(), t.Scenario.String(),
		t.WeeklyPhase.String(), t.DailyPhase.String(), t.DayType.String())
	fmt.Fprintf(&sb, "POI Tier%d (confluence %d) di zona benar. ",
		t.POITier, t.POIConfluence)
	fmt.Fprintf(&sb, "Trigger ITL/ITH %s 5m → fill %.2f, SL %.2f, TP %.2f (RR 1:%g). ",
		t.ITHITLType.String(), t.Entry, t.SL, t.TP, t.RR)
	fmt.Fprintf(&sb, "Hasil: %s — exit %s @ %.2f (%+.2fR).",
		verdict, t.ExitReason, t.ExitPrice, t.RRealized)
	return sb.String()
}

// WriteXLSX menulis per-trade record (skema sama dengan WriteCSV) ke satu sheet
// "trades" pada file .xlsx, PLUS kolom terakhir "reason" berisi TradeReason.
func WriteXLSX(path string, res engine.Result) error {
	header := []string{
		"timestamp_ny", "direction", "regime", "session", "qt_scenario",
		"weekly_phase", "daily_phase", "day_type", "ith_itl_type",
		"entry", "sl", "tp", "rr_target", "poi_tier", "poi_tf", "poi_kinds", "poi_confluence",
		"lot", "risk_usd", "balance_basis",
		"exit_reason", "exit_price", "exit_timestamp_ny", "r_realized", "pnl_usd",
		"would_be_skipped", "skip_reason", "flip_timing", "reason",
	}
	rows := make([][]string, 0, len(res.Trades))
	for _, t := range res.Trades {
		rows = append(rows, []string{
			t.Time.Format(time.RFC3339),
			t.Dir.String(),
			t.Regime.String(),
			t.Session.String(),
			t.Scenario.String(),
			t.WeeklyPhase.String(),
			t.DailyPhase.String(),
			t.DayType.String(),
			t.ITHITLType.String(),
			f64(t.Entry), f64(t.SL), f64(t.TP),
			fmt.Sprintf("1:%g", t.RR),
			strconv.Itoa(t.POITier),
			t.POITF.String(),
			t.POIKinds,
			strconv.Itoa(t.POIConfluence),
			f64(t.Lot), f64(t.RiskUSD), f64(t.BalanceAt),
			t.ExitReason, f64(t.ExitPrice), t.ExitTime.Format(time.RFC3339),
			f64(t.RRealized), f64(t.PnLUSD),
			strconv.FormatBool(t.WouldBeSkipped), t.SkipReason,
			t.FlipTiming.String(),
			TradeReason(t),
		})
	}
	return xlsx.Write(path, "trades", header, rows)
}
