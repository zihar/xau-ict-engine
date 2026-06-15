package main

import (
	"strings"
	"testing"
	"time"
)

func TestFmtDur(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Minute, "45 menit"},
		{59*time.Minute + 40*time.Second, "1 jam"}, // round ke menit → 60
		{9 * time.Hour, "9 jam"},
		{9*time.Hour + 15*time.Minute, "9 jam 15 menit"},
		{60 * time.Hour, "2 hari 12 jam"}, // weekend off — menit dibuang
	}
	for _, c := range cases {
		if got := fmtDur(c.d); got != c.want {
			t.Errorf("fmtDur(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestOfflineNote(t *testing.T) {
	now := time.Date(2026, 6, 3, 7, 0, 0, 0, time.UTC)
	iv := 5 * time.Minute
	cases := []struct {
		name     string
		lastTick time.Time
		interval time.Duration
		contains string // "" = harus tanpa catatan
	}{
		{"state lama tanpa field (zero)", time.Time{}, iv, ""},
		{"tick normal 5m", now.Add(-5 * time.Minute), iv, ""},
		{"di bawah ambang 30m", now.Add(-29 * time.Minute), iv, ""},
		{"pas ambang 30m", now.Add(-30 * time.Minute), iv, "30 menit"},
		{"shutdown semalam 9 jam", now.Add(-9 * time.Hour), iv, "9 jam"},
		// interval besar → ambang ikut naik ke 6×interval (bukan 30m flat)
		{"interval 1h, gap 2h = normal", now.Add(-2 * time.Hour), time.Hour, ""},
		{"interval 1h, gap 7h = offline", now.Add(-7 * time.Hour), time.Hour, "7 jam"},
	}
	for _, c := range cases {
		got := offlineNote(c.lastTick, now, c.interval)
		if c.contains == "" {
			if got != "" {
				t.Errorf("%s: harus tanpa catatan, dapat %q", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.contains) {
			t.Errorf("%s: catatan %q tidak memuat %q", c.name, got, c.contains)
		}
		if !strings.Contains(got, "sebelum offline") {
			t.Errorf("%s: catatan %q tanpa frasa konteks diff", c.name, got)
		}
	}
}
