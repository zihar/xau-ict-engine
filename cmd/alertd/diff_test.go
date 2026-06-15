package main

import (
	"reflect"
	"testing"

	"forex-backtest/internal/detectors"
	"forex-backtest/internal/engine"
)

// poi = helper bikin entri NextPOITF ringkas untuk test diff.
func poi(tf detectors.TFKind, dir detectors.Direction, bot, top float64, tier, conf int) engine.NextPOITF {
	return engine.NextPOITF{TF: tf, Dir: dir, FullBottom: bot, FullTop: top, Tier: tier, Confluence: conf}
}

func TestWatchlistDiff(t *testing.T) {
	h1Sell := poi(detectors.TFH1, detectors.Bearish, 4606.58, 4607.68, 2, 2)
	h4Sell := poi(detectors.TFH4, detectors.Bearish, 4588.88, 4607.51, 2, 1)
	scan := func(pois ...engine.NextPOITF) engine.ScanNarrative {
		return engine.ScanNarrative{NextPOIsByTF: pois}
	}
	withMO := func(n engine.ScanNarrative, mo, price float64) engine.ScanNarrative {
		n.HasMO, n.MOPrice, n.Price = true, mo, price
		return n
	}

	cases := []struct {
		name      string
		oldFP     string
		n         engine.ScanNarrative
		wantLines []string
		wantMarks map[string]bool
	}{
		{
			name:      "tidak ada perubahan",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2;H4|bearish|4588.88-4607.51|T2|1",
			n:         scan(h1Sell, h4Sell),
			wantLines: nil,
			wantMarks: map[string]bool{},
		},
		{
			name:      "zona baru",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2",
			n:         scan(h1Sell, h4Sell),
			wantLines: []string{"Zona BARU: H4 SELL 4588.88→4607.51 (Tier 2, 1 komponen)"},
			wantMarks: map[string]bool{"H4 SELL": true},
		},
		{
			name:      "zona hilang",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2;H4|bearish|4588.88-4607.51|T2|1",
			n:         scan(h1Sell),
			wantLines: []string{"Zona HILANG: H4 SELL 4588.88→4607.51"},
			wantMarks: map[string]bool{},
		},
		{
			name:  "confluence + tier + batas berubah",
			oldFP: "H1|bearish|4606.58-4607.68|T2|2;H4|bearish|4580.00-4607.51|T3|2",
			n:     scan(h1Sell, h4Sell),
			wantLines: []string{
				"H4 SELL: batas 4580.00→4607.51 jadi 4588.88→4607.51, Tier 3 jadi 2, confluence 2 jadi 1 komponen",
			},
			wantMarks: map[string]bool{"H4 SELL": true},
		},
		{
			name:  "kiriman pertama (oldFP kosong) → semua BARU",
			oldFP: "",
			n:     scan(h1Sell),
			wantLines: []string{
				"Zona BARU: H1 SELL 4606.58→4607.68 (Tier 2, 2 komponen)",
			},
			wantMarks: map[string]bool{"H1 SELL": true},
		},
		{
			name:  "arah dibedakan (BUY vs SELL di TF sama)",
			oldFP: "H1|bearish|4606.58-4607.68|T2|2",
			n: scan(
				h1Sell,
				poi(detectors.TFH1, detectors.Bullish, 4463.01, 4497.91, 2, 3),
			),
			wantLines: []string{"Zona BARU: H1 BUY 4463.01→4497.91 (Tier 2, 3 komponen)"},
			wantMarks: map[string]bool{"H1 BUY": true},
		},
		{
			name:      "MO crossing: below → above (zona sama, tanpa angka)",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2;MO|below",
			n:         withMO(scan(h1Sell), 4499.43, 4529.95),
			wantLines: []string{"MO: harga MENYEBERANG ke ATAS MO — masuk premium"},
			wantMarks: map[string]bool{},
		},
		{
			name:      "MO crossing: above → below",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2;MO|above",
			n:         withMO(scan(h1Sell), 4499.43, 4480.00),
			wantLines: []string{"MO: harga MENYEBERANG ke BAWAH MO — masuk discount"},
			wantMarks: map[string]bool{},
		},
		{
			name:      "MO terbentuk (fingerprint legacy tanpa token = none)",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2",
			n:         withMO(scan(h1Sell), 4499.43, 4480.00),
			wantLines: []string{"MO trading day baru terbentuk — harga mulai DI BAWAH MO (discount)"},
			wantMarks: map[string]bool{},
		},
		{
			name:      "MO hilang (trading day baru, Asia)",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2;MO|above",
			n:         scan(h1Sell), // HasMO=false
			wantLines: []string{"Trading day baru (Asia) — MO hari ini belum terbentuk"},
			wantMarks: map[string]bool{},
		},
		{
			name:  "Asia close A → baris diff (transisi dari legacy '-')",
			oldFP: "H1|bearish|4606.58-4607.68|T2|2;MO|above",
			n: func() engine.ScanNarrative {
				s := withMO(scan(h1Sell), 4499.43, 4529.95)
				s.AsiaClosed = true // AsiaScenario zero = AMDX → token A
				return s
			}(),
			wantLines: []string{"Asia close: AKUMULASI (A) — ekspektasi AMDX hari ini"},
			wantMarks: map[string]bool{},
		},
		{
			name:  "REH H1 tersapu (hilang + harga melewati level)",
			oldFP: "H1|bearish|4606.58-4607.68|T2|2;MO|above;REH|H1|4533.10|2",
			n: func() engine.ScanNarrative {
				s := withMO(scan(h1Sell), 4499.43, 4540.00) // harga 4540 > 4533.10
				return s
			}(),
			wantLines: []string{"REH H1 4533.10 (2×) TERSAPU — liquidity diambil"},
			wantMarks: map[string]bool{},
		},
		{
			name:      "REH legacy 3-field dianggap H1 (kompat) — tersapu",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2;MO|above;REH|4533.10|2",
			n:         withMO(scan(h1Sell), 4499.43, 4540.00),
			wantLines: []string{"REH H1 4533.10 (2×) TERSAPU — liquidity diambil"},
			wantMarks: map[string]bool{},
		},
		{
			name:      "REH hilang keluar window (harga TIDAK melewati) → diam",
			oldFP:     "H1|bearish|4606.58-4607.68|T2|2;MO|above;REH|H1|4533.10|2",
			n:         withMO(scan(h1Sell), 4499.43, 4520.00), // 4520 < 4533.10
			wantLines: nil,
			wantMarks: map[string]bool{},
		},
		{
			name:  "REL H4 baru terbentuk + REH H1 berubah count",
			oldFP: "H1|bearish|4606.58-4607.68|T2|2;MO|above;REH|H1|4533.10|2",
			n: func() engine.ScanNarrative {
				s := withMO(scan(h1Sell), 4499.43, 4520.00)
				s.RelEquals = []engine.RelEqualMark{
					{Kind: "REH", TF: detectors.TFH1, Level: 4533.10, Count: 3},
					{Kind: "REL", TF: detectors.TFH4, Level: 4463.01, Count: 2},
				}
				return s
			}(),
			wantLines: []string{
				"REH H1: 4533.10 (2×) → 4533.10 (3×)",
				"REL H4 baru terbentuk di 4463.01 (2× low)",
			},
			wantMarks: map[string]bool{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, marks := watchlistDiff(tc.oldFP, tc.n)
			if !reflect.DeepEqual(lines, tc.wantLines) {
				t.Errorf("lines = %q, want %q", lines, tc.wantLines)
			}
			if !reflect.DeepEqual(marks, tc.wantMarks) {
				t.Errorf("marks = %v, want %v", marks, tc.wantMarks)
			}
		})
	}
}

