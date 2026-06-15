// Package chartann memetakan hasil engine.Narrate (ScanNarrative) jadi anotasi
// chart viz.Annotations: zona POI, leg Fibonacci (dari-mana-ke-mana) + level
// 0/0.5/1, marker ITH/ITL, marker pivot trigger 5m, dan garis entry/SL/TP.
// Dipakai bersama oleh cmd/narrate & cmd/entries (glue engine↔viz).
package chartann

import (
	"fmt"
	"sort"
	"time"

	"forex-backtest/internal/engine"
	"forex-backtest/internal/viz"
)

// Warna konsisten dgn konvensi chart trader.
const (
	colEntry = "#2962ff"
	colSL    = "#ef5350"
	colTP    = "#26a69a"
	colEQ    = "#888888"
	colFib   = "#9c27b0" // ungu utk leg + level Fibonacci
	colITL   = "#26a69a" // low/ITL hijau
	colITH   = "#ef5350" // high/ITH merah
	colTrig  = "#2962ff" // pivot trigger biru

	// Warna garis tepi PD array box per Kind.
	colFVG = "#42a5f5" // biru
	colVI  = "#26c6da" // cyan
	colOB  = "#ff9800" // oranye
	colBB  = "#ab47bc" // ungu
)

// pdrColor mengembalikan warna stroke + fill semi-transparan (alpha rendah)
// untuk satu Kind PD array. Default abu-abu kalau Kind tak dikenal.
func pdrColor(kind string) (stroke, fill string) {
	switch kind {
	case "FVG":
		return colFVG, "rgba(66,165,245,0.07)"
	case "VI":
		return colVI, "rgba(38,198,218,0.07)"
	case "OB":
		return colOB, "rgba(255,152,0,0.07)"
	case "BB":
		return colBB, "rgba(171,71,188,0.07)"
	default:
		return "#888888", "rgba(136,136,136,0.06)"
	}
}

// relevantPDRs menyaring PDR yang ditampilkan supaya chart tidak penuh ratusan
// box: hanya yang SEARAH bias (kandidat POI sejati) — counter-direction bukan
// kandidat — lalu ambil `cap` terdekat ke harga; komponen POI terpilih (InPOI)
// SELALU disertakan. bias kosong/"-" → tampilkan semua (cap by proximity).
func relevantPDRs(n engine.ScanNarrative, maxN int) []engine.PDRMark {
	wantDir := ""
	switch n.Bias {
	case "buy":
		wantDir = "bullish"
	case "sell":
		wantDir = "bearish"
	}
	mid := func(d engine.PDRMark) float64 { return (d.Top + d.Bottom) / 2 }
	dist := func(d engine.PDRMark) float64 {
		x := mid(d) - n.Price
		if x < 0 {
			return -x
		}
		return x
	}
	var cand []engine.PDRMark
	for _, d := range n.PDRs {
		if d.InPOI {
			continue // dimasukkan terpisah di akhir (selalu ada)
		}
		if wantDir != "" && d.Dir != wantDir {
			continue
		}
		cand = append(cand, d)
	}
	sort.Slice(cand, func(i, j int) bool { return dist(cand[i]) < dist(cand[j]) })
	if len(cand) > maxN {
		cand = cand[:maxN]
	}
	// Komponen POI selalu disertakan.
	for _, d := range n.PDRs {
		if d.InPOI {
			cand = append(cand, d)
		}
	}
	return cand
}

// fibLegInWindow melaporkan apakah leg Fibonacci makro layak digambar sebagai
// diagonal: minimal SATU anchor (start ATAU end) jatuh di dalam rentang waktu
// window chart (n.Plot). Kalau kedua anchor di luar window, diagonal akan
// di-clamp ke tepi yang sama → garis vertikal menyesatkan, jadi jangan digambar
// (cukup level horizontal). Window kosong / anchor nol → anggap tak terlihat.
func fibLegInWindow(n engine.ScanNarrative) bool {
	if len(n.Plot) == 0 || n.FibStartTime.IsZero() {
		return false
	}
	t0 := n.Plot[0].Time
	t1 := n.Plot[len(n.Plot)-1].Time
	inWin := func(t time.Time) bool {
		return !t.Before(t0) && !t.After(t1)
	}
	return inWin(n.FibStartTime) || inWin(n.FibEndTime)
}

