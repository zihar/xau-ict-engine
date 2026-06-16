// Package viz me-render candlestick chart + anotasi (zona POI, garis level,
// judul) sebagai SVG murni tanpa dependency apa pun di luar std-lib.
//
// Tujuannya menggantikan "screenshot chart" pada workflow trader: output SVG
// bisa langsung dibuka sebagai gambar di browser. Package ini sengaja berdiri
// sendiri (hanya import std-lib + xau-ict-engine/internal/data) supaya tidak
// ada coupling/import cycle dengan engine maupun detector.
package viz

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"xau-ict-engine/internal/data"
)

// Zone = pita harga horizontal melintang penuh lebar chart (mis. POI / zona
// discount). Digambar sebagai rect dari kiri ke kanan plot.
type Zone struct {
	Low, High float64
	Label     string
	Fill      string    // warna CSS, mis. "rgba(38,166,154,0.18)" hijau / "rgba(239,83,80,0.18)" merah
	T0        time.Time // opsional: bila di-set, zona digambar sebagai KOTAK (dari T0, lebar dibatasi) bukan pita full-width
	Accent    string    // opsional: warna garis tepi + pill label (highlight) — dipakai bila T0 di-set, mis. POI
}

// Level = garis harga horizontal (mis. entry/SL/TP/equilibrium).
type Level struct {
	Price float64
	Label string
	Color string // mis. "#2962ff" entry, "#ef5350" SL, "#26a69a" TP, "#888" eq
	Dash  bool   // true = garis putus-putus
}

// Marker = titik penanda di (Time, Price), mis. pivot ITH/ITL atau trigger 5m.
type Marker struct {
	Time  time.Time
	Price float64
	Label string
	Color string // mis. "#ef5350" utk high/ITH, "#26a69a" utk low/ITL
	Up    bool   // true = segitiga menunjuk ATAS (low/ITL), false = menunjuk BAWAH (high/ITH)
}

// Segment = garis diagonal dari (T0,P0) ke (T1,P1), mis. leg Fibonacci yg ditarik.
type Segment struct {
	T0, T1 time.Time
	P0, P1 float64
	Label  string
	Color  string
	Dash   bool
}

// PDBox = kotak zona PD array (FVG/OB/BB/VI) dari candle pembentuk ke kanan.
type PDBox struct {
	T0        time.Time // mulai (candle pembentuk); digambar s/d tepi kanan plot
	Low, High float64
	Label     string
	Stroke    string // warna garis tepi (per kind)
	Fill      string // fill semi-transparan (boleh "")
	Emphasize bool   // true = bagian POI terpilih (garis tebal)
}

// Annotations adalah kumpulan anotasi yang ditumpuk di atas candlestick.
type Annotations struct {
	Title    string
	Subtitle string
	Zones    []Zone
	Levels   []Level
	Markers  []Marker
	Segments []Segment
	Boxes    []PDBox

	// PriceLo/PriceHi (opsional) = override rentang harga sumbu Y. Bila
	// PriceHi > PriceLo, RenderSVG memakai window ini APA ADANYA (tanpa
	// auto-range/padding) supaya bisa "zoom" ke aksi harga terbaru tanpa
	// digepengkan swing/level jauh. Anotasi/candle di luar window di-clamp ke
	// tepi plot (yOf), jadi tak tumpah ke judul/bawah. Nol = auto (default).
	PriceLo, PriceHi float64
}

// Konstanta layout kanvas (16:9) + margin.
const (
	canvasW = 1200.0
	canvasH = 675.0

	marginTop    = 70.0 // ruang untuk judul + subtitle
	marginBottom = 30.0 // ruang sumbu waktu (opsional)
	marginLeft   = 12.0 // tepi kiri plot
	marginRight  = 88.0 // ruang axis harga di kanan

	pricePadFrac  = 0.03 // padding ~3% atas-bawah pada skala harga
	bodyWidthFrac = 0.7  // lebar body candle relatif terhadap slot per-candle
	numPriceTicks = 5    // jumlah tick label harga di axis kanan
	boxMaxWidthFrac = 0.28 // lebar maks zona/PD-box (fraksi lebar plot) — kotak "wajar", tak membentang penuh ke kanan

	colorUp   = "#26a69a"
	colorDown = "#ef5350"
)

// plotArea adalah area gambar candlestick (di dalam margin).
type plotArea struct {
	left, top, right, bottom float64
}

