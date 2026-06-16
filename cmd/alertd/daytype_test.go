package main

import (
	"strings"
	"testing"
	"time"

	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/engine"
	"xau-ict-engine/internal/notify"
)

// fakeNotifier menangkap pesan tanpa kirim ke Telegram.
type fakeNotifier struct{ msgs []string }

func (f *fakeNotifier) SendMessage(text string) error { f.msgs = append(f.msgs, text); return nil }

func tfAtH1(at time.Time) engine.TFData {
	return engine.TFData{H1: []data.Candle{{Time: at}}}
}

func TestMaybeSendDayTypeAlert(t *testing.T) {
	loc, err := detectors.NYLocation()
	if err != nil {
		t.Skipf("tzdata NY tak tersedia: %v", err)
	}
	// Jun 15 2026 = Senin. Trading-day "now=07:00 NY" → mulai Jun 14 18:00 NY,
	// London close Jun 15 06:00 NY. Candle 05:00 NY = trading-day yang sama.
	nyEligible := time.Date(2026, 6, 15, 7, 0, 20, 0, loc)  // setelah London close
	nyEarly := time.Date(2026, 6, 15, 2, 0, 20, 0, loc)     // masih sesi London (sebelum 06:00)
	candleSameDay := time.Date(2026, 6, 15, 5, 0, 0, 0, loc) // last London hour, trading-day sama
	wantDay := detectors.TradingDayStart(nyEligible, loc)

	t.Run("normal → tak kirim", func(t *testing.T) {
		fn := &fakeNotifier{}
		d := &daemon{notifier: fn}
		st := notify.State{}
		d.maybeSendDayTypeAlert(engine.ScanNarrative{QTDayType: "normal"}, tfAtH1(candleSameDay), nyEligible, &st)
		if len(fn.msgs) != 0 {
			t.Errorf("normal harus diam, malah kirim %d", len(fn.msgs))
		}
	})

	t.Run("heavy_accum sebelum London close → tak kirim", func(t *testing.T) {
		fn := &fakeNotifier{}
		d := &daemon{notifier: fn}
		st := notify.State{}
		earlyCandle := time.Date(2026, 6, 15, 1, 0, 0, 0, loc)
		d.maybeSendDayTypeAlert(engine.ScanNarrative{QTDayType: "heavy_accum"}, tfAtH1(earlyCandle), nyEarly, &st)
		if len(fn.msgs) != 0 {
			t.Errorf("sebelum London close harus diam, malah kirim %d", len(fn.msgs))
		}
	})

	t.Run("heavy_accum setelah London close → kirim sekali + set state", func(t *testing.T) {
		fn := &fakeNotifier{}
		d := &daemon{notifier: fn}
		st := notify.State{}
		d.maybeSendDayTypeAlert(engine.ScanNarrative{QTDayType: "heavy_accum"}, tfAtH1(candleSameDay), nyEligible, &st)
		if len(fn.msgs) != 1 {
			t.Fatalf("harus kirim 1, dapat %d", len(fn.msgs))
		}
		if !strings.Contains(fn.msgs[0], "HEAVY ACCUM") {
			t.Errorf("pesan tak mengandung HEAVY ACCUM: %q", fn.msgs[0])
		}
		if !st.LastDayTypeAlert.Equal(wantDay) {
			t.Errorf("LastDayTypeAlert = %v, mau %v", st.LastDayTypeAlert, wantDay)
		}
		// Dedup: tick ulang trading-day yang sama → tak kirim lagi.
		d.maybeSendDayTypeAlert(engine.ScanNarrative{QTDayType: "heavy_accum"}, tfAtH1(candleSameDay), nyEligible, &st)
		if len(fn.msgs) != 1 {
			t.Errorf("dedup gagal: kirim %d (mau tetap 1)", len(fn.msgs))
		}
	})

	t.Run("trading-day berikutnya → kirim lagi", func(t *testing.T) {
		fn := &fakeNotifier{}
		d := &daemon{notifier: fn}
		st := notify.State{LastDayTypeAlert: wantDay} // sudah dialert hari ini
		nextNow := nyEligible.AddDate(0, 0, 1)
		nextCandle := candleSameDay.AddDate(0, 0, 1)
		d.maybeSendDayTypeAlert(engine.ScanNarrative{QTDayType: "heavy_expanding"}, tfAtH1(nextCandle), nextNow, &st)
		if len(fn.msgs) != 1 {
			t.Fatalf("trading-day baru harus kirim 1, dapat %d", len(fn.msgs))
		}
		if !strings.Contains(fn.msgs[0], "HEAVY EXPANDING") {
			t.Errorf("pesan tak mengandung HEAVY EXPANDING: %q", fn.msgs[0])
		}
	})

	t.Run("candle basi lintas trading-day → tak kirim", func(t *testing.T) {
		fn := &fakeNotifier{}
		d := &daemon{notifier: fn}
		st := notify.State{}
		staleCandle := time.Date(2026, 6, 13, 10, 0, 0, 0, loc) // 2 trading-day lampau
		d.maybeSendDayTypeAlert(engine.ScanNarrative{QTDayType: "heavy_accum"}, tfAtH1(staleCandle), nyEligible, &st)
		if len(fn.msgs) != 0 {
			t.Errorf("candle basi harus diam, malah kirim %d", len(fn.msgs))
		}
	})

	t.Run("H1 kosong → tak kirim & tak panic", func(t *testing.T) {
		fn := &fakeNotifier{}
		d := &daemon{notifier: fn}
		st := notify.State{}
		d.maybeSendDayTypeAlert(engine.ScanNarrative{QTDayType: "heavy_accum"}, engine.TFData{}, nyEligible, &st)
		if len(fn.msgs) != 0 {
			t.Errorf("H1 kosong harus diam, malah kirim %d", len(fn.msgs))
		}
	})
}

func TestFormatDayTypeAlert(t *testing.T) {
	loc, err := detectors.NYLocation()
	if err != nil {
		t.Skipf("tzdata NY tak tersedia: %v", err)
	}
	day := time.Date(2026, 6, 15, 18, 0, 0, 0, loc)
	accum := formatDayTypeAlert("heavy_accum", day, loc)
	if !strings.Contains(accum, "HEAVY ACCUM") || !strings.Contains(accum, "BLOCK") {
		t.Errorf("format accum salah: %q", accum)
	}
	exp := formatDayTypeAlert("heavy_expanding", day, loc)
	if !strings.Contains(exp, "HEAVY EXPANDING") || !strings.Contains(exp, "caution") {
		t.Errorf("format expanding salah: %q", exp)
	}
}