// Build menyusun anotasi LENGKAP dari narasi. includeFibLevels=false menahan
// level 0/1 + leg (berguna kalau Fib makro jauh di luar layar → bikin skala
// chart melebar; EQ tetap digambar kalau masuk akal).
func Build(n engine.ScanNarrative, includeFibLevels bool) viz.Annotations {
	var ann viz.Annotations

	// PD array boxes (FVG/VI/OB/BB) untuk verifikasi visual pemilihan POI. Window
	// bisa berisi ratusan PDR → tampilkan hanya yang RELEVAN: searah bias (kandidat
	// POI sejati) + terdekat ke harga (cap), plus SEMUA komponen POI terpilih
	// (InPOI). Warna per Kind; box POI di-emphasize. POI zone prominent di atasnya.
	// poiT0 = waktu komponen POI (InPOI) paling awal → anchor kotak POI.
	poiT0 := n.At
	for _, d := range relevantPDRs(n, 12) {
		stroke, fill := pdrColor(d.Kind)
		arrow := "↑"
		if d.Dir == "bearish" {
			arrow = "↓"
		}
		ann.Boxes = append(ann.Boxes, viz.PDBox{
			T0:        d.Time,
			Low:       d.Bottom,
			High:      d.Top,
			Label:     d.Kind + arrow,
			Stroke:    stroke,
			Fill:      fill,
			Emphasize: d.InPOI,
		})
		if d.InPOI && d.Time.Before(poiT0) {
			poiT0 = d.Time
		}
	}

	// Zona POI digambar sebagai KOTAK ber-anchor waktu (bukan pita full-width) +
	// label highlight (pill accent, teks putih tebal). Hijau buy/discount, merah sell/premium.
	if n.HasPOI {
		fill, accent := "rgba(38,166,154,0.18)", "#26a69a"
		if n.Bias == "sell" {
			fill, accent = "rgba(239,83,80,0.18)", "#ef5350"
		}
		ann.Zones = append(ann.Zones, viz.Zone{
			Low: n.POIBottom, High: n.POITop,
			Label: fmt.Sprintf("POI %.2f–%.2f", n.POIBottom, n.POITop),
			Fill:  fill, T0: poiT0, Accent: accent,
		})
	}

	// Fibonacci: leg (segmen dari start→end) + level 0/0.5/1.
	if n.HasFib {
		ann.Levels = append(ann.Levels, viz.Level{Price: n.FibEq, Label: "Fib 0.5 (EQ)", Color: colEQ, Dash: true})
		if includeFibLevels {
			// Leg Fib makro sering ditarik dari swing weekly/daily yang terjadi
			// JAUH sebelum window chart (~120 candle H1 terakhir). Kalau KEDUA
			// anchor leg di luar window, segmen diagonal akan di-clamp ke tepi
			// kiri/kanan oleh viz → kolaps jadi garis vertikal di pinggir yang
			// MENYESATKAN. Maka: gambar diagonal HANYA bila minimal satu anchor
			// berada di dalam window; selain itu cukup level horizontal + tandai
			// label "(di luar layar)" supaya tetap terbaca tanpa garis ngaco.
			legVisible := fibLegInWindow(n)
			fibNote := ""
			if !legVisible {
				fibNote = " (di luar layar)"
			}
			ann.Levels = append(ann.Levels,
				viz.Level{Price: n.FibLow, Label: "Fib 0.0" + fibNote, Color: colFib, Dash: true},
				viz.Level{Price: n.FibHigh, Label: "Fib 1.0" + fibNote, Color: colFib, Dash: true},
			)
			if !n.FibStartTime.IsZero() && legVisible {
				ann.Segments = append(ann.Segments, viz.Segment{
					T0: n.FibStartTime, P0: n.FibStartPrice,
					T1: n.FibEndTime, P1: n.FibEndPrice,
					Label: fmt.Sprintf("Fib leg %s (%.2f→%.2f)", n.FibDir, n.FibStartPrice, n.FibEndPrice),
					Color: colFib, Dash: true,
				})
			}
		}
	}

	// Midnight Open (pertemuan 8): garis horizontal divider premium/discount
	// hari ini (open candle 00:00 NY). Solid oranye — beda dari Fib (dash ungu).
	if n.HasMO {
		const colMO = "#ff6d00"
		ann.Levels = append(ann.Levels, viz.Level{
			Price: n.MOPrice,
			Label: "MO 00:00 NY", // harga ditambahkan drawLevel di ujung kanan
			Color: colMO,
		})
	}

	// REH/REL (pertemuan 1-2 + 10, fractal H1/H4/D): level liquidity equal yang
	// belum disapu — garis cyan dash + marker kecil di tiap swing penyusun
	// (verifikasi visual grup "double top/bottom"-nya nyata). Marker hanya
	// digambar bila waktunya masih DI DALAM window plot (swing HTF bisa jauh
	// sebelum window → garis level tetap, marker di-skip supaya tak clamp ngaco).
	const colRelEq = "#00acc1"
	for _, m := range n.RelEquals {
		ann.Levels = append(ann.Levels, viz.Level{
			Price: m.Level,
			Label: fmt.Sprintf("%s %s (%d×)", m.Kind, m.TF.String(), m.Count), // harga oleh drawLevel
			Color: colRelEq,
			Dash:  true,
		})
		for _, s := range m.Swings {
			if len(n.Plot) > 0 && s.Time.Before(n.Plot[0].Time) {
				continue
			}
			ann.Markers = append(ann.Markers, viz.Marker{
				Time: s.Time, Price: s.Price, Label: m.Kind,
				Color: colRelEq, Up: m.Kind == "REL",
			})
		}
	}

	// Marker ITH/ITL (intermediate) di window.
	for _, s := range n.Swings {
		if s.Kind == "ITL" {
			ann.Markers = append(ann.Markers, viz.Marker{Time: s.Time, Price: s.Price, Label: "ITL", Color: colITL, Up: true})
		} else {
			ann.Markers = append(ann.Markers, viz.Marker{Time: s.Time, Price: s.Price, Label: "ITH", Color: colITH, Up: false})
		}
	}

	// Pivot trigger 5m (highlight).
	if n.HasTrig {
		ann.Markers = append(ann.Markers, viz.Marker{
			Time: n.TrigTime, Price: n.TrigPrice,
			Label: "TRIG " + n.TrigKind, Color: colTrig, Up: n.TrigKind == "ITL",
		})
	}

	// Entry / SL / TP.
	if n.HasEntry {
		ann.Levels = append(ann.Levels,
			viz.Level{Price: n.Entry, Label: "ENTRY", Color: colEntry},
			viz.Level{Price: n.SL, Label: "SL", Color: colSL, Dash: true},
			viz.Level{Price: n.TP, Label: "TP", Color: colTP, Dash: true},
		)
	}
	return ann
}
