package detectors

import (
	"testing"
)

// relEqOf memfilter hasil DetectRelEquals per kind (bantuan assert).
func relEqOf(levels []RelEqualLevel, kind SwingKind) []RelEqualLevel {
	var out []RelEqualLevel
	for _, l := range levels {
		if l.Kind == kind {
			out = append(out, l)
		}
	}
	return out
}

func TestDetectRelEqualsREH(t *testing.T) {
	// Dua STH hampir sama (4640.0 & 4639.8, selisih 0.2 = 2 pip) yang BELUM
	// disapu — double top klasik.
	cs := candlesFromHL([][2]float64{
		{4600, 4580},
		{4640, 4610}, // STH 4640.0
		{4605, 4585},
		{4639.8, 4609}, // STH 4639.8 (≈equal)
		{4600, 4580},
		{4595, 4575}, // tidak pernah menembus 4640
	})
	rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingHigh) // toleransi $0.5 (5 pip)
	if len(rehs) != 1 {
		t.Fatalf("REH = %d grup, want 1 (%+v)", len(rehs), rehs)
	}
	if rehs[0].Level != 4640.0 || rehs[0].Count != 2 {
		t.Errorf("REH level=%.2f count=%d, want level=4640.00 count=2", rehs[0].Level, rehs[0].Count)
	}
}

func TestDetectRelEqualsTigaSwing(t *testing.T) {
	// Triple top: 4640, 4639.9, 4640.3 → satu grup count=3, Level = tertinggi (4640.3).
	cs := candlesFromHL([][2]float64{
		{4600, 4580},
		{4640, 4610}, // STH
		{4605, 4585},
		{4639.9, 4609}, // STH
		{4600, 4580},
		{4640.3, 4610}, // STH (ekstrem grup)
		{4598, 4578},
		{4590, 4570},
	})
	rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingHigh)
	if len(rehs) != 1 || rehs[0].Count != 3 {
		t.Fatalf("want 1 grup count=3, got %+v", rehs)
	}
	if rehs[0].Level != 4640.3 {
		t.Errorf("Level = %.2f, want 4640.30 (ekstrem grup — sweep harus melewati semua)", rehs[0].Level)
	}
}

func TestDetectRelEqualsToleransiBatas(t *testing.T) {
	// Selisih 0.6 > toleransi 0.5 → BUKAN grup equal (dua swing sendiri-sendiri).
	cs := candlesFromHL([][2]float64{
		{4600, 4580},
		{4640, 4610}, // STH 4640.0
		{4605, 4585},
		{4639.4, 4609}, // STH 4639.4 (selisih 0.6)
		{4600, 4580},
		{4595, 4575},
	})
	if rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingHigh); len(rehs) != 0 {
		t.Fatalf("selisih di luar toleransi harusnya 0 grup, got %+v", rehs)
	}
	// Selisih persis = toleransi → masih grup (<=).
	cs2 := candlesFromHL([][2]float64{
		{4600, 4580},
		{4640, 4610},
		{4605, 4585},
		{4639.5, 4609}, // selisih persis 0.5
		{4600, 4580},
		{4595, 4575},
	})
	if rehs := relEqOf(DetectRelEquals(cs2, 0.5, 0, 10), SwingHigh); len(rehs) != 1 {
		t.Fatalf("selisih = toleransi harusnya 1 grup, got %+v", rehs)
	}
}

func TestDetectRelEqualsSweptDibuang(t *testing.T) {
	// REH 4640/4639.8 disapu TUNTAS oleh wick 4642 (melewati toleransi) → grup
	// hilang, dan 4642 sendirian tidak membentuk grup (count 1).
	cs := candlesFromHL([][2]float64{
		{4600, 4580},
		{4640, 4610},
		{4605, 4585},
		{4639.8, 4609},
		{4600, 4580},
		{4642, 4575}, // wick 4642 > 4640 (+ melewati toleransi) → swept
		{4590, 4570},
	})
	if rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingHigh); len(rehs) != 0 {
		t.Fatalf("grup tersapu harus dibuang, got %+v", rehs)
	}
}

