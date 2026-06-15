package viz

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"forex-backtest/internal/data"
)

// dummyCandles membuat beberapa candle naik-turun untuk uji render.
func dummyCandles() []data.Candle {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return []data.Candle{
		{Time: base, Open: 2000, High: 2010, Low: 1995, Close: 2008, Volume: 100},                    // naik
		{Time: base.Add(time.Hour), Open: 2008, High: 2012, Low: 1990, Close: 1992, Volume: 120},     // turun
		{Time: base.Add(2 * time.Hour), Open: 1992, High: 2005, Low: 1988, Close: 2003, Volume: 90},  // naik
		{Time: base.Add(3 * time.Hour), Open: 2003, High: 2003, Low: 1980, Close: 1985, Volume: 110}, // turun
		{Time: base.Add(4 * time.Hour), Open: 1985, High: 2020, Low: 1984, Close: 2018, Volume: 150}, // naik besar
	}
}

func TestRenderSVG_Basic(t *testing.T) {
	var buf bytes.Buffer
	ann := Annotations{
		Title:    "XAUUSD H1 Backtest",
		Subtitle: "POI discount + entry/SL/TP",
		Zones: []Zone{
			{Low: 1990, High: 2000, Label: "POI discount", Fill: "rgba(38,166,154,0.18)"},
		},
		Levels: []Level{
			{Price: 1998, Label: "Entry", Color: "#2962ff"},
			{Price: 1982, Label: "SL", Color: "#ef5350", Dash: true},
			{Price: 2025, Label: "TP", Color: "#26a69a"},
		},
	}

	if err := RenderSVG(&buf, dummyCandles(), ann); err != nil {
		t.Fatalf("RenderSVG error: %v", err)
	}
	out := buf.String()

	for _, want := range []string{"<svg", "</svg>", "rect", "line", "XAUUSD H1 Backtest"} {
		if !strings.Contains(out, want) {
			t.Errorf("output SVG tidak mengandung %q", want)
		}
	}
	// Subtitle + label anotasi ikut ter-render.
	for _, want := range []string{"POI discount", "Entry", "SL", "TP"} {
		if !strings.Contains(out, want) {
			t.Errorf("output SVG tidak mengandung label %q", want)
		}
	}
	// Garis putus-putus untuk level Dash:true.
	if !strings.Contains(out, "stroke-dasharray") {
		t.Errorf("output SVG tidak mengandung stroke-dasharray untuk level Dash")
	}
}

