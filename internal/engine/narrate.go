// Narrative engine: untuk satu titik waktu, hasilkan NARASI langkah-demi-langkah
// (Bahasa Indonesia) bagaimana engine memindai piramida TDA→DB→AMS→QT→POI→Entry,
// plus level harga (POI/Fib/entry/SL/TP) untuk divisualisasikan.
//
// Berbeda dari evaluate() (engine.go) yang exit dini begitu satu gate gagal:
// Narrate TIDAK exit dini — tiap gate dicatat (pass/fail) dan scan diteruskan
// sejauh mungkin supaya narasi tetap kaya walau setup batal. Ini meniru cara
// seorang trader "ngomong" sambil membaca chart.

package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/state"
)

// StepStatus = hasil satu langkah scan (untuk pewarnaan UI).
type StepStatus int

const (
	StepPass StepStatus = iota // gate lolos
	StepFail                   // gate gagal (jadi alasan NO SETUP)
	StepInfo                   // informatif (bukan gate hard)
)

// StepTrace = satu baris narasi langkah scan.
type StepTrace struct {
	Name   string // mis. "1. TDA — Weekly Order Flow"
	Status StepStatus
	Text   string // narasi Indonesia, 1-3 kalimat
}

// ScanNarrative = hasil narasi + level harga pada satu titik waktu `at`.
// AMSStruct = ringkasan ITL/ITH ter-konfirmasi TERBARU di 1H (per arah, independen
// bias) — dipakai alertd untuk mendeteksi perubahan AMS (terbentuk / di-break).
type AMSStruct struct {
	Present   bool      // ada ITL/ITH ter-konfirmasi terbaru di 1H
	Pivot     float64   // harga pivot
	PivotTime time.Time // identitas unik intermediate → deteksi "terbentuk baru"
	Active    bool      // belum di-break ke arah pembalik
	Type      string    // "standard" | "fast_early"
}

type ScanNarrative struct {
	At       time.Time
	Steps    []StepTrace
	Bias     string // "buy" | "sell" | "-"
	HasSetup bool   // true kalau semua gate lolos & ada entry valid
	Decision string // ringkas, mis. "ENTRY BUY @ ..." atau "NO SETUP — ..."

	HasPOI        bool
	POITop        float64
	POIBottom     float64
	POITF         detectors.TFKind // TF asal POI terpilih (fractal multi-TF)
	POIComponents []PDRMark        // komponen penyusun POI terpilih (untuk -poi)

	// Next-POI: saat TIDAK ada POI ter-tag/disentuh, ini POI valid TERDEKAT searah
	// bias di zona benar — "harga berapa yang ditunggu". NextPOIDistance = jarak
	// harga sekarang ke tepi POI terdekat (0 kalau sudah di dalam zona).
	HasNextPOI      bool
	NextPOITop      float64
	NextPOIBottom   float64
	NextPOITier     int
	NextPOIDistance float64

	// NextPOIsByTF: daftar pantauan multi-TF — POI valid TERDEKAT per timeframe
	// (H1/H4/D) untuk DUA arah: searah bias (zona setup valid) dan counter-bias
	// (info pemantauan bila bias berbalik). Terisi hanya saat step POI dievaluasi.
	NextPOIsByTF []NextPOITF

	HasFib  bool
	FibLow  float64
	FibHigh float64
	FibEq   float64

	// Midnight Open (pertemuan 8): open candle 00:00 NY trading day berjalan —
	// divider premium/discount harian. HasMO=false saat MO belum terbentuk
	// (sesi Asia 18:00–00:00 NY) atau gap data.
	HasMO   bool
	MOPrice float64
	MOTime  time.Time

	// RelEquals = REH/REL terdekat (pertemuan 1-2 + 10): grup 2+ swing hampir
	// selevel yang BELUM disapu = liquidity target. Maks 2 entri: REH terdekat
	// DI ATAS harga + REL terdekat DI BAWAH.
	RelEquals []RelEqualMark

	// Klasifikasi sesi Asia trading day berjalan (Layer D): A (akumulasi →
	// ekspektasi AMDX) vs X (sudah expansive → XAMD). AsiaScenario hanya
	// bermakna saat AsiaClosed (final setelah 00:00 NY, stabil s/d 18:00 NY).
	AsiaClosed   bool
	AsiaScenario detectors.Scenario
	AsiaHasFVG   bool // ada FVG 1H di sesi Asia (untuk catatan: X tanpa FVG = penentu murni true-move)

	HasEntry bool
	Entry    float64
	SL       float64
	TP       float64
	Lot      float64

	// Anchor leg Fibonacci makro (weekly impulse) — "ditarik dari mana ke mana".
	FibStartTime  time.Time
	FibStartPrice float64
	FibEndTime    time.Time
	FibEndPrice   float64
	FibDir        string // "bullish" | "bearish" (arah leg)

	// ITH/ITL (intermediate) di dalam window Plot (H1) — untuk ditandai di chart.
	Swings []SwingMark

	// PDRs = SEMUA komponen PD Array (FVG/VI/OB/BB) di window POI — bukan hanya
	// POI terpilih. Berguna untuk verifikasi visual apakah pemilihan POI benar.
	PDRs []PDRMark

	// Pivot trigger entry 5m (ITL utk buy / ITH utk sell).
	HasTrig   bool
	TrigKind  string // "ITL" | "ITH"
	TrigTime  time.Time
	TrigPrice float64

	Price float64       // harga (close H1) pada `at`
	Plot  []data.Candle // window H1 untuk visualisasi: ~120 candle terakhir s/d `at`

	// --- Ringkasan untuk seksi FormatWatchlist (pantauan ringkas, user 2026-06-03) ---
	// Diisi sepanjang Narrate; dipakai formatter watchlist 6-seksi (bukan narasi
	// piramida penuh). Semua nilai mirror step yang sama → 0 drift.
	WeeklyOFDir     string    // arah TDA: "bullish" | "bearish" | "" (OF undefined)
	DailyBiasNote   string    // keterangan ringkas daily bias (= teks step DB)
	AMSChecked      bool      // step AMS sempat dievaluasi (bias sudah terdefinisi)
	AMSActive       bool      // ITL/ITH searah bias aktif di 1H & belum di-break
	AMSKind         string    // "ITL" | "ITH" (yang dicari sesuai bias)
	AMSPivot        float64   // harga pivot AMS (saat AMSActive)
	AMSITL          AMSStruct // ITL ter-konfirmasi terbaru di 1H (dua arah, utk alert AMS)
	AMSITH          AMSStruct // ITH ter-konfirmasi terbaru di 1H (dua arah, utk alert AMS)
	QTSession       string    // session.String() pada candle keputusan
	QTLondonQ       int       // quarter London 1-4; 0 kalau di luar sesi London
	QTSkenario      string    // "amdx" | "xamd" | "" (weekly QT — diisi HANYA setelah Senin close, D.4)
	QTWeeklyPending bool      // true = weekly QT belum final (candle Senin belum close) → tampil "(menunggu Senin close)"
	QTWeeklyPhase   string    // "A"/"M"/"D"/"X"/"Special"
	QTDailyPhase    string
	QTDayType       string
	QTWeekday       string
	QTSessionQ      int // sub-kuartal sesi saat ini (1-4)
	// QT Session — kuartal sesi saat ini + window + fase (fractal AMD dari Q1 sesi).
	QTSessName     string // "Asia" | "London" | "New York" | "PM Session" (sesi saat ini)
	QTSessQStartNY string // window kuartal saat ini, mis. "22:30"
	QTSessQEndNY   string // mis. "00:00"
	QTSessPhase    string // fase kuartal saat ini: "A"/"M"/"D"/"X"; "" = Q1 berjalan/data kurang
	// QT Daily — fase sesi saat ini + skenario harian (anchor Asia, setelah Asia close).
	QTDailyScenario  string // "AMDX" | "XAMD" | "" (kosong sebelum Asia close)
	QTDailySessPhase string // fase sesi saat ini dalam AMD harian: "A"/"M"/"D"/"X"
	// Q1 sesi saat ini — klasifikasi A/X dari M15 window [SessionStart, +90m)
	QTQ1Scenario string // "A" | "X" | "" (kosong jika in progress atau data kurang)
	// Monthly AMD context — level tertinggi: minggu dalam bulan.
	MonthlyScenario string    // "amdx" | "xamd" | ""
	MonthlyPhase    string    // fase minggu saat ini dalam monthly AMD
	MonthlyWeekNum  int       // 1-4
	ArrayByTF       []TFArray // komponen PD Array hidup per TF (H1/H4/D)
}

// TFArray = komponen PD Array hidup (belum jebol) pada satu timeframe, untuk
// seksi "Komponen Array" watchlist. PDRs belum dipangkas — formatter yang memilih
// yang terdekat ke harga.
type TFArray struct {
	TF   detectors.TFKind
	PDRs []PDRMark
}

// RelEqualMark = satu level REH/REL untuk narasi/watchlist/chart.
type RelEqualMark struct {
	Kind     string           // "REH" | "REL"
	TF       detectors.TFKind // timeframe asal grup (H1/H4/D — fractal seperti POI)
	Level    float64          // ekstrem grup (sweep harus melewati semua)
	Count    int              // jumlah swing penyusun
	Distance float64          // jarak harga sekarang ke level
	Swings   []SwingMark
}

// SwingMark = satu titik intermediate (ITH/ITL) di window Plot untuk anotasi chart.
type SwingMark struct {
	Time  time.Time
	Price float64
	Kind  string // "ITH" | "ITL"
}

// PDRMark = satu komponen PD Array di window POI untuk anotasi chart.
type PDRMark struct {
	Kind   string // "FVG" | "VI" | "OB" | "BB"
	Dir    string // "bullish" | "bearish"
	Top    float64
	Bottom float64
	Time   time.Time // waktu candle pembentuk (acuan)
	InPOI  bool      // true kalau komponen ini bagian dari POI terpilih
}

// poiComponentMarks mengubah komponen POI terpilih jadi PDRMark (pakai PDR.Time
// yang sudah diisi DetectPDRs) — dipakai dump -poi lintas-TF.
func poiComponentMarks(poi detectors.POI) []PDRMark {
	out := make([]PDRMark, 0, len(poi.Components))
	for _, c := range poi.Components {
		out = append(out, PDRMark{
			Kind: c.Kind.String(), Dir: c.Dir.String(),
			Top: c.Top, Bottom: c.Bottom, Time: c.Time, InPOI: true,
		})
	}
	return out
}