func (p plotArea) width() float64  { return p.right - p.left }
func (p plotArea) height() float64 { return p.bottom - p.top }

// RenderSVG menulis chart candlestick + anotasi sebagai SVG ke w.
// candles = data OHLC kronologis (boleh kosong → tetap render frame + judul).
func RenderSVG(w io.Writer, candles []data.Candle, ann Annotations) error {
	plot := plotArea{
		left:   marginLeft,
		top:    marginTop,
		right:  canvasW - marginRight,
		bottom: canvasH - marginBottom,
	}

	// Hitung rentang harga gabungan: candles + semua zona + semua level,
	// supaya seluruh anotasi pasti masuk frame.
	lo, hi, hasRange := priceRange(candles, ann)
	// Override "zoom": pakai window harga eksplisit apa adanya (tanpa padding).
	if ann.PriceHi > ann.PriceLo {
		lo, hi, hasRange = ann.PriceLo, ann.PriceHi, true
	}

	var b strings.Builder

	// Header SVG + background terang.
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g" font-family="Helvetica,Arial,sans-serif">`+"\n",
		canvasW, canvasH, canvasW, canvasH)
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%g" height="%g" fill="#fafafa"/>`+"\n", canvasW, canvasH)
	// Border tipis area plot.
	fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g" fill="#ffffff" stroke="#e0e0e0" stroke-width="1"/>`+"\n",
		plot.left, plot.top, plot.width(), plot.height())

	// Kalau ada rentang harga, definisikan fungsi konversi harga→y.
	// y terbalik: harga tinggi di atas.
	yOf := func(price float64) float64 {
		if !hasRange || hi == lo {
			return plot.top + plot.height()/2
		}
		frac := (price - lo) / (hi - lo)
		y := plot.bottom - frac*plot.height()
		// Clamp ke area plot: saat zoom, candle/level di luar window tetap
		// terkurung di tepi (tak tumpah ke judul/bawah). Tanpa zoom, range
		// sudah mencakup semua → clamp jadi no-op.
		if y < plot.top {
			y = plot.top
		}
		if y > plot.bottom {
			y = plot.bottom
		}
		return y
	}

	// inView = harga p berada di dalam window sumbu Y. Dipakai saat zoom/crop
	// untuk MENYEMBUNYIKAN label anotasi yang seluruhnya di luar view (kalau
	// tidak, semuanya ter-clamp ke tepi & label-nya numpuk di pojok). Tanpa
	// range / tanpa zoom → semua in-view, jadi no-op.
	inView := func(p float64) bool { return !hasRange || (p >= lo && p <= hi) }

	if hasRange {
		// Grid halus + axis harga (tick label di kanan).
		drawPriceAxis(&b, plot, lo, hi, yOf)
	}

	// Konversi waktu→x. Endpoint di luar rentang candle di-clamp ke tepi plot
	// supaya segmen/marker tetap tergambar menuju tepi (lihat xOfTime).
	xOf := func(t time.Time) float64 { return xOfTime(plot, candles, t) }

	// Zona digambar lebih dulu (paling belakang) supaya candle & garis di atasnya.
	for _, z := range ann.Zones {
		drawZone(&b, plot, z, xOf, yOf)
	}

	// PD array boxes (FVG/OB/BB/VI) di belakang candle: dari candle pembentuk
	// (T0) sampai tepi kanan plot, fill semi-transparan + garis tepi per kind.
	// Rect digambar dulu (di belakang candle); LABEL ditunda & di-stagger di
	// akhir supaya tak tumpuk antar-box berdekatan & tetap di atas candle.
	for _, bx := range ann.Boxes {
		drawPDBoxRect(&b, plot, bx, xOf, yOf)
	}

	// Candlestick.
	if len(candles) > 0 {
		drawCandles(&b, plot, candles, yOf)
		drawTimeAxis(&b, plot, candles)
	}

	// Label PD box: layout anti-tumpuk (di atas candle supaya terbaca). Label
	// di-anchor ke TOP box; kalau top-nya di luar view (saat crop) ia ter-clamp ke
	// tepi atas & numpuk → lewati labelnya (rect zona tetap digambar). Box masih
	// terlihat sebagai pita, hanya tanpa teks.
	var boxLabels []PDBox
	for _, bx := range ann.Boxes {
		if inView(bx.High) {
			boxLabels = append(boxLabels, bx)
		}
	}
	drawPDBoxLabels(&b, plot, boxLabels, xOf, yOf)

	// Label highlight zona-kotak (POI) di atas candle supaya menonjol.
	drawZoneBoxLabels(&b, plot, ann.Zones, xOf, yOf)

	// Level garis horizontal di atas segalanya.
	for _, lv := range ann.Levels {
		drawLevel(&b, plot, lv, yOf)
	}

	// Segment (garis Fibonacci leg) digambar sebelum marker supaya titik
	// pivot (segitiga) berada di atas garis.
	for _, s := range ann.Segments {
		drawSegment(&b, plot, s, xOf, yOf)
	}

	// Marker (titik ITH/ITL / trigger) di atas segalanya. Yang di luar view
	// (saat crop) dilewati supaya tak menempel & numpuk di tepi.
	for _, m := range ann.Markers {
		if inView(m.Price) {
			drawMarker(&b, m, xOf, yOf)
		}
	}

	// Judul + subtitle di kiri atas.
	drawTitle(&b, ann)

	b.WriteString("</svg>\n")

	_, err := io.WriteString(w, b.String())
	return err
}

