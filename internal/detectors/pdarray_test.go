package detectors

import (
	"testing"

	"forex-backtest/internal/data"
)

func TestConfluenceAndSelection(t *testing.T) {
	// Dua PDR bullish beririsan (FVG + OB) di zona discount → 1 POI confluence 2.
	pdrs := []PDR{
		{Kind: KindFVG, Dir: Bullish, Top: 102, Bottom: 100, Index: 5},
		{Kind: KindOB, Dir: Bullish, Top: 101.5, Bottom: 99, Index: 4},
		{Kind: KindFVG, Dir: Bullish, Top: 130, Bottom: 128, Index: 9}, // terpisah (premium)
	}
	pois := BuildPOIs(pdrs, 2, 0)
	if len(pois) != 1 {
		t.Fatalf("mau 1 POI confluence, dapat %d (%+v)", len(pois), pois)
	}
	p := pois[0]
	if p.Confluence() != 2 {
		t.Errorf("confluence = %d, mau 2", p.Confluence())
	}
	// irisan = [max(100,99), min(102,101.5)] = [100, 101.5]
	if p.Bottom != 100 || p.Top != 101.5 {
		t.Errorf("irisan = [%g,%g], mau [100,101.5]", p.Bottom, p.Top)
	}

	// Fib impulse 90..140 → eq 115. POI mid ~100.75 = discount → valid untuk buy.
	fib := Fib{Low: 90, High: 140}
	sel, ok := SelectPOI(pois, 101, fib, Bullish)
	if !ok {
		t.Fatal("POI discount harusnya kepilih untuk buy")
	}
	if !sel.Contains(101) {
		t.Error("POI terpilih harus mengandung harga 101")
	}
}

func TestTrappedSellersFilter(t *testing.T) {
	// POI bullish tapi di premium → trapped, harus ditolak untuk buy.
	pdrs := []PDR{
		{Kind: KindFVG, Dir: Bullish, Top: 132, Bottom: 130, Index: 5},
		{Kind: KindOB, Dir: Bullish, Top: 131, Bottom: 129, Index: 4},
	}
	pois := BuildPOIs(pdrs, 2, 0)
	fib := Fib{Low: 90, High: 140} // eq 115; POI mid ~130.5 = premium
	if _, ok := SelectPOI(pois, 130.5, fib, Bullish); ok {
		t.Error("POI bullish di premium harus ditolak (trapped sellers)")
	}
}

func TestClusterTier_BBFVG(t *testing.T) {
	// BB + FVG beririsan → Tier 2.
	cs := []PDR{
		{Kind: KindBB, Dir: Bullish, Top: 102, Bottom: 100},
		{Kind: KindFVG, Dir: Bullish, Top: 101.5, Bottom: 99.5},
	}
	if got := clusterTier(cs); got != 2 {
		t.Errorf("tier BB+FVG = %d, mau 2", got)
	}
	// VI ada → Tier 1.
	cs2 := append(cs, PDR{Kind: KindVI, Dir: Bullish, Top: 101, Bottom: 100.5})
	if got := clusterTier(cs2); got != 1 {
		t.Errorf("tier dengan VI = %d, mau 1", got)
	}
}

func TestClusterTier_VINoAutoTier1(t *testing.T) {
	// Bug#5: VI sendirian dengan 1 OB (bukan BB/FVG, cluster <3) JANGAN auto-Tier1.
	cs := []PDR{
		{Kind: KindVI, Dir: Bullish, Top: 101, Bottom: 100.5},
		{Kind: KindOB, Dir: Bullish, Top: 101, Bottom: 99},
	}
	if got := clusterTier(cs); got != 2 {
		t.Errorf("VI+OB (konfluen lemah) tier = %d, mau 2 (bukan auto-Tier1)", got)
	}
	// VI + FVG (komponen struktural) → Tier 1 sah.
	cs2 := []PDR{
		{Kind: KindVI, Dir: Bullish, Top: 101, Bottom: 100.5},
		{Kind: KindFVG, Dir: Bullish, Top: 101.2, Bottom: 100},
	}
	if got := clusterTier(cs2); got != 1 {
		t.Errorf("VI+FVG tier = %d, mau 1", got)
	}
	// VI + OB + OB (>=3 komponen) → Tier 1 sah (konfluen bermakna via jumlah).
	cs3 := []PDR{
		{Kind: KindVI, Dir: Bullish, Top: 101, Bottom: 100.5},
		{Kind: KindOB, Dir: Bullish, Top: 101, Bottom: 99},
		{Kind: KindOB, Dir: Bullish, Top: 101.3, Bottom: 100.4},
	}
	if got := clusterTier(cs3); got != 1 {
		t.Errorf("VI+OB+OB (>=3) tier = %d, mau 1", got)
	}
}