// Narrate memindai piramida pada titik waktu `at` dan menghasilkan narasi
// langkah-demi-langkah + level harga. Berbeda dari evaluate(): TIDAK exit dini —
// tiap gate dicatat (pass/fail) dan scan diteruskan sejauh mungkin untuk narasi kaya.
func Narrate(tf TFData, cfg Config, at time.Time) ScanNarrative {
	loc, err := detectors.NYLocation()
	if err != nil {
		loc = time.UTC
	}

	n := ScanNarrative{At: at, Bias: "-"}

	// ---- Step 0: cari candle 1H terakhir dengan Time <= at ----
	i := lastIdxLE(tf.H1, at)
	if i < 0 {
		n.Steps = append(n.Steps, StepTrace{
			Name:   "0. Data",
			Status: StepInfo,
			Text:   "Data candle 1H belum cukup pada titik waktu ini — tidak ada candle yang sudah close di " + at.In(loc).Format("2006-01-02 15:04") + " NY.",
		})
		n.Decision = "NO SETUP — data belum cukup"
		return n
	}
	now := tf.H1[i].Time.Add(time.Hour) // close candle 1H ini
	n.Price = tf.H1[i].Close
	n.Plot = window(tf.H1[:i+1], 120)

	// ITH/ITL di dalam window Plot (H1) — selalu bisa diisi untuk anotasi chart.
	// Bug#3: pakai filter magnitude (minLeg = MarksMinATRMult × ATR_H1) supaya
	// hanya pivot signifikan (ekstrem lokal nyata) yang ditandai — bukan micro-pivot
	// 3-bar yang bikin mark "ITL salah tempat".
	marksMinLeg := 0.0
	if cfg.MarksMinATRMult > 0 && len(n.Plot) > cfg.ATRPeriod {
		if atr, ok := detectors.ATRAt(n.Plot, len(n.Plot)-1, cfg.ATRPeriod); ok {
			marksMinLeg = cfg.MarksMinATRMult * atr
		}
	}
	marks := detectors.DetectIntermediatesMin(n.Plot, cfg.BreakType, marksMinLeg)
	if cfg.AMSStrictStructure {
		marks = detectors.DetectIntermediatesStrictMin(n.Plot, cfg.BreakType, marksMinLeg)
	}
	for _, it := range marks {
		n.Swings = append(n.Swings, SwingMark{
			Time:  it.Pivot.Time,
			Price: it.Pivot.Price,
			Kind:  it.Kind.String(),
		})
	}

	h1Closed := tf.H1[:i+1]
	weekly := closedBy(tf.Weekly, now)
	daily := closedBy(tf.Daily, now)

	// firstFail menyimpan alasan gate pertama yang gagal (untuk Decision NO SETUP).
	firstFail := ""
	recordFail := func(reason string) {
		if firstFail == "" {
			firstFail = reason
		}
	}

	// ============================================================
	// Step 1 — TDA: Weekly Order Flow
	// ============================================================
	if len(weekly) < 3 {
		n.Steps = append(n.Steps, StepTrace{
			Name:   "1. TDA — Weekly Order Flow",
			Status: StepFail,
			Text:   fmt.Sprintf("Candle weekly belum cukup (%d candle) untuk menentukan order flow definitif.", len(weekly)),
		})
		recordFail("weekly belum cukup")
		n.Decision = "NO SETUP — " + firstFail
		return n
	}

	minLegW := ofMinLeg(weekly, cfg) // identik dgn evaluate (fix #5)
	ofDir, anchors, ofFlip, ofOK := state.WeeklyOFFull(weekly, minLegW, cfg.OFBearConfirmWeeks)
	if !ofOK {
		n.Steps = append(n.Steps, StepTrace{
			Name:   "1. TDA — Weekly Order Flow",
			Status: StepFail,
			Text:   "Weekly order flow masih UNDEFINED (belum ada leg impulse jelas di mingguan). Tanpa OF master, seluruh piramida tidak bisa di-bias.",
		})
		recordFail("weekly OF undefined")
		n.Decision = "NO SETUP — " + firstFail
		return n
	}

	// Invalidation guard (LTL utk OF bullish / LTH utk OF bearish).
	invalidated := false
	switch ofDir {
	case detectors.Bullish:
		invalidated = anchors.HasLTL && n.Price < anchors.LTL.Price
	case detectors.Bearish:
		invalidated = anchors.HasLTH && n.Price > anchors.LTH.Price
	}

	ofText := fmt.Sprintf("Weekly OF %s.", ofDir.String())
	n.WeeklyOFDir = ofDir.String() // ringkasan TDA untuk watchlist
	switch ofDir {
	case detectors.Bullish:
		if anchors.HasLTL {
			ofText += fmt.Sprintf(" LTL aktif (invalidation) di %.2f — selama harga (%.2f) bertahan di atasnya, struktur bullish valid.", anchors.LTL.Price, n.Price)
		} else {
			ofText += " Belum ada LTL aktif sebagai garis invalidation."
		}
		if anchors.HasLTH {
			ofText += fmt.Sprintf(" LTH di %.2f jadi likuiditas target di atas.", anchors.LTH.Price)
		}
	case detectors.Bearish:
		if anchors.HasLTH {
			ofText += fmt.Sprintf(" LTH aktif (invalidation) di %.2f — selama harga (%.2f) bertahan di bawahnya, struktur bearish valid.", anchors.LTH.Price, n.Price)
		} else {
			ofText += " Belum ada LTH aktif sebagai garis invalidation."
		}
		if anchors.HasLTL {
			ofText += fmt.Sprintf(" LTL di %.2f jadi likuiditas target di bawah.", anchors.LTL.Price)
		}
	}
	// Umur OF (gate MaxOFAgeDays — mirror evaluate persis).
	ofAgeDays := int(now.Sub(ofFlip).Hours() / 24)
	ofStale := cfg.MaxOFAgeDays > 0 && now.Sub(ofFlip) > time.Duration(cfg.MaxOFAgeDays)*24*time.Hour
	ofText += fmt.Sprintf(" Umur OF %d hari sejak flip.", ofAgeDays)
	switch {
	case ofStale:
		ofText += fmt.Sprintf(" OF sudah TERLALU TUA (> %d hari) — impulse kemungkinan kehabisan tenaga, skip (gate OF-maturity).", cfg.MaxOFAgeDays)
		n.Steps = append(n.Steps, StepTrace{Name: "1. TDA — Weekly Order Flow", Status: StepFail, Text: ofText})
		recordFail("weekly OF terlalu tua")
	case invalidated:
		ofText += " NAMUN level invalidation sudah ditembus harga — OF aktif sedang ter-invalidasi, jangan ambil setup."
		n.Steps = append(n.Steps, StepTrace{Name: "1. TDA — Weekly Order Flow", Status: StepFail, Text: ofText})
		recordFail("invalidation level (LTL/LTH) sudah tersentuh")
	default:
		n.Steps = append(n.Steps, StepTrace{Name: "1. TDA — Weekly Order Flow", Status: StepPass, Text: ofText})
	}

	// ============================================================
	// Regime (Kunci #2): impulse vs retrace → bias.
	// Komputasi TETAP (dipakai downstream DB/POI/Entry), tapi BUKAN node narasi
	// terpisah — asal bias dijelaskan menyatu di langkah DB di bawah (piramida
	// kanonik: TDA → DB langsung, tanpa layer regime di tengah).
	// ============================================================
	regime := state.ComputeRegimeMin(weekly, ofDir, cfg.RetraceFVGGate, cfg.MinGapPips, minLegW)
	if cfg.ImpulseOnly {
		regime = state.RegimeImpulse
	}
	// SkipRetrace: mirror evaluate — regime retrace = NO TRADE (di-skip).
	retraceSkipped := cfg.SkipRetrace && regime == state.RegimeRetrace
	if retraceSkipped {
		n.Steps = append(n.Steps, StepTrace{
			Name:   "1b'. Regime — Kunci #2",
			Status: StepFail,
			Text:   "Regime RETRACE (leg korektif lawan weekly OF) dan SkipRetrace aktif — periode retrace tidak ditradingkan (subset retrace PF~0.3). Skip.",
		})
		recordFail("regime retrace (di-skip)")
	}
	// Pre-compute daily bias + Skenario B (mirror engine STEP 1.5).
	// Harus SEBELUM bias final ditentukan agar early override masuk ke biasNote.
	dailyBias, biasFlip, dailyRefLevel, dailyOK := state.DailyBiasRef(daily, biasMinLeg(daily, cfg))
	biasAgeReq := biasAgeMin(cfg, dailyBias)
	biasFresh := dailyOK && biasAgeReq > 0 && now.Sub(biasFlip) < time.Duration(biasAgeReq)*24*time.Hour
	isSkenarioB := false
	var skenarioBDir detectors.Direction
	if dailyOK && regime == state.RegimeImpulse {
		isSkenarioB, skenarioBDir = state.DetectSkenarioB(anchors, ofDir, dailyBias, dailyRefLevel)
	}

	bias := state.TradeDirection(ofDir, regime)
	if isSkenarioB && cfg.AllowEarlyFlip {
		bias = skenarioBDir // override bias early flip (mirror evaluate)
	}
	if bias == detectors.Bullish {
		n.Bias = "buy"
	} else {
		n.Bias = "sell"
	}
	var biasNote string
	switch {
	case isSkenarioB && cfg.AllowEarlyFlip:
		biasNote = fmt.Sprintf("Bias %s (B.3 Skenario B — early flip counter weekly OF %s definitif). ", n.Bias, ofDir.String())
	case isSkenarioB:
		biasNote = fmt.Sprintf("Bias %s (searah weekly OF %s, regime impulse). [Skenario B terdeteksi — AllowEarlyFlip=false, early entry dinonaktifkan]. ", n.Bias, ofDir.String())
	case regime == state.RegimeImpulse:
		biasNote = fmt.Sprintf("Bias %s (searah weekly OF %s, regime impulse). ", n.Bias, ofDir.String())
	default:
		biasNote = fmt.Sprintf("Bias %s (leg korektif lawan weekly OF %s, koreksi ke weekly FVG). ", n.Bias, ofDir.String())
	}

	// ============================================================
	// Step 2 — Daily Bias alignment (strict) / Skenario B (early)
	// ============================================================
	dailyAligned := dailyOK && dailyBias == bias && !biasFresh
	var dbText string
	stepName := "2. DB — Daily Bias"
	if !dailyOK {
		dbText = biasNote + "Daily bias belum terdefinisi (belum ada close-break swing daily terakhir). Tunggu konfirmasi harian."
		n.Steps = append(n.Steps, StepTrace{Name: stepName, Status: StepFail, Text: dbText})
		recordFail("daily bias belum terdefinisi")
	} else if isSkenarioB {
		// Skenario B terdeteksi — early path atau diblokir (AllowEarlyFlip)
		stepName = "2. DB — Daily Bias (Skenario B)"
		biasAgeDays := int(now.Sub(biasFlip).Hours() / 24)
		switch {
		case biasFresh:
			dbText = biasNote + fmt.Sprintf("Skenario B terdeteksi (daily %s, refLevel %.2f < LTH/LTL), TAPI bias baru flip %d hari lalu (< %d hari) — whipsaw belum matang, skip.",
				dailyBias.String(), dailyRefLevel, biasAgeDays, biasAgeReq)
			n.Steps = append(n.Steps, StepTrace{Name: stepName, Status: StepFail, Text: dbText})
			recordFail("skenario B early — daily bias baru flip")
		case !cfg.AllowEarlyFlip:
			dbText = biasNote + fmt.Sprintf("Skenario B terdeteksi (daily %s, refLevel %.2f, umur %d hari) — AllowEarlyFlip=false, entry early dinonaktifkan. Skip (perlakuan sama dengan alignment mismatch).",
				dailyBias.String(), dailyRefLevel, biasAgeDays)
			n.Steps = append(n.Steps, StepTrace{Name: stepName, Status: StepFail, Text: dbText})
			recordFail("skenario B tapi AllowEarlyFlip=false")
		default:
			dbText = biasNote + fmt.Sprintf("Skenario B terkonfirmasi: daily %s break intermediate swing %.2f (< LTH/LTL), umur %d hari — early entry diizinkan (G.4 FlipTimingEarly).",
				dailyBias.String(), dailyRefLevel, biasAgeDays)
			n.Steps = append(n.Steps, StepTrace{Name: stepName, Status: StepPass, Text: dbText})
		}
	} else if dailyBias != bias {
		dbText = biasNote + fmt.Sprintf("Daily bias %s TIDAK align dengan bias engine %s — harian sedang lawan arah, skip (alignment strict).", dailyBias.String(), n.Bias)
		n.Steps = append(n.Steps, StepTrace{Name: stepName, Status: StepFail, Text: dbText})
		recordFail("daily bias tidak align")
	} else if biasFresh {
		dbText = biasNote + fmt.Sprintf("Daily bias %s align, TAPI baru flip %d hari lalu (< %d hari) — whipsaw belum matang, tunggu bias settle (gate bias-maturity).",
			dailyBias.String(), int(now.Sub(biasFlip).Hours()/24), biasAgeReq)
		n.Steps = append(n.Steps, StepTrace{Name: stepName, Status: StepFail, Text: dbText})
		recordFail("daily bias baru flip (belum matang)")
	} else {
		dbText = biasNote + fmt.Sprintf("Daily bias %s align dengan bias engine %s (umur %d hari) — harian mendukung arah, lanjut.",
			dailyBias.String(), n.Bias, int(now.Sub(biasFlip).Hours()/24))
		n.Steps = append(n.Steps, StepTrace{Name: stepName, Status: StepPass, Text: dbText})
	}
	n.DailyBiasNote = dbText // keterangan ringkas untuk watchlist
	_ = dailyAligned         // dipakai downstream untuk ringkasan watchlist

	// ============================================================
	// Step 3 — AMS: struktur intermediate 1H searah bias (Layer 3, C.4)
	// ============================================================
	// Window 1H SAMA dengan engine.evaluate (window(h1Closed, cfg.H1Window))
	// supaya gate AMS identik (0 DRIFT). Buy → wajib ITL aktif; sell → ITH aktif.
	amsIt, amsOK := detectors.ActiveIntermediate(window(h1Closed, cfg.H1Window), amsKind(bias), cfg.BreakType, 0, cfg.AMSStrictStructure)
	wantKind := amsKind(bias).String()
	// Ringkasan AMS untuk watchlist (konfirmasi ITL/ITH 1H terbentuk atau belum).
	n.AMSChecked, n.AMSActive, n.AMSKind = true, amsOK, wantKind
	if amsOK {
		n.AMSPivot = amsIt.Pivot.Price
	}
	// AMS dua arah (ITL & ITH, independen bias) untuk alert perubahan AMS di alertd.
	// Window + breakType + minLeg=0 identik gate AMS supaya set ITL/ITH yang dialert =
	// yang dipertimbangkan gate. ActiveIntermediate mengembalikan yang TERBARU per kind:
	// ok=true → masih aktif; ok=false → ada tapi sudah di-break (Present tetap true) atau
	// tak ada sama sekali (Pivot.Time zero → Present false).
	amsWin := window(h1Closed, cfg.H1Window)
	if itl, itlActive := detectors.ActiveIntermediate(amsWin, detectors.ITLow, cfg.BreakType, 0, cfg.AMSStrictStructure); !itl.Pivot.Time.IsZero() {
		n.AMSITL = AMSStruct{Present: true, Pivot: itl.Pivot.Price, PivotTime: itl.Pivot.Time, Active: itlActive, Type: itl.Type.String()}
	}
	if ith, ithActive := detectors.ActiveIntermediate(amsWin, detectors.ITHigh, cfg.BreakType, 0, cfg.AMSStrictStructure); !ith.Pivot.Time.IsZero() {
		n.AMSITH = AMSStruct{Present: true, Pivot: ith.Pivot.Price, PivotTime: ith.Pivot.Time, Active: ithActive, Type: ith.Type.String()}
	}
	if !cfg.AMSGate {
		// Gate OFF: AMS jadi PENANDA kualitas (tidak memblokir). Tampilkan tetap.
		if amsOK {
			amsText := fmt.Sprintf("[PENANDA, gate off] %s aktif di 1H (pivot %.2f, %s) searah bias %s — struktur intraday MENDUKUNG (sinyal lebih kuat). Lanjut.",
				wantKind, amsIt.Pivot.Price, amsIt.Type.String(), n.Bias)
			n.Steps = append(n.Steps, StepTrace{Name: "3. AMS — Struktur Intermediate (1H)", Status: StepPass, Text: amsText})
		} else {
			amsText := fmt.Sprintf("[PENANDA, gate off] Belum ada %s aktif searah bias %s di 1H — struktur intraday belum mendukung (sinyal lebih lemah), TAPI gate AMS off jadi entry tetap lanjut.",
				wantKind, n.Bias)
			n.Steps = append(n.Steps, StepTrace{Name: "3. AMS — Struktur Intermediate (1H)", Status: StepInfo, Text: amsText})
		}
	} else if amsOK {
		amsText := fmt.Sprintf("%s aktif terbentuk di 1H (pivot %.2f, %s) & belum di-break — struktur intraday searah bias %s, fokus arah mingguan (C.4) terkonfirmasi.",
			wantKind, amsIt.Pivot.Price, amsIt.Type.String(), n.Bias)
		n.Steps = append(n.Steps, StepTrace{Name: "3. AMS — Struktur Intermediate (1H)", Status: StepPass, Text: amsText})
	} else {
		amsText := fmt.Sprintf("Belum ada %s aktif searah bias %s di 1H (struktur intermediate belum terbentuk, atau %s terakhir sudah di-break) — fokus arah mingguan (C.4) belum valid, skip.",
			wantKind, n.Bias, wantKind)
		n.Steps = append(n.Steps, StepTrace{Name: "3. AMS — Struktur Intermediate (1H)", Status: StepFail, Text: amsText})
		recordFail(fmt.Sprintf("tak ada %s 1H aktif searah bias", wantKind))
	}

	// ============================================================
	// Step 4 — QT timing (session, scenario, phase, asia, day type)
	// ============================================================
	session := detectors.Session(tf.H1[i].Time, loc)
	wd := detectors.TradingWeekday(tf.H1[i].Time, loc)
	asiaH1, atr1h, asiaPrevClose, asiaClosed := asiaSlice(h1Closed, loc, cfg.ATRPeriod, now)

	// Expose klasifikasi Asia (A vs X) untuk watchlist — hanya saat sudah FINAL
	// (Asia close 00:00 NY); input identik dengan klasifikasi skenario di bawah.
	n.AsiaClosed = asiaClosed
	if asiaClosed {
		n.AsiaScenario = detectors.ClassifyDailyG(asiaH1, asiaAnchor(cfg.AsiaGapAnchor, asiaPrevClose), atr1h, cfg.MinAtrMult, cfg.MinGapPips, cfg.AsiaRangeRatio, cfg.AsiaAXMode, cfg.AsiaRequireFVG)
		n.AsiaHasFVG = len(detectors.DetectFVGs(asiaH1, cfg.MinGapPips)) > 0
	}

	// Monthly AMD context: minggu ke-berapa dalam bulan + klasifikasi berdasar week-1.
	// Independen dari gate QT — selalu dihitung agar tampil di watchlist.
	{
		msc, wkNum, mOK := classifyMonthlyQT(closedBy(tf.H4, now), now, loc, cfg.ATRPeriod, cfg.MinAtrMult)
		n.MonthlyWeekNum = wkNum
		if mOK {
			n.MonthlyScenario = msc.String()
			n.MonthlyPhase = monthlyPhaseForWeekNum(msc, wkNum).String()
		}
	}

	qtPass := true
	var qtText string

	londonQ := detectors.LondonQuarter(tf.H1[i].Time, loc)
	n.QTSession, n.QTLondonQ = session.String(), londonQ
	// Quarter sesi (display) dihitung dari candle M15 TERAKHIR yang sudah close, bukan H1,
	// supaya konsisten dengan alert Q1-close M15: quarter 90m berganti tepat di batasnya
	// (mis. Q1 Asia berakhir 19:30, bukan tertahan resolusi H1 sampai 20:00/21:00). Fallback
	// ke H1 bila M15 kosong. Display-only (gate QT tetap pakai H1 di evaluate) → P&L identik.
	qtRef := tf.H1[i].Time
	for j := len(tf.M15) - 1; j >= 0; j-- {
		if !tf.M15[j].Time.Add(15 * time.Minute).After(now) { // M15 sudah close (Time+15m <= now)
			qtRef = tf.M15[j].Time
			break
		}
	}
	n.QTSessionQ = detectors.SessionQuarter(qtRef, loc)
	n.QTSessName = qtSessionLabel(session)

	// Q1 sesi saat ini dari M15: window [SessionStart, +90m).
	sessSt := detectors.SessionStart(qtRef, loc)
	q1End := sessSt.Add(90 * time.Minute)
	if n.QTSessionQ > 1 && len(tf.M15) > 0 {
		q1M15 := m15InWindow(tf.M15, sessSt, q1End)
		if len(q1M15) >= 2 {
			// Mirror alertd q1Scenario: cari ATR di candle STRICTLY BEFORE q1End
			// (bukan <=) — sama persis dgn c.Time.Before(b) di alertd, 0-drift.
			lastM15 := -1
			for j, c := range tf.M15 {
				if c.Time.Before(q1End) {
					lastM15 = j
				} else {
					break
				}
			}
			var atr15 float64
			if lastM15 >= 0 {
				if atr, ok := detectors.ATRAt(tf.M15, lastM15, cfg.ATRPeriod); ok {
					atr15 = atr
				}
			}
			q1Prev := asiaAnchor(cfg.Q1GapAnchor, prevCloseBefore(tf.M15, sessSt))
			q1sc := detectors.ClassifyDailyG(q1M15, q1Prev, atr15, cfg.MinAtrMult, cfg.MinGapPips, cfg.Q1RangeRatio, cfg.Q1AXMode, false)
			if q1sc == detectors.XAMD {
				n.QTQ1Scenario = "X"
			} else {
				n.QTQ1Scenario = "A"
			}
		}
	}

	// QT Session — window kuartal SAAT INI + fase (fractal AMD dari Q1 sesi).
	// Kuartal sesi 1.5 jam: window = [sessSt + (q-1)·90m, sessSt + q·90m).
	qStart := sessSt.Add(time.Duration(n.QTSessionQ-1) * 90 * time.Minute)
	qEnd := sessSt.Add(time.Duration(n.QTSessionQ) * 90 * time.Minute)
	n.QTSessQStartNY = qStart.In(loc).Format("15:04")
	n.QTSessQEndNY = qEnd.In(loc).Format("15:04")
	// Fase kuartal saat ini = AMD fractal: Q1 sesi (A/X) → AMDX/XAMD, lalu
	// DailyPhase dgn kuartal dipetakan ke posisi sesi (Q1→Asia … Q4→PM).
	if n.QTSessionQ >= 2 && n.QTQ1Scenario != "" {
		sc := detectors.AMDX
		if n.QTQ1Scenario == "X" {
			sc = detectors.XAMD
		}
		n.QTSessPhase = detectors.DailyPhase(sc, quarterToSession(n.QTSessionQ)).String()
	}

	// QT Daily — fase sesi saat ini + skenario harian (anchor Asia, setelah close).
	if n.AsiaClosed {
		n.QTDailyScenario = strings.ToUpper(n.AsiaScenario.String())
		n.QTDailySessPhase = detectors.DailyPhase(n.AsiaScenario, session).String()
	}
	londonMinQ := 3
	if cfg.LondonQ4Only {
		londonMinQ = 4
	}
	// Mirror evaluate: sweep likuiditas Asia berlawanan bias = manipulasi selesai
	// → bypass gate quarter/jam London (LondonSweepEntry).
	londonBypass := cfg.LondonSweepEntry && session == detectors.London &&
		asiaSwept(h1Closed, loc, now, bias)
	if cfg.SessionPMGate && session == detectors.PM {
		qtText = fmt.Sprintf("Sesi PM (Q4, %s NY) — di luar window entry (entry hanya Asia/London/NY-AM). Skip.", session.String())
		qtPass = false
		recordFail("sesi PM (di luar window entry)")
	} else if hourSkipped(tf.H1[i].Time, loc, cfg.SkipEntryHoursNY) &&
		(!cfg.SkipEntryNewsOnly || cfg.NewsSkipHourStarts[tf.H1[i].Time]) {
		// Mode news-only: jam kandidat hanya di-skip bila candle benar-benar memuat
		// rilis USD high-impact. Tanpa news (atau mode off + candidate) → blanket.
		if cfg.SkipEntryNewsOnly {
			qtText = fmt.Sprintf("Jam %02d:00 NY — ada rilis USD high-impact (CPI/NFP) ±08:30 ET. Skip (mode news-only).",
				tf.H1[i].Time.In(loc).Hour())
		} else {
			qtText = fmt.Sprintf("Jam %02d:00 NY masuk daftar skip-entry (%s) — jam rawan rilis berita (mis. 08:30 ET). Skip.",
				tf.H1[i].Time.In(loc).Hour(), cfg.SkipEntryHoursNY)
		}
		qtPass = false
		recordFail("jam entry di-skip (news hour)")
	} else if (cfg.LondonQ34Only || cfg.LondonQ4Only) && session == detectors.London && londonQ < londonMinQ && !londonBypass {
		qtText = fmt.Sprintf("Sesi London Q%d (Phase 2: %s) — manipulasi London (sweep likuiditas Asia berlawanan OF) belum selesai. Aman entry minimal Q%d. Skip.",
			londonQ, map[int]string{1: "akumulasi 00:00-01:30", 2: "manipulasi 01:30-03:00", 3: "ekspansi awal 03:00-04:30"}[londonQ], londonMinQ)
		qtPass = false
		recordFail("London belum mencapai quarter entry minimum")
	} else if cfg.LondonMinHourNY > 0 && session == detectors.London && !londonBypass && tf.H1[i].Time.In(loc).Hour() < cfg.LondonMinHourNY {
		qtText = fmt.Sprintf("Sesi London jam %02d:00 NY — entry London baru diizinkan mulai %02d:00 (buang candle 03:00, sisa manipulasi). Skip.",
			tf.H1[i].Time.In(loc).Hour(), cfg.LondonMinHourNY)
		qtPass = false
		recordFail("London sebelum jam entry minimum")
	} else if cfg.AsiaCloseGate && !asiaClosed {
		qtText = fmt.Sprintf("Sesi %s, tapi sesi Asia trading-day ini BELUM close (00:00 NY) — daily belum bisa diklasifikasi AMDX/XAMD. Tunggu Asia selesai.", session.String())
		qtPass = false
		recordFail("sesi Asia belum close")
	} else {
		// Mirror evaluate(): gate yang OFF tidak memblokir — klasifikasi tetap
		// dihitung (Asia parsial pun dipakai, sama seperti engine) dan kondisi
		// yang tadinya gating ditampilkan sebagai PENANDA (pola AMS).
		// Daily QT (D.2): skenario harian dari sesi Asia (1H). Asia parsial pun dipakai
		// (mirror engine.evaluate) — INI untuk daily phase, BUKAN weekly. Jangan diubah.
		scenario := detectors.ClassifyDailyG(asiaH1, asiaAnchor(cfg.AsiaGapAnchor, asiaPrevClose), atr1h, cfg.MinAtrMult, cfg.MinGapPips, cfg.AsiaRangeRatio, cfg.AsiaAXMode, cfg.AsiaRequireFVG)
		dailyPhase := detectors.DailyPhase(scenario, session)
		// Weekly QT (D.4): klasifikasi candle SENIN di 4H, dinilai SETELAH Senin close,
		// dikunci utk seminggu. Detektor & TF beda dari daily (fractal: daily→1H, weekly→4H).
		// weekOK=false (Senin belum close) → weekly QT belum final → tampil "(menunggu Senin close)".
		weeklyScenario, weekOK := classifyWeeklyQT(closedBy(tf.H4, now), now, loc, cfg.ATRPeriod, cfg.MinAtrMult, cfg.MinGapPips)
		weeklyPhase := detectors.WeeklyPhase(weeklyScenario, wd)
		phaseOK := dailyPhase.Tradeable()
		if cfg.WeeklyPhaseGate {
			phaseOK = phaseOK && weeklyPhase.Tradeable()
		}
		dayType := classifyDayType(h1Closed, daily, loc, cfg, now)
		if weekOK {
			n.QTSkenario = weeklyScenario.String()
			n.QTWeeklyPhase = weeklyPhase.String()
		} else {
			n.QTWeeklyPending = true // candle Senin belum close → weekly QT belum final (D.4)
		}
		n.QTDailyPhase = dailyPhase.String()
		n.QTDayType = dayType.String()
		n.QTWeekday = wd.String()

		weeklyNote := " (weekly phase dimatikan — hanya daily yang gating)"
		if cfg.WeeklyPhaseGate {
			weeklyNote = ""
		}
		if !cfg.QTPhaseGate {
			weeklyNote = " (QTPhaseGate off — phase info saja, tak gating)"
		}
		weeklyDesc := "weekly " + weeklyScenario.String() + " (Sen close)"
		if !weekOK {
			weeklyDesc = "weekly menunggu Senin close"
		}
		qtText = fmt.Sprintf("Sesi %s, hari %s, skenario harian %s → %s, daily phase %s%s.",
			session.String(), wd.String(), scenario.String(), weeklyDesc, dailyPhase.String(), weeklyNote)
		if session == detectors.PM {
			qtText += " [PENANDA, gate off] Sesi PM — SessionPMGate off, entry PM diizinkan."
		}
		if !asiaClosed {
			qtText += " [PENANDA, gate off] Asia belum close — AsiaCloseGate off, klasifikasi pakai data Asia parsial."
		}
		switch {
		case cfg.QTPhaseGate && !phaseOK:
			req := "daily phase perlu M/D"
			if cfg.WeeklyPhaseGate {
				req = "perlu M/D atau Jumat-Special di kedua TF"
			}
			qtText += fmt.Sprintf(" Phase TIDAK tradeable (%s) → skip.", req)
			qtPass = false
			recordFail("phase tidak tradeable")
		case cfg.DayTypeGate && dayType == detectors.DayHeavyAccum:
			qtText += " Day type HEAVY_ACCUM (range sesi terlalu sempit) — pasar akumulasi, jangan trade."
			qtPass = false
			recordFail("day type heavy_accum")
		default:
			note := ""
			if !phaseOK && !cfg.QTPhaseGate {
				note = " [PENANDA, gate off] Phase tidak tradeable, tapi QTPhaseGate off — lanjut."
			}
			qtText += fmt.Sprintf(" Day type %s — timing QT lolos.%s", dayType.String(), note)
		}
	}
	if londonBypass {
		qtText += " [SWEEP-ENTRY] Likuiditas Asia sudah tersapu berlawanan bias — gate jam/quarter London di-bypass (LondonSweepEntry)."
	}
	st := StepPass
	if !qtPass {
		st = StepFail
	}
	n.Steps = append(n.Steps, StepTrace{Name: "4. QT — Timing & Quarter", Status: st, Text: qtText})

	// ============================================================
	// Step 4½ — Midnight Open (pertemuan 8): divider premium/discount
	// ============================================================
	// Mirror evaluate STEP 4.5 persis (helper moDiscountOK yang sama, candle
	// keputusan tf.H1[i].Time yang sama) — 0 drift.
	moPrice, moTime, moOK := detectors.MidnightOpenPrice(h1Closed, tf.H1[i].Time, loc)
	moStatus := StepInfo
	var moText string
	if !moOK {
		moText = "MO (open candle 00:00 NY) trading day ini BELUM terbentuk — masih sesi Asia (18:00–00:00 NY) atau candle 00:00 hilang dari cache. Gate pass-through; divider premium/discount belum tersedia."
	} else {
		n.HasMO = true
		n.MOPrice = moPrice
		n.MOTime = moTime
		sisi, zona := "DI ATAS", "premium"
		if n.Price < moPrice {
			sisi, zona = "DI BAWAH", "discount"
		}
		moText = fmt.Sprintf("MO (open 00:00 NY, %s) di %.2f. Harga %.2f %s MO = zona %s hari ini.",
			moTime.In(loc).Format("2006-01-02"), moPrice, n.Price, sisi, zona)
		pass := moDiscountOK(n.Price, bias, moPrice)
		switch {
		case cfg.MOGate && pass:
			moStatus = StepPass
			moText += fmt.Sprintf(" Bias %s di zona benar (buy butuh discount, sell butuh premium) → gate lolos.", n.Bias)
		case cfg.MOGate:
			moStatus = StepFail
			moText += fmt.Sprintf(" Bias %s di zona SALAH — jangan buy di premium / sell di discount → skip.", n.Bias)
			recordFail("harga di sisi salah Midnight Open")
		default:
			aturanOK := "sisi benar"
			if !pass {
				aturanOK = "sisi SALAH"
			}
			moText += fmt.Sprintf(" [PENANDA, gate off] Aturan MO: buy hanya di bawah MO, sell hanya di atas — bias %s saat ini di %s.", n.Bias, aturanOK)
		}
	}
	n.Steps = append(n.Steps, StepTrace{Name: "4½. MO — Midnight Open", Status: moStatus, Text: moText})

	// ============================================================
	// Step 4¾ — Liquidity REH/REL (pertemuan 1-2 + 10)
	// ============================================================
	// Mirror evaluate STEP 4.6 (helper relEqTolerance + relEqSweepState yang
	// sama; gate tetap H1) — 0 drift. Penanda FRACTAL multi-TF (pola POI):
	// per TF H1/H4/D, REH terdekat di atas + REL terdekat di bawah. Lookback =
	// RelEqLookbackBars CANDLE per TF (horizon waktu otomatis melebar di HTF).
	relTol := relEqTolerance(n.Price, cfg)
	relSrcs := []struct {
		tf      detectors.TFKind
		candles []data.Candle
	}{
		{detectors.TFH1, h1Closed},
		{detectors.TFH4, closedBy(tf.H4, now)},
		{detectors.TFD, daily},
	}
	for _, src := range relSrcs {
		if len(src.candles) < 3 {
			continue
		}
		var nearREH, nearREL *detectors.RelEqualLevel
		for _, g := range detectors.DetectRelEquals(src.candles, relTol, cfg.RelEqLookbackBars, cfg.RelEqMaxGapBars) {
			g := g
			switch {
			case g.Kind == detectors.SwingHigh && g.Level >= n.Price:
				if nearREH == nil || g.Level < nearREH.Level {
					nearREH = &g
				}
			case g.Kind == detectors.SwingLow && g.Level <= n.Price:
				if nearREL == nil || g.Level > nearREL.Level {
					nearREL = &g
				}
			}
		}
		for _, g := range []*detectors.RelEqualLevel{nearREH, nearREL} {
			if g == nil {
				continue
			}
			mark := RelEqualMark{
				Kind: "REL", TF: src.tf, Level: g.Level, Count: g.Count,
				Distance: absPrice(g.Level - n.Price),
			}
			if g.Kind == detectors.SwingHigh {
				mark.Kind = "REH"
			}
			for _, s := range g.Swings {
				mark.Swings = append(mark.Swings, SwingMark{Time: s.Time, Price: s.Price, Kind: mark.Kind})
			}
			n.RelEquals = append(n.RelEquals, mark)
		}
	}
	relState := relEqSweepState(h1Closed, bias, cfg)
	relStatus := StepInfo
	var relText string
	if len(n.RelEquals) == 0 {
		relText = fmt.Sprintf("Tidak ada REH/REL di H1/H4/D (window %d candle per TF) — tidak ada tumpukan liquidity equal yang menonjol.", cfg.RelEqLookbackBars)
	} else {
		var parts []string
		for _, m := range n.RelEquals {
			arah := "di atas"
			if m.Kind == "REL" {
				arah = "di bawah"
			}
			parts = append(parts, fmt.Sprintf("%s %s %.2f (%d×) ~%.2f poin %s",
				m.Kind, m.TF.String(), m.Level, m.Count, m.Distance, arah))
		}
		relText = "Liquidity equal: " + strings.Join(parts, "; ") + "."
	}
	switch {
	case cfg.RelEqSweepGate && relState == "no_sweep":
		relStatus = StepFail
		relText += fmt.Sprintf(" Wait-for-sweep: liquidity searah bias %s BELUM disapu dalam %d candle terakhir → skip (rule: tunggu MM ambil liquidity dulu).", n.Bias, cfg.RelEqSweepFreshBars)
		recordFail("REH/REL belum tersapu (wait-for-sweep)")
	case cfg.RelEqSweepGate:
		relStatus = StepPass
		relText += fmt.Sprintf(" Wait-for-sweep lolos (state: %s).", relState)
	default:
		relText += fmt.Sprintf(" [PENANDA, gate off] Rule wait-for-sweep: tunggu sisi searah bias disapu dulu (state: %s).", relState)
	}
	n.Steps = append(n.Steps, StepTrace{Name: "4¾. Liquidity — REH/REL", Status: relStatus, Text: relText})

	// ============================================================
	// Step 5 — Fibonacci makro + POI / PD Array
	// ============================================================
	// Sumber Fib zona harus konsisten dengan evaluate(): weekly (makro) atau
	// daily (presisi, default). Anchor leg FibStart/End ditarik dari sumber yang
	// sama supaya narasi & chart cocok dengan gate yang dipakai engine.
	fibCandles := weekly
	if cfg.ZoneFibTF == ZoneFibDaily {
		fibCandles = daily
	}
	fib, fibOK := zoneFib(weekly, daily, ofDir, cfg.ZoneFibTF, cfg.MinSwingATRMultHTF, cfg.ATRPeriod, cfg.MinRetracePct, detectors.DefaultMaxCalibration, n.Price)
	if fibOK {
		n.HasFib = true
		n.FibLow = fib.Low
		n.FibHigh = fib.High
		n.FibEq = fib.Equilibrium()

		// Anchor leg Fibonacci makro: hitung impulse langsung untuk dapat titik
		// "dari mana ke mana" (zoneFib hanya balikin Low/High). Filter magnitude
		// = mult × ATR sumber, konsisten dgn zoneFib.
		minLegHTF := 0.0
		if cfg.MinSwingATRMultHTF > 0 && len(fibCandles) > cfg.ATRPeriod {
			if atr, ok := detectors.ATRAt(fibCandles, len(fibCandles)-1, cfg.ATRPeriod); ok {
				minLegHTF = cfg.MinSwingATRMultHTF * atr
			}
		}
		z := detectors.ZigzagMin(detectors.DetectSwings(fibCandles), minLegHTF)
		if imp, ok := detectors.FindValidImpulseZ(z, fibCandles, ofDir, cfg.MinRetracePct, detectors.DefaultMaxCalibration); ok {
			n.FibStartTime = imp.Start.Time
			n.FibStartPrice = imp.Start.Price
			n.FibEndTime = imp.End.Time
			n.FibEndPrice = imp.End.Price
			n.FibDir = imp.Dir.String()
		}
	}

	// FibZoneGate off + Fib tak terbentuk → engine LANJUT tanpa filter zona
	// (macroFib = Fib{} kosong). Mirror di sini supaya narrate 0-drift.
	if !fibOK {
		fib = detectors.Fib{}
	}

	poiPass := false
	var poiText string
	if !fibOK && cfg.FibZoneGate {
		poiText = "Belum bisa membentuk Fibonacci makro dari last-impulse weekly — zona premium/discount tak terdefinisi. Skip POI (FibZoneGate ON)."
		recordFail("fib makro tidak terbentuk")
	} else {
		// zoneInfo = prefix narasi; saat Fib tak terdefinisi (gate off), POI dipilih
		// tanpa filter premium/discount.
		zoneInfo := fmt.Sprintf("Fib makro %.2f–%.2f (EQ %.2f)", fib.Low, fib.High, fib.Equilibrium())
		if !fib.Defined() {
			zoneInfo = "Fib makro tak terbentuk (FibZoneGate off → POI dipilih TANPA filter zona premium/discount)"
		}
		eqZone := "discount (cocok buy)"
		if bias == detectors.Bearish {
			eqZone = "premium (cocok sell)"
		}
		// plotWin = window candle yg dipakai DetectPDRs; PDR.Index RELATIF ke window
		// ini, jadi waktu candle pembentuk = plotWin[p.Index].Time.
		plotWin := window(h1Closed, cfg.H1Window)
		pdrs := dropBPR(dropOB(detectors.DetectPDRs(plotWin, cfg.MinGapPips, cfg.VIMinGapPips, cfg.FVGSwingBreakAdjacency, cfg.BPRMaxDistanceCandles, cfg.IFVGRequireNoSameDirFVG, cfg.OBStrict, cfg.BPRDirectional, cfg.BBRequireDisplacement, cfg.FVGBreakGeometric), cfg.DisableOB), cfg.DisableBPR)
		pdrs = detectors.FilterLivePDRs(plotWin, pdrs, cfg.POIBreakWick) // buang PDR yg sudah di-break, kecuali IFVG — konsisten dgn engine

		var pois []detectors.POI
		if cfg.FractalPOI {
			h4 := window(closedBy(tf.H4, now), cfg.H1Window)
			dWin := window(daily, cfg.H1Window)
			pois = fractalPOIs(plotWin, h4, dWin, weekly, cfg) // identik dgn engine
			// Daftar pantauan multi-TF dihitung SEBELUM AgendaGate menyempitkan
			// pois ke satu target — pantauan harus lihat semua kandidat.
			n.NextPOIsByTF = nextPOIsByTF(pois, n.Price, fib, bias)
			// Komponen Array per-TF (watchlist) — PDR hidup di tiap TF, sumber
			// deteksi identik dgn buildTFPOIs (0 drift).
			n.ArrayByTF = []TFArray{
				{TF: detectors.TFH1, PDRs: liveTFPDRMarks(plotWin, cfg)},
				{TF: detectors.TFH4, PDRs: liveTFPDRMarks(h4, cfg)},
				{TF: detectors.TFD, PDRs: liveTFPDRMarks(dWin, cfg)},
			}
			if cfg.AgendaGate {
				if target, okT := agendaTarget(pois, n.Price, bias, cfg.AgendaNearest); okT {
					pois = []detectors.POI{target}
				}
			}
		} else {
			pois = filterPOIs(detectors.FilterPOITier(detectors.BuildPOIs(pdrs, cfg.ConfluenceMin, poiMaxWidth(plotWin, cfg)), cfg.MaxPOITier), cfg)
			n.NextPOIsByTF = nextPOIsByTF(pois, n.Price, fib, bias) // non-fractal: hanya H1
			n.ArrayByTF = []TFArray{{TF: detectors.TFH1, PDRs: liveTFPDRMarks(plotWin, cfg)}}
		}
		var poi detectors.POI
		var ok bool
		if cfg.POITouchWindowBars > 0 {
			poi, ok = detectors.SelectPOITouched(pois, window(h1Closed, cfg.POITouchWindowBars), fib, bias)
		} else {
			poi, ok = detectors.SelectPOI(pois, n.Price, fib, bias)
		}

		// Isi SEMUA PDR (sekalipun tak ada POI terpilih) untuk verifikasi visual.
		// InPOI = true bila komponen ini ada di poi.Components (cocokkan Kind+Top+
		// Bottom+Index) saat POI terpilih.
		for _, p := range pdrs {
			var t time.Time
			if p.Index >= 0 && p.Index < len(plotWin) {
				t = plotWin[p.Index].Time
			}
			inPOI := false
			if ok {
				for _, c := range poi.Components {
					if c.Kind == p.Kind && c.Index == p.Index &&
						c.Top == p.Top && c.Bottom == p.Bottom {
						inPOI = true
						break
					}
				}
			}
			n.PDRs = append(n.PDRs, PDRMark{
				Kind:   p.Kind.String(),
				Dir:    p.Dir.String(),
				Top:    p.Top,
				Bottom: p.Bottom,
				Time:   t,
				InPOI:  inPOI,
			})
		}

		usedFallback := false
		if !ok && cfg.Kunci3Fallback {
			// Kunci #3 fallback (F.5): tak ada POI normal → opposing liquidity jadi
			// POI fallback (band tipis). KONSISTEN dgn evaluate() (knob & helper sama).
			if lvl, okLiq := opposingLiquidity(weekly, daily, bias, n.Price); okLiq {
				poi = fallbackPOI(bias, lvl, cfg.Kunci3FallbackBandPips)
				ok = true
				usedFallback = true
			}
		}

		if ok && usedFallback {
			n.HasPOI = true
			n.POITop = poi.Top
			n.POIBottom = poi.Bottom
			poiPass = true
			oldName := "Old High"
			if bias == detectors.Bullish {
				oldName = "Old Low"
			}
			poiText = fmt.Sprintf("%s; tak ada POI/FVG searah bias %s di zona benar. Kunci #3 fallback AKTIF → target opposing liquidity (%s) di %.2f, POI fallback band %.2f–%.2f. Lanjut trigger entry.",
				zoneInfo, bias.String(), oldName, poi.Mid(), poi.Bottom, poi.Top)
		} else if ok {
			n.HasPOI = true
			n.POITop = poi.Top
			n.POIBottom = poi.Bottom
			n.POITF = poi.TF
			n.POIComponents = poiComponentMarks(poi)
			poiPass = true
			zoneTail := ""
			if fib.Defined() {
				zoneTail = fmt.Sprintf(" — zona %s", fib.Zone(poi.Mid()).String())
			}
			poiText = fmt.Sprintf("%s; cari POI searah bias di zona %s. Ketemu POI %s di TF %s, %.2f–%.2f, Tier %d, confluence %d komponen%s, good opportunity.",
				zoneInfo, eqZone, bias.String(), poi.TF.String(), poi.Bottom, poi.Top, poi.Tier, poi.Confluence(), zoneTail)
		} else {
			poiText = fmt.Sprintf("%s; zona target %s. Belum ada POI valid yang di-tag harga (%.2f) — tunggu harga masuk PD Array.",
				zoneInfo, eqZone, n.Price)
			recordFail("belum ada POI valid di zona benar yang di-tag harga")

			// Fitur: kalau ada POI valid TERDEKAT searah bias, beri tahu user harga
			// berapa yang ditunggu (zona + jarak + arah rally/turun).
			if np, okNP := detectors.NearestValidPOI(pois, n.Price, fib, bias); okNP {
				n.HasNextPOI = true
				n.NextPOITop = np.Top
				n.NextPOIBottom = np.Bottom
				n.NextPOITier = np.Tier
				n.NextPOIDistance = poiEdgeDistance(np, n.Price)
				// Arah tunggu: untuk buy, POI discount biasanya DI BAWAH harga (tunggu
				// harga turun); untuk sell, POI premium DI ATAS (tunggu rally).
				dirHint := "turun"
				if n.Price < np.Bottom {
					dirHint = "rally"
				}
				poiText += fmt.Sprintf(" POI valid terdekat searah bias di %.2f–%.2f (Tier %d), ~%.2f poin (%.0f pip) dari harga sekarang — tunggu harga %s ke zona itu.",
					np.Bottom, np.Top, np.Tier, n.NextPOIDistance, n.NextPOIDistance/detectors.PipSizeGold, dirHint)
			}

			// Walau Kunci #3 OFF: kalau memang tak ada POI, sebutkan KONTEKS opposing
			// liquidity (mirip next-POI) supaya user tahu fallback yang TERSEDIA bila
			// knob diaktifkan. Tidak mengubah keputusan (tetap NO SETUP saat OFF).
			if lvl, okLiq := opposingLiquidity(weekly, daily, bias, n.Price); okLiq {
				oldName := "Old High di atas"
				if bias == detectors.Bullish {
					oldName = "Old Low di bawah"
				}
				poiText += fmt.Sprintf(" Kunci #3 fallback (OFF): opposing liquidity %s di %.2f bisa jadi target bila fallback diaktifkan.",
					oldName, lvl)
			}
		}
	}
	st = StepPass
	if !poiPass {
		st = StepFail
	}
	n.Steps = append(n.Steps, StepTrace{Name: "5. POI — Fibonacci & PD Array", Status: st, Text: poiText})

	// ============================================================
	// Step 6 — Trigger entry 5m (ITL/ITH) + SL/TP/Lot
	// ============================================================
	entryPass := false
	entryRR := 0.0
	var entryText string
	if !poiPass {
		entryText = "Tidak ada POI valid, jadi trigger entry 5m tidak dievaluasi (harga belum di zona eksekusi)."
		n.Steps = append(n.Steps, StepTrace{Name: "6. Entry — Trigger 5m", Status: StepInfo, Text: entryText})
	} else {
		m5 := m5Window(tf.M5, loc, cfg, now)
		// POI untuk trigger mode reject/sweep + SL body_poi: bentuk ulang dari
		// level POI yang sudah dipilih (identik dgn evaluate).
		poiForSL := detectors.POI{Dir: bias, Top: n.POITop, Bottom: n.POIBottom}
		trig, ok := entryTrigger5m(m5, bias, entryFromIdx(m5, now, cfg), cfg, poiForSL)
		if !ok {
			entryText = fmt.Sprintf("Harga sudah di POI tapi belum ada konfirmasi trigger 5m (mode %s). Tunggu trigger.", triggerModeName(cfg))
			n.Steps = append(n.Steps, StepTrace{Name: "6. Entry — Trigger 5m", Status: StepFail, Text: entryText})
			recordFail("belum ada trigger entry 5m")
		} else {
			fillC := m5[trig.fillIdx]
			entry := fillC.Open
			ltfPivot := trig.pivot

			sl := detectors.SLPrice(bias, cfg.SLMode, ltfPivot, poiForSL, cfg.SLBufferPips)

			if !slSane(bias, entry, sl) {
				entryText = fmt.Sprintf("Konfirmasi trigger 5m ada (pivot %.2f), tapi SL %.2f berada di sisi salah relatif entry %.2f — setup ditolak (slSane gagal).",
					ltfPivot, sl, entry)
				n.Steps = append(n.Steps, StepTrace{Name: "6. Entry — Trigger 5m", Status: StepFail, Text: entryText})
				recordFail("SL tidak sane")
			} else {
				rr := detectors.RRTarget(wd)
				tp := detectors.TPPrice(bias, entry, sl, rr)
				tpNote := ""
				// Identik dgn evaluate: retrace → TP = level weekly FVG target.
				if cfg.RetraceTPToFVG && regime == state.RegimeRetrace {
					if tpFVG, rrFVG, okFVG := retraceTP(snapshot{weekly: weekly, ofDir: ofDir, bias: bias, minLegW: minLegW}, cfg, entry, sl); okFVG {
						tp, rr = tpFVG, rrFVG
						tpNote = " — TP = level weekly FVG target retrace"
					}
				}
				entryRR = rr
				lot, _ := detectors.LotSize(cfg.StartBalance, cfg.RiskPct, absf(entry-sl), detectors.ContractOz, detectors.LotStep, detectors.LotMin)

				n.HasEntry = true
				n.Entry = entry
				n.SL = sl
				n.TP = tp
				n.Lot = lot
				entryPass = true

				// Pivot trigger entry 5m (ITL utk buy / ITH utk sell / mode alternatif).
				n.HasTrig = true
				n.TrigKind = trig.kind
				n.TrigTime = trig.pivotTime
				n.TrigPrice = trig.pivot

				rrNote := ""
				if wd == time.Monday || wd == time.Friday {
					rrNote = fmt.Sprintf(" (%s → RR 1:2)", wd.String())
				}
				entryText = fmt.Sprintf("Konfirmasi trigger 5m %s (%s) di pivot %.2f, fill di open candle berikutnya %.2f. SL %.2f, TP %.2f (RR 1:%s%s)%s, sizing %.2f lot. Entry valid.",
					trig.kind, trig.itType.String(), ltfPivot, entry, sl, tp, fmtRR(rr), rrNote, tpNote, lot)
				n.Steps = append(n.Steps, StepTrace{Name: "6. Entry — Trigger 5m", Status: StepPass, Text: entryText})
			}
		}
	}

	// ============================================================
	// HasSetup + Decision
	// ============================================================
	// AMS hanya gating kalau AMSGate ON; saat off ia cuma penanda (tak memblokir).
	amsPass := amsOK || !cfg.AMSGate
	allGatesPass := !invalidated && ofOK && !ofStale && !retraceSkipped && dailyAligned && amsPass && qtPass && poiPass && entryPass
	n.HasSetup = allGatesPass && n.HasEntry

	if n.HasSetup {
		dirWord := "BUY"
		if bias == detectors.Bearish {
			dirWord = "SELL"
		}
		n.Decision = fmt.Sprintf("ENTRY %s @ %.2f SL %.2f TP %.2f (1:%s, %.2f lot)",
			dirWord, n.Entry, n.SL, n.TP, fmtRR(entryRR), n.Lot)
	} else {
		if firstFail == "" {
			firstFail = "salah satu gate belum lolos"
		}
		n.Decision = "NO SETUP — " + firstFail
	}

	return n
}

