package detectors

import (
	"math"
	"time"

	"forex-backtest/internal/data"
)

// Default Section M (Layer G/H/I).
const (
	RRDefault    = 3.0   // rr_default (FIXED 1:3)
	RRReduced    = 2.0   // rr_reduced (1:2 untuk entry Senin & Jumat — user 2026-06-01)
	RiskPct      = 0.005 // risk_per_trade_pct
	ContractOz   = 100.0 // gold_contract_oz (1 lot = 100 oz, $1 = $100/lot)
	LotStep      = 0.01
	LotMin       = 0.01
	SLBufferPips = 5.0 // sl_buffer_pips_gold ($0.50)
)

// SLMode = dual-track SL anchor (H.1).
type SLMode int

const (
	SLLtfStructure SLMode = iota // DEFAULT — wick ITL/ITH 5m + buffer (personal user)
	SLBodyPOI                    // body POI 15m/1H + buffer (David)
)

// SLPrice menghitung harga SL (H.1, dual-track). dir = arah trade.
// ltfPivot = harga wick ITL (buy) / ITH (sell) 5m; dipakai mode ltf_structure.
// poi = POI terpilih; dipakai mode body_poi (pakai Bottom/Top body).
func SLPrice(dir Direction, mode SLMode, ltfPivot float64, poi POI, bufferPips float64) float64 {
	buf := PipsToPrice(bufferPips)
	if dir == Bullish {
		anchor := ltfPivot
		if mode == SLBodyPOI {
			anchor = poi.Bottom
		}
		return anchor - buf
	}
	anchor := ltfPivot
	if mode == SLBodyPOI {
		anchor = poi.Top
	}
	return anchor + buf
}

// TPPrice = entry ± rr × |entry−SL| (H.2, H-Q4a). risk diukur murni dari entry→SL.
func TPPrice(dir Direction, entry, sl, rr float64) float64 {
	risk := abs(entry - sl)
	if dir == Bullish {
		return entry + rr*risk
	}
	return entry - rr*risk
}

// RRTarget: 1:2 untuk entry Senin & Jumat, selain itu 1:3 (H.2, H-Q5a + user
// 2026-06-01: tambah Senin). Senin & Jumat = tepi minggu (likuiditas/volatilitas
// awal-akhir siklus) → target lebih konservatif.
func RRTarget(wd time.Weekday) float64 {
	if wd == time.Monday || wd == time.Friday {
		return RRReduced
	}
	return RRDefault
}

// LotSize menerapkan position sizing I.1 (worked example user):
//
//	risk_$ = balance × riskPct ; risk_per_lot = SL_distance$ × contractOz
//	lot = round_down(risk_$ / risk_per_lot, step)
//
// ok=false kalau lot < min (SL terlalu jauh untuk risk segitu).
func LotSize(balance, riskPct, slDistance, contractOz, step, min float64) (lot float64, ok bool) {
	if slDistance <= 0 || contractOz <= 0 || balance <= 0 {
		return 0, false
	}
	riskUSD := balance * riskPct
	riskPerLot := slDistance * contractOz
	lot = floorStep(riskUSD/riskPerLot, step)
	return lot, lot >= min
}

func floorStep(x, step float64) float64 {
	if step <= 0 {
		return x
	}
	return math.Floor(x/step) * step
}

// EntryTrigger mencari trigger entry confirmation di 5m (G.2): ITL 5m (buy) /
// ITH 5m (sell) yang terkonfirmasi pada/setelah `fromIdx` (saat harga sampai
// POI). Mengembalikan intermediate pemicu + index fill (= OPEN candle 5m
// BERIKUTNYA setelah candle break, `entry_fill: next_5m_open`).
func EntryTrigger(m5 []data.Candle, dir Direction, fromIdx int, breakType BreakType) (it Intermediate, fillIdx int, ok bool) {
	return EntryTriggerMin(m5, dir, fromIdx, breakType, 0, false, false)
}

// EntryTriggerMin = EntryTrigger dengan filter magnitude swing 5m (minLegPips,
// dikonversi ke harga via PipsToPrice) + opsi requireStandard (hanya terima
// ITL/ITH tipe STANDARD, lewati fast_early). Meredam trigger noise/early.
// minLegPips <= 0 → tanpa filter magnitude; requireStandard=false → terima semua tipe.
func EntryTriggerMin(m5 []data.Candle, dir Direction, fromIdx int, breakType BreakType, minLegPips float64, requireStandard, strict bool) (it Intermediate, fillIdx int, ok bool) {
	want := ITLow
	if dir == Bearish {
		want = ITHigh
	}
	for _, x := range detectIntermediates(m5, breakType, PipsToPrice(minLegPips), strict) {
		if x.Kind != want || x.ConfirmIndex < fromIdx {
			continue
		}
		if requireStandard && x.Type != ITStandard {
			continue // lewati fast_early — cari standard berikutnya
		}
		fi := x.ConfirmIndex + 1
		if fi >= len(m5) {
			return Intermediate{}, 0, false // fill belum tersedia
		}
		return x, fi, true
	}
	return Intermediate{}, 0, false
}