func TestBuildPOIs_WidthCap(t *testing.T) {
	// 3 FVG bullish saling beririsan rantai transitif membentuk band lebar [100,108].
	// Tanpa cap: 1 cluster confluence 3. Dengan cap sempit: penggabungan dihentikan.
	pdrs := []PDR{
		{Kind: KindFVG, Dir: Bullish, Top: 104, Bottom: 100, Index: 1},
		{Kind: KindFVG, Dir: Bullish, Top: 106, Bottom: 103, Index: 2},
		{Kind: KindFVG, Dir: Bullish, Top: 108, Bottom: 105, Index: 3},
	}
	// Tanpa cap → 1 POI confluence 3, lebar irisan = [105,104]? cek: chain overlap.
	uncapped := BuildPOIs(pdrs, 2, 0)
	if len(uncapped) != 1 {
		t.Fatalf("tanpa cap mau 1 cluster, dapat %d", len(uncapped))
	}
	// Dengan cap 1.0: irisan awal [103,104] (lebar 1) OK, gabung FVG ke-3 → [105,104]
	// invalid overlap (105>104) jadi berhenti natural. Pakai cap untuk band yg lebih
	// longgar: cap 0.5 menolak penggabungan yg bikin irisan > 0.5.
	capped := BuildPOIs(pdrs, 2, 0.5)
	for _, p := range capped {
		if (p.Top - p.Bottom) > 0.5+1e-9 {
			t.Errorf("POI lebar %.2f melebihi cap 0.5", p.Top-p.Bottom)
		}
	}
}

func TestVI_RequiresSameDirection(t *testing.T) {
	// Bug#6: VI hanya emit kalau KEDUA candle searah. Gap di titik balik (bull→bear)
	// TIDAK boleh jadi VI.
	candles := []data.Candle{
		{Open: 100, High: 103, Low: 99, Close: 102},  // bullish
		{Open: 105, High: 106, Low: 101, Close: 101}, // bearish, open gap-up dari prev close 102
	}
	vis := DetectVolumeImbalances(candles, 5) // gap 3 (>= $0.50)
	if len(vis) != 0 {
		t.Errorf("gap di titik balik (bull→bear) tak boleh jadi VI, dapat %d", len(vis))
	}
	// Dua-duanya bullish dengan gap → VI sah.
	candles2 := []data.Candle{
		{Open: 100, High: 103, Low: 99, Close: 102},  // bullish
		{Open: 105, High: 108, Low: 104, Close: 107}, // bullish, gap-up dari 102
	}
	vis2 := DetectVolumeImbalances(candles2, 5)
	if len(vis2) != 1 || vis2[0].Dir != Bullish {
		t.Errorf("dua candle bullish gap → mau 1 VI bullish, dapat %+v", vis2)
	}
}