// triggerModeName = nama mode trigger utk narasi ("" = itl default).
func triggerModeName(cfg Config) string {
	if cfg.EntryTriggerMode == "" {
		return "itl"
	}
	return cfg.EntryTriggerMode
}

// fmtRR memformat rasio RR: bulat → "3", pecahan → "1.85" (TP retrace = level
// FVG menghasilkan RR pecahan).
func fmtRR(rr float64) string {
	if rr == float64(int(rr)) {
		return fmt.Sprintf("%.0f", rr)
	}
	return fmt.Sprintf("%.2f", rr)
}

// lastIdxLE = index candle terakhir dengan Time <= at, atau -1 kalau tak ada.
func lastIdxLE(c []data.Candle, at time.Time) int {
	// candle kronologis → cari batas pertama yang After(at), mundur satu.
	n := sort.Search(len(c), func(i int) bool { return c[i].Time.After(at) })
	return n - 1
}

// poiEdgeDistance = jarak HARGA ke tepi POI terdekat (0 kalau price di dalam zona).
// Dipakai untuk narasi next-POI ("~X poin dari harga sekarang").
func poiEdgeDistance(p detectors.POI, price float64) float64 {
	switch {
	case price > p.Top:
		return price - p.Top
	case price < p.Bottom:
		return p.Bottom - price
	default:
		return 0
	}
}

