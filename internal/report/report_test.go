package report

import (
	"math"
	"testing"

	"xau-ict-engine/internal/engine"
)

func tr(r, pnl float64) engine.Trade {
	t := engine.Trade{RRealized: r, PnLUSD: pnl}
	return t
}

func TestComputeMetrics(t *testing.T) {
	// 3 menang (+3R) , 2 kalah (-1R). totR = 9-2 = 7; PF = 9/2 = 4.5.
	trades := []engine.Trade{tr(3, 300), tr(-1, -100), tr(3, 300), tr(-1, -100), tr(3, 300)}
	m := Compute(trades, 25000)
	if m.Trades != 5 || m.Wins != 3 {
		t.Fatalf("trades=%d wins=%d", m.Trades, m.Wins)
	}
	if m.WinRate != 60 {
		t.Errorf("winrate=%g mau 60", m.WinRate)
	}
	if m.TotalR != 7 {
		t.Errorf("totalR=%g mau 7", m.TotalR)
	}
	if math.Abs(m.ProfitFactor-4.5) > 1e-9 {
		t.Errorf("PF=%g mau 4.5", m.ProfitFactor)
	}
	if m.NetPnLUSD != 700 || m.EndEquity != 25700 {
		t.Errorf("netPnL=%g end=%g", m.NetPnLUSD, m.EndEquity)
	}
}

func TestMaxConsecLossAndDD(t *testing.T) {
	// urutan: +3, -1, -1, -1, +3 → max consec loss = 3; DD R = 3 (dari peak 3 ke 0).
	trades := []engine.Trade{tr(3, 0), tr(-1, 0), tr(-1, 0), tr(-1, 0), tr(3, 0)}
	m := Compute(trades, 25000)
	if m.MaxConsecLoss != 3 {
		t.Errorf("maxConsecLoss=%d mau 3", m.MaxConsecLoss)
	}
	if m.MaxDrawdownR != 3 {
		t.Errorf("maxDDr=%g mau 3", m.MaxDrawdownR)
	}
}

func TestProfitFactorInfWhenNoLoss(t *testing.T) {
	m := Compute([]engine.Trade{tr(3, 0), tr(3, 0)}, 1000)
	if !math.IsInf(m.ProfitFactor, 1) {
		t.Errorf("PF tanpa loss harus +Inf, dapat %g", m.ProfitFactor)
	}
}

func TestEmptyTrades(t *testing.T) {
	m := Compute(nil, 5000)
	if m.Trades != 0 || m.EndEquity != 5000 {
		t.Errorf("empty: trades=%d end=%g", m.Trades, m.EndEquity)
	}
}