func TestNearestValidPOI(t *testing.T) {
	// Fib impulse 90..140 → eq 115. Untuk buy (Bullish) valid hanya POI di
	// discount (< 115). Tiga POI: dua discount (100..102 & 105..107), satu premium
	// (120..122). Harga 110 di atas keduanya → tunggu harga TURUN; yg dipilih =
	// discount TERDEKAT (105..107, tepi atas 107 → jarak 3) bukan 100..102 (jarak 8).
	fib := Fib{Low: 90, High: 140}
	pois := []POI{
		{Dir: Bullish, Top: 102, Bottom: 100, Tier: 2, Components: []PDR{{}, {}}},
		{Dir: Bullish, Top: 107, Bottom: 105, Tier: 2, Components: []PDR{{}, {}}},
		{Dir: Bullish, Top: 122, Bottom: 120, Tier: 1, Components: []PDR{{}, {}}}, // premium → invalid utk buy
		{Dir: Bearish, Top: 108, Bottom: 106, Tier: 1, Components: []PDR{{}, {}}}, // arah lawan → invalid
	}
	np, ok := NearestValidPOI(pois, 110, fib, Bullish)
	if !ok {
		t.Fatal("harus ada POI valid terdekat untuk buy")
	}
	if np.Bottom != 105 || np.Top != 107 {
		t.Errorf("POI terdekat = [%g,%g], mau [105,107]", np.Bottom, np.Top)
	}

	// Tak ada kandidat searah & zona benar → ok=false.
	if _, ok := NearestValidPOI(pois, 110, fib, Bearish); ok {
		// satu-satunya bearish (106..108) ada di premium (mid 107 < eq 115 = discount) →
		// untuk sell zona premium yg valid, jadi discount-mid ditolak → tak ada kandidat.
		t.Error("tak ada POI bearish di zona premium → harusnya ok=false")
	}

	// Belum-jebol: POI discount yg sudah ditembus ke bawah (price < Bottom) ditolak.
	low := []POI{{Dir: Bullish, Top: 102, Bottom: 100, Tier: 2, Components: []PDR{{}, {}}}}
	if _, ok := NearestValidPOI(low, 99, fib, Bullish); ok {
		t.Error("POI yg sudah dijebol ke bawah (price<Bottom) harus ditolak untuk buy")
	}
}

func TestFVGSwingBreak_Bullish(t *testing.T) {
	// Skenario: swing-high di idx 1 (high 105 > tetangga), lalu harga jeda, lalu
	// terbentuk FVG bullish + break swing-high ke ATAS. FVG adjacent (<=3) ke swing
	// yg di-break → promosi KindFVGBreak (Tier-2).
	//
	// idx: 0    1(SH) 2     3     4
	candles := []data.Candle{
		{Open: 100, High: 102, Low: 99, Close: 101},    // 0
		{Open: 101, High: 105, Low: 100, Close: 104},   // 1 swing-high (105 > 102 & > 103)
		{Open: 104, High: 103, Low: 102, Close: 102.5}, // 2 (FVG kiri-leg: high 103)
		{Open: 103, High: 110, Low: 108, Close: 109},   // 3 FVG-tengah; break 105 ke atas
		{Open: 109, High: 112, Low: 108.5, Close: 111}, // 4 FVG kanan-leg: low 108.5 > 103
	}
	// Bullish FVG di idx 3: right.Low(108.5) > left.High(103) → gap [103,108.5].
	pdrs := DetectPDRs(candles, 5, 0, 3, 5, false, false, false, false, false) // minGap $0.50; gap 5.5 lolos
	var fb int
	for _, p := range pdrs {
		if p.Kind == KindFVGBreak {
			fb++
			if p.Dir != Bullish {
				t.Errorf("FVGBreak dir = %v, mau Bullish", p.Dir)
			}
			if tierOf(p.Kind) != 2 {
				t.Errorf("FVGBreak tier = %d, mau 2", tierOf(p.Kind))
			}
		}
	}
	if fb == 0 {
		t.Fatalf("harusnya ada KindFVGBreak (Kunci#1), pdrs=%+v", pdrs)
	}
	// Cluster yg mengandung FVGBreak harus Tier-2 (atau lebih baik).
	pois := BuildPOIs(pdrs, 2, 0)
	bestT := 99
	for _, p := range pois {
		for _, c := range p.Components {
			if c.Kind == KindFVGBreak && p.Tier < bestT {
				bestT = p.Tier
			}
		}
	}
	if bestT > 2 {
		t.Errorf("cluster ber-FVGBreak tier = %d, mau <=2", bestT)
	}
}