// priceRange menghitung min/max harga dari candles digabung dengan semua
// Zone.Low/High & Level.Price, lalu memberi padding ~3% atas-bawah.
// hasRange=false kalau tidak ada satu pun nilai harga (chart kosong total).
func priceRange(candles []data.Candle, ann Annotations) (lo, hi float64, hasRange bool) {
	lo, hi = math.Inf(1), math.Inf(-1)
	acc := func(v float64) {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
		hasRange = true
	}

	for _, c := range candles {
		acc(c.High)
		acc(c.Low)
	}
	for _, z := range ann.Zones {
		acc(z.Low)
		acc(z.High)
	}
	for _, lv := range ann.Levels {
		acc(lv.Price)
	}
	for _, m := range ann.Markers {
		acc(m.Price)
	}
	for _, s := range ann.Segments {
		acc(s.P0)
		acc(s.P1)
	}
	for _, bx := range ann.Boxes {
		acc(bx.Low)
		acc(bx.High)
	}

	if !hasRange {
		return 0, 0, false
	}
	if hi == lo {
		// Hindari pembagian nol: lebarkan sedikit di sekitar nilai tunggal.
		pad := math.Abs(hi) * pricePadFrac
		if pad == 0 {
			pad = 1
		}
		return lo - pad, hi + pad, true
	}
	pad := (hi - lo) * pricePadFrac
	return lo - pad, hi + pad, true
}

// drawPriceAxis menggambar garis grid horizontal + label harga di kanan.
func drawPriceAxis(b *strings.Builder, plot plotArea, lo, hi float64, yOf func(float64) float64) {
	for i := 0; i <= numPriceTicks; i++ {
		frac := float64(i) / float64(numPriceTicks)
		price := lo + frac*(hi-lo)
		y := yOf(price)
		// Garis grid halus.
		fmt.Fprintf(b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="#eeeeee" stroke-width="1"/>`+"\n",
			plot.left, y, plot.right, y)
		// Label harga (2 desimal) di sebelah kanan axis.
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="11" fill="#666" dominant-baseline="middle">%s</text>`+"\n",
			plot.right+6, y, escapeXML(fmt.Sprintf("%.2f", price)))
	}
}