// NextPOITF = POI valid terdekat untuk satu pasangan (TF, arah) — baris daftar
// pantauan multi-TF di ScanNarrative.NextPOIsByTF.
//
// Dua versi zona: Top/Bottom = INTI (irisan ketat semua komponen — level di mana
// engine men-tag harga "di dalam POI"; bisa nol-lebar), FullTop/FullBottom =
// RENTANG PENUH komponen (band bawah→atas seluruh PD Array penyusun — zona yang
// digambar trader di chart).
type NextPOITF struct {
	TF         detectors.TFKind
	Dir        detectors.Direction // arah trade POI (Bullish=buy, Bearish=sell)
	Top        float64             // inti (irisan) — batas atas
	Bottom     float64             // inti (irisan) — batas bawah
	FullTop    float64             // rentang penuh komponen — batas atas
	FullBottom float64             // rentang penuh komponen — batas bawah
	Tier       int
	Confluence int
	Kinds      string  // rincian komponen per kind, mis. "1×VI + 2×FVG" (urut tier terbaik)
	Distance   float64 // jarak harga ke tepi zona INTI; 0 = harga sedang DI DALAM zona
}

// poiKindSummary merangkum komponen POI per kind urut tier terbaik dulu —
// menjawab "confluence-nya terdiri dari apa": mis. "1×VI + 2×FVG + 2×IFVG".
func poiKindSummary(p detectors.POI) string {
	order := []detectors.PDRKind{
		detectors.KindVI, detectors.KindFVGBreak, detectors.KindBB,
		detectors.KindBPR, detectors.KindFVG, detectors.KindIFVG, detectors.KindOB,
	}
	count := map[detectors.PDRKind]int{}
	for _, c := range p.Components {
		count[c.Kind]++
	}
	var parts []string
	for _, k := range order {
		if n := count[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d×%s", n, k.String()))
		}
	}
	return strings.Join(parts, " + ")
}

