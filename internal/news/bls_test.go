package news

import (
	"testing"
	"time"
)

// fixtureBLS = potongan respons BLS v2 ASLI (api.bls.gov, 2026-06-10) untuk CPI
// all-items SA (m/m) & NSA (y/y) + core. Dipakai menguji parse + transform tanpa
// jaringan. Nilai indeks otentik → angka turunan bisa dicek manual.
const fixtureBLS = `{
  "status":"REQUEST_SUCCEEDED",
  "message":[],
  "Results":{"series":[
    {"seriesID":"CUSR0000SA0","data":[
      {"year":"2026","period":"M05","value":"333.979"},
      {"year":"2026","period":"M04","value":"332.407"},
      {"year":"2025","period":"M05","value":"321.500"},
      {"year":"2025","period":"M13","value":"320.000"}
    ]},
    {"seriesID":"CUUR0000SA0","data":[
      {"year":"2026","period":"M05","value":"335.123"},
      {"year":"2025","period":"M05","value":"321.700"}
    ]},
    {"seriesID":"CES0000000001","data":[
      {"year":"2026","period":"M05","value":"159001"},
      {"year":"2026","period":"M04","value":"158829"}
    ]}
  ]}
}`

func TestParseBLS(t *testing.T) {
	data, err := parseBLS([]byte(fixtureBLS))
	if err != nil {
		t.Fatalf("parseBLS: %v", err)
	}
	cpi := data["CUSR0000SA0"]
	if len(cpi) != 3 { // M13 (rata-rata tahunan) harus di-skip
		t.Fatalf("CUSR0000SA0: mau 3 titik bulanan (M13 di-skip), dapat %d", len(cpi))
	}
	if v, ok := findBLSValue(cpi, 2026, 5); !ok || v != 333.979 {
		t.Fatalf("M05 2026 = %v ok=%v, mau 333.979", v, ok)
	}
}

func TestParseBLSStatusError(t *testing.T) {
	_, err := parseBLS([]byte(`{"status":"REQUEST_NOT_PROCESSED","message":["limit"],"Results":{}}`))
	if err == nil {
		t.Fatal("status non-success harus error")
	}
}

func TestBLSActualTransforms(t *testing.T) {
	data, _ := parseBLS([]byte(fixtureBLS))
	// ref bulan = Mei 2026 (rilis 10 Jun → lapor bulan lalu).
	cases := []struct {
		name string
		id   string
		tf   blsTransform
		want string
	}{
		// (333.979-332.407)/332.407*100 = 0.4729 → 0.5%
		{"cpi m/m", "CUSR0000SA0", blsMoM, "0.5%"},
		// (335.123-321.700)/321.700*100 = 4.173 → 4.2%
		{"cpi y/y", "CUUR0000SA0", blsYoY, "4.2%"},
		// 159001-158829 = 172 → 172K
		{"nfp delta", "CES0000000001", blsDeltaK, "172K"},
	}
	for _, c := range cases {
		got, ok := blsActual(data[c.id], c.tf, 2026, 5)
		if !ok || got != c.want {
			t.Errorf("%s: dapat %q ok=%v, mau %q", c.name, got, ok, c.want)
		}
	}
}

func TestBLSActualMissingRefMonth(t *testing.T) {
	data, _ := parseBLS([]byte(fixtureBLS))
	// Minta Juni 2026 (M06) yang tak ada di fixture → harus ok=false (belum publish).
	if _, ok := blsActual(data["CUSR0000SA0"], blsMoM, 2026, 6); ok {
		t.Fatal("bulan ref belum publish harus ok=false (jangan isi prematur)")
	}
}

func TestBLSSeriesFor(t *testing.T) {
	cases := []struct {
		title string
		id    string
		tf    blsTransform
		ok    bool
	}{
		{"CPI m/m", "CUSR0000SA0", blsMoM, true},
		{"CPI y/y", "CUUR0000SA0", blsYoY, true},
		{"Core CPI m/m", "CUSR0000SA0L1E", blsMoM, true},
		{"Core CPI y/y", "CUUR0000SA0L1E", blsYoY, true},
		{"PPI m/m", "WPSFD4", blsMoM, true},
		{"Core PPI m/m", "WPSFD49116", blsMoM, true},
		{"Non-Farm Employment Change", "CES0000000001", blsDeltaK, true},
		{"Unemployment Rate", "", 0, false},
	}
	for _, c := range cases {
		s, ok := blsSeriesFor(c.title)
		if ok != c.ok || (ok && (s.id != c.id || s.transform != c.tf)) {
			t.Errorf("%q: dapat {%s,%d} ok=%v, mau {%s,%d} ok=%v", c.title, s.id, s.transform, ok, c.id, c.tf, c.ok)
		}
	}
}

func TestEnrichFromBLSGuardRefMonth(t *testing.T) {
	// Release di-set akhir bulan dengan kolom Actual kosong; tanpa jaringan kita
	// tak bisa fetch, tapi prevMonth & seleksi event harus benar.
	r := Release{
		Name: "CPI", Kind: KindInflation,
		Time: time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC),
		Events: []Event{
			{Title: "CPI m/m", Forecast: "0.5%"},
			{Title: "Unemployment Rate", Forecast: "4.0%"}, // tak ada padanan BLS → diabaikan
		},
	}
	// Ref bulan rilis Juni → Mei (5).
	if y, m := prevMonth(r.Time.Year(), int(r.Time.Month())); y != 2026 || m != 5 {
		t.Fatalf("prevMonth(2026,6) = %d,%d mau 2026,5", y, m)
	}
}