// drawTimeAxis menggambar ~6 label tanggal di bawah plot (pakai marginBottom yang
// sudah dicadangkan) + tick kecil, supaya sumbu waktu terbaca. Waktu = candle.Time
// (UTC, sama seperti data tersimpan). Untuk window pendek (≤4 hari) label menyertakan
// jam ("Jan 2 15h"); selain itu hanya tanggal ("Jan 2"). Label tepi di-anchor ke
// dalam (start/end) supaya tak terpotong tepi kanvas.
func drawTimeAxis(b *strings.Builder, plot plotArea, candles []data.Candle) {
	n := len(candles)
	if n == 0 {
		return
	}
	slot := plot.width() / float64(n)
	intraday := candles[n-1].Time.Sub(candles[0].Time) <= 4*24*time.Hour
	const ticks = 6
	seen := map[int]bool{}
	for i := 0; i <= ticks; i++ {
		idx := int(math.Round(float64(i) / float64(ticks) * float64(n-1)))
		if idx < 0 {
			idx = 0
		}
		if idx > n-1 {
			idx = n - 1
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		x := plot.left + (float64(idx)+0.5)*slot
		fmt.Fprintf(b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="#cccccc" stroke-width="1"/>`+"\n",
			x, plot.bottom, x, plot.bottom+4)
		label := candles[idx].Time.Format("Jan 2")
		if intraday {
			label = candles[idx].Time.Format("Jan 2 15h")
		}
		anchor := "middle"
		if idx == 0 {
			anchor = "start"
		} else if idx == n-1 {
			anchor = "end"
		}
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="10" fill="#999999" text-anchor="%s">%s</text>`+"\n",
			x, plot.bottom+16, anchor, escapeXML(label))
	}
}

// zoneGeom menghitung x0, lebar, top, height sebuah Zone. Bila T0 di-set zona
// jadi KOTAK (mulai T0, lebar dibatasi boxMaxWidthFrac); selain itu pita penuh.
func zoneGeom(plot plotArea, z Zone, xOf func(time.Time) float64, yOf func(float64) float64) (x0, w, top, h float64) {
	yHigh := yOf(z.High)
	yLow := yOf(z.Low)
	top = math.Min(yHigh, yLow)
	h = math.Abs(yLow - yHigh)
	if h < 1 {
		h = 1
	}
	x0, w = plot.left, plot.width()
	if !z.T0.IsZero() {
		x0 = xOf(z.T0)
		if x0 > plot.right {
			x0 = plot.right
		}
		w = plot.right - x0
		if mw := boxMaxWidthFrac * plot.width(); w > mw {
			w = mw
		}
	}
	return x0, w, top, h
}

// drawZone menggambar rect zona. Pita full-width (T0 kosong) langsung dengan label
// plain. Zona-KOTAK (T0 di-set, mis. POI) hanya rect + tepi accent di sini; label
// highlight-nya ditunda ke drawZoneBoxLabels (digambar DI ATAS candle).
func drawZone(b *strings.Builder, plot plotArea, z Zone, xOf func(time.Time) float64, yOf func(float64) float64) {
	x0, w, top, h := zoneGeom(plot, z, xOf, yOf)
	fill := z.Fill
	if fill == "" {
		fill = "rgba(120,120,120,0.15)"
	}
	strokeAttr := ""
	if !z.T0.IsZero() && z.Accent != "" {
		strokeAttr = fmt.Sprintf(` stroke="%s" stroke-width="2"`, escapeXML(z.Accent))
	}
	fmt.Fprintf(b, `<rect x="%g" y="%g" width="%g" height="%g" fill="%s"%s/>`+"\n",
		x0, top, w, h, escapeXML(fill), strokeAttr)
	// Pita full-width: label plain di pojok kiri-atas (label kotak ditunda).
	if z.Label != "" && z.T0.IsZero() {
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="11" fill="#444">%s</text>`+"\n",
			plot.left+6, top+13, escapeXML(z.Label))
	}
}

// drawZoneBoxLabels menggambar label HIGHLIGHT (pill accent + teks putih tebal,
// font besar) untuk zona-kotak (T0+Accent) — dipanggil SETELAH candle agar tak
// tertutup. Pill ditaruh tepat di atas kotak; kalau mepet tepi atas, ke dalam.
func drawZoneBoxLabels(b *strings.Builder, plot plotArea, zones []Zone, xOf func(time.Time) float64, yOf func(float64) float64) {
	for _, z := range zones {
		if z.T0.IsZero() || z.Label == "" {
			continue
		}
		x0, _, top, _ := zoneGeom(plot, z, xOf, yOf)
		accent := z.Accent
		if accent == "" {
			accent = "#333333"
		}
		const fs = 13.0
		tw := float64(len([]rune(z.Label)))*fs*0.6 + 12
		ph := fs + 6
		py := top - ph - 2 // pill di atas kotak
		if py < plot.top+1 {
			py = top + 2 // mepet atas → taruh di dalam kotak
		}
		fmt.Fprintf(b, `<rect x="%g" y="%g" width="%g" height="%g" rx="3" fill="%s"/>`+"\n",
			x0, py, tw, ph, escapeXML(accent))
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="%g" font-weight="bold" fill="#ffffff">%s</text>`+"\n",
			x0+6, py+fs-1, fs, escapeXML(z.Label))
	}
}