// poiFullRange = band bawah→atas SELURUH komponen cluster (bukan irisan).
// POI tanpa komponen (mis. fallback) → jatuh ke Top/Bottom POI sendiri.
func poiFullRange(p detectors.POI) (bottom, top float64) {
	bottom, top = p.Bottom, p.Top
	for _, c := range p.Components {
		if c.Bottom < bottom {
			bottom = c.Bottom
		}
		if c.Top > top {
			top = c.Top
		}
	}
	return bottom, top
}

// nextPOIsByTF menghitung POI valid TERDEKAT per timeframe (H1/H4/D) untuk dua
// arah: searah bias dan counter-bias. Sumber = pois hasil pipeline (sudah lewat
// gate tier/confluence yang sama dengan engine) → 0 drift engine↔narrate.
// Validitas per arah pakai NearestValidPOI (zona premium/discount + belum-jebol).
func nextPOIsByTF(pois []detectors.POI, price float64, fib detectors.Fib, bias detectors.Direction) []NextPOITF {
	counter := detectors.Bearish
	if bias == detectors.Bearish {
		counter = detectors.Bullish
	}
	var out []NextPOITF
	for _, tfk := range []detectors.TFKind{detectors.TFH1, detectors.TFH4, detectors.TFD} {
		var group []detectors.POI
		for _, p := range pois {
			if p.TF == tfk {
				group = append(group, p)
			}
		}
		for _, dir := range []detectors.Direction{bias, counter} {
			np, ok := detectors.NearestValidPOI(group, price, fib, dir)
			if !ok {
				continue
			}
			fullBot, fullTop := poiFullRange(np)
			out = append(out, NextPOITF{
				TF: tfk, Dir: dir, Top: np.Top, Bottom: np.Bottom,
				FullTop: fullTop, FullBottom: fullBot,
				Tier: np.Tier, Confluence: np.Confluence(), Kinds: poiKindSummary(np),
				Distance: poiEdgeDistance(np, price),
			})
		}
	}
	return out
}

