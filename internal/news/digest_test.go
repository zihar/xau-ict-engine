package news

import (
	"strings"
	"testing"
	"time"
)

// digestJSON = feed pekan dgn CPI (Rab), PPI (Kam), FOMC (Rab), + noise non-USD.
const digestJSON = `[
 {"title":"CPI m/m","country":"USD","date":"2026-06-10T08:30:00-04:00","impact":"High","forecast":"0.3%","previous":"0.6%"},
 {"title":"CPI y/y","country":"USD","date":"2026-06-10T08:30:00-04:00","impact":"High","forecast":"4.2%","previous":"3.8%"},
 {"title":"FOMC Statement","country":"USD","date":"2026-06-10T14:00:00-04:00","impact":"High","forecast":"","previous":""},
 {"title":"Federal Funds Rate","country":"USD","date":"2026-06-10T14:00:00-04:00","impact":"High","forecast":"4.50%","previous":"4.50%"},
 {"title":"PPI m/m","country":"USD","date":"2026-06-11T08:30:00-04:00","impact":"High","forecast":"0.7%","previous":"1.4%"},
 {"title":"German Bund Auction","country":"EUR","date":"2026-06-09T05:00:00-04:00","impact":"High","forecast":"","previous":""},
 {"title":"Some Survey","country":"USD","date":"2026-06-09T10:00:00-04:00","impact":"Medium","forecast":"50","previous":"49"}
]`

func TestDigestName(t *testing.T) {
	cases := map[string]string{
		"CPI m/m":            "CPI",
		"Core PPI m/m":       "PPI",
		"Non-Farm Payrolls":  "NFP",
		"FOMC Statement":     "FOMC",
		"Federal Funds Rate": "FOMC",
		"Retail Sales m/m":   "", // high-impact tapi di luar cakupan digest
		"German Bund":        "",
	}
	for title, want := range cases {
		if got := DigestName(title); got != want {
			t.Errorf("DigestName(%q) = %q, ingin %q", title, got, want)
		}
	}
}

func TestWeekDigestReleases(t *testing.T) {
	events, err := ParseCalendar([]byte(digestJSON))
	if err != nil {
		t.Fatalf("ParseCalendar: %v", err)
	}
	rel := WeekDigestReleases(events)
	// CPI (1 grup @Rab 08:30), FOMC (1 grup @Rab 14:00, 2 event), PPI (1 grup @Kam).
	// EUR & USD-Medium dibuang. = 3 release.
	if len(rel) != 3 {
		t.Fatalf("ingin 3 release, dapat %d: %+v", len(rel), rel)
	}
	// Terurut menaik waktu: CPI (12:30Z) → FOMC (18:00Z) → PPI (besok 12:30Z).
	if rel[0].Name != "CPI" || rel[1].Name != "FOMC" || rel[2].Name != "PPI" {
		t.Errorf("urutan salah: %s, %s, %s", rel[0].Name, rel[1].Name, rel[2].Name)
	}
	if rel[1].Kind != KindOther {
		t.Errorf("FOMC Kind = %v, ingin KindOther", rel[1].Kind)
	}
}

func TestBuildWeekDigest(t *testing.T) {
	events, _ := ParseCalendar([]byte(digestJSON))
	now := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC) // Senin pagi
	msg := BuildWeekDigest(WeekDigestReleases(events), now)

	for _, want := range []string{"NEWS HIGH-IMPACT", "CPI", "PPI", "FOMC", "3 rilis", "BEARISH gold", "penentu arah Fed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("digest tak memuat %q\n--- pesan ---\n%s", want, msg)
		}
	}
}

func TestBuildWeekDigestEmpty(t *testing.T) {
	now := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	msg := BuildWeekDigest(nil, now)
	if !strings.Contains(msg, "Tak ada rilis high-impact") {
		t.Errorf("pesan minggu kosong tak sesuai:\n%s", msg)
	}
}

func TestWeekKeyDedup(t *testing.T) {
	var s State
	s.Sent = map[string]Sent{}
	now := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	k := WeekKey(now)
	if s.DigestSent(k) {
		t.Fatal("digest belum dikirim tapi DigestSent=true")
	}
	s.MarkDigest(k, now)
	if !s.DigestSent(k) {
		t.Fatal("setelah MarkDigest harusnya DigestSent=true")
	}
	// Minggu berbeda → key beda → belum terkirim.
	if s.DigestSent(WeekKey(now.AddDate(0, 0, 7))) {
		t.Error("minggu berikutnya seharusnya belum terkirim")
	}
}