// pdBoxGeom menghitung geometri (x0 ter-clamp, top, height) sebuah PDBox.
func pdBoxGeom(plot plotArea, bx PDBox, xOf func(time.Time) float64, yOf func(float64) float64) (x0, top, h float64) {
	x0 = xOf(bx.T0)
	if x0 > plot.right {
		x0 = plot.right
	}
	yHigh := yOf(bx.High)
	yLow := yOf(bx.Low)
	top = math.Min(yHigh, yLow)
	h = math.Abs(yLow - yHigh)
	if h < 1 {
		h = 1
	}
	return x0, top, h
}

// drawPDBoxRect menggambar HANYA rect zona PD array (tanpa label) dari candle
// pembentuk (T0) sampai tepi kanan plot, y dari High→Low. Stroke = garis tepi
// per kind (tebal kalau Emphasize = bagian POI terpilih), Fill = isi
// semi-transparan. x0 di-clamp ke tepi plot oleh xOfTime. Label digambar
// terpisah oleh drawPDBoxLabels (anti-tumpuk).
func drawPDBoxRect(b *strings.Builder, plot plotArea, bx PDBox, xOf func(time.Time) float64, yOf func(float64) float64) {
	x0, top, h := pdBoxGeom(plot, bx, xOf, yOf)
	w := plot.right - x0
	if mw := boxMaxWidthFrac * plot.width(); w > mw {
		w = mw // kotak "wajar" — tak membentang penuh ke kanan
	}

	stroke := bx.Stroke
	if stroke == "" {
		stroke = "#888"
	}
	fill := bx.Fill
	if fill == "" {
		fill = "none"
	}
	strokeW := 1.0
	if bx.Emphasize {
		strokeW = 2.5
	}
	fmt.Fprintf(b, `<rect x="%g" y="%g" width="%g" height="%g" fill="%s" stroke="%s" stroke-width="%g"/>`+"\n",
		x0, top, w, h, escapeXML(fill), escapeXML(stroke), strokeW)
}

// drawPDBoxLabels menata label semua PDBox supaya tidak tumpuk saat box
// berdekatan. Strategi: urut label dari atas ke bawah (per y), lalu untuk tiap
// label paksa jarak vertikal minimal labelLineH dari label sebelumnya yang
// berada pada kolom-x yang sama/berdekatan (geser ke bawah). Label diletakkan
// di pojok kiri-atas box (x0+3); kalau di-geser melewati dasar box, tetap
// digambar (lebih baik terbaca daripada hilang). Box tanpa Label dilewati.
func drawPDBoxLabels(b *strings.Builder, plot plotArea, boxes []PDBox, xOf func(time.Time) float64, yOf func(float64) float64) {
	const (
		labelLineH = 11.0 // tinggi baris label (font 9 + sedikit jarak)
		colTol     = 60.0 // dua label dianggap "berdekatan" bila |Δx| < colTol
	)

	type lbl struct {
		x, y   float64
		stroke string
		text   string
	}
	var labels []lbl
	for _, bx := range boxes {
		if bx.Label == "" {
			continue
		}
		x0, top, _ := pdBoxGeom(plot, bx, xOf, yOf)
		stroke := bx.Stroke
		if stroke == "" {
			stroke = "#888"
		}
		labels = append(labels, lbl{x: x0 + 3, y: top + 10, stroke: stroke, text: bx.Label})
	}

	// Urut dari atas ke bawah supaya penggeseran ke bawah deterministik.
	sort.SliceStable(labels, func(i, j int) bool { return labels[i].y < labels[j].y })

	// Untuk tiap label, jaga jarak vertikal minimal dari label sebelumnya yang
	// kolom-x-nya berdekatan (hindari tumpuk). placed = label yang sudah final.
	var placed []lbl
	for _, l := range labels {
		// Geser l ke BAWAH sampai tak ada label sekolom yang terlalu rapat.
		// Tiap pass mendorong l ke posisi DI BAWAH label sekolom terbawah yang
		// masih bertabrakan → progres monoton turun, jadi pasti berhenti dalam
		// <= len(placed) pass (cap defensif terhadap drift float). Bug lama:
		// `for{}` tanpa batas + bump per-match bisa berputar selamanya saat dua
		// label sekolom saling "memantulkan" l (lihat regresi di chart_test.go).
		for pass := 0; pass <= len(placed); pass++ {
			maxBottom := l.y
			collide := false
			for _, p := range placed {
				if math.Abs(p.x-l.x) < colTol && math.Abs(p.y-l.y) < labelLineH {
					collide = true
					if p.y+labelLineH > maxBottom {
						maxBottom = p.y + labelLineH
					}
				}
			}
			if !collide {
				break
			}
			l.y = maxBottom // geser ke bawah label sekolom terbawah yang bentrok
		}
		// Jaga tetap di dalam area plot (jangan keluar bawah).
		if l.y > plot.bottom-2 {
			l.y = plot.bottom - 2
		}
		placed = append(placed, l)
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="9" fill="%s">%s</text>`+"\n",
			l.x, l.y, escapeXML(l.stroke), escapeXML(l.text))
	}
}

// drawCandles menggambar seluruh candlestick: wick (high→low) + body (open→close).
func drawCandles(b *strings.Builder, plot plotArea, candles []data.Candle, yOf func(float64) float64) {
	n := len(candles)
	slot := plot.width() / float64(n) // lebar slot per candle
	bodyW := slot * bodyWidthFrac
	if bodyW < 1 {
		bodyW = 1 // tetap terlihat saat n besar
	}

	for i, c := range candles {
		cx := plot.left + (float64(i)+0.5)*slot
		color := colorUp
		if c.Close < c.Open {
			color = colorDown
		}

		yHigh := yOf(c.High)
		yLow := yOf(c.Low)
		// Wick: garis tipis high→low.
		fmt.Fprintf(b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1"/>`+"\n",
			cx, yHigh, cx, yLow, color)

		// Body: rect open→close. Jaga tinggi minimal 1px untuk doji.
		yOpen := yOf(c.Open)
		yClose := yOf(c.Close)
		top := math.Min(yOpen, yClose)
		h := math.Abs(yClose - yOpen)
		if h < 1 {
			h = 1
		}
		fmt.Fprintf(b, `<rect x="%g" y="%g" width="%g" height="%g" fill="%s"/>`+"\n",
			cx-bodyW/2, top, bodyW, h, color)
	}
}

