package news

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Kind = jenis indikator (menentukan arah pemetaan surprise → bias gold).
type Kind int

const (
	KindOther     Kind = iota
	KindInflation      // CPI / PPI: actual > forecast = "panas" → bearish gold
	KindJobs           // NFP / Payrolls: actual > forecast = "beat" → bearish gold (rezim hawkish 2026)
)

// KindOf menebak jenis indikator dari judul event.
func KindOf(title string) Kind {
	t := strings.ToUpper(title)
	switch {
	case strings.Contains(t, "CPI"), strings.Contains(t, "PPI"),
		strings.Contains(t, "PCE"), strings.Contains(t, "PRICE INDEX"):
		return KindInflation
	case strings.Contains(t, "NON-FARM"), strings.Contains(t, "NONFARM"),
		strings.Contains(t, "NFP"), strings.Contains(t, "PAYROLL"):
		return KindJobs
	}
	return KindOther
}

// Surprise = hasil rilis relatif forecast.
type Surprise int

const (
	SurpriseUnknown Surprise = iota // actual/forecast tak tersedia
	SurpriseInline                  // sama dengan forecast (dalam toleransi)
	SurpriseAbove                   // actual > forecast
	SurpriseBelow                   // actual < forecast
)

// Bias = arah dampak ke XAUUSD.
type Bias int

const (
	BiasNeutral Bias = iota
	BiasBearish
	BiasBullish
)

func (b Bias) String() string {
	switch b {
	case BiasBearish:
		return "BEARISH gold"
	case BiasBullish:
		return "BULLISH gold"
	default:
		return "NETRAL"
	}
}

// ParseNumber mengubah nilai feed ("0.3%", "85K", "-92K", "1.4", "<0.1%") jadi
// float. Menangani sufiks %, K (ribu), M (juta), B (miliar), prefiks </>, koma
// pemisah ribuan, dan spasi. ok=false bila string kosong / tak ber-angka.
func ParseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	s = strings.TrimLeft(s, "<>~≈ ")
	s = strings.ReplaceAll(s, ",", "")
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "%"):
		s = strings.TrimSuffix(s, "%")
	case strings.HasSuffix(s, "K"), strings.HasSuffix(s, "k"):
		s, mult = s[:len(s)-1], 1e3
	case strings.HasSuffix(s, "M"), strings.HasSuffix(s, "m"):
		s, mult = s[:len(s)-1], 1e6
	case strings.HasSuffix(s, "B"), strings.HasSuffix(s, "b"):
		s, mult = s[:len(s)-1], 1e9
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v * mult, true
}

// classifySurprise membandingkan actual vs forecast satu Event. Toleransi epsilon
// kecil (data sudah dibulatkan ke 0.1) → selisih nyata sekecil apapun = Above/Below.
func classifySurprise(e Event) Surprise {
	a, okA := ParseNumber(e.Actual)
	f, okF := ParseNumber(e.Forecast)
	if !okA || !okF {
		return SurpriseUnknown
	}
	const eps = 1e-9
	switch {
	case math.Abs(a-f) <= eps:
		return SurpriseInline
	case a > f:
		return SurpriseAbove
	default:
		return SurpriseBelow
	}
}

// biasFor memetakan (jenis, surprise) → bias gold. Mengembalikan juga label
// surprise yang ramah-dibaca ("panas/lemah" utk inflasi, "beat/miss" utk jobs).
//
// ⚠️ Pemetaan ini mengasumsikan REZIM 2026 (gold sensitif ke Fed/yield: data
// kuat = hawkish = bearish gold). Di rezim rate-cut, jobs-beat bisa berbeda.
// Konteks ini di-embed ke pesan, bukan disembunyikan.
func biasFor(kind Kind, s Surprise) (bias Bias, label string) {
	switch kind {
	case KindInflation:
		switch s {
		case SurpriseAbove:
			return BiasBearish, "PANAS (di atas forecast)"
		case SurpriseBelow:
			return BiasBullish, "LEMAH (di bawah forecast)"
		case SurpriseInline:
			return BiasNeutral, "SESUAI forecast"
		}
	case KindJobs:
		switch s {
		case SurpriseAbove:
			return BiasBearish, "BEAT (di atas forecast)"
		case SurpriseBelow:
			return BiasBullish, "MISS (di bawah forecast)"
		case SurpriseInline:
			return BiasNeutral, "SESUAI forecast"
		}
	}
	return BiasNeutral, "tak terklasifikasi"
}