// FormatNextPOIsByTF merender daftar pantauan POI per-TF jadi baris teks polos —
// SATU sumber format untuk cmd/narrate (terminal) dan cmd/alertd (Telegram,
// dibungkus blok monospace) supaya tampilannya identik. Nil kalau daftar kosong.
func FormatNextPOIsByTF(n ScanNarrative) []string {
	if len(n.NextPOIsByTF) == 0 {
		return nil
	}
	var out []string
	for _, p := range n.NextPOIsByTF {
		arah := "BUY"
		biasMatch := n.Bias == "buy"
		if p.Dir == detectors.Bearish {
			arah = "SELL"
			biasMatch = n.Bias == "sell"
		}
		var jarak string
		switch {
		case p.Distance == 0:
			jarak = "harga DI DALAM zona"
		case p.Bottom > n.Price:
			jarak = fmt.Sprintf("~%.2f poin (%.0f pip) di atas harga", p.Distance, p.Distance/detectors.PipSizeGold)
		default:
			jarak = fmt.Sprintf("~%.2f poin (%.0f pip) di bawah harga", p.Distance, p.Distance/detectors.PipSizeGold)
		}
		inti := ""
		if p.FullBottom != p.Bottom || p.FullTop != p.Top {
			if p.Bottom == p.Top {
				inti = fmt.Sprintf("  (inti %.2f)", p.Bottom)
			} else {
				inti = fmt.Sprintf("  (inti %.2f–%.2f)", p.Bottom, p.Top)
			}
		}
		tail := ""
		if !biasMatch {
			tail = "  [counter-bias — bukan setup valid saat ini]"
		}
		out = append(out, fmt.Sprintf("%-4s %-5s zona %.2f → %.2f%s  Tier %d  %s%s",
			p.TF.String(), arah, p.FullBottom, p.FullTop, inti, p.Tier, jarak, tail))
		out = append(out, fmt.Sprintf("     confluence %d komponen: %s", p.Confluence, p.Kinds))
	}
	return out
}

