package news

import (
	"testing"
	"time"
)

// mkRelease = Release sederhana untuk uji keputusan (satu event headline m/m).
func mkRelease(name string, t time.Time, actual string) Release {
	return Release{
		Name: name,
		Kind: KindInflation,
		Time: t,
		Events: []Event{
			{Title: name + " m/m", Time: t, Forecast: "0.3%", Previous: "0.2%", Actual: actual},
		},
	}
}

func emptyState() State { return State{Sent: map[string]Sent{}} }

func TestDecidePre(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r := mkRelease("CPI", now.Add(30*time.Minute), "") // belum rilis, 30 menit lagi
	st := emptyState()
	mode, mins := Decide(r, now, st, 60*time.Minute, 12*time.Hour)
	if mode != "pre" {
		t.Fatalf("mode = %q, mau \"pre\"", mode)
	}
	if mins != 30 {
		t.Fatalf("mins = %d, mau 30", mins)
	}
}

func TestDecidePreSudahSent(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r := mkRelease("CPI", now.Add(30*time.Minute), "")
	st := emptyState()
	st.MarkPre(r.Key(), now)
	if mode, _ := Decide(r, now, st, 60*time.Minute, 12*time.Hour); mode != "none" {
		t.Fatalf("mode = %q, mau \"none\" (pre sudah dikirim)", mode)
	}
}

func TestDecidePreDiLuarWindow(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r := mkRelease("CPI", now.Add(2*time.Hour), "") // 120 menit > prewindow 60m
	if mode, _ := Decide(r, now, emptyState(), 60*time.Minute, 12*time.Hour); mode != "none" {
		t.Fatalf("mode = %q, mau \"none\" (di luar prewindow)", mode)
	}
}

func TestDecidePost(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r := mkRelease("CPI", now.Add(-5*time.Minute), "0.5%") // sudah rilis 5 menit lalu
	mode, _ := Decide(r, now, emptyState(), 60*time.Minute, 12*time.Hour)
	if mode != "post" {
		t.Fatalf("mode = %q, mau \"post\"", mode)
	}
}

func TestDecidePostSudahSent(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r := mkRelease("CPI", now.Add(-5*time.Minute), "0.5%")
	st := emptyState()
	st.MarkPost(r.Key(), now)
	if mode, _ := Decide(r, now, st, 60*time.Minute, 12*time.Hour); mode != "none" {
		t.Fatalf("mode = %q, mau \"none\" (post sudah dikirim)", mode)
	}
}

func TestDecidePostTerlaluTua(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	r := mkRelease("CPI", now.Add(-13*time.Hour), "0.5%") // > stale 12h
	if mode, _ := Decide(r, now, emptyState(), 60*time.Minute, 12*time.Hour); mode != "none" {
		t.Fatalf("mode = %q, mau \"none\" (rilis terlalu tua)", mode)
	}
}

func TestPickTargetTerdekat(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	releases := []Release{
		mkRelease("CPI", now.Add(3*time.Hour), ""),    // jauh
		mkRelease("CPI", now.Add(30*time.Minute), ""), // terdekat
		mkRelease("PPI", now.Add(10*time.Minute), ""), // nama beda
	}
	r, ok := PickTarget(releases, "CPI", now, 12*time.Hour)
	if !ok {
		t.Fatal("ok = false, mau true")
	}
	if got := r.Time.Sub(now); got != 30*time.Minute {
		t.Fatalf("dipilih rilis +%s, mau +30m", got)
	}
}

func TestPickTargetBuangYangLebihTuaDariStale(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	releases := []Release{
		mkRelease("CPI", now.Add(-13*time.Hour), "0.5%"), // lebih tua dari stale 12h → buang
		mkRelease("CPI", now.Add(5*time.Hour), ""),       // valid (akan dipilih)
	}
	r, ok := PickTarget(releases, "CPI", now, 12*time.Hour)
	if !ok {
		t.Fatal("ok = false, mau true (masih ada 1 rilis valid)")
	}
	if got := r.Time.Sub(now); got != 5*time.Hour {
		t.Fatalf("dipilih rilis +%s, mau +5h (yang -13h harus dibuang)", got)
	}
}

func TestPickTargetTidakAda(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	releases := []Release{mkRelease("PPI", now.Add(30*time.Minute), "")}
	if _, ok := PickTarget(releases, "NFP", now, 12*time.Hour); ok {
		t.Fatal("ok = true, mau false (tak ada NFP)")
	}
}

func TestParseNames(t *testing.T) {
	got := ParseNames(" cpi, PPI ,nfp,CPI,, ")
	want := []string{"CPI", "PPI", "NFP"}
	if len(got) != len(want) {
		t.Fatalf("len = %d (%v), mau %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, mau %q", i, got[i], want[i])
		}
	}
}
