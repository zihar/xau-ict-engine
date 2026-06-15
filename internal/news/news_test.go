package news

import (
	"testing"
	"time"
)

// sampleJSON = potongan nyata feed Forex Factory minggu CPI/PPI 10–11 Jun 2026
// (actual masih kosong = pra-rilis).
const sampleJSON = `[
 {"title":"Core CPI m/m","country":"USD","date":"2026-06-10T08:30:00-04:00","impact":"High","forecast":"0.5%","previous":"0.4%"},
 {"title":"CPI m/m","country":"USD","date":"2026-06-10T08:30:00-04:00","impact":"High","forecast":"0.3%","previous":"0.6%"},
 {"title":"CPI y/y","country":"USD","date":"2026-06-10T08:30:00-04:00","impact":"High","forecast":"4.2%","previous":"3.8%"},
 {"title":"PPI m/m","country":"USD","date":"2026-06-11T08:30:00-04:00","impact":"High","forecast":"0.7%","previous":"1.4%"},
 {"title":"Core PPI m/m","country":"USD","date":"2026-06-11T08:30:00-04:00","impact":"High","forecast":"0.5%","previous":"1.0%"},
 {"title":"Some Bank Holiday","country":"EUR","date":"2026-06-10T03:00:00-04:00","impact":"Low","forecast":"","previous":""}
]`

func TestParseAndFilter(t *testing.T) {
	events, err := ParseCalendar([]byte(sampleJSON))
	if err != nil {
		t.Fatalf("ParseCalendar: %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("ingin 6 event, dapat %d", len(events))
	}
	usd := FilterUSDHighImpact(events)
	if len(usd) != 5 {
		t.Fatalf("ingin 5 event USD high-impact, dapat %d", len(usd))
	}
	// Waktu di-normalisasi ke UTC: 08:30 ET (-04:00) = 12:30 UTC.
	want := time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)
	for _, e := range usd {
		if e.Title == "CPI y/y" && !e.Time.Equal(want) {
			t.Errorf("CPI y/y waktu = %s, ingin %s", e.Time, want)
		}
	}
}

func TestParseNumber(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"0.3%", 0.3, true},
		{"4.2%", 4.2, true},
		{"85K", 85000, true},
		{"-92K", -92000, true},
		{"1.4", 1.4, true},
		{"<0.1%", 0.1, true},
		{"1,250", 1250, true},
		{"", 0, false},
		{"n/a", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseNumber(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseNumber(%q) = %v,%v ingin %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestGroupAndHeadline(t *testing.T) {
	events, _ := ParseCalendar([]byte(sampleJSON))
	rels := GroupReleases(FilterUSDHighImpact(events), DefaultName)
	if len(rels) != 2 {
		t.Fatalf("ingin 2 release (CPI, PPI), dapat %d", len(rels))
	}
	byName := map[string]Release{}
	for _, r := range rels {
		byName[r.Name] = r
	}
	cpi, ok := byName["CPI"]
	if !ok {
		t.Fatal("release CPI tak ditemukan")
	}
	if len(cpi.Events) != 3 {
		t.Errorf("CPI ingin 3 event, dapat %d", len(cpi.Events))
	}
	if cpi.Kind != KindInflation {
		t.Errorf("CPI kind = %v, ingin Inflation", cpi.Kind)
	}
	h, ok := cpi.Headline()
	if !ok || h.Title != "CPI m/m" {
		t.Errorf("headline CPI = %q (ok=%v), ingin \"CPI m/m\"", h.Title, ok)
	}
}

func TestClassifyBias(t *testing.T) {
	events, _ := ParseCalendar([]byte(sampleJSON))
	rels := GroupReleases(FilterUSDHighImpact(events), DefaultName)
	var cpi Release
	for _, r := range rels {
		if r.Name == "CPI" {
			cpi = r
		}
	}

	// Inflasi PANAS: CPI m/m actual 0.5% > forecast 0.3% → bearish gold.
	hot := cpi.WithActual(map[string]string{"CPI m/m": "0.5%", "CPI y/y": "4.5%", "Core CPI m/m": "0.6%"})
	if !hot.Released() {
		t.Fatal("hot harus Released (semua actual terisi)")
	}
	if v := hot.Classify(); v.Bias != BiasBearish || v.Surprise != SurpriseAbove {
		t.Errorf("CPI panas → bias=%v surprise=%v, ingin Bearish/Above", v.Bias, v.Surprise)
	}

	// Inflasi LEMAH: CPI m/m actual 0.1% < forecast 0.3% → bullish gold.
	soft := cpi.WithActual(map[string]string{"CPI m/m": "0.1%", "CPI y/y": "3.6%", "Core CPI m/m": "0.2%"})
	if v := soft.Classify(); v.Bias != BiasBullish || v.Surprise != SurpriseBelow {
		t.Errorf("CPI lemah → bias=%v surprise=%v, ingin Bullish/Below", v.Bias, v.Surprise)
	}

	// SESUAI forecast → netral.
	inline := cpi.WithActual(map[string]string{"CPI m/m": "0.3%", "CPI y/y": "4.2%", "Core CPI m/m": "0.5%"})
	if v := inline.Classify(); v.Bias != BiasNeutral || v.Surprise != SurpriseInline {
		t.Errorf("CPI sesuai → bias=%v surprise=%v, ingin Neutral/Inline", v.Bias, v.Surprise)
	}
}

func TestJobsBias(t *testing.T) {
	// NFP beat (data kuat) → bearish gold di rezim hawkish; miss → bullish.
	r := Release{Name: "NFP", Kind: KindJobs, Time: time.Now(), Events: []Event{
		{Title: "Non-Farm Employment Change", Forecast: "85K", Previous: "115K", Actual: "172K"},
	}}
	if v := r.Classify(); v.Bias != BiasBearish || v.Surprise != SurpriseAbove {
		t.Errorf("NFP beat → bias=%v, ingin Bearish", v.Bias)
	}
	r.Events[0].Actual = "40K"
	if v := r.Classify(); v.Bias != BiasBullish {
		t.Errorf("NFP miss → bias=%v, ingin Bullish", v.Bias)
	}
}

func TestStateDedup(t *testing.T) {
	st := State{Sent: map[string]Sent{}}
	r := Release{Name: "CPI", Time: time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)}
	key := r.Key()
	if st.PreSent(key) || st.PostSent(key) {
		t.Fatal("state baru: pre/post harus belum terkirim")
	}
	now := time.Now()
	st.MarkPre(key, now)
	if !st.PreSent(key) || st.PostSent(key) {
		t.Error("setelah MarkPre: PreSent harus true, PostSent false")
	}
	st.MarkPost(key, now)
	if !st.PostSent(key) {
		t.Error("setelah MarkPost: PostSent harus true")
	}
	if key != "CPI@2026-06-10T12:30:00Z" {
		t.Errorf("Key() = %q tak sesuai format", key)
	}
}