// drawLevel menggambar garis harga horizontal melintang + label/harga di kanan.
func drawLevel(b *strings.Builder, plot plotArea, lv Level, yOf func(float64) float64) {
	y := yOf(lv.Price)
	color := lv.Color
	if color == "" {
		color = "#888"
	}
	dash := ""
	if lv.Dash {
		dash = ` stroke-dasharray="6 4"`
	}
	fmt.Fprintf(b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.5"%s/>`+"\n",
		plot.left, y, plot.right, y, escapeXML(color), dash)

	// Label + harga di ujung kanan (dekat axis), digeser sedikit ke atas garis.
	label := lv.Label
	if label != "" {
		label += " "
	}
	fmt.Fprintf(b, `<text x="%g" y="%g" font-size="11" fill="%s" text-anchor="end">%s</text>`+"\n",
		plot.right-4, y-3, escapeXML(color), escapeXML(fmt.Sprintf("%s%.2f", label, lv.Price)))
}

// drawTitle menggambar judul (bold ~20px) + subtitle (abu ~13px) di kiri atas.
func drawTitle(b *strings.Builder, ann Annotations) {
	if ann.Title != "" {
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="20" font-weight="bold" fill="#222">%s</text>`+"\n",
			marginLeft, 28.0, escapeXML(ann.Title))
	}
	if ann.Subtitle != "" {
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="13" fill="#888">%s</text>`+"\n",
			marginLeft, 48.0, escapeXML(ann.Subtitle))
	}
}

