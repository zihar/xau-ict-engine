package news

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// wib = zona tampilan WIB (UTC+7, tanpa DST). ET ditampilkan via offset asli event.
var wib = time.FixedZone("WIB", 7*60*60)

// Verdict = ringkasan klasifikasi satu Release pasca-rilis (dari event headline).
type Verdict struct {
	Surprise Surprise
	Bias     Bias
	Label    string // "PANAS (di atas forecast)" dst
	Tilt     Bias   // condong TIPIS dari tiebreaker core m/m (BiasNeutral = tak ada)
	Tiebreak string // alasan tiebreaker (kosong = tak ada)
}

// Classify mengevaluasi Release yang sudah rilis → Verdict berdasar event headline.
//
// Tiebreaker core: pada INFLASI, bila headline m/m PAS forecast (NETRAL), core m/m
// (gauge inti yang paling diperhatikan Fed) dipakai sebagai penentu condong TIPIS.
// Bias FORMAL tetap dari headline (playbook & emoji tak dibesar-besarkan) — tiebreaker
// hanya menambah arah tipis + alasan supaya bias NETRAL tak understate sinyal core.
func (r Release) Classify() Verdict {
	h, ok := r.Headline()
	if !ok {
		return Verdict{}
	}
	s := classifySurprise(h)
	bias, label := biasFor(r.Kind, s)
	v := Verdict{Surprise: s, Bias: bias, Label: label}

	if r.Kind == KindInflation && s == SurpriseInline {
		if c, ok := r.coreMoM(); ok {
			switch classifySurprise(c) {
			case SurpriseAbove:
				v.Tilt = BiasBearish
				v.Tiebreak = "core m/m di ATAS forecast (sedikit panas) — gauge inti Fed"
			case SurpriseBelow:
				v.Tilt = BiasBullish
				v.Tiebreak = "core m/m di BAWAH forecast (sedikit lunak) — gauge inti Fed"
			}
		}
	}
	return v
}

// coreMoM mencari event "core … m/m" (gauge inflasi inti). ok=false bila tak ada.
func (r Release) coreMoM() (Event, bool) {
	for _, e := range r.Events {
		t := strings.ToLower(e.Title)
		if strings.Contains(t, "core") && strings.Contains(t, "m/m") {
			return e, true
		}
	}
	return Event{}, false
}

