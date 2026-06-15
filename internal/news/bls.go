package news

// Fallback BLS — mengisi angka `actual` dari API resmi U.S. Bureau of Labor
// Statistics (penerbit asli CPI/PPI/NFP) saat mirror Forex Factory telat publish.
// BLS menyajikan angka SEKETIKA di jam rilis (08:30 ET), otoritatif, JSON, gratis
// (tanpa key = 25 query/hari; key terdaftar gratis = 500/hari via BLS_API_KEY).
// Std-lib only (net/http + encoding/json) sesuai konvensi repo. READ-ONLY.
//
// BLS menyajikan INDEKS (mis. CPI 333.979), bukan persen rilis → m/m & y/y
// dihitung di sini dari indeks (presisi 3 desimal). Hasilnya bisa ±0.1 dari angka
// resmi yang dibulatkan BLS → caller menandai sumber via BLSNote.
//
// Series ID terverifikasi langsung ke api.bls.gov (lihat bls_test.go):
//   CPI all-items  SA m/m  = CUSR0000SA0      NSA y/y = CUUR0000SA0
//   Core CPI       SA m/m  = CUSR0000SA0L1E   NSA y/y = CUUR0000SA0L1E
//   PPI final-dem  SA m/m  = WPSFD4           Core    = WPSFD49116
//   NFP (total nonfarm, ribuan) = CES0000000001 (rilis = selisih bulanan)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const blsAPIURL = "https://api.bls.gov/publicAPI/v2/timeseries/data/"

// BLSNote = catatan sumber yang ditempel caller ke pesan pasca-rilis bila ada
// angka yang diisi dari BLS (transparansi: angka turunan indeks, bukan rilis resmi).
const BLSNote = "\n\n_ℹ️ Sebagian angka dihitung dari indeks BLS (penerbit asli) — bisa ±0.1 dari rilis resmi yang dibulatkan._"

// blsTransform = cara menurunkan angka rilis dari deret indeks BLS.
type blsTransform int

const (
	blsMoM    blsTransform = iota // % perubahan bulan-ke-bulan (butuh bulan ref & ref-1)
	blsYoY                        // % perubahan tahun-ke-tahun (butuh bulan ref & ref tahun lalu)
	blsDeltaK                     // selisih level dalam ribuan (NFP: ref − ref-1), format "172K"
)

// blsSeries = pemetaan satu event → deret BLS + transform.
type blsSeries struct {
	id        string
	transform blsTransform
}

// blsSeriesFor memetakan judul event (faireconomy) → deret BLS. Substring-based
// (idiom KindOf/DefaultName) supaya tahan variasi judul. ok=false = tak ada padanan.
func blsSeriesFor(title string) (blsSeries, bool) {
	t := strings.ToLower(title)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if !strings.Contains(t, s) {
				return false
			}
		}
		return true
	}
	core := strings.Contains(t, "core")
	switch {
	// NFP: "Non-Farm Employment Change" → total nonfarm (ribuan), selisih bulanan.
	case has("non-farm"), has("nonfarm"), has("payroll"), has("nfp"):
		return blsSeries{"CES0000000001", blsDeltaK}, true
	case has("cpi", "m/m") && core:
		return blsSeries{"CUSR0000SA0L1E", blsMoM}, true
	case has("cpi", "y/y") && core:
		return blsSeries{"CUUR0000SA0L1E", blsYoY}, true
	case has("cpi", "m/m"):
		return blsSeries{"CUSR0000SA0", blsMoM}, true
	case has("cpi", "y/y"):
		return blsSeries{"CUUR0000SA0", blsYoY}, true
	case has("ppi", "m/m") && core:
		return blsSeries{"WPSFD49116", blsMoM}, true
	case has("ppi", "m/m"):
		return blsSeries{"WPSFD4", blsMoM}, true
	}
	return blsSeries{}, false
}

// blsPoint = satu titik bulanan deret BLS.
type blsPoint struct {
	Year  int
	Month int // 1..12 dari period "M01".."M12"
	Value float64
}

// blsRawResp = bentuk JSON respons BLS v2 (field yang dipakai saja).
type blsRawResp struct {
	Status  string   `json:"status"`
	Message []string `json:"message"`
	Results struct {
		Series []struct {
			SeriesID string `json:"seriesID"`
			Data     []struct {
				Year   string `json:"year"`
				Period string `json:"period"`
				Value  string `json:"value"`
			} `json:"data"`
		} `json:"series"`
	} `json:"Results"`
}