func TestFVGSwingBreak_NoBreakNoPromo(t *testing.T) {
	// FVG bullish ADA tapi TIDAK ada swing-high yg di-break ke atas di dekatnya →
	// tak boleh ada KindFVGBreak (tier lain tak rusak).
	candles := []data.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100.5},
		{Open: 100.5, High: 101.5, Low: 100, Close: 101}, // FVG kiri-leg
		{Open: 101, High: 102, Low: 101, Close: 101.5},   // FVG tengah
		{Open: 101.5, High: 109, Low: 107, Close: 108},   // FVG kanan-leg: low 107 > 101.5
	}
	pdrs := DetectPDRs(candles, 5, 0, 3, 5, false, false, false, false, false)
	for _, p := range pdrs {
		if p.Kind == KindFVGBreak {
			t.Errorf("tak ada swing-high di-break → tak boleh KindFVGBreak, dapat %+v", p)
		}
	}
}

func TestBPR_Overlap(t *testing.T) {
	// BISI (FVG bullish) + SIBI (FVG bearish) dengan range beririsan, jarak <=5 candle
	// → BPR (dua arah, zona = irisan).
	// FVG bullish idx 1: right.Low(106) > left.High(101) → [101,106].
	// FVG bearish idx 4: left.Low > right.High → gap turun yg overlap [101,106].
	candles := []data.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100.5},    // 0 (kiri bullish)
		{Open: 100.5, High: 102, Low: 100, Close: 101.5}, // 1 bullish FVG-tengah
		{Open: 106, High: 108, Low: 106, Close: 107},     // 2 (kanan bullish: low 106 > 101)
		{Open: 107, High: 109, Low: 104, Close: 105},     // 3 (kiri bearish: low 104)
		{Open: 105, High: 105, Low: 103, Close: 103.5},   // 4 bearish FVG-tengah
		{Open: 103, High: 102.5, Low: 100, Close: 100.5}, // 5 (kanan bearish: high 102.5 < 104)
	}
	pdrs := DetectPDRs(candles, 5, 0, 3, 5, false, false, false, false, false)
	var bull, bear int
	for _, p := range pdrs {
		if p.Kind == KindBPR {
			if tierOf(p.Kind) != 3 {
				t.Errorf("BPR tier = %d, mau 3", tierOf(p.Kind))
			}
			if p.Dir == Bullish {
				bull++
			} else {
				bear++
			}
			if p.Top <= p.Bottom {
				t.Errorf("BPR zona invalid [%g,%g]", p.Bottom, p.Top)
			}
		}
	}
	if bull == 0 || bear == 0 {
		t.Errorf("BPR harus emit dua arah (bull=%d bear=%d), pdrs=%+v", bull, bear, pdrs)
	}
}

func TestBPR_Directional(t *testing.T) {
	// Fixture sama TestBPR_Overlap: FVG bullish idx 1 + FVG bearish idx 4.
	// Mode directional (arg ke-8 = true): BPR cuma SATU PDR, arah = FVG lebih baru
	// (idx 4 = bearish) → Bearish. Tak ada padding dua-arah (pertemuan 4 "menentukan arah").
	candles := []data.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100.5},
		{Open: 100.5, High: 102, Low: 100, Close: 101.5},
		{Open: 106, High: 108, Low: 106, Close: 107},
		{Open: 107, High: 109, Low: 104, Close: 105},
		{Open: 105, High: 105, Low: 103, Close: 103.5},
		{Open: 103, High: 102.5, Low: 100, Close: 100.5},
	}
	pdrs := DetectPDRs(candles, 5, 0, 3, 5, false, false, true, false, false)
	var bull, bear int
	for _, p := range pdrs {
		if p.Kind != KindBPR {
			continue
		}
		if p.Dir == Bullish {
			bull++
		} else {
			bear++
		}
	}
	// Tiap pasang BPR di fixture ini punya FVG lebih baru = bearish → semua Bearish,
	// TAK ADA Bullish (padding dua-arah hilang — itu inti perbaikan directional).
	if bull != 0 {
		t.Errorf("BPR directional harus 0 PDR Bullish (no padding), dapat bull=%d", bull)
	}
	if bear == 0 {
		t.Errorf("BPR directional harus ada PDR Bearish (FVG lebih baru bearish), dapat 0")
	}
}

