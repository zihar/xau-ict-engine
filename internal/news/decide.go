package news

import (
	"strings"
	"time"
)

// Decide menentukan aksi untuk satu rilis: "pre" (ancang-ancang), "post"
// (pasca-rilis), atau "none". mins = menit (bertanda) dari now ke waktu rilis.
//
// Logika dipromosikan dari cmd/newsalert supaya bisa di-reuse daemon alertd
// (prinsip 0-drift repo: SATU sumber keputusan). Verbatim dari versi lama:
//   - rilis sudah AnyReleased & belum post-sent & masih dalam stale → "post".
//   - belum rilis, dalam prewindow, belum pre-sent → "pre".
//   - selainnya → "none".
func Decide(r Release, now time.Time, st State, prewindow, stale time.Duration) (mode string, mins int) {
	key := r.Key()
	until := r.Time.Sub(now)
	mins = int(until.Round(time.Minute).Minutes())

	if r.AnyReleased() {
		if !st.PostSent(key) && now.Sub(r.Time) <= stale {
			return "post", mins
		}
		return "none", mins
	}
	// Belum rilis (actual masih kosong).
	if until > 0 && until <= prewindow && !st.PreSent(key) {
		return "pre", mins
	}
	return "none", mins
}

// PickTarget memilih rilis indikator `name` yang paling relevan terhadap now:
// rilis dengan |Time-now| terkecil yang masih dalam jendela layak (belum lewat
// lebih dari `stale`). ok=false bila tak ada. Dipromosikan dari cmd/newsalert.
func PickTarget(releases []Release, name string, now time.Time, stale time.Duration) (Release, bool) {
	var best Release
	found := false
	var bestDist time.Duration
	for _, r := range releases {
		if !strings.EqualFold(r.Name, name) {
			continue
		}
		// Buang rilis yang sudah jauh lewat (mis. minggu sebelumnya dalam feed).
		if now.Sub(r.Time) > stale {
			continue
		}
		dist := absDur(r.Time.Sub(now))
		if !found || dist < bestDist {
			best, bestDist, found = r, dist, true
		}
	}
	return best, found
}

// ParseNames memecah CSV indikator → daftar nama ter-normalisasi (uppercase,
// trim, buang kosong & duplikat). Dipromosikan dari cmd/newsalert.
func ParseNames(csv string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.Split(csv, ",") {
		n := strings.ToUpper(strings.TrimSpace(p))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// absDur = nilai mutlak durasi (helper PickTarget).
func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