// xOfTime memetakan sebuah waktu ke koordinat x pada plot, mengikuti tata
// letak candle (x = pusat slot candle, sama seperti drawCandles).
//
// Aturan:
//   - candles kosong → fallback ke tengah plot (tetap aman, tidak panic).
//   - t lebih awal dari candle pertama → clamp ke tepi KIRI plot.
//   - t lebih akhir dari candle terakhir → clamp ke tepi KANAN plot.
//   - di antaranya → cari candle dengan Time terdekat ke t lalu pakai pusat
//     slot-nya. Ini cukup untuk anotasi (marker/segment) tanpa perlu
//     interpolasi sub-candle.
func xOfTime(plot plotArea, candles []data.Candle, t time.Time) float64 {
	n := len(candles)
	if n == 0 {
		return plot.left + plot.width()/2
	}
	slot := plot.width() / float64(n)
	centerX := func(i int) float64 { return plot.left + (float64(i)+0.5)*slot }

	// Clamp ke tepi kalau di luar rentang waktu candle.
	if t.Before(candles[0].Time) {
		return plot.left
	}
	if t.After(candles[n-1].Time) {
		return plot.right
	}

	// Cari indeks candle dengan |Time - t| terkecil.
	best := 0
	bestDiff := absDuration(candles[0].Time.Sub(t))
	for i := 1; i < n; i++ {
		d := absDuration(candles[i].Time.Sub(t))
		if d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return centerX(best)
}

// absDuration mengembalikan nilai absolut sebuah durasi.
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// drawSegment menggambar garis diagonal dari (T0,P0) ke (T1,P1) + label kecil
// dekat titik tengah. Endpoint di-clamp ke tepi plot oleh xOfTime.
//
// Guard: kalau KEDUA endpoint ter-clamp ke tepi-x yang SAMA (kiri↔kiri atau
// kanan↔kanan), segmen kolaps jadi garis vertikal di pinggir yang menyesatkan
// (mis. leg Fib yang anchor-nya jauh di luar window). Dalam kasus itu segmen
// tidak digambar — pemanggil (chartann) seharusnya sudah menyaring, ini lapis
// pertahanan agar tak ada artefak vertikal di tepi.
func drawSegment(b *strings.Builder, plot plotArea, s Segment, xOf func(time.Time) float64, yOf func(float64) float64) {
	x0, y0 := xOf(s.T0), yOf(s.P0)
	x1, y1 := xOf(s.T1), yOf(s.P1)
	if (x0 == plot.left && x1 == plot.left) || (x0 == plot.right && x1 == plot.right) {
		return
	}
	color := s.Color
	if color == "" {
		color = "#888"
	}
	dash := ""
	if s.Dash {
		dash = ` stroke-dasharray="6 4"`
	}
	fmt.Fprintf(b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.5"%s/>`+"\n",
		x0, y0, x1, y1, escapeXML(color), dash)
	if s.Label != "" {
		// Label di sekitar titik tengah, digeser sedikit ke atas garis.
		mx := (x0 + x1) / 2
		my := (y0 + y1) / 2
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="11" fill="%s">%s</text>`+"\n",
			mx+4, my-4, escapeXML(color), escapeXML(s.Label))
	}
}

// drawMarker menggambar segitiga kecil (~6px) di (x, y(Price)) + label.
// Up=true → ujung ke atas, segitiga ditaruh sedikit DI BAWAH harga (penanda
// low/ITL). Up=false → ujung ke bawah, sedikit DI ATAS harga (high/ITH).
func drawMarker(b *strings.Builder, m Marker, xOf func(time.Time) float64, yOf func(float64) float64) {
	x := xOf(m.Time)
	y := yOf(m.Price)
	color := m.Color
	if color == "" {
		color = "#444"
	}
	const half = 5.0 // setengah lebar alas segitiga
	const gap = 3.0  // jarak dari harga ke alas segitiga

	var points string
	var labelY float64
	if m.Up {
		// Ujung ke ATAS, segitiga di bawah harga: apex di (x, y+gap).
		apexY := y + gap
		baseY := apexY + 2*half
		points = fmt.Sprintf("%g,%g %g,%g %g,%g", x, apexY, x-half, baseY, x+half, baseY)
		labelY = baseY + 11
	} else {
		// Ujung ke BAWAH, segitiga di atas harga: apex di (x, y-gap).
		apexY := y - gap
		baseY := apexY - 2*half
		points = fmt.Sprintf("%g,%g %g,%g %g,%g", x, apexY, x-half, baseY, x+half, baseY)
		labelY = baseY - 4
	}
	fmt.Fprintf(b, `<polygon points="%s" fill="%s"/>`+"\n", points, escapeXML(color))
	if m.Label != "" {
		fmt.Fprintf(b, `<text x="%g" y="%g" font-size="11" fill="%s" text-anchor="middle">%s</text>`+"\n",
			x, labelY, escapeXML(color), escapeXML(m.Label))
	}
}

// escapeXML meng-escape karakter spesial XML supaya teks/atribut tetap valid:
// & < > " '.
func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