// absPrice = nilai absolut selisih harga (helper kecil narasi/penanda).
func absPrice(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// swingWord = kata jenis swing untuk narasi ("high" utk REH, "low" utk REL).
func swingWord(kind string) string {
	if kind == "REH" {
		return "high"
	}
	return "low"
}

// FormatRelEqLines = baris penanda REH/REL (per TF, fractal) untuk seksi
// pantauan — SATU sumber format untuk cmd/narrate (terminal) dan cmd/alertd
// (Telegram). Nil kalau tidak ada grup equal dalam window.
func FormatRelEqLines(n ScanNarrative) []string {
	var out []string
	for _, m := range n.RelEquals {
		arah := "di atas"
		if m.Kind == "REL" {
			arah = "di bawah"
		}
		out = append(out, fmt.Sprintf("%s %-3s %.2f (%d× %s) — ~%.2f poin %s (liquidity target)",
			m.Kind, m.TF.String(), m.Level, m.Count, swingWord(m.Kind), m.Distance, arah))
	}
	return out
}

// FormatAsiaLine = satu baris klasifikasi sesi Asia (A vs X) untuk seksi
// pantauan — SATU sumber format untuk cmd/narrate dan cmd/alertd. Kosong
// selama sesi Asia masih berjalan (klasifikasi belum final).
func FormatAsiaLine(n ScanNarrative) string {
	if !n.AsiaClosed {
		return ""
	}
	if n.AsiaScenario == detectors.XAMD {
		if !n.AsiaHasFVG {
			// X dari true-move saja — FVG 1H tak ada (syarat-1 D.2 dimatikan,
			// jadi catatan informatif: displacement tanpa imbalance tertinggal).
			return "Asia hari ini: X (sudah expansive) → ekspektasi skenario XAMD — catatan: TANPA FVG 1H"
		}
		return "Asia hari ini: X (sudah expansive) → ekspektasi skenario XAMD"
	}
	return "Asia hari ini: AKUMULASI (A) → ekspektasi skenario AMDX"
}

// FormatMOLine = satu baris penanda Midnight Open untuk seksi pantauan — SATU
// sumber format untuk cmd/narrate (terminal) dan cmd/alertd (Telegram) supaya
// tampilannya identik. Catatan analisa 2026-06-02: MO TIDAK dipakai sebagai
// gate (London×MO diuji, no edge) — murni konteks premium/discount harian.
func FormatMOLine(n ScanNarrative) string {
	if !n.HasMO {
		return "MO hari ini belum terbentuk (sesi Asia — muncul 00:00 NY)"
	}
	// Tanpa angka (permintaan user 2026-06-02): cukup kondisi atas/bawah —
	// level persisnya tetap ada di step narasi "4½. MO" & garis chart.
	if n.Price < n.MOPrice {
		return "Saat ini harga DI BAWAH MO (discount)"
	}
	return "Saat ini harga DI ATAS MO (premium)"
}

// FormatNarrative merender narasi piramida LENGKAP (header + tiap langkah +
// decision) jadi baris teks polos ber-bingkai — SATU sumber format untuk
// cmd/narrate (terminal) dan cmd/alertd (Telegram, dibungkus blok monospace)
// supaya tampilannya identik. width = lebar wrap teks langkah: terminal 88,
// Telegram ~42 supaya rapi di layar HP.
func FormatNarrative(n ScanNarrative, instrument string, width int) []string {
	loc, err := detectors.NYLocation()
	if err != nil {
		loc = time.UTC
	}
	out := []string{
		fmt.Sprintf("┌─ SCAN %s @ %s ─ harga %.2f",
			instrument, n.At.In(loc).Format("2006-01-02 15:04 MST"), n.Price),
		"│",
	}
	for _, s := range n.Steps {
		icon := "•"
		switch s.Status {
		case StepPass:
			icon = "✓"
		case StepFail:
			icon = "✗"
		}
		out = append(out, fmt.Sprintf("│ %s %s", icon, s.Name))
		for _, line := range wrapWords(s.Text, width) {
			out = append(out, "│     "+line)
		}
	}
	out = append(out, "│")
	// n.Decision sudah berawalan "NO SETUP — ..." saat gagal; jangan dobel.
	head := "└─ ❌ "
	if n.HasSetup {
		head = "└─ ✅ SETUP VALID — "
	}
	for i, line := range wrapWords(n.Decision, width) {
		if i == 0 {
			out = append(out, head+line)
			continue
		}
		out = append(out, "      "+line)
	}
	return out
}

// wrapWords memecah teks jadi baris <= width kata-per-kata (narasi rapi di
// terminal/Telegram).
func wrapWords(s string, width int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		switch {
		case line == "":
			line = w
		case len(line)+1+len(w) > width:
			out = append(out, line)
			line = w
		default:
			line += " " + w
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// FormatWatchlist merender pesan PANTAUAN ringkas untuk watchlist (revisi layout
// mobile 2026-06-08) — pengganti narasi piramida penuh. Urutan baris flat (presisi
// di HP, tanpa indent bersarang):
//
//	header (instrumen · harga + waktu NY)
//	TDA · Bias (ringkas, tanpa alasan panjang)
//	AMS ITL / AMS ITH (dua arah, ◀ = searah bias)
//	QT Bulanan / Mingguan / Daily / Session (berurutan TF tertinggi→sesi berjalan)
//	Array per-TF (komponen TERDEKAT ke harga + REL/REH + SIBI/BISI)
//	MO (divider premium/discount, terpisah paling bawah)
//
// SATU sumber format untuk cmd/alertd (Telegram, dibungkus monospace, width 42) dan
// cmd/narrate (terminal, width 88) — tampilan identik. Semua nilai dari field
// ScanNarrative yang sudah mirror step engine → 0 drift. width = lebar wrap.
func FormatWatchlist(n ScanNarrative, instrument string, width int) []string {
	loc, err := detectors.NYLocation()
	if err != nil {
		loc = time.UTC
	}
	// Header — instrumen + harga di baris 1, waktu NY di baris 2 (ringkas utk HP).
	out := []string{
		fmt.Sprintf("📍 Watchlist %s · %.2f", instrument, n.Price),
		n.At.In(loc).Format("2006-01-02 15:04 MST"),
		"",
	}

	// TDA + Bias — arah weekly OF & bias harian ringkas (tanpa alasan panjang).
	tda := "-"
	switch n.WeeklyOFDir {
	case "bullish":
		tda = "Bullish"
	case "bearish":
		tda = "Bearish"
	}
	bias := "-"
	switch n.Bias {
	case "buy":
		bias = "Buy"
	case "sell":
		bias = "Sell"
	}
	out = append(out, fmt.Sprintf("TDA: %s · Bias: %s", tda, bias))

	// AMS — ITL/ITH 1H dua arah (◀ = sisi searah bias).
	out = append(out, amsWatchLines(n)...)

	// QT — Bulanan → Mingguan → Daily → Session (dari TF tertinggi ke sesi berjalan).
	out = append(out, qtWatchLines(n, width)...)

	// Komponen Array per-TF (terdekat ke harga, maks 2 per sisi).
	out = append(out, arrayWatchLines(n, width)...)

	// MO — divider premium/discount harian, terpisah di paling bawah.
	out = append(out, "", moWatchLine(n))

	return out
}

// wrapPrefixed membungkus teks di belakang prefix yang dipertahankan APA ADANYA
// (termasuk spasi padding utk alignment kolom — beda dgn wrapLabeled yang lewat
// wrapWords/strings.Fields sehingga spasi ganda kolaps). Baris lanjutan di-indent
// selebar prefix. Hitung rune (bukan byte) agar en-dash/· tak salah ukur.
func wrapPrefixed(prefix, text string, width int) []string {
	rlen := func(s string) int { return len([]rune(s)) }
	pad := strings.Repeat(" ", rlen(prefix))
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{strings.TrimRight(prefix, " ")}
	}
	if width <= 0 { // 0 = jangan hard-wrap (mis. jalur Telegram teks-biasa)
		return []string{prefix + strings.Join(words, " ")}
	}
	var out []string
	line := prefix + words[0]
	for _, w := range words[1:] {
		if rlen(line)+1+rlen(w) > width {
			out = append(out, line)
			line = pad + w
		} else {
			line += " " + w
		}
	}
	return append(out, line)
}

// wrapLabeled membungkus "label + teks" ke <= width; baris lanjutan diberi indent
// selebar label supaya keterangan rapi (terminal/Telegram).
func wrapLabeled(label, text string, width int) []string {
	lines := wrapWords(label+text, width)
	pad := strings.Repeat(" ", len(label))
	for i := 1; i < len(lines); i++ {
		lines[i] = pad + lines[i]
	}
	return lines
}

// amsWatchLines = AMS 1H dua arah, satu baris per sisi (ITL lalu ITH); "◀" menandai
// sisi searah bias. Saat bias belum terdefinisi → satu baris penanda.
func amsWatchLines(n ScanNarrative) []string {
	if !n.AMSChecked {
		return []string{"AMS: — (bias belum terdefinisi)"}
	}
	// Status ITL & ITH DUA ARAH (dari AMSITL/AMSITH) — tandai mana yang searah bias (◀).
	side := func(kind string, s AMSStruct) string {
		if !s.Present {
			return fmt.Sprintf("AMS %s —", kind)
		}
		st := "aktif"
		if !s.Active {
			st = "broken"
		}
		mark := ""
		if kind == n.AMSKind {
			mark = " ◀"
		}
		return fmt.Sprintf("AMS %s %.2f %s%s", kind, s.Pivot, st, mark)
	}
	return []string{side("ITL", n.AMSITL), side("ITH", n.AMSITH)}
}

// qtWatchLines = grup QT berurutan Bulanan → Mingguan → Daily → Session, satu baris
// per level (label di-pad agar nilai sejajar). MO TIDAK lagi di sini — dipindah ke
// baris terpisah paling bawah (moWatchLine). Nilai panjang di-wrap ke <= width.
func qtWatchLines(n ScanNarrative, width int) []string {
	line := func(label, val string) []string {
		return wrapPrefixed(fmt.Sprintf("QT %-8s ", label), val, width)
	}
	var out []string

	// Bulanan — monthly AMD (minggu dalam bulan).
	if n.MonthlyScenario != "" {
		q1 := "A"
		if n.MonthlyScenario == "xamd" {
			q1 = "X"
		}
		out = append(out, line("Bulanan", fmt.Sprintf("Q1=%s → %s", q1, strings.ToUpper(n.MonthlyScenario)))...)
	} else {
		out = append(out, line("Bulanan", "—")...)
	}

	// Mingguan (D.4) — final HANYA setelah Senin close (terkunci seminggu); sebelum
	// itu penanda. JANGAN tampilkan A/X palsu.
	if n.QTSkenario != "" {
		sen := "A"
		if n.QTSkenario == "xamd" {
			sen = "X"
		}
		out = append(out, line("Mingguan", fmt.Sprintf("Sen=%s → %s", sen, strings.ToUpper(n.QTSkenario)))...)
	} else if n.QTWeeklyPending {
		out = append(out, line("Mingguan", "menunggu Senin close")...)
	} else {
		out = append(out, line("Mingguan", "—")...)
	}

	// Daily — fase sesi saat ini + skenario harian (anchor Asia).
	if n.QTDailyScenario != "" {
		out = append(out, line("Daily", fmt.Sprintf("%s = %s → %s", n.QTSessName, n.QTDailySessPhase, n.QTDailyScenario))...)
	} else {
		out = append(out, line("Daily", fmt.Sprintf("%s = belum close Asia", n.QTSessName))...)
	}

	// Session — kuartal sesi saat ini + window + fase.
	sessPhase := n.QTSessPhase
	if sessPhase == "" {
		if n.QTSessionQ == 1 {
			sessPhase = "berjalan" // Q1 belum close → A/X belum terklasifikasi
		} else {
			sessPhase = "?" // data M15 belum cukup
		}
	}
	out = append(out, line("Session", fmt.Sprintf("Q%d %s %s–%s = %s",
		n.QTSessionQ, n.QTSessName, n.QTSessQStartNY, n.QTSessQEndNY, sessPhase))...)

	return out
}

// moWatchLine = baris MO (Midnight Open, open 00:00 NY) — divider premium/discount
// harian, dirender terpisah di paling bawah watchlist.
func moWatchLine(n ScanNarrative) string {
	if !n.HasMO {
		return "MO: belum terbentuk"
	}
	side := "DI ATAS (premium)"
	if n.Price < n.MOPrice {
		side = "DI BAWAH (discount)"
	}
	return fmt.Sprintf("MO: %.2f — %s", n.MOPrice, side)
}

// quarterToSession memetakan nomor kuartal sesi (1-4) ke posisi SessionKind yang
// setara untuk fractal AMD (Q1→Asia, Q2→London, Q3→NY-AM, Q4→PM) — dipakai
// menghitung fase kuartal lewat DailyPhase.
func quarterToSession(q int) detectors.SessionKind {
	switch q {
	case 1:
		return detectors.Asia
	case 2:
		return detectors.London
	case 3:
		return detectors.NYAM
	default:
		return detectors.PM
	}
}

// qtSessionLabel = nama sesi untuk display QT.
func qtSessionLabel(s detectors.SessionKind) string {
	switch s {
	case detectors.Asia:
		return "Asia"
	case detectors.London:
		return "London"
	case detectors.NYAM:
		return "New York"
	case detectors.PM:
		return "PM Session"
	default:
		return s.String()
	}
}

// m15InWindow = M15 candle dengan Time di [from, to).
func m15InWindow(m15 []data.Candle, from, to time.Time) []data.Candle {
	var out []data.Candle
	for _, c := range m15 {
		if !c.Time.Before(from) && c.Time.Before(to) {
			out = append(out, c)
		}
	}
	return out
}

// weekOfMonthNY = minggu ke-berapa (1-4) dalam bulan untuk waktu `t` di timezone NY.
// Hari 1-7 = minggu 1, 8-14 = minggu 2, 15-21 = minggu 3, 22+ = minggu 4.
func weekOfMonthNY(t time.Time, loc *time.Location) int {
	d := t.In(loc).Day()
	w := (d-1)/7 + 1
	if w > 4 {
		return 4
	}
	return w
}

// classifyMonthlyQT menentukan skenario AMD bulanan berdasarkan H4 candle di hari
// kalender 1–7 bulan ini (week-1 proxy). Pakai H4 agar bekerja sejak hari pertama
// bulan — tidak menunggu weekly candle close.
// Returns (scenario, weekNum, ok). ok=false hanya jika belum ada H4 di hari 1–7.
func classifyMonthlyQT(h4Closed []data.Candle, now time.Time, loc *time.Location, atrPeriod int, minAtrMult float64) (detectors.Scenario, int, bool) {
	nowNY := now.In(loc)
	weekNum := weekOfMonthNY(now, loc)
	year, month := nowNY.Year(), nowNY.Month()

	// Kumpulkan H4 candle di hari 1–7 bulan ini sebagai proxy week-1.
	var week1H4 []data.Candle
	lastIdx := -1
	for i, c := range h4Closed {
		cNY := c.Time.In(loc)
		if cNY.Year() == year && cNY.Month() == month && cNY.Day() <= 7 {
			week1H4 = append(week1H4, c)
			lastIdx = i
		}
	}
	if len(week1H4) == 0 {
		return detectors.AMDX, weekNum, false // belum ada data H4 week-1
	}

	// ATR H4 pada ujung week-1 sebagai threshold ekspansif.
	var atr4h float64
	if atr, ok := detectors.ATRAt(h4Closed, lastIdx, atrPeriod); ok {
		atr4h = atr
	}
	// requireFVG=false: level bulanan → penentu murni directional move H4.
	sc := detectors.ClassifyDailyEx(week1H4, atr4h, minAtrMult, 0, 0, "atr", false)
	return sc, weekNum, true
}

// classifyWeeklyQT menentukan skenario QT mingguan (D.4): klasifikasi candle SENIN
// di view 4H, dinilai SETELAH Senin close, dikunci untuk seminggu. "Senin" (anchor
// 18:00 NY, konvensi NY-close/ICT) = sesi hari Senin = BUKA Minggu 18:00 → TUTUP
// Senin 18:00 NY. XAMD bila Senin tinggalkan FVG 4H AND |close−open|_Senin >= minAtrMult×ATR_4H.
//
// Returns (scenario, ok). ok=false (→ caller tampil "menunggu Senin close") bila
// candle Senin minggu ini BELUM close (now < Senin 18:00 NY) atau data H4 Senin
// belum cukup. Karena window Senin tetap sepanjang minggu, nilai terkunci Sen18:00–Jum.
func classifyWeeklyQT(h4Closed []data.Candle, now time.Time, loc *time.Location, atrPeriod int, minAtrMult, minGapPips float64) (detectors.Scenario, bool) {
	// Anchor candle Senin lewat trading-day start (18:00 NY). Konvensi NY-close (ICT):
	// candle harian "Senin" = sesi hari Senin = BUKA Minggu 18:00 → TUTUP Senin 18:00 NY
	// (price action Senin: Asia/London/NY-AM). Trading week dimulai Minggu 18:00; mundur
	// ke awal week itu = open candle Senin. TradingDayStart menangani shift 18:00 (mis.
	// Senin pagi NY masih trading-day yang dibuka Minggu 18:00).
	tds := detectors.TradingDayStart(now, loc)
	mondayStart := tds.AddDate(0, 0, -int(tds.Weekday())) // Minggu 18:00 NY = OPEN candle Senin
	mondayEnd := mondayStart.Add(24 * time.Hour)          // Senin 18:00 NY = CLOSE candle Senin
	if now.Before(mondayEnd) {
		return detectors.AMDX, false // candle Senin belum close (now < Senin 18:00) → weekly QT belum final
	}

	var monday4h []data.Candle
	lastIdx := -1
	for i, c := range h4Closed {
		if !c.Time.Before(mondayStart) && c.Time.Before(mondayEnd) {
			monday4h = append(monday4h, c)
			lastIdx = i
		}
	}
	if len(monday4h) < 2 {
		return detectors.AMDX, false // data H4 Senin belum cukup
	}

	var atr4h float64
	if atr, ok := detectors.ATRAt(h4Closed, lastIdx, atrPeriod); ok {
		atr4h = atr
	}
	// ClassifyWeekly = mode "atr" + FVG wajib (D.4: syarat-1 FVG 4H AND syarat-2 directional).
	return detectors.ClassifyWeekly(monday4h, atr4h, minAtrMult, minGapPips), true
}

// monthlyPhaseForWeekNum memetakan nomor minggu (1-4) ke Phase AMD bulanan.
// AMDX: Minggu1=A, 2=M, 3=D, 4=X. XAMD: Minggu1=X, 2=A, 3=M, 4=D.
func monthlyPhaseForWeekNum(sc detectors.Scenario, weekNum int) detectors.Phase {
	if weekNum < 1 {
		weekNum = 1
	}
	if weekNum > 4 {
		weekNum = 4
	}
	if sc == detectors.XAMD {
		return [4]detectors.Phase{detectors.PhaseX, detectors.PhaseA, detectors.PhaseM, detectors.PhaseD}[weekNum-1]
	}
	return [4]detectors.Phase{detectors.PhaseA, detectors.PhaseM, detectors.PhaseD, detectors.PhaseX}[weekNum-1]
}

// watchlistMOLine = baris MO untuk watchlist DENGAN harga open (permintaan user
// 2026-06-03 — beda dgn FormatMOLine yang sengaja tanpa angka).
func watchlistMOLine(n ScanNarrative) string {
	if !n.HasMO {
		return "MO: belum terbentuk (sesi Asia — muncul 00:00 NY)"
	}
	side := "harga DI ATAS MO (premium)"
	if n.Price < n.MOPrice {
		side = "harga DI BAWAH MO (discount)"
	}
	return fmt.Sprintf("MO: open %.2f — %s", n.MOPrice, side)
}

// arrayWatchLines merender komponen PD Array per-TF (H1/H4/D) sebagai daftar
// VERTIKAL grup-per-TF (rapi di HP teks-biasa, tak ke-wrap): baris header "<TF>:"
// lalu satu baris "- <label>" per komponen TERDEKAT ke harga (FVG dilabeli
// SIBI/BISI; non-FVG dapat tag Buy/Sell via pdrDisplayLabel) + REL/REH, urut
// bawah→atas. width tak dipakai (tiap komponen sudah satu baris pendek).
func arrayWatchLines(n ScanNarrative, _ int) []string {
	var out []string
	for _, ta := range n.ArrayByTF {
		comps := nearestPDRLabels(ta.PDRs, n.Price, 2)
		rel := relEqLabelsForTF(n, ta.TF)
		if len(comps) == 0 && len(rel) == 0 {
			continue
		}
		out = append(out, tfShort(ta.TF)+":")
		for _, c := range comps {
			out = append(out, "- "+c)
		}
		for _, r := range rel {
			out = append(out, "- "+r)
		}
	}
	if len(out) == 0 {
		out = append(out, "Array: (tak ada komponen hidup di window)")
	}
	return out
}


// nearestPDRLabels memilih hingga k komponen TERDEKAT ke harga di tiap sisi
// (atas/bawah), lalu mengurutkannya bawah→atas untuk dibaca. Label FVG jadi
// SIBI (bearish) / BISI (bullish); kind lain pakai namanya.
func nearestPDRLabels(pdrs []PDRMark, price float64, k int) []string {
	type scored struct {
		m PDRMark
		d float64
	}
	var above, below []scored
	for _, m := range pdrs {
		mid := (m.Top + m.Bottom) / 2
		d := mid - price
		if d < 0 {
			d = -d
		}
		if mid >= price {
			above = append(above, scored{m, d})
		} else {
			below = append(below, scored{m, d})
		}
	}
	sort.Slice(above, func(i, j int) bool { return above[i].d < above[j].d })
	sort.Slice(below, func(i, j int) bool { return below[i].d < below[j].d })
	if len(above) > k {
		above = above[:k]
	}
	if len(below) > k {
		below = below[:k]
	}
	chosen := append(below, above...)
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].m.Bottom < chosen[j].m.Bottom })
	out := make([]string, 0, len(chosen))
	seen := map[string]bool{} // dedup label identik (PDR tumpang-tindih di level sama)
	for _, s := range chosen {
		l := pdrDisplayLabel(s.m)
		if seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// pdrDisplayLabel = "NAMA [Buy/Sell] bawah–atas" (atau "NAMA [Buy/Sell] harga" bila
// tipis). Tag arah Buy/Sell ditambahkan untuk kind yang namanya BELUM mengisyaratkan
// sisi (BB/VI/FVGBreak/IFVG/OB/BPR); FVG dikecualikan karena sudah jadi BISI/SIBI.
func pdrDisplayLabel(m PDRMark) string {
	name := pdrDisplayName(m.Kind, m.Dir)
	if m.Kind != "FVG" {
		switch m.Dir {
		case "bullish":
			name += " Buy"
		case "bearish":
			name += " Sell"
		}
	}
	if m.Top == m.Bottom {
		return fmt.Sprintf("%s %.2f", name, m.Top)
	}
	return fmt.Sprintf("%s %.2f–%.2f", name, m.Bottom, m.Top)
}

// pdrDisplayName memetakan kind PD Array ke label tampilan. FVG (imbalance murni)
// dilabeli SIBI (sell-side, bearish) / BISI (buy-side, bullish); kind lain tetap
// (arah ditambah sbg tag Buy/Sell oleh pdrDisplayLabel).
func pdrDisplayName(kind, dir string) string {
	if kind == "FVG" {
		if dir == "bearish" {
			return "SIBI"
		}
		return "BISI"
	}
	return kind
}

// relEqLabelsForTF = REL/REH milik TF tertentu, format ringkas "REH 4630 (2×)".
func relEqLabelsForTF(n ScanNarrative, tf detectors.TFKind) []string {
	var out []string
	for _, m := range n.RelEquals {
		if m.TF != tf {
			continue
		}
		out = append(out, fmt.Sprintf("%s %.2f (%d×)", m.Kind, m.Level, m.Count))
	}
	return out
}

// tfShort = label TF pendek untuk seksi array ("1H"/"4H"/"D").
func tfShort(tf detectors.TFKind) string {
	switch tf {
	case detectors.TFH1:
		return "1H"
	case detectors.TFH4:
		return "4H"
	case detectors.TFD:
		return "D"
	default:
		return tf.String()
	}
}

// liveTFPDRMarks mendeteksi PDR HIDUP (belum jebol) pada satu window TF dan
// mengubahnya jadi []PDRMark untuk seksi "Komponen Array per-TF" di watchlist.
//
// SENGAJA TIDAK menerapkan filter FVGBreakTFs (keputusan user 2026-06-04): seksi ini
// = tampilan STRUKTUR MENTAH ("apa yang ada di chart"), jadi zona FVG-near-BOS tetap
// dilabeli "FVGBreak" di SEMUA TF (informasional — user mau tetap lihat strukturnya).
// Filter FVGBreakTFs="H1,D" hanya berlaku di buildTFPOIs (jalur SELEKSI/tiering POI):
// di H4/W zona itu dipilih sbg Tier-4 FVG. Jadi label array (FVGBreak) bisa beda dgn
// tier seleksi di H4/W — itu DISENGAJA, BUKAN drift keputusan (sinyal entry tetap via
// buildTFPOIs). JANGAN "perbaiki" jadi ikut filter tanpa konfirmasi user.
func liveTFPDRMarks(win []data.Candle, cfg Config) []PDRMark {
	if len(win) < 3 {
		return nil
	}
	pdrs := dropBPR(dropOB(detectors.DetectPDRs(win, cfg.MinGapPips, cfg.VIMinGapPips, cfg.FVGSwingBreakAdjacency, cfg.BPRMaxDistanceCandles, cfg.IFVGRequireNoSameDirFVG, cfg.OBStrict, cfg.BPRDirectional, cfg.BBRequireDisplacement, cfg.FVGBreakGeometric), cfg.DisableOB), cfg.DisableBPR)
	pdrs = detectors.FilterLivePDRs(win, pdrs, cfg.POIBreakWick)
	out := make([]PDRMark, 0, len(pdrs))
	for _, p := range pdrs {
		var t time.Time
		if p.Index >= 0 && p.Index < len(win) {
			t = win[p.Index].Time
		}
		out = append(out, PDRMark{
			Kind: p.Kind.String(), Dir: p.Dir.String(),
			Top: p.Top, Bottom: p.Bottom, Time: t,
		})
	}
	return out
}

// entryITName = nama struktur LTF pemicu sesuai arah (buy=ITL, sell=ITH).
func entryITName(dir detectors.Direction) string {
	if dir == detectors.Bearish {
		return "ITH"
	}
	return "ITL"
}