func TestDetectRelEqualsSweepDiBatasToleransiMembentukGrupBaru(t *testing.T) {
	// Semantik yang DISENGAJA: sweep yang cuma melewati level DALAM toleransi
	// (wick 4640.5, toleransi 0.5) = swing high baru yang membentuk grup equal
	// BARU ber-level 4640.5 (stop baru menumpuk di atasnya) — bukan "tersapu
	// tuntas". Grup tetap dilaporkan dengan ekstrem ter-update.
	cs := candlesFromHL([][2]float64{
		{4600, 4580},
		{4640, 4610},
		{4605, 4585},
		{4639.8, 4609},
		{4600, 4580},
		{4640.5, 4575}, // wick tepat di batas toleransi → re-anchor, bukan sweep tuntas
		{4590, 4570},
	})
	rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingHigh)
	if len(rehs) != 1 || rehs[0].Level != 4640.5 || rehs[0].Count != 3 {
		t.Fatalf("want 1 grup level=4640.50 count=3 (kronologis: 4640 & 4639.8 bergabung saat ekstrem masih 4640, lalu 4640.5 re-anchor), got %+v", rehs)
	}
}

func TestDetectRelEqualsMaxGap(t *testing.T) {
	// Dua STH selevel tapi terpisah 12 candle — bukan double top "sejajar"
	// (user: maks 10 candle), jadi BUKAN grup. maxGap<=0 = tanpa batas (legacy).
	pairs := [][2]float64{
		{4600, 4580},
		{4640, 4610}, // STH idx1
	}
	for i := 0; i < 11; i++ { // 11 candle datar di antaranya
		pairs = append(pairs, [2]float64{4600 - float64(i), 4580 - float64(i)})
	}
	pairs = append(pairs,
		[2]float64{4639.8, 4609}, // STH idx13 (gap 12 candle dari idx1)
		[2]float64{4595, 4575},
		[2]float64{4590, 4570},
	)
	cs := candlesFromHL(pairs)
	if rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingHigh); len(rehs) != 0 {
		t.Fatalf("gap 12 > maxGap 10 harusnya 0 grup, got %+v", rehs)
	}
	if rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 0), SwingHigh); len(rehs) != 1 {
		t.Fatalf("maxGap 0 (tanpa batas) harusnya 1 grup, got %+v", rehs)
	}
	if rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 12), SwingHigh); len(rehs) != 1 {
		t.Fatalf("gap 12 = maxGap 12 harusnya 1 grup, got %+v", rehs)
	}
}

func TestDetectRelEqualsRELMirror(t *testing.T) {
	// Double bottom 4560.0 & 4560.2 belum disapu → REL Level = terendah (4560.0).
	cs := candlesFromHL([][2]float64{
		{4620, 4600},
		{4590, 4560}, // STL 4560.0
		{4615, 4595},
		{4592, 4560.2}, // STL 4560.2
		{4620, 4600},
		{4625, 4605},
	})
	rels := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingLow)
	if len(rels) != 1 || rels[0].Count != 2 || rels[0].Level != 4560.0 {
		t.Fatalf("want 1 REL count=2 level=4560.00, got %+v", rels)
	}
	// Sapu ke bawah → hilang.
	cs = append(cs, hl(4600, 4559.9, len(cs)))
	if rels := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingLow); len(rels) != 0 {
		t.Fatalf("REL tersapu harus hilang, got %+v", rels)
	}
}

func TestDetectRelEqualsLookback(t *testing.T) {
	// Pasangan equal ada di awal data; lookback sempit memotongnya → tidak terdeteksi.
	cs := candlesFromHL([][2]float64{
		{4600, 4580},
		{4640, 4610}, // STH (di luar lookback nanti)
		{4605, 4585},
		{4639.8, 4609}, // STH (di luar lookback nanti)
		{4600, 4580},
		{4595, 4575},
		{4590, 4570},
		{4585, 4565},
	})
	if rehs := relEqOf(DetectRelEquals(cs, 0.5, 0, 10), SwingHigh); len(rehs) != 1 {
		t.Fatalf("tanpa lookback harusnya 1 grup, got %+v", rehs)
	}
	if rehs := relEqOf(DetectRelEquals(cs, 0.5, 4, 10), SwingHigh); len(rehs) != 0 {
		t.Fatalf("lookback 4 candle terakhir harusnya 0 grup, got %+v", rehs)
	}
}
