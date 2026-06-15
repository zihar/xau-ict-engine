package engine

import (
	"sort"
	"time"

	"forex-backtest/internal/data"
	"forex-backtest/internal/detectors"
)

// simulateAll mensimulasikan tiap sinyal jadi trade (Section K). R realized
// independen dari sizing (TP/SL fixed 1:RR), jadi dihitung di sini; sizing/$
// diisi belakangan oleh applySizing.
func simulateAll(m5 []data.Candle, cfg Config, loc *time.Location, sigs []Signal) []Trade {
	trades := make([]Trade, 0, len(sigs))
	for _, sig := range sigs {
		trades = append(trades, simulateOne(m5, loc, sig))
	}
	return trades
}

// simulateOne men-scan M5 maju dari fill, cek SL/TP tiap candle. Kalau satu
// candle M5 kena dua-duanya → SL dulu (konservatif, K). Jumat 17:00 NY &
// friday_force_close → tutup di close (R aktual). Habis data → exit eod_data.
func simulateOne(m5 []data.Candle, loc *time.Location, sig Signal) Trade {
	t := Trade{Signal: sig}
	start := sort.Search(len(m5), func(i int) bool { return !m5[i].Time.Before(sig.fillM5Time) })
	// fill ada di start; mulai resolusi dari candle berikutnya (fill = open candle itu).
	for i := start; i < len(m5); i++ {
		c := m5[i]
		// Jumat force-close 17:00 NY (sebelum cek TP/SL candle ini? — cek dulu
		// agar TP/SL yang kena lebih awal di jam itu tetap tercatat; gunakan
		// boundary 17:00 untuk paksa keluar di harga close candle terakhir < 17:00).
		if isFridayClose(c.Time, loc) {
			t.ExitTime, t.ExitPrice, t.ExitReason = c.Time, c.Open, "friday_close"
			t.RRealized = rMultiple(sig, c.Open)
			return t
		}
		hitSL := touched(sig.Dir, c, sig.SL, false)
		hitTP := touched(sig.Dir, c, sig.TP, true)
		switch {
		case hitSL && hitTP:
			t.ExitTime, t.ExitPrice, t.ExitReason = c.Time, sig.SL, "sl" // konservatif
			t.RRealized = -1
			return t
		case hitSL:
			t.ExitTime, t.ExitPrice, t.ExitReason = c.Time, sig.SL, "sl"
			t.RRealized = -1
			return t
		case hitTP:
			t.ExitTime, t.ExitPrice, t.ExitReason = c.Time, sig.TP, "tp"
			t.RRealized = sig.RR
			return t
		}
	}
	// habis data → tutup di close terakhir
	if start < len(m5) {
		last := m5[len(m5)-1]
		t.ExitTime, t.ExitPrice, t.ExitReason = last.Time, last.Close, "eod_data"
		t.RRealized = rMultiple(sig, last.Close)
	} else {
		t.ExitReason = "no_fill_data"
	}
	return t
}

// touched: apakah candle menyentuh `level` ke arah TP (tp=true) atau SL.
func touched(dir detectors.Direction, c data.Candle, level float64, tp bool) bool {
	if dir == detectors.Bullish {
		if tp {
			return c.High >= level // TP di atas
		}
		return c.Low <= level // SL di bawah
	}
	if tp {
		return c.Low <= level // sell TP di bawah
	}
	return c.High >= level // sell SL di atas
}

// rMultiple = R realized pada harga keluar (signed; +RR ~ menang, -1 ~ kalah).
func rMultiple(sig Signal, exit float64) float64 {
	risk := sig.Entry - sig.SL // long: +, short: -
	if risk == 0 {
		return 0
	}
	return (exit - sig.Entry) / risk
}

// isFridayClose: candle berada di/melewati 17:00 NY hari Jumat (friday_force_close, K).
func isFridayClose(t time.Time, loc *time.Location) bool {
	nt := t.In(loc)
	return nt.Weekday() == time.Friday && nt.Hour() >= 17
}

