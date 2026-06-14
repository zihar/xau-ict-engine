package detectors

import (
	"testing"
	"time"
)

func nyLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := NYLocation()
	if err != nil {
		t.Fatalf("NYLocation: %v", err)
	}
	return loc
}

func TestSessionStart(t *testing.T) {
	loc := nyLoc(t)
	// (jam NY, jam awal sesi yang diharapkan)
	cases := []struct{ h, want int }{
		{18, 18}, {19, 18}, {23, 18}, // Asia 18:00
		{0, 0}, {1, 0}, {5, 0}, // London 00:00
		{6, 6}, {7, 6}, {11, 6}, // NY-AM 06:00
		{12, 12}, {13, 12}, {17, 12}, // PM 12:00
	}
	for _, c := range cases {
		tt := time.Date(2026, 6, 3, c.h, 45, 0, 0, loc)
		got := SessionStart(tt, loc).In(loc).Hour()
		if got != c.want {
			t.Errorf("SessionStart(jam %02d) = %02d, mau %02d", c.h, got, c.want)
		}
	}
}

func TestSessionQuarter(t *testing.T) {
	loc := nyLoc(t)
	mk := func(h, m int) time.Time { return time.Date(2026, 6, 3, h, m, 0, 0, loc) }
	cases := []struct {
		t    time.Time
		want int
	}{
		{mk(18, 0), 1}, {mk(19, 29), 1}, // Asia Q1 18:00–19:30
		{mk(19, 30), 2}, {mk(20, 59), 2}, // Q2 19:30–21:00
		{mk(21, 0), 3}, {mk(22, 30), 4}, // Q3/Q4
		{mk(0, 0), 1}, {mk(1, 30), 2}, // London
		{mk(6, 0), 1}, {mk(7, 30), 2}, // NY-AM
	}
	for _, c := range cases {
		if got := SessionQuarter(c.t, loc); got != c.want {
			t.Errorf("SessionQuarter(%s) = %d, mau %d", c.t.In(loc).Format("15:04"), got, c.want)
		}
	}
}