func TestIFVG_Inversion(t *testing.T) {
	// FVG bullish [101,106] lalu di-close DI BAWAH 101 (gagal hold) → flip jadi IFVG
	// BEARISH (resistance) dgn range FVG lama.
	candles := []data.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100.5},    // 0 kiri
		{Open: 100.5, High: 102, Low: 100, Close: 101.5}, // 1 tengah
		{Open: 106, High: 108, Low: 106, Close: 107},     // 2 kanan: low 106 > 101 → FVG [101,106]
		{Open: 105, High: 105.5, Low: 100, Close: 100.5}, // 3
		{Open: 100, High: 100.5, Low: 98, Close: 99},     // 4 close 99 < 101 → jebol bawah
	}
	pdrs := DetectPDRs(candles, 5, 0, 3, 5, false, false, false, false, false)
	var found bool
	for _, p := range pdrs {
		if p.Kind == KindIFVG {
			found = true
			if p.Dir != Bearish {
				t.Errorf("IFVG dir = %v, mau Bearish (flip dari bullish gagal)", p.Dir)
			}
			if tierOf(p.Kind) != 4 {
				t.Errorf("IFVG tier = %d, mau 4", tierOf(p.Kind))
			}
		}
	}
	if !found {
		t.Fatalf("FVG bullish yg jebol ke bawah harus jadi IFVG bearish, pdrs=%+v", pdrs)
	}
}

func TestIFVG_FallbackRule(t *testing.T) {
	// Bullish FVG [101,106] gagal (close < 101) → kandidat IFVG bearish. TAPI di momen
	// flip SUDAH ada FVG bearish [100.5,106] yang live (searah dgn IFVG bearish).
	//   - Fallback OFF → IFVG tetap muncul (perilaku lama, paritas).
	//   - Fallback ON  → IFVG ditekan ("harga sediakan FVG searah, fallback tak perlu").
	candles := []data.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100.5},    // 0 kiri bull (High=101)
		{Open: 100.5, High: 102, Low: 100, Close: 101.5}, // 1 tengah bull
		{Open: 106, High: 108, Low: 106, Close: 107},     // 2 kanan bull (Low106>101 → bull FVG [101,106]); jg kiri bear (Low=106)
		{Open: 105, High: 105.5, Low: 99, Close: 99.5},   // 3 tengah bear; close 99.5 < 101 → bull FVG gagal
		{Open: 99, High: 100.5, Low: 98, Close: 99},      // 4 kanan bear (High100.5<106 → bear FVG [100.5,106] @idx3)
	}
	countIFVG := func(pdrs []PDR) int {
		n := 0
		for _, p := range pdrs {
			if p.Kind == KindIFVG {
				n++
			}
		}
		return n
	}
	if off := DetectPDRs(candles, 5, 0, 3, 5, false, false, false, false, false); countIFVG(off) == 0 {
		t.Fatalf("fallback OFF: IFVG harus muncul (perilaku lama), pdrs=%+v", off)
	}
	if on := DetectPDRs(candles, 5, 0, 3, 5, true, false, false, false, false); countIFVG(on) != 0 {
		t.Errorf("fallback ON: IFVG harus ditekan krn ada FVG bearish searah yg live, pdrs=%+v", on)
	}
}

func TestRealData_PDR_Smoke(t *testing.T) {
	candles := loadH1OrSkip(t)
	pdrs := DetectPDRs(candles, 5, 15, 3, 5, false, false, false, false, false)
	if len(pdrs) == 0 {
		t.Fatal("harusnya ada PDR di data nyata")
	}
	pois := BuildPOIs(pdrs, 2, 0)
	var byKind = map[string]int{}
	for _, p := range pdrs {
		byKind[p.Kind.String()]++
	}
	t.Logf("H1: %d PDR %v → %d POI (confluence>=2)", len(pdrs), byKind, len(pois))
}
