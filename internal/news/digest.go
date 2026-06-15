package news

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// digest.go = reminder MINGGUAN (Senin pagi): daftar rilis high-impact yang
// relevan ke XAUUSD sepanjang minggu ini (CPI/PPI/NFP + FOMC), dikelompokkan
// per-hari. Heads-up "minggu ini ada apa" — pelengkap alert pra/pasca per-rilis
// (BuildPreMessage/BuildPostMessage), bukan pengganti. READ-ONLY: hanya kalender.

// DigestName memetakan judul event → label indikator yang masuk reminder
// mingguan: superset DefaultName (CPI/PPI/NFP) + FOMC (statement / suku bunga).
// FOMC tak punya forecast yang kita klasifikasi ke bias → ditampilkan sbg event
// "tonton" (dampak dua arah), bukan diberi arah surprise. "" = abaikan.
func DigestName(title string) string {
	if n := DefaultName(title); n != "" {
		return n
	}
	t := strings.ToUpper(title)
	if strings.Contains(t, "FOMC") || strings.Contains(t, "FEDERAL FUNDS RATE") {
		return "FOMC"
	}
	return ""
}

// WeekDigestReleases menyaring & mengelompokkan event high-impact USD menjadi
// rilis yang masuk reminder mingguan (CPI/PPI/NFP/FOMC), terurut menaik waktu.
// Diasumsikan events = feed "thisweek" (sudah sebatas minggu ini).
func WeekDigestReleases(events []Event) []Release {
	rel := GroupReleases(FilterUSDHighImpact(events), DigestName)
	sort.SliceStable(rel, func(i, j int) bool { return rel[i].Time.Before(rel[j].Time) })
	return rel
}

// kindEmoji = penanda visual jenis rilis di digest.
func kindEmoji(k Kind) string {
	switch k {
	case KindInflation:
		return "🔥"
	case KindJobs:
		return "💼"
	default:
		return "🏦" // FOMC / suku bunga
	}
}

// digestKindHint = catatan ringkas arah dampak (rezim 2026) per jenis.
func digestKindHint(k Kind) string {
	switch k {
	case KindInflation:
		return "panas (> forecast) → BEARISH gold"
	case KindJobs:
		return "kuat (> forecast) → BEARISH gold"
	default:
		return "penentu arah Fed — dampak besar dua arah"
	}
}

// BuildWeekDigest merangkai pesan reminder mingguan (Markdown Telegram) dari
// releases hasil WeekDigestReleases. now dipakai hanya untuk header bila kosong.
// Minggu tanpa rilis → pesan "minggu tenang" (tetap dikirim sbg konfirmasi).
func BuildWeekDigest(releases []Release, now time.Time) string {
	var b strings.Builder
	if len(releases) == 0 {
		b.WriteString("📅 *NEWS XAUUSD — minggu ini*\n")
		fmt.Fprintf(&b, "_pekan %s_\n\n", weekRangeNote(now))
		b.WriteString("Tak ada rilis high-impact (CPI/PPI/NFP/FOMC) terjadwal pekan ini. Pasar cenderung digerakkan teknikal.\n")
		b.WriteString("\n_Edukasi, bukan saran finansial._")
		return b.String()
	}

	first := releases[0].Time.In(wib)
	last := releases[len(releases)-1].Time.In(wib)
	b.WriteString("📅 *NEWS HIGH-IMPACT XAUUSD — minggu ini*\n")
	fmt.Fprintf(&b, "_%s – %s_\n", dmShort(first), dmShort(last))
	fmt.Fprintf(&b, "%d rilis terpantau (CPI/PPI/NFP/FOMC):\n", len(releases))

	curDay := ""
	for _, r := range releases {
		if d := dayLabel(r.Time.In(wib)); d != curDay {
			fmt.Fprintf(&b, "\n*%s*\n", d)
			curDay = d
		}
		fmt.Fprintf(&b, "🕒 %s — *%s* %s\n", clockLine(r.Time), r.Name, kindEmoji(r.Kind))
		if r.Kind == KindOther { // FOMC: tak ada forecast utk klasifikasi.
			fmt.Fprintf(&b, "   → %s\n", digestKindHint(r.Kind))
			continue
		}
		for _, e := range r.sortedEvents() {
			b.WriteString("   " + preEventLine(e) + "\n")
		}
		fmt.Fprintf(&b, "   → %s\n", digestKindHint(r.Kind))
	}

	b.WriteString("\n_⚠️ Forecast = kondisi Senin, bisa berubah jelang rilis — angka final di alert pra-rilis._")
	b.WriteString("\n_Detail surprise→bias dikirim otomatis tiap rilis (pra & pasca). Edukasi, bukan saran finansial._")
	return b.String()
}

// weekRangeNote = "Senin 8 Jun" (acuan awal pekan dari now, untuk pesan kosong).
func weekRangeNote(now time.Time) string {
	return dayLabel(now.In(wib))
}