// sortedEvents mengurutkan event Release untuk tampilan: headline m/m → headline
// y/y → core m/m → core y/y → sisanya (judul alfabetis).
func (r Release) sortedEvents() []Event {
	es := make([]Event, len(r.Events))
	copy(es, r.Events)
	rank := func(title string) int {
		t := strings.ToLower(title)
		core := strings.Contains(t, "core")
		yy := strings.Contains(t, "y/y")
		switch {
		case !core && !yy:
			return 0
		case !core && yy:
			return 1
		case core && !yy:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(es, func(i, j int) bool {
		ri, rj := rank(es[i].Title), rank(es[j].Title)
		if ri != rj {
			return ri < rj
		}
		return es[i].Title < es[j].Title
	})
	return es
}

// hariID / bulanID = nama hari & bulan Bahasa Indonesia (indeks Weekday/Month).
var hariID = []string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
var bulanID = []string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// dayLabel = "Rabu 10 Jun" (w sudah di-zona WIB oleh pemanggil).
func dayLabel(w time.Time) string {
	return fmt.Sprintf("%s %d %s", hariID[w.Weekday()], w.Day(), bulanID[w.Month()])
}

// dmShort = "10 Jun" (w sudah di-zona WIB).
func dmShort(w time.Time) string {
	return fmt.Sprintf("%d %s", w.Day(), bulanID[w.Month()])
}

// clockLine = "19:30 WIB (08:30 ET)" — jam saja, untuk pengelompokan per-hari.
func clockLine(t time.Time) string {
	etStr := ""
	if loc, err := time.LoadLocation("America/New_York"); err == nil {
		etStr = fmt.Sprintf(" (%s ET)", t.In(loc).Format("15:04"))
	}
	return fmt.Sprintf("%s WIB%s", t.In(wib).Format("15:04"), etStr)
}

// timeLine = "Rabu 10 Jun 19:30 WIB (08:30 ET)".
func timeLine(t time.Time) string {
	return dayLabel(t.In(wib)) + " " + clockLine(t)
}

// preEventLine = "• CPI y/y: fc 4.2% (prev 3.8%)" + panah arah vs previous.
func preEventLine(e Event) string {
	s := fmt.Sprintf("• %s: fc %s", e.Title, dash(e.Forecast))
	if e.Previous != "" {
		s += fmt.Sprintf(" (prev %s)", e.Previous)
		if arrow := vsArrow(e.Forecast, e.Previous); arrow != "" {
			s += " " + arrow
		}
	}
	return s
}

// postEventLine = "• CPI y/y: 4.5% vs 4.2% fc (prev 3.8%) → PANAS".
func postEventLine(e Event, kind Kind) string {
	s := fmt.Sprintf("• %s: *%s* vs %s fc", e.Title, dash(e.Actual), dash(e.Forecast))
	if e.Previous != "" {
		s += fmt.Sprintf(" (prev %s)", e.Previous)
	}
	_, label := biasFor(kind, classifySurprise(e))
	if label != "tak terklasifikasi" {
		s += " → " + tagWord(label)
	}
	return s
}

// tagWord memendekkan label ke kata kunci untuk baris ("PANAS"/"LEMAH"/"BEAT"/"MISS"/"SESUAI").
func tagWord(label string) string {
	for _, w := range []string{"PANAS", "LEMAH", "BEAT", "MISS", "SESUAI"} {
		if strings.HasPrefix(label, w) {
			return w
		}
	}
	return label
}

// vsArrow = "↑"/"↓"/"" membandingkan forecast vs previous (naik/turun ekspektasi).
func vsArrow(forecast, previous string) string {
	f, okF := ParseNumber(forecast)
	p, okP := ParseNumber(previous)
	if !okF || !okP || f == p {
		return ""
	}
	if f > p {
		return "↑ (ekspektasi naik)"
	}
	return "↓ (ekspektasi turun)"
}

// humanizeMins memformat menit jadi ringkas: "45 menit", "2 jam", "2 jam 10 menit",
// "1 hari 3 jam". Dipakai countdown header pra-rilis.
func humanizeMins(m int) string {
	if m < 60 {
		return fmt.Sprintf("±%d menit", m)
	}
	if m < 24*60 {
		h, mm := m/60, m%60
		if mm == 0 {
			return fmt.Sprintf("±%d jam", h)
		}
		return fmt.Sprintf("±%d jam %d menit", h, mm)
	}
	h := m / 60
	return fmt.Sprintf("±%d hari %d jam", h/24, h%24)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// biasEmoji untuk header pesan.
func biasEmoji(b Bias) string {
	switch b {
	case BiasBearish:
		return "📉"
	case BiasBullish:
		return "📈"
	default:
		return "➖"
	}
}

// BuildPreMessage = pesan ANCANG-ANCANG pra-rilis (Markdown Telegram). nowcast =
// catatan leading indicator (boleh ""); support = zona harga kunci (mis.
// "$4.350–4.500"); mins = berapa menit lagi (untuk header, <=0 disembunyikan).
func BuildPreMessage(r Release, nowcast, support string, mins int) string {
	var b strings.Builder
	when := ""
	if mins > 0 {
		when = " — " + humanizeMins(mins) + " lagi"
	}
	fmt.Fprintf(&b, "📅 *ANCANG-ANCANG: %s*%s\n%s\n\n", r.Name, when, timeLine(r.Time))

	b.WriteString("*Forecast vs Previous:*\n")
	for _, e := range r.sortedEvents() {
		b.WriteString(preEventLine(e) + "\n")
	}

	if strings.TrimSpace(nowcast) != "" {
		fmt.Fprintf(&b, "\n*Nowcast (leading):* %s\n", nowcast)
	} else {
		b.WriteString("\n_Nowcast leading belum tersambung otomatis (isi via -nowcast)._\n")
	}

	// Ekspektasi dari forecast vs previous pada ANCHOR narasi (utk inflasi =
	// headline y/y, angka yang dikutip pasar; selainnya = headline m/m).
	if a, ok := r.narrativeAnchor(); ok {
		switch vsArrow(a.Forecast, a.Previous) {
		case "↑ (ekspektasi naik)":
			fmt.Fprintf(&b, "\nKonsensus condong *%s* (%s naik vs previous). Bila *actual ≥ forecast* → tekanan *BEARISH gold*; *di bawah forecast* → ruang *relief bounce*.\n", hotWord(r.Kind), anchorLabel(a))
		case "↓ (ekspektasi turun)":
			fmt.Fprintf(&b, "\nKonsensus melihat %s *mereda* vs previous. Kejutan ke ATAS forecast yang paling menggerakkan gold (bearish).\n", anchorLabel(a))
		default:
			b.WriteString("\nFokus ke *arah surprise* (actual vs forecast), bukan angka mutlak.\n")
		}
	}
	if dn := r.divergenceNote(); dn != "" {
		b.WriteString(dn + "\n")
	}

	if support != "" {
		fmt.Fprintf(&b, "🎯 Level kunci: *%s* (support, low 2026).\n", support)
	}
	b.WriteString("⏱ Tunggu *15–30 menit* pasca-rilis (spike pertama = noise). Lihat *core m/m*.\n")
	b.WriteString("\n_Edukasi, bukan saran finansial._")
	return b.String()
}

// BuildPostMessage = pesan PASCA-RILIS dengan actual, surprise, bias gold, playbook.
func BuildPostMessage(r Release, support string) string {
	v := r.Classify()
	var b strings.Builder
	fmt.Fprintf(&b, "🚨 *RILIS: %s*\n%s\n\n", r.Name, timeLine(r.Time))

	for _, e := range r.sortedEvents() {
		b.WriteString(postEventLine(e, r.Kind) + "\n")
	}

	biasStr := v.Bias.String()
	if v.Tilt != BiasNeutral {
		biasStr += " — condong tipis " + v.Tilt.String()
	}
	fmt.Fprintf(&b, "\n%s *Bias: %s*\n", biasEmoji(v.Bias), biasStr)
	fmt.Fprintf(&b, "Surprise headline: *%s*\n", v.Label)
	if v.Tiebreak != "" {
		fmt.Fprintf(&b, "⚖️ Tiebreaker: %s\n", v.Tiebreak)
	}
	b.WriteString(playbookLine(r.Kind, v.Bias, support))
	b.WriteString("⏱ Spike pertama = noise — tunggu *15–30 menit* untuk konfirmasi arah.\n")
	b.WriteString("⚠️ _Pemetaan rezim 2026 (gold sensitif Fed/yield). Edukasi, bukan saran finansial._")
	return b.String()
}

// narrativeAnchor memilih event yang jadi jangkar NARASI ekspektasi pra-rilis.
// Untuk inflasi: headline y/y (angka yang dikutip pasar & paling mencerminkan
// tren inflasi) — bukan m/m yang rentan base-effect. Selainnya: headline.
func (r Release) narrativeAnchor() (Event, bool) {
	if r.Kind == KindInflation {
		for _, e := range r.Events {
			t := strings.ToLower(e.Title)
			if strings.Contains(t, "y/y") && !strings.Contains(t, "core") {
				return e, true
			}
		}
	}
	return r.Headline()
}

// anchorLabel = sebutan ringkas anchor untuk narasi ("y/y"/"m/m"/judul).
func anchorLabel(e Event) string {
	t := strings.ToLower(e.Title)
	switch {
	case strings.Contains(t, "y/y"):
		return "tahunan (y/y)"
	case strings.Contains(t, "m/m"):
		return "bulanan (m/m)"
	default:
		return e.Title
	}
}

// divergenceNote memberi catatan bila arah m/m dan y/y headline BERBEDA — kasus
// base-effect (mis. m/m turun karena bulan pembanding tinggi, tapi y/y tetap
// naik). Penting supaya "m/m turun" tak salah dibaca sebagai inflasi melunak.
func (r Release) divergenceNote() string {
	if r.Kind != KindInflation {
		return ""
	}
	var mm, yy Event
	var okMM, okYY bool
	for _, e := range r.Events {
		t := strings.ToLower(e.Title)
		if strings.Contains(t, "core") {
			continue
		}
		if strings.Contains(t, "m/m") {
			mm, okMM = e, true
		} else if strings.Contains(t, "y/y") {
			yy, okYY = e, true
		}
	}
	if !okMM || !okYY {
		return ""
	}
	mmA, yyA := vsArrow(mm.Forecast, mm.Previous), vsArrow(yy.Forecast, yy.Previous)
	if mmA == "" || yyA == "" || mmA == yyA {
		return ""
	}
	if strings.HasPrefix(yyA, "↑") {
		return "⚠️ Catatan: m/m forecast *turun* tapi y/y *naik* — kemungkinan base-effect; sinyal inflasi tahunan justru *menguat* (tetap condong bearish gold)."
	}
	return "⚠️ Catatan: m/m forecast *naik* tapi y/y *turun* — base-effect; baca core m/m sebagai penengah."
}

// hotWord = istilah "panas/beat" sesuai jenis indikator (untuk narasi).
func hotWord(kind Kind) string {
	if kind == KindJobs {
		return "beat (data kuat)"
	}
	return "panas (inflasi naik)"
}

// playbookLine = satu-dua baris playbook sesuai bias.
func playbookLine(kind Kind, bias Bias, support string) string {
	lvl := "$4.350–4.500"
	if support != "" {
		lvl = support
	}
	switch bias {
	case BiasBearish:
		if kind == KindJobs {
			return fmt.Sprintf("Playbook: data kuat → ekspektasi Fed hawkish → USD/yield naik → *gold tertekan*. Uji break tegas < %s.\n", lvl)
		}
		return fmt.Sprintf("Playbook: inflasi panas → USD/yield naik → *gold tertekan*. Uji break tegas < %s.\n", lvl)
	case BiasBullish:
		return fmt.Sprintf("Playbook: data lemah → ruang rate-cut → *relief bounce* dari support %s. Tren besar tetap rapuh.\n", lvl)
	default:
		return fmt.Sprintf("Playbook: surprise kecil → dampak terbatas; gold ikut teknikal di sekitar %s. Cek *core m/m* sebagai tiebreaker.\n", lvl)
	}
}
