package main

import (
	"testing"
	"time"

	"xau-ict-engine/internal/detectors"
)

func TestLatestQ1Close(t *testing.T) {
	loc, err := detectors.NYLocation()
	if err != nil {
		t.Fatalf("NYLocation: %v", err)
	}
	mk := func(h, m int) time.Time { return time.Date(2026, 6, 3, h, m, 20, 0, loc) }
	cases := []struct {
		name     string
		now      time.Time
		wantSess detectors.SessionKind
		wantOK   bool
		wantHM   string // jam:menit close Q1 (NY)
	}{
		{"asia Q1 baru close", mk(19, 30), detectors.Asia, true, "19:30"},
		{"asia Q2 (Q1 sudah close)", mk(19, 35), detectors.Asia, true, "19:30"},
		{"london Q1 close", mk(1, 30), detectors.London, true, "01:30"},
		{"ny-am Q1 close", mk(7, 30), detectors.NYAM, true, "07:30"},
		{"pm Q1 close — TIDAK dialert", mk(13, 30), detectors.PM, false, "13:30"},
		{"masih di Q1 asia → fallback PM (tak dialert)", mk(18, 30), detectors.PM, false, "13:30"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, sess, ok := latestQ1Close(c.now, loc)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, mau %v", ok, c.wantOK)
			}
			if sess != c.wantSess {
				t.Errorf("sess = %v, mau %v", sess, c.wantSess)
			}
			if got := b.In(loc).Format("15:04"); got != c.wantHM {
				t.Errorf("Q1 close = %s, mau %s", got, c.wantHM)
			}
		})
	}
}

func TestTradingDayQ1Slots(t *testing.T) {
	loc, _ := detectors.NYLocation()
	// now = 10:00 NY (NY-AM) → trading day mulai 18:00 NY hari SEBELUMNYA.
	now := time.Date(2026, 6, 3, 10, 0, 0, 0, loc)
	slots := tradingDayQ1Slots(now, loc)
	if len(slots) != 3 {
		t.Fatalf("mau 3 slot, dapat %d", len(slots))
	}
	want := []struct {
		sess             detectors.SessionKind
		startHM, closeHM string
	}{
		{detectors.Asia, "18:00", "19:30"},
		{detectors.London, "00:00", "01:30"},
		{detectors.NYAM, "06:00", "07:30"},
	}
	for i, w := range want {
		s := slots[i]
		if s.sess != w.sess {
			t.Errorf("slot %d sess = %v, mau %v", i, s.sess, w.sess)
		}
		if got := s.start.In(loc).Format("15:04"); got != w.startHM {
			t.Errorf("slot %d start = %s, mau %s", i, got, w.startHM)
		}
		if got := s.close.In(loc).Format("15:04"); got != w.closeHM {
			t.Errorf("slot %d close = %s, mau %s", i, got, w.closeHM)
		}
	}
	// Asia start harus SEBELUM London/NY-AM (trading day kronologis).
	if !slots[0].start.Before(slots[1].start) || !slots[1].start.Before(slots[2].start) {
		t.Error("urutan slot tidak kronologis Asia→London→NY-AM")
	}
}

// Tick TEPAT setelah close (≤ interval) memicu; tick jauh setelahnya tidak —
// mereplikasi guard now.Sub(b) di maybeSendQ1Alert.
func TestQ1ImmediateTickGuard(t *testing.T) {
	loc, _ := detectors.NYLocation()
	interval := 5 * time.Minute
	b, _, ok := latestQ1Close(time.Date(2026, 6, 3, 19, 30, 20, 0, loc), loc)
	if !ok {
		t.Fatal("Asia Q1 harus ok")
	}
	fresh := time.Date(2026, 6, 3, 19, 30, 20, 0, loc) // 20s setelah close
	stale := time.Date(2026, 6, 3, 19, 45, 20, 0, loc) // 15m setelah close
	if d := fresh.Sub(b); d < 0 || d >= interval {
		t.Errorf("tick fresh (%s) seharusnya memicu (diff %s)", fresh.Format("15:04:05"), d)
	}
	if d := stale.Sub(b); d >= 0 && d < interval {
		t.Errorf("tick stale (%s) seharusnya TIDAK memicu (diff %s)", stale.Format("15:04:05"), d)
	}
}