// applySizing mengisi Lot/RiskUSD/PnLUSD/BalanceAt tiap trade + menerapkan
// weekly re-baseline (I.1). Event (entry & exit) diproses urut waktu agar equity
// realistis untuk trade konkuren; basis sizing = equity di awal minggu kalender
// trade itu dibuka.
func applySizing(res *Result, cfg Config, loc *time.Location, trades []Trade) {
	type ev struct {
		t      time.Time
		isExit bool
		idx    int
	}
	evs := make([]ev, 0, len(trades)*2)
	for i, tr := range trades {
		evs = append(evs, ev{t: tr.Time, isExit: false, idx: i})
		et := tr.ExitTime
		if et.IsZero() {
			et = tr.Time
		}
		evs = append(evs, ev{t: et, isExit: true, idx: i})
	}
	sort.SliceStable(evs, func(a, b int) bool {
		if evs[a].t.Equal(evs[b].t) {
			return !evs[a].isExit && evs[b].isExit // entry sebelum exit di waktu sama
		}
		return evs[a].t.Before(evs[b].t)
	})

	equity := cfg.StartBalance
	weekBasis := cfg.StartBalance
	curWeek := -1
	openCount := 0
	dayCount := map[int]int{} // yearday → jumlah trade dibuka (execution layer)

	for _, e := range evs {
		tr := &trades[e.idx]
		if !e.isExit {
			// awal minggu baru → re-baseline (I.1)
			if wk := isoWeekKey(e.t, loc); wk != curWeek {
				curWeek = wk
				weekBasis = equity
			}
			tr.BalanceAt = weekBasis
			slDist := absf(tr.Entry - tr.SL)
			lot, _ := detectors.LotSize(weekBasis, cfg.RiskPct, slDist, detectors.ContractOz, detectors.LotStep, detectors.LotMin)
			tr.Lot = lot
			tr.RiskUSD = weekBasis * cfg.RiskPct

			// Execution layer (J.2) — cuma tag, tak block.
			if cfg.ExecLayerOn {
				yd := yearDay(tr.Time, loc)
				if cfg.MaxTradePerDay > 0 && dayCount[yd] >= cfg.MaxTradePerDay {
					tr.WouldBeSkipped, tr.SkipReason = true, "max_trade_per_day"
				}
				if cfg.Concurrency > 0 && openCount >= cfg.Concurrency {
					tr.WouldBeSkipped = true
					if tr.SkipReason == "" {
						tr.SkipReason = "concurrency"
					}
				}
				dayCount[yd]++
			}
			openCount++
		} else {
			move := tr.ExitPrice - tr.Entry
			if tr.Dir == detectors.Bearish {
				move = -move
			}
			tr.PnLUSD = tr.Lot * detectors.ContractOz * move
			equity += tr.PnLUSD
			if openCount > 0 {
				openCount--
			}
		}
	}

	// Sinkronkan tag execution layer kembali ke Result.Signals (urut sama).
	res.Trades = trades
	if cfg.ExecLayerOn {
		byTime := map[time.Time]*Trade{}
		for i := range trades {
			byTime[trades[i].fillM5Time] = &trades[i]
		}
		for i := range res.Signals {
			if tr, ok := byTime[res.Signals[i].fillM5Time]; ok {
				res.Signals[i].WouldBeSkipped = tr.WouldBeSkipped
				res.Signals[i].SkipReason = tr.SkipReason
			}
		}
	}
}

// isoWeekKey = kunci unik tahun+minggu ISO (basis re-baseline mingguan).
func isoWeekKey(t time.Time, loc *time.Location) int {
	y, w := t.In(loc).ISOWeek()
	return y*100 + w
}

func yearDay(t time.Time, loc *time.Location) int {
	nt := t.In(loc)
	return nt.Year()*1000 + nt.YearDay()
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