func TestRenderSVG_EmptyCandles(t *testing.T) {
	var buf bytes.Buffer
	ann := Annotations{Title: "Empty", Subtitle: "no data"}

	// Tidak boleh panic saat candles kosong; tetap render frame + judul.
	if err := RenderSVG(&buf, nil, ann); err != nil {
		t.Fatalf("RenderSVG (kosong) error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"<svg", "</svg>", "Empty"} {
		if !strings.Contains(out, want) {
			t.Errorf("output SVG kosong tidak mengandung %q", want)
		}
	}
}

func TestRenderSVG_MarkersAndSegments(t *testing.T) {
	cd := dummyCandles()
	base := cd[0].Time

	var buf bytes.Buffer
	ann := Annotations{
		Title: "Markers + Segments",
		Markers: []Marker{
			// Marker low/ITL (Up), waktu di dalam rentang candle.
			{Time: cd[3].Time, Price: 1980, Label: "ITL", Color: "#26a69a", Up: true},
			// Marker high/ITH (down), waktu di dalam rentang candle.
			{Time: cd[1].Time, Price: 2012, Label: "ITH", Color: "#ef5350", Up: false},
			// Marker dengan waktu JAUH di kiri rentang candle → harus tetap
			// ter-render (clamp ke tepi kiri), tidak panic.
			{Time: base.Add(-100 * time.Hour), Price: 1990, Label: "ANCHOR", Color: "#2962ff", Up: true},
		},
		Segments: []Segment{
			// Leg Fibonacci dari low awal ke high akhir.
			{T0: cd[3].Time, P0: 1980, T1: cd[4].Time, P1: 2020, Label: "Fib leg", Color: "#9c27b0", Dash: true},
			// Segmen dengan endpoint di luar rentang (kiri & kanan) → clamp.
			{T0: base.Add(-200 * time.Hour), P0: 1995, T1: base.Add(200 * time.Hour), P1: 2015, Label: "weekly", Color: "#ff9800"},
		},
	}

	if err := RenderSVG(&buf, cd, ann); err != nil {
		t.Fatalf("RenderSVG error: %v", err)
	}
	out := buf.String()

	// Marker = polygon (segitiga); segment = line tambahan.
	if !strings.Contains(out, "polygon") {
		t.Errorf("output SVG tidak mengandung polygon (marker)")
	}
	if !strings.Contains(out, "<line") {
		t.Errorf("output SVG tidak mengandung line (segment)")
	}
	// Label marker & segment ikut ter-render.
	for _, want := range []string{"ITL", "ITH", "ANCHOR", "Fib leg", "weekly"} {
		if !strings.Contains(out, want) {
			t.Errorf("output SVG tidak mengandung label %q", want)
		}
	}
	// Garis putus-putus untuk segment Dash:true.
	if !strings.Contains(out, "stroke-dasharray") {
		t.Errorf("output SVG tidak mengandung stroke-dasharray untuk segment Dash")
	}
}

func TestRenderSVG_MarkerOutOfRangeEmptyCandles(t *testing.T) {
	// Marker dengan candles kosong tidak boleh panic; tetap render frame.
	var buf bytes.Buffer
	ann := Annotations{
		Title: "No candles",
		Markers: []Marker{
			{Time: time.Now(), Price: 100, Label: "X", Up: true},
		},
		Segments: []Segment{
			{T0: time.Now(), P0: 100, T1: time.Now().Add(time.Hour), P1: 110, Label: "S"},
		},
	}
	if err := RenderSVG(&buf, nil, ann); err != nil {
		t.Fatalf("RenderSVG (kosong + marker) error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"<svg", "</svg>", "polygon", "X", "S"} {
		if !strings.Contains(out, want) {
			t.Errorf("output SVG tidak mengandung %q", want)
		}
	}
}

func TestRenderSVG_Boxes(t *testing.T) {
	cd := dummyCandles()
	base := cd[0].Time

	var buf bytes.Buffer
	ann := Annotations{
		Title: "PD Array boxes",
		Boxes: []PDBox{
			// Box biasa (FVG) dari candle dalam rentang.
			{T0: cd[1].Time, Low: 1990, High: 2000, Label: "FVG↑", Stroke: "#42a5f5", Fill: "rgba(66,165,245,0.07)"},
			// Box emphasize (bagian POI) → garis tebal.
			{T0: cd[2].Time, Low: 1985, High: 1995, Label: "OB↑", Stroke: "#ff9800", Fill: "rgba(255,152,0,0.07)", Emphasize: true},
			// Box dgn T0 di luar kiri rentang → clamp ke tepi kiri, tetap ter-render.
			{T0: base.Add(-100 * time.Hour), Low: 2015, High: 2025, Label: "VI↓", Stroke: "#26c6da"},
		},
	}

	if err := RenderSVG(&buf, cd, ann); err != nil {
		t.Fatalf("RenderSVG error: %v", err)
	}
	out := buf.String()

	// Label tiap box ter-render.
	for _, want := range []string{"FVG", "OB", "VI"} {
		if !strings.Contains(out, want) {
			t.Errorf("output SVG tidak mengandung label box %q", want)
		}
	}
	// Warna stroke per kind ikut muncul.
	for _, want := range []string{"#42a5f5", "#ff9800", "#26c6da"} {
		if !strings.Contains(out, want) {
			t.Errorf("output SVG tidak mengandung warna stroke %q", want)
		}
	}
	// Box emphasize pakai stroke tebal 2.5.
	if !strings.Contains(out, `stroke-width="2.5"`) {
		t.Errorf("output SVG tidak mengandung stroke-width 2.5 untuk box Emphasize")
	}
}

// TestRenderSVG_ManyOverlappingBoxLabels adalah regresi untuk BUG hang:
// `drawPDBoxLabels` dulu pakai `for{}` tak-terbatas yang bisa berputar selamanya
// saat banyak box berbagi kolom-x yang sama & label-nya saling memantul. Kasus
// nyata: cmd/entries macet di entry tertentu (CPU 100% berjam-jam), 1 SVG 0-byte.
// Banyak box dengan T0 sama (kolom-x identik) + Low/High berdekatan harus render
// CEPAT tanpa hang. Test berjalan di goroutine dengan deadline sebagai jaring.
func TestRenderSVG_ManyOverlappingBoxLabels(t *testing.T) {
	cd := dummyCandles()
	var boxes []PDBox
	// 40 box di kolom-x SAMA (T0 sama) dengan rentang harga yang nyaris bertumpuk
	// → semua label jatuh di kolom & y berdekatan = pemicu pantulan loop lama.
	for i := 0; i < 40; i++ {
		lo := 1990.0 + float64(i)*0.1
		boxes = append(boxes, PDBox{
			T0:     cd[1].Time,
			Low:    lo,
			High:   lo + 0.5,
			Label:  "FVG",
			Stroke: "#42a5f5",
		})
	}
	ann := Annotations{Title: "overlap stress", Boxes: boxes}

	done := make(chan error, 1)
	go func() {
		var buf bytes.Buffer
		done <- RenderSVG(&buf, cd, ann)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RenderSVG error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RenderSVG hang (>5s) pada banyak label box sekolom — regresi loop tak-terbatas drawPDBoxLabels")
	}
}

func TestRenderSVG_EscapesXML(t *testing.T) {
	var buf bytes.Buffer
	ann := Annotations{
		Title: `A & B < C > "D" 'E'`,
		Zones: []Zone{
			{Low: 1, High: 2, Label: "zone & <tag>"},
		},
		Levels: []Level{
			{Price: 1.5, Label: "lvl & <x>", Color: "#000"},
		},
	}

	if err := RenderSVG(&buf, dummyCandles(), ann); err != nil {
		t.Fatalf("RenderSVG error: %v", err)
	}
	out := buf.String()

	// Karakter spesial harus ter-encode, bukan muncul mentah.
	for _, enc := range []string{"&amp;", "&lt;", "&gt;", "&quot;", "&apos;"} {
		if !strings.Contains(out, enc) {
			t.Errorf("output SVG tidak meng-encode %q", enc)
		}
	}
	// Pastikan tidak ada label mentah yang lolos tanpa escape.
	if strings.Contains(out, "zone & <tag>") {
		t.Errorf("label zona tidak ter-escape: %q ditemukan mentah", "zone & <tag>")
	}
	if strings.Contains(out, "A & B") {
		t.Errorf("judul tidak ter-escape: %q ditemukan mentah", "A & B")
	}
}
