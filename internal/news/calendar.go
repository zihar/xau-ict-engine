// Package news menarik kalender ekonomi (Forex Factory weekly JSON feed, gratis
// tanpa API key) lalu mengklasifikasi "surprise" tiap rilis high-impact dan
// memetakannya ke bias XAUUSD via playbook (CPI/PPI/NFP). Std-lib only
// (net/http + encoding/json) — sesuai konvensi repo. READ-ONLY: hanya menarik
// data kalender, tidak ada eksekusi order.
//
// Sumber: https://nfs.faireconomy.media/ff_calendar_thisweek.json — mirror
// publik kalender Forex Factory. Field per-event: title, country, date (RFC3339
// + offset), impact, forecast, previous, actual ("" sebelum rilis). Karena ini
// mirror tak-resmi, FetchCalendar mengembalikan error jelas bila feed berubah
// bentuk/down — pemanggil wajib menangani (jangan diam-diam dianggap "tak ada rilis").
package news

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// CalendarURL = mirror publik kalender mingguan Forex Factory (JSON, tanpa key).
const CalendarURL = "https://nfs.faireconomy.media/ff_calendar_thisweek.json"

// Event = satu baris kalender ekonomi. Forecast/Previous/Actual disimpan MENTAH
// (string seperti di feed: "0.3%", "85K", "" bila kosong) — parsing angka
// dilakukan saat klasifikasi supaya bentuk asli tetap bisa ditampilkan apa adanya.
type Event struct {
	Title    string
	Country  string
	Impact   string
	Time     time.Time
	Forecast string
	Previous string
	Actual   string // "" = belum rilis
}

// Released = true bila feed sudah mengisi kolom Actual (angka rilis keluar).
func (e Event) Released() bool { return strings.TrimSpace(e.Actual) != "" }

// rawEvent = bentuk JSON mentah dari feed (field yang dipakai saja).
type rawEvent struct {
	Title    string `json:"title"`
	Country  string `json:"country"`
	Date     string `json:"date"`
	Impact   string `json:"impact"`
	Forecast string `json:"forecast"`
	Previous string `json:"previous"`
	Actual   string `json:"actual"`
}

// FetchCalendarRaw menarik body mentah feed mingguan. client opsional (nil =
// default timeout 20s). User-Agent di-set non-default karena sebagian CDN
// menolak UA Go bawaan. Non-2xx (mis. 429 rate-limit) → error jelas.
func FetchCalendarRaw(client *http.Client) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, CalendarURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xau-ict-engine-newsalert/1.0 (+https://github.com/)")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kalender feed HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	return body, nil
}

// FetchCalendar menarik & mem-parse feed mingguan (tanpa cache).
func FetchCalendar(client *http.Client) ([]Event, error) {
	body, err := FetchCalendarRaw(client)
	if err != nil {
		return nil, err
	}
	return ParseCalendar(body)
}

// FetchCalendarCached menarik feed live; bila SUKSES menyimpan body mentah ke
// cachePath lalu mem-parse. Bila GAGAL (mis. 429 / jaringan), fallback ke cache
// terakhir di cachePath. fromCache=true menandai hasil berasal dari cache (basi)
// — pemanggil sebaiknya mencatat peringatan. cachePath "" = tanpa cache (sama
// dengan FetchCalendar). Error hanya bila live gagal DAN cache tak tersedia.
func FetchCalendarCached(client *http.Client, cachePath string) (events []Event, fromCache bool, err error) {
	body, ferr := FetchCalendarRaw(client)
	if ferr == nil {
		if cachePath != "" {
			if werr := os.WriteFile(cachePath, body, 0o644); werr != nil {
				// Gagal nulis cache bukan fatal — data live tetap dipakai.
				_ = werr
			}
		}
		ev, perr := ParseCalendar(body)
		return ev, false, perr
	}
	if cachePath == "" {
		return nil, false, ferr
	}
	cached, rerr := os.ReadFile(cachePath)
	if rerr != nil {
		return nil, false, fmt.Errorf("live gagal (%v) & cache tak terbaca (%v)", ferr, rerr)
	}
	ev, perr := ParseCalendar(cached)
	if perr != nil {
		return nil, false, fmt.Errorf("live gagal (%v) & cache rusak (%v)", ferr, perr)
	}
	return ev, true, nil
}

// ParseCalendar mem-parse body JSON feed → []Event (dipisah dari FetchCalendar
// supaya bisa diuji tanpa jaringan). Tanggal yang gagal di-parse → event di-skip
// (dengan asumsi feed sehat; bila SEMUA gagal, error supaya tak diam-diam kosong).
func ParseCalendar(body []byte) ([]Event, error) {
	var raw []rawEvent
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse kalender JSON: %w (snippet: %s)", err, snippet(body))
	}
	out := make([]Event, 0, len(raw))
	badDates := 0
	for _, r := range raw {
		t, err := time.Parse(time.RFC3339, r.Date)
		if err != nil {
			badDates++
			continue
		}
		out = append(out, Event{
			Title:    strings.TrimSpace(r.Title),
			Country:  strings.TrimSpace(r.Country),
			Impact:   strings.TrimSpace(r.Impact),
			Time:     t.UTC(),
			Forecast: strings.TrimSpace(r.Forecast),
			Previous: strings.TrimSpace(r.Previous),
			Actual:   strings.TrimSpace(r.Actual),
		})
	}
	if len(raw) > 0 && len(out) == 0 {
		return nil, fmt.Errorf("kalender: %d event tapi semua tanggalnya gagal di-parse (format feed berubah?)", len(raw))
	}
	return out, nil
}

// FilterUSDHighImpact menyaring event USD ber-impact High (case-insensitive) —
// rilis yang relevan ke XAUUSD via jalur Fed/USD/yield.
func FilterUSDHighImpact(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if strings.EqualFold(e.Country, "USD") && strings.EqualFold(e.Impact, "High") {
			out = append(out, e)
		}
	}
	return out
}

// snippet memotong body untuk pesan error (hindari dump ribuan baris).
func snippet(b []byte) string {
	const n = 180
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