// fetchBLS menarik beberapa deret sekaligus (1 query) → map[id][]blsPoint. key
// opsional (kosong = 25 query/hari; terdaftar = 500/hari). startYear..endYear
// harus mencakup ≥13 bulan untuk y/y (caller pakai refYear-1..refYear).
func fetchBLS(client *http.Client, key string, ids []string, startYear, endYear int) (map[string][]blsPoint, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	reqBody := map[string]interface{}{
		"seriesid":  ids,
		"startyear": strconv.Itoa(startYear),
		"endyear":   strconv.Itoa(endYear),
	}
	if key != "" {
		reqBody["registrationkey"] = key
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, blsAPIURL, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("BLS API HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	return parseBLS(body)
}

// parseBLS mem-parse body respons BLS → map[id][]blsPoint (dipisah agar bisa diuji
// tanpa jaringan). Titik non-bulanan (M13 = rata-rata tahunan) di-skip.
func parseBLS(body []byte) (map[string][]blsPoint, error) {
	var raw blsRawResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse BLS JSON: %w (snippet %s)", err, snippet(body))
	}
	if raw.Status != "REQUEST_SUCCEEDED" {
		return nil, fmt.Errorf("BLS status %q: %s", raw.Status, strings.Join(raw.Message, "; "))
	}
	out := map[string][]blsPoint{}
	for _, s := range raw.Results.Series {
		var pts []blsPoint
		for _, d := range s.Data {
			if !strings.HasPrefix(d.Period, "M") {
				continue
			}
			m, err := strconv.Atoi(strings.TrimPrefix(d.Period, "M"))
			if err != nil || m < 1 || m > 12 {
				continue // lewati M13 dst
			}
			y, err := strconv.Atoi(d.Year)
			if err != nil {
				continue
			}
			v, ok := ParseNumber(d.Value)
			if !ok {
				continue
			}
			pts = append(pts, blsPoint{Year: y, Month: m, Value: v})
		}
		out[s.SeriesID] = pts
	}
	return out, nil
}

// EnrichFromBLS mengisi kolom Actual yang masih KOSONG pada Release dari API BLS.
// Hanya mengisi bila BLS sudah punya data untuk BULAN REFERENSI rilis (= bulan
// sebelum bulan rilis; CPI/PPI/NFP selalu melapor bulan lalu). Guard ini mencegah
// pengisian prematur/salah-bulan: sebelum BLS publish, deret terbarunya = bulan
// lebih lama → tak cocok → tak diisi (caller tetap menunggu). Event yang sudah
// punya Actual (dari faireconomy) tidak ditimpa. filled = jumlah event terisi;
// error jaringan/parse dikembalikan (caller boleh lanjut dengan release asli).
func EnrichFromBLS(r Release, client *http.Client, key string, now time.Time) (Release, int, error) {
	refYear, refMonth := prevMonth(r.Time.UTC().Year(), int(r.Time.UTC().Month()))

	need := map[int]blsSeries{} // index event → deret
	idset := map[string]bool{}
	var ids []string
	for i, e := range r.Events {
		if e.Released() {
			continue
		}
		s, ok := blsSeriesFor(e.Title)
		if !ok {
			continue
		}
		need[i] = s
		if !idset[s.id] {
			idset[s.id] = true
			ids = append(ids, s.id)
		}
	}
	if len(ids) == 0 {
		return r, 0, nil
	}

	data, err := fetchBLS(client, key, ids, refYear-1, refYear)
	if err != nil {
		return r, 0, err
	}

	out := r
	out.Events = make([]Event, len(r.Events))
	copy(out.Events, r.Events)
	filled := 0
	for i, s := range need {
		v, ok := blsActual(data[s.id], s.transform, refYear, refMonth)
		if !ok {
			continue // BLS belum publish bulan referensi → biarkan kosong (tunggu)
		}
		out.Events[i].Actual = v
		filled++
	}
	return out, filled, nil
}

// blsActual menghitung nilai rilis terformat dari deret + transform untuk bulan
// referensi. ok=false bila titik yang dibutuhkan tak ada (BLS belum publish) →
// caller tak mengisi.
func blsActual(pts []blsPoint, tf blsTransform, refYear, refMonth int) (string, bool) {
	cur, ok := findBLSValue(pts, refYear, refMonth)
	if !ok {
		return "", false
	}
	switch tf {
	case blsMoM:
		py, pm := prevMonth(refYear, refMonth)
		prev, ok := findBLSValue(pts, py, pm)
		if !ok || prev == 0 {
			return "", false
		}
		return pctStr((cur - prev) / prev * 100), true
	case blsYoY:
		prev, ok := findBLSValue(pts, refYear-1, refMonth)
		if !ok || prev == 0 {
			return "", false
		}
		return pctStr((cur - prev) / prev * 100), true
	case blsDeltaK:
		py, pm := prevMonth(refYear, refMonth)
		prev, ok := findBLSValue(pts, py, pm)
		if !ok {
			return "", false
		}
		return fmt.Sprintf("%.0fK", math.Round(cur-prev)), true
	}
	return "", false
}

// findBLSValue mencari nilai pada (year, month) di deret. ok=false bila tak ada.
func findBLSValue(pts []blsPoint, year, month int) (float64, bool) {
	for _, p := range pts {
		if p.Year == year && p.Month == month {
			return p.Value, true
		}
	}
	return 0, false
}

// prevMonth mengembalikan bulan sebelum (year, month), menangani batas Januari.
func prevMonth(year, month int) (int, int) {
	if month == 1 {
		return year - 1, 12
	}
	return year, month - 1
}

// pctStr membulatkan persen ke 1 desimal + sufiks "%" (mis. 0.4729 → "0.5%").
func pctStr(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', 1, 64) + "%"
}