// Release = sekelompok Event satu indikator pada satu timestamp (mis. semua baris
// "CPI*" 10 Jun 08:30): headline + core + varian m/m & y/y. Memudahkan satu pesan
// gabungan ketimbang 4 alert terpisah.
type Release struct {
	Name   string // label indikator: "CPI" / "PPI" / "NFP"
	Kind   Kind
	Time   time.Time
	Events []Event
}

// Released = true bila SEMUA event inti sudah punya Actual (rilis lengkap).
func (r Release) Released() bool {
	for _, e := range r.Events {
		if !e.Released() {
			return false
		}
	}
	return len(r.Events) > 0
}

// AnyReleased = true bila MINIMAL satu event sudah punya Actual (rilis mulai masuk).
func (r Release) AnyReleased() bool {
	for _, e := range r.Events {
		if e.Released() {
			return true
		}
	}
	return false
}

// Headline mengembalikan event "utama" Release (judul yang TIDAK mengandung "core"
// dan mengandung "m/m" bila ada; jatuh ke event pertama). Dipakai sebagai dasar
// klasifikasi bias utama.
func (r Release) Headline() (Event, bool) {
	var mm, any Event
	var okMM, okAny bool
	for _, e := range r.Events {
		t := strings.ToLower(e.Title)
		if strings.Contains(t, "core") {
			continue
		}
		if !okAny {
			any, okAny = e, true
		}
		if strings.Contains(t, "m/m") {
			mm, okMM = e, true
		}
	}
	if okMM {
		return mm, true
	}
	if okAny {
		return any, true
	}
	if len(r.Events) > 0 {
		return r.Events[0], true
	}
	return Event{}, false
}

// WithActual mengembalikan salinan Release dengan kolom Actual tiap event diisi
// dari map[judul]nilai (judul yang tak ada di map dibiarkan). Dipakai untuk
// PREVIEW/simulasi pesan pasca-rilis (cmd -simulate) & test — TIDAK menyentuh
// data feed asli.
func (r Release) WithActual(actuals map[string]string) Release {
	out := r
	out.Events = make([]Event, len(r.Events))
	copy(out.Events, r.Events)
	for i := range out.Events {
		if a, ok := actuals[out.Events[i].Title]; ok {
			out.Events[i].Actual = a
		}
	}
	return out
}

// GroupReleases mengelompokkan event (umumnya hasil FilterUSDHighImpact) per
// indikator + timestamp. nameOf memetakan judul → label indikator ("" = abaikan).
// Default pakai DefaultName.
func GroupReleases(events []Event, nameOf func(string) string) []Release {
	if nameOf == nil {
		nameOf = DefaultName
	}
	type key struct {
		name string
		t    time.Time
	}
	idx := map[key]int{}
	var out []Release
	for _, e := range events {
		name := nameOf(e.Title)
		if name == "" {
			continue
		}
		k := key{name, e.Time}
		if i, ok := idx[k]; ok {
			out[i].Events = append(out[i].Events, e)
			continue
		}
		idx[k] = len(out)
		out = append(out, Release{Name: name, Kind: KindOf(e.Title), Time: e.Time, Events: []Event{e}})
	}
	return out
}

// DefaultName memetakan judul event → label indikator yang kita pantau. "" =
// indikator lain (diabaikan oleh GroupReleases).
func DefaultName(title string) string {
	t := strings.ToUpper(title)
	switch {
	case strings.Contains(t, "PPI"):
		return "PPI"
	case strings.Contains(t, "CPI"):
		return "CPI"
	case strings.Contains(t, "NON-FARM"), strings.Contains(t, "NONFARM"),
		strings.Contains(t, "NFP"), strings.Contains(t, "PAYROLL"):
		return "NFP"
	}
	return ""
}