// TestWatchlistTrigger memastikan hanya PERUBAHAN BERMAKNA yang mengirim pesan
// (anti-spam, user 2026-06-02): zona/REH-REL berubah & MO crossing = kirim;
// transisi MO harian none↔side = diam.
func TestWatchlistTrigger(t *testing.T) {
	const zona = "H1|bearish|4606.58-4607.68|T2|2"
	cases := []struct {
		name  string
		oldFP string
		newFP string
		want  bool
	}{
		{"identik", zona + ";MO|above", zona + ";MO|above", false},
		{"zona berubah", zona + ";MO|above", "H1|bearish|4600.00-4607.68|T2|2;MO|above", true},
		{"zona hilang", zona + ";MO|above", "MO|above", true},
		{"REH muncul", zona + ";MO|above", zona + ";MO|above;REH|H1|4533.10|2", true},
		{"REH berubah count", zona + ";MO|above;REH|H1|4533.10|2", zona + ";MO|above;REH|H1|4533.10|3", true},
		{"MO crossing above→below", zona + ";MO|above", zona + ";MO|below", true},
		{"MO harian: none→above (terbentuk) SAJA", zona + ";MO|none", zona + ";MO|above", false},
		{"MO harian: above→none (Asia) SAJA", zona + ";MO|above", zona + ";MO|none", false},
		{"MO harian + zona berubah → tetap kirim", zona + ";MO|none", "H1|bearish|4600.00-4607.68|T2|2;MO|above", true},
		{"Asia close: -→A memicu (00:00 NY)", zona + ";MO|none;ASIA|-", zona + ";MO|above;ASIA|A", true},
		{"Asia close: -→X memicu", zona + ";MO|none;ASIA|-", zona + ";MO|above;ASIA|X", true},
		{"Asia reset harian: A→- silent", zona + ";MO|above;ASIA|A", zona + ";MO|none;ASIA|-", false},
		{"Asia legacy tanpa token → '-' (A→- kompat silent)", zona + ";MO|above;ASIA|A", zona + ";MO|none", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchlistTrigger(tc.oldFP, tc.newFP); got != tc.want {
				t.Errorf("watchlistTrigger = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWatchlistDiffRoundTrip memastikan fingerprint hasil watchlistFingerprint
// (termasuk token MO) bisa di-parse balik dan diff-nya kosong (konsistensi
// format tulis ↔ baca).
func TestWatchlistDiffRoundTrip(t *testing.T) {
	n := engine.ScanNarrative{
		NextPOIsByTF: []engine.NextPOITF{
			poi(detectors.TFH1, detectors.Bearish, 4606.58, 4607.68, 2, 2),
			poi(detectors.TFH4, detectors.Bullish, 4439.19, 4494.77, 2, 3),
			poi(detectors.TFD, detectors.Bearish, 4584.38, 4644.31, 2, 1),
		},
		HasMO: true, MOPrice: 4499.43, Price: 4529.95,
		RelEquals: []engine.RelEqualMark{
			{Kind: "REH", TF: detectors.TFH1, Level: 4533.10, Count: 2},
			{Kind: "REL", TF: detectors.TFH4, Level: 4463.01, Count: 3},
			{Kind: "REL", TF: detectors.TFD, Level: 4417.43, Count: 2},
		},
	}
	fp := watchlistFingerprint(n)
	lines, marks := watchlistDiff(fp, n)
	if len(lines) != 0 || len(marks) != 0 {
		t.Errorf("round-trip harus tanpa diff; lines=%q marks=%v (fp=%q)", lines, marks, fp)
	}
	// Fingerprint tanpa MO (Asia) pun harus round-trip bersih.
	n.HasMO = false
	fp = watchlistFingerprint(n)
	if lines, marks := watchlistDiff(fp, n); len(lines) != 0 || len(marks) != 0 {
		t.Errorf("round-trip Asia harus tanpa diff; lines=%q marks=%v (fp=%q)", lines, marks, fp)
	}
}
