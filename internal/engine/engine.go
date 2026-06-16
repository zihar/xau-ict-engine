// Catatan implementasi engine (package doc ada di doc.go).
//
// TF master keputusan = 1H (N.1). State HTF (W/D/H4) di-refresh tiap candle 1H
// close memakai HANYA candle yang sudah COMPLETE pada saat itu (closedBy) —
// menghindari lookahead bias. Deteksi 1H pakai rolling window (cfg.H1Window)
// supaya tidak O(n^2) di puluhan ribu candle.
//
// Aproksimasi yang sengaja diambil (didokumentasikan demi kejujuran POC):
//   - POI dihitung dari rolling window H1 terakhir, bukan seluruh sejarah.
//   - Zone makro (premium/discount) dipakai dari weekly last-impulse (A.1);
//     untuk regime RETRACE ini approx (idealnya pakai fib leg retrace).
//   - Day type hanya stage accum (D.7) dari range sesi H1 trading-day berjalan.
//   - Intrabar SL/TP di-resolve di M5 (TF terhalus yang kita punya); kalau satu
//     candle M5 kena SL & TP sekaligus -> asumsikan SL dulu (konservatif, K).
//   - Candle HTF/M5 yang masih terbentuk di-drop (next-open proxy), jadi entry
//     bisa telat 1 candle — sengaja, biar tidak melihat masa depan.

package engine

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/news"
	"xau-ict-engine/internal/state"
)

// ZoneFibTF = sumber timeframe Fib untuk gate zona premium/discount POI.
// Spec F-Q1: weekly = bias makro, daily/1H = zona presisi.
type ZoneFibTF int

const (
	ZoneFibWeekly ZoneFibTF = iota // Fib makro dari weekly (lebar, gampang stale)
	ZoneFibDaily                   // Fib dari daily (presisi, lebih banyak candle — default)
)

// Config = parameter engine (mirror Section M + knob backtest). DefaultConfig
// memakai default Section M.
type Config struct {
	// Deteksi
	MinGapPips         float64 // fvg_min_gap_pips_gold
	VIMinGapPips       float64 // threshold KHUSUS Volume Imbalance (Bug#5: lebih besar dari FVG supaya VI tak murah/banjir). 0 = pakai MinGapPips
	POIMaxWidthATRMult float64 // CAP lebar cluster POI = N×ATR_H1 (Bug#5: cegah cluster chain serap PDR berjauhan & tier inflasi). 0 = tanpa cap

	// PD Array F.1 lanjutan
	FVGSwingBreakAdjacency float64 // Kunci #1: jarak adjacency (candle) FVG ke swing yg baru di-break → promosi Tier-2 (fvg_swing_break_adjacency, default 12). 0 = default internal

	// FVGBreakGeometric (opt-in 2026-06-11, default false): pakai penentuan FVGBreak
	// versi geometris (STH/STL struktural + level swing DI DALAM zona FVG; mirror Pine)
	// pengganti broken-flag lama. TERBUKTI OOS-DOMINATED (IS/OOS −34R/−29R, PF & DD lebih
	// buruk di semua adj) → DEFAULT OFF. Disimpan utk re-eval saat sampel diperbesar.
	FVGBreakGeometric bool

	// FVGBreakTFs (WIP per-TF FVGBreak, 2026-06-04): allow-list TF (CSV "H1,D")
	// yang BOLEH dapat promosi Tier-2 KindFVGBreak; di TF lain FVG tetap Tier-4
	// (KindFVGBreak di-strip sebelum BuildPOIs). Kosong = SEMUA TF (perilaku lama,
	// paritas). Dasar: cross-tab FVGBreak akretif di H1, netral di D, merugikan di H4.
	FVGBreakTFs           string
	BPRMaxDistanceCandles float64             // BPR: jarak maks (candle) antara BISI & SIBI yg overlap (bpr_max_distance_candles, default 5). 0 = default internal
	ConfluenceMin         int                 // poi confluence minimum (strict overlap)
	BreakType             detectors.BreakType // itl_ith_break_type
	ATRPeriod             int                 // atr_period
	MinAtrMult            float64             // qt directional close threshold (× ATR)
	AsiaAXMode            string              // asia_ax_mode: "atr" (D.2, default) | "ratio" | "range"
	AsiaRangeRatio        float64             // asia_range_ratio: ambang X di mode "ratio" (true-move/range-sesi >= ini → X)
	AsiaRequireFVG        bool                // asia_require_fvg: X wajib disertai FVG Asia; false = penentu murni true-move
	AsiaGapAnchor         bool                // asia_gap_anchor: true-move Asia (D.2) di-anchor ke close candle SEBELUM sesi (gap/Volume Imbalance ikut terhitung), bukan open candle pertama
	Q1AXMode              string              // q1_ax_mode: mode klasifikasi A/X Q1 sesi (watchlist/alert): "atr" | "range" | "ratio"
	Q1RangeRatio          float64             // q1_range_ratio: ambang X mode "ratio" untuk Q1 (directional/range >= ini → X)
	Q1GapAnchor           bool                // q1_gap_anchor: true-move Q1 sesi di-anchor ke close candle SEBELUM sesi (gap/Volume Imbalance), bukan open
	MinRetracePct         float64             // swing_validity_min_retrace_pct
	H1Window              int                 // rolling window candle 1H untuk POI/ITH-ITL
	M5DayWindow           bool                // batasi M5 entry-trigger ke trading-day berjalan
	POITouchWindowBars    int                 // POI valid kalau harga MENYENTUH zonanya dalam N candle H1 terakhir (bukan harus di dalam zona persis di candle keputusan); 0 = point-in-time lama
	EntryFreshBars        int                 // konfirmasi 5m harus terjadi dalam N candle H1 terakhir (fill ≈ waktu keputusan); 0 = retrospektif seluruh trading-day

	// Magnitude filter swing (tunable, antisipasi A.1/C — redam swing 3-bar rapat).
	// 0 = tanpa filter (faithful default). Dalam pip gold (1 pip = $0.10).
	MinSwingPipsM5     float64 // filter ITL/ITH 5m (trigger entry) — paling berdampak ke noise
	MinSwingATRMultHTF float64 // filter zigzag HTF (anchor Fibo) sebagai kelipatan ATR sumber (×ATR) — auto-scale ke harga/volatilitas. 0 = tanpa filter. (Bug#2: pip absolut salah skala di gold)

	// MarksMinATRMult (Bug#3): filter magnitude untuk mark ITL/ITH di CHART (narrate.go).
	// minLeg = MarksMinATRMult × ATR_H1(window plot) — hanya pivot dengan leg >= minLeg
	// yang ditandai, supaya mark = ekstrem lokal nyata (bukan micro-pivot 3-bar). Murni
	// kosmetik anotasi chart; TIDAK memengaruhi gate evaluate. 0 = tanpa filter (mark semua).
	MarksMinATRMult float64

	// Risk / SL / sizing
	SLMode       detectors.SLMode // sl_anchor (default ltf_structure)
	SLBufferPips float64          // sl_buffer_pips_gold
	RiskPct      float64          // risk_per_trade_pct
	StartBalance float64          // saldo awal (basis minggu pertama)

	// Day type
	HeavyAccumMaxRangePct float64 // heavy_accumulation_max_range_pct
	HeavyAccumConfirmNY   bool    // heavy_accum_confirm_ny: stage-3 D.7 (hybrid Opsi C) — heavy_accum di-CONFIRM hanya kalau NY (06:00–12:00) juga kompresi (range < pct×ATR) DAN tak ada FVG 1H baru di NY; NY displacement → DaySuspectedAccum (tak nge-gate). false = stage-1 langsung jadi verdict (lama)

	// Execution layer (J.2) — DEFAULT OFF (cuma nge-tag, tak pernah block sinyal)
	ExecLayerOn    bool
	MaxTradePerDay int // 0 = tak terbatas
	Concurrency    int // 0 = tak terbatas

	ImpulseOnly bool // true = paksa regime IMPULSE (bias = arah weekly OF; matikan flip retrace counter-OF)

	// RetraceFVGGate = true (default): retrace HARUS punya target weekly FVG &
	// berakhir saat FVG terisi (Section E penuh — tutup leak counter-trend).
	// false = retrace unbounded (perilaku lama).
	RetraceFVGGate bool

	// ZoneFibTF = sumber Fib untuk gate zona premium/discount POI (weekly=makro
	// stale, daily=presisi default). Lihat F-Q1.
	ZoneFibTF ZoneFibTF

	// Quality gate (temuan breakdown 2026-05-31: edge di standard-trigger & POI
	// tier tinggi). RequireStandardTrigger: hanya entry kalau ITL/ITH 5m = STANDARD
	// (buang fast_early). MaxPOITier: hanya POI tier <= N (0 = tanpa batas).
	RequireStandardTrigger bool
	MaxPOITier             int

	// AMSStrictStructure (2026-06-09): deteksi ITH/ITL pakai jalur STRICT C.2/C.3 —
	// enforce SYARAT 2 (STL/STH kanan wajib di sisi benar, "standard-only") + konfirmasi
	// break STL/STH TERBARU (ratchet), bukan pra-pivot. Menyamakan engine dgn indikator
	// "Luna Trade AMS Detection". false = jalur legacy (cuma syarat 1 + pra-pivot).
	// Berdampak ke gate AMS H1 + trigger entry 5m. ⚠️ ubah baseline — validasi WF OOS.
	AMSStrictStructure bool

	// Kunci3Fallback (F.5): saat STEP POI GAGAL (tak ada POI/FVG valid searah
	// bias), aktifkan fallback opposing-liquidity. Target = Old High terdekat di
	// ATAS harga (bias sell) / Old Low terdekat di BAWAH harga (bias buy), dari
	// swing weekly/daily signifikan (LTH/LTL anchor). POI fallback = band tipis
	// (Kunci3FallbackBandPips di tiap sisi level) → lanjut trigger entry biasa.
	// DEFAULT FALSE (konservatif): jangan diam-diam ubah edge; diuji di fase sweep.
	// OFF → perilaku lama (NO SETUP saat tak ada POI).
	Kunci3Fallback bool
	// Kunci3FallbackBandPips = setengah-lebar band POI fallback di sekitar level
	// Old High/Low (pip gold). 0 = default internal. Hanya dipakai saat
	// Kunci3Fallback=true.
	Kunci3FallbackBandPips float64

	// Gate toggles (evaluasi 2026-05-31): set FALSE untuk MELEWATI gate tsb saat
	// eksperimen "kalau gate X di-off, hasilnya gimana". Default TRUE = gate aktif.
	//   AsiaCloseGate : wajib Asia session close sebelum klasifikasi (J.1 #8)
	//   QTPhaseGate   : wajib daily phase tradeable (QT phase, J.1 #2/#3/D.5)
	//   WeeklyPhaseGate: tambahan AND weekly phase tradeable di atas QTPhaseGate.
	//                   Default FALSE (rule weekly DIMATIKAN, keputusan user 2026-06-01)
	//                   — weekly phase dianggap over-filter (anchor harian, bukan Senin).
	//   DayTypeGate   : tolak heavy_accum (J.1 #5)
	//   FibZoneGate   : wajib zona Fib valid (retrace ≥0.5); OFF → Fib tak jadi
	//                   gate keras, hanya melewati filter premium/discount POI.
	//   SessionPMGate : tolak entry di sesi PM (Q4 12:00-18:00 NY, J.1 #4).
	//                   Default TRUE. OFF = izinkan entry PM (untuk A/B).
	AsiaCloseGate   bool
	QTPhaseGate     bool
	WeeklyPhaseGate bool
	DayTypeGate     bool
	FibZoneGate     bool
	SessionPMGate   bool

	// MOGate (pertemuan 8, Midnight Open): wajib entry di sisi DISCOUNT relatif
	// MO (open candle 00:00 NY trading day berjalan) — buy hanya saat harga DI
	// BAWAH MO, sell hanya DI ATAS MO ("jangan buy di premium, jangan sell di
	// discount"). Saat MO belum terbentuk (sesi Asia 18:00–00:00) gate
	// pass-through. Default FALSE — diukur dulu (breakdown "MO relative" +
	// walk-forward) sebelum di-lock. CPE (toleransi NY) belum diimplement.
	MOGate bool

	// REH/REL — Relative Equal High/Low (pertemuan 1-2 liquidity + pertemuan 10
	// QT): dua+ swing sejenis hampir selevel & belum di-sweep = liquidity target.
	//   RelEqTolerancePct   : toleransi "equal" sebagai % harga (pertemuan 10: 0.05).
	//   RelEqTolerancePips  : override pips gold (pertemuan 1-2: 5); >0 menang atas pct.
	//   RelEqLookbackBars   : window H1 scan REH/REL (≈ window chart).
	//   RelEqMaxGapBars     : gap maks antar swing berurutan dalam grup (candle
	//                         per TF) — double top harus sejajar dekat (user: 10).
	//   RelEqSweepGate      : wait-for-sweep (rule materi #6) — sell butuh REH
	//                         tersapu baru-baru ini, buy mirror REL; tak ada grup →
	//                         pass-through. Default FALSE — diukur dulu.
	//   RelEqSweepFreshBars : "baru-baru ini" = sweep dalam N candle H1 terakhir.
	RelEqTolerancePct   float64
	RelEqTolerancePips  float64
	RelEqLookbackBars   int
	RelEqMaxGapBars     int
	RelEqSweepGate      bool
	RelEqSweepFreshBars int

	// AMSGate (Layer 3, C.4): wajib ada ITL aktif 1H (buy) / ITH aktif (sell)
	// searah bias. Default TRUE. OFF = lewati struktur intermediate (untuk A/B).
	AMSGate bool

	// AgendaGate (butuh FractalPOI): kalau ada POI HTF (TF>=H4) searah bias, pilih
	// yang PALING JAUH di arah draw (buy=Bottom terendah di bawah/at harga; sell=Top
	// tertinggi di atas/at harga) sebagai SATU-SATUNYA target — entry hanya di situ,
	// abaikan POI lebih dekat/di atasnya ("agenda besar diambil dulu", user 2026-06-01).
	// Tanpa POI HTF → fall back ke pool fractal normal. Default false (eksperimen).
	AgendaGate bool
	// AgendaNearest: pilih FVG HTF TERDEKAT di arah draw (true) vs TERJAUH (false).
	AgendaNearest bool

	// FractalPOI: deteksi POI di banyak TF (H1+H4+D+W), bukan cuma H1 (user
	// 2026-06-01). HTF butuh confluence 1 (FVG tunggal fresh sudah valid) & bypass
	// MaxPOITier; H1 tetap ConfluenceMin + MaxPOITier. Ranking: TF terbesar dulu.
	// false = perilaku lama (POI H1 saja).
	FractalPOI bool

	// POIBreakWick: invalidasi komponen PD Array yang sudah di-break setelah
	// terbentuk. true = wick cukup (Low/High menembus, lebih ketat); false = harus
	// body close. IFVG dikecualikan (lahir dari break). Default true (user 2026-06-01).
	POIBreakWick bool

	// LondonQ34Only (Phase 2, QT per Session): saat sesi London, HANYA izinkan
	// entry di Q3 (03:00-04:30 NY) atau Q4 (04:30-06:00 NY). Q1 akumulasi & Q2
	// manipulasi (sweep likuiditas Asia berlawanan OF) di-skip — masuk setelah
	// manipulasi selesai. Default TRUE. Sesi lain (Asia/NY AM) tak terpengaruh.
	LondonQ34Only bool

	// LondonQ4Only (eksperimen 2026-06-02): London HANYA Q4 (04:30-06:00 NY) —
	// lebih ketat dari LondonQ34Only (Q3 ikut di-skip). Default FALSE (uji dulu).
	LondonQ4Only bool

	// OFSwingATRMult (fix diagnosa #5, 2026-06-02): filter magnitude zigzag untuk
	// WeeklyOF + ComputeRegime — swing weekly hanya dihitung struktur kalau leg
	// >= N×ATR(weekly, ATRPeriod). Menutup leak "OF flip bearish di micro swing
	// tengah bull run → short counter-trend PF0.70". 0 = off (perilaku lama).
	OFSwingATRMult float64

	// BiasSwingATRMult (audit 2026-06-02): filter magnitude zigzag untuk
	// DailyBias — unfiltered flip 17×/tahun & sisi bearish < koin (44%).
	// Swing daily dihitung struktur kalau leg >= N×ATR(daily). 0 = off.
	BiasSwingATRMult float64

	// DisableOB (user 2026-06-02): buang komponen Order Block (KindOB) dari pool
	// PD Array SEMUA TF — logika OB diragukan, di-off dulu sampai direview.
	DisableOB bool

	// OBStrict (pertemuan 4, user 2026-06-04): OB versi-benar = REVERSAL asli yg
	// butuh (a) displacement ber-FVG, (b) candle lawan-arah terakhir sblm kaki FVG,
	// (c) liquidity sweep old swing sebelum reversal (pembeda stop-hunt). false =
	// detectOBs proxy lama (0-drift). Relevan hanya saat DisableOB=false. Detail:
	// detectOBsStrict di internal/detectors/pdarray.go.
	OBStrict bool

	// BPRDirectional (pertemuan 4, user 2026-06-04): BPR versi-benar = DIRECTIONAL
	// ("dari BPR kita bisa menentukan arah market"). true = emit SATU PDR arah FVG
	// lebih baru (zona tetap IRISAN — zona-FVG-penuh diuji & merugikan, lihat
	// detectBPRs). false = perilaku lama dua-arah (0-drift). LOCK true 2026-06-04.
	BPRDirectional bool

	// DisableBPR (user 2026-06-04): buang komponen BPR dari pool PD Array semua TF.
	// Dgn zona-benar (FVG terakhir penuh) BPR = net-loser (PF bucket 0.58/−13R, seret
	// OOS 2.08→1.94) — edge asli BPR = HOLD/BREAK directional di luar scope ImpulseOnly.
	// true = off (default). Tetap di-detect utk penanda narasi/chart.
	DisableBPR bool

	// IFVGRequireNoSameDirFVG (pertemuan 4 "IFVG Fallback Rule", user 2026-06-04):
	// IFVG = POI CADANGAN — cuma sah kalau "harga tidak menyediakan FVG searah".
	// Saat true, IFVG arah D ditekan kalau saat momen flip (candle konfirmasi
	// penembusan) masih ada FVG searah D yang LIVE (belum gagal/ter-close-tembus).
	// false = perilaku lama (tiap FVG gagal selalu jadi IFVG). Detail: detectIFVGs.
	IFVGRequireNoSameDirFVG bool

	// SkipEntryHoursNY = jam NY (awal candle H1, CSV mis. "8") yang DILARANG
	// entry. Temuan 2026-06-02: jam 08:00 NY = jam terburuk sepanjang hari
	// (n=52, PF0.62, −16R) — candle 08:00-09:00 mencakup rilis berita 08:30 ET
	// (CPI/NFP dst). Kosong = tanpa skip.
	SkipEntryHoursNY string

	// SkipEntryNewsOnly (LIVE-only, di-set alertd/narrate via -news-skip) — saat
	// true, skip-hour (SkipEntryHoursNY) jadi KONDISIONAL: candle hanya di-skip
	// bila benar-benar memuat rilis USD high-impact (ada di NewsSkipHourStarts);
	// jam kandidat TANPA rilis → boleh entry. false = blanket (default/backtest/
	// parity — feed kalender hanya minggu berjalan, tak bisa divalidasi OOS).
	SkipEntryNewsOnly bool
	// NewsSkipHourStarts = set awal-jam UTC candle H1 (hour-aligned) yang memuat
	// rilis USD high-impact, dibangun BuildNewsSkipSet dari kalender minggu ini.
	// Dipakai HANYA bila SkipEntryNewsOnly. nil = tak ada/feed gagal → caller
	// menjaga SkipEntryNewsOnly=false (fallback blanket protektif).
	NewsSkipHourStarts map[time.Time]bool

	// LondonMinHourNY (user 2026-06-02): entry London hanya kalau jam NY (awal
	// candle H1) >= N. Default 4 — buang candle 03:00 (sisa manipulasi; PF0.80-0.94)
	// tapi pertahankan 04:00 (jam London terbaik, PF1.50). 0 = off (Q34 saja).
	LondonMinHourNY int

	// RetraceTPToFVG (user 2026-06-02): trade regime RETRACE pakai TP = level
	// weekly FVG target (alasan struktural retrace itu sendiri), BUKAN RR 1:3.
	// Dasar: fill-rate target 81% tapi sering dangkal (median $73) & cepat
	// (median 1 minggu) — RR jauh tak selaras dgn perilaku fill. RR di-recompute
	// dari jarak TP aktual. Fallback ke RR standar bila target tak valid/salah sisi.
	RetraceTPToFVG bool

	// OFBearConfirmWeeks (bedah flip 2026-06-02): flip WeeklyOF bull→bear butuh
	// konfirmasi kelanjutan — close weekly ke-(trigger+N) tetap di bawah close
	// trigger; kalau tidak, trigger hangus. Bullish 6/6 flip benar (tanpa syarat);
	// bearish 5/7 whipsaw → asimetris. 0 = off (perilaku lama).
	OFBearConfirmWeeks int

	// MinBiasAgeDays (bedah staleness 2026-06-02): entry hanya bila daily bias
	// sudah berumur >= N hari sejak flip. Bias baru flip (<=5 hari) = whipsaw
	// belum matang: PF0.77, bearish-fresh win 14.5%/−25R. 0 = off.
	MinBiasAgeDays int

	// MinBiasAgeDaysBull (user 2026-06-03): karantina umur-bias KHUSUS bias
	// BULLISH — fresh-bullish jauh lebih jinak (PF1.09) daripada fresh-bearish
	// (PF0.47), jadi flip balik searah tren besar tak perlu nunggu selama
	// flip bearish. <0 = ikut MinBiasAgeDays (simetris); 0 = bullish tanpa karantina.
	MinBiasAgeDaysBull int

	// MaxOFAgeDays: entry hanya bila weekly OF berumur <= N hari sejak flip.
	// OF tua (>60 hari, 60% sampel) = impulse kehabisan tenaga (PF1.29 vs ~2.2
	// di 0-60 hari). 0 = off.
	MaxOFAgeDays int

	// SkipRetrace (user 2026-06-02): regime RETRACE = NO TRADE sama sekali.
	// Beda dari ImpulseOnly (yang MEMAKSA trade searah OF di periode retrace —
	// berisiko long di tengah koreksi). Subset retrace kronis busuk (PF~0.3).
	SkipRetrace bool

	// AllowEarlyFlip (B.3 Skenario B, G.4 entry_flip_timing): izinkan entry
	// saat daily bias sudah flip ke arah berlawanan weekly OF definitif TAPI
	// dailyRefLevel masih di bawah LTH/di atas LTL (= intermediate swing,
	// bukan macro break). Entry di-tag FlipTimingEarly. Default false
	// (entry hanya di OF definitif = backward-compatible).
	AllowEarlyFlip bool

	// MaxConfluence (temuan 2026-06-02): cap ATAS jumlah komponen POI — semua TF.
	// Confluence tinggi terbukti memburuk MONOTON (H4 conf4+ PF0.30, D conf4+
	// PF0.00): zona rame = band lebar/kontes likuiditas, bukan kekuatan.
	// POI dengan Confluence() > N dibuang. 0 = off.
	MaxConfluence int

	// BBNeedsFVG (uji user 2026-06-02, alternatif sebelum drop-BB total): POI
	// yang MENGANDUNG Breaker (KindBB) tapi TIDAK mengandung keluarga imbalance
	// (FVG/FVGBreak/IFVG) dibuang — BB hanya sah berdampingan FVG. Dasar:
	// D-1×BB murni PF0.92, D-ada-BB PF0.83. Default false (diuji dulu).
	BBNeedsFVG bool

	// BBRequireDisplacement (uji user 2026-06-09): BB hanya sah kalau FVG penyahnya
	// = displacement IMPULSIF yang KELUAR dari range candle BB (bukan gap mungil yg
	// muter di area BB). Bullish: FVG.Top > BB.High; bearish: FVG.Bottom < BB.Low.
	// Tanpa ini detectBBs cuma cek "ada FVG dlm <=3 candle" tanpa peduli ukuran/posisi.
	// Default false (paritas → diuji dulu). Rules F.1: "keberadaan FVG = bukti move
	// impulsive" — knob ini memperketat tafsir "impulsive" jadi displacement-keluar-range.
	BBRequireDisplacement bool

	// EntryTriggerMode (uji user 2026-06-03 — trigger ITL/ITH 5m memotong 87%
	// kandidat di tahap akhir, cari alternatif):
	//   "itl"        (default) — struktur ITL/ITH 5m standard (perilaku lama)
	//   "disp"       — candle 5m displacement searah bias (body >= DispATRMult×ATR m5)
	//   "reject"     — candle 5m rejection: wick masuk zona POI, close keluar searah bias
	//   "dispreject" — disp ATAU reject (gabungan B+C)
	//   "sweep"      — candle 5m menembus swing-low (buy) / swing-high (sell) 5m
	//                  terakhir → langsung entry (usul user: entry di sweep-nya)
	EntryTriggerMode string

	// DispATRMult = ambang body candle displacement (× ATR m5, mode disp/dispreject).
	// Provisional 1.0 — di-sweep.
	DispATRMult float64

	// LondonSweepEntry (rule London user 2026-06-02, konsep "Q2 sweep low Asia
	// → Q3 naik → ITL 5m baru = entry"): jam London yang biasanya diblok
	// (LondonQ34Only / LondonMinHourNY) BOLEH entry bila likuiditas sesi Asia
	// trading-day ini sudah DI-SWEEP berlawanan bias (buy → wick H1 pasca-Asia
	// menembus LOW Asia; sell → menembus HIGH Asia) — manipulasi dianggap
	// selesai, tak perlu tunggu jam. Syarat POI-touch + trigger 5m standard
	// tetap berlaku. Default false (diuji dulu).
	LondonSweepEntry bool
}

// DefaultConfig = preset Section M (semua execution-limit OFF, alert mode).
func DefaultConfig() Config {
	return Config{
		MinGapPips:             5,
		VIMinGapPips:           15,     // Bug#5: VI threshold 3× FVG ($1.50) — VI harus gap signifikan, bukan tiap noise $0.50
		POIMaxWidthATRMult:     0.5,    // Bug#5: cap lebar cluster POI = 0.5×ATR_H1 (cegah band lebar belasan PDR berjauhan)
		FVGSwingBreakAdjacency: 12,     // Kunci #1: FVG <=12 candle dari swing yg baru di-break → Tier-2 (LOCK 2026-06-04: puncak kurva IS+OOS, +7R OOS vs adj=3, PF/DD datar; saturasi >=15)
		FVGBreakGeometric:      false,  // default OFF — versi geometris (mirror Pine) OOS-dominated (2026-06-11); opt-in utk re-eval -from 2018
		FVGBreakTFs:            "H1,D", // LOCK 2026-06-04: promosi Tier-2 FVGBreak hanya H1+D (strip H4+W). OOS-dominan: R terjaga, PF 2.05→2.08, DD 8.9→6.2%. FVGBreak merugikan di H4, netral di D
		BPRMaxDistanceCandles:  5,      // BPR: BISI+SIBI overlap dalam <=5 candle (F.1)
		ConfluenceMin:          2,
		BreakType:              detectors.DefaultBreakType,
		ATRPeriod:              14,
		MinAtrMult:             1.5,
		AsiaAXMode:             "atr",
		AsiaRangeRatio:         0.5,
		AsiaRequireFVG:         false,
		AsiaGapAnchor:          true,  // DEFAULT ON (user 2026-06-15): true-move Asia di-anchor ke close candle sebelum sesi → gap pembukaan (Volume Imbalance) ikut dihitung
		Q1AXMode:               "atr", // default sama dgn Asia; ganti "range"/"ratio" utk Q1 lebih sensitif
		Q1RangeRatio:           0.3,   // ratio mode: dir/range >= 0.3 → X (satu arah > 30% dari total range)
		Q1GapAnchor:            true,  // DEFAULT ON (user 2026-06-15): true-move Q1 sesi di-anchor ke close candle sebelum sesi (gap/Volume Imbalance)
		MinRetracePct:          detectors.DefaultMinRetracePct,
		H1Window:               300,
		M5DayWindow:            true,
		POITouchWindowBars:     4, // user 2026-06-03: sentuhan POI berlaku 4 jam (lever volume: +31% trade, OOS +18% R/PF2.19 vs 2.36 — trade-off frekuensi diterima user). 2 = mode kualitas-maks lama
		EntryFreshBars:         1,
		MinSwingPipsM5:         0,
		MinSwingATRMultHTF:     1.0, // filter swing HTF >= 1× ATR sumber (provisional; di-sweep). Bug#2 fix: ATR-relative, bukan pip absolut $20.
		MarksMinATRMult:        1.0, // Bug#3: mark ITL/ITH di chart hanya kalau leg >= 1× ATR_H1 (pivot signifikan, bukan micro 3-bar).
		SLMode:                 detectors.SLLtfStructure,
		SLBufferPips:           detectors.SLBufferPips,
		RiskPct:                detectors.RiskPct,
		StartBalance:           25000,
		HeavyAccumMaxRangePct:  0.4,
		HeavyAccumConfirmNY:    false, // OFF (default, parity); stage-3 NY-confirm — uji IS/OOS dulu sebelum lock
		ExecLayerOn:            false,
		MaxTradePerDay:         3,
		Concurrency:            0,
		// ImpulseOnly=true (user 2026-06-02, uji "matikan retrace"): retrace
		// counter-OF dimatikan, periode gate-sweep-open tetap trade SEARAH OF.
		// OOS: +197R/PF2.01/DD7.9% vs default-lama +180R/1.96/8.4 vs SkipRetrace
		// +180R/2.02/8.0 — ImpulseOnly menang R dgn PF/DD setara, 4/4 fold positif.
		// (Catatan sejarah: di config era awal knob ini pernah diuji & buruk —
		// konteks gate sudah jauh berubah.) SkipRetrace knob tetap tersedia.
		ImpulseOnly:            true,
		RetraceFVGGate:         true,         // Section E penuh: retrace terikat target weekly FVG
		ZoneFibTF:              ZoneFibDaily, // perbaikan default: zona dari daily (non-stale)
		RequireStandardTrigger: true,         // quality: buang fast_early (PF0.77) — edge di standard (PF1.75)
		AMSStrictStructure:     false,        // 2026-06-09: default OFF (paritas) — diuji WF OOS sebelum lock
		// MaxPOITier 2→3 (uji reminder user 2026-06-02): hanya berlaku pool H1
		// (HTF bypass). Marginal T3-H1 kecil tapi positif (n=6, PF3.00 IS);
		// OOS dominan tipis semua metrik (+205R/PF2.04/DD7.9% vs +197R/2.01/7.9).
		// Tier 4/0 tak menambah apa pun kecuali 1 loser → stop di 3.
		MaxPOITier:             3,
		Kunci3Fallback:         false, // F.5: DEFAULT OFF — perilaku lama (NO SETUP saat tak ada POI). Diuji di sweep.
		Kunci3FallbackBandPips: 30,    // band fallback ±30 pip ($3) sekitar Old High/Low — provisional, di-sweep
		// Gate config keputusan user 2026-06-01 (setelah evaluasi QT + funnel):
		AsiaCloseGate:   false, // OFF — Asia-close tak lagi gating
		QTPhaseGate:     false, // OFF — QT phase (daily) tak lagi gating
		WeeklyPhaseGate: false, // OFF — weekly phase dimatikan (over-filter)
		DayTypeGate:     true,  // ON — heavy_accum terbukti protektif (off → OOS −4R→−18R)
		FibZoneGate:     false, // OFF — Fib tak jadi gate keras (hanya filter premium/discount POI)
		SessionPMGate:   false, // OFF (user 2026-06-02) — PM session DINYALAKAN. A/B: OOS +76R→+161R/PF1.30→1.45, win 32.5%→35.0%; subset PM win ~43.7%. Gate J.1 #4 over-conservative.
		AMSGate:         false, // OFF (user 2026-06-01) — A/B: OOS +56R→+116R/PF naik. AMS tetap ditampilkan di narasi sbg PENANDA (bukan gate).
		MOGate:          false, // OFF (pertemuan 8, 2026-06-02) — Midnight Open divider premium/discount. Diukur dulu via breakdown "MO relative" + A/B + WF sebelum lock; MO tetap tampil di narasi/chart sbg PENANDA.
		// REH/REL (pertemuan 1-2 + 10, 2026-06-02): penanda liquidity selalu-on; gate wait-for-sweep OFF sampai terbukti (pola MO).
		RelEqTolerancePct:   0.05, // toleransi "equal" 0.05% harga (pertemuan 10; auto-scale era 1600→4700)
		RelEqTolerancePips:  0,    // 0 = pakai pct; kandidat pertemuan 1-2: 5 pip ($0.50) — diuji di sweep
		RelEqLookbackBars:   120,  // window scan = window chart H1
		RelEqMaxGapBars:     10,   // user 2026-06-02: swing equal maks 10 candle sejajar (bukan puncak kebetulan berhari-hari)
		RelEqSweepGate:      false,
		RelEqSweepFreshBars: 6,
		LondonQ34Only:       true,  // Phase 2 — London hanya Q3/Q4 (lewati manipulasi Q1/Q2)
		LondonQ4Only:        false, // uji 2026-06-02: OOS PF/DD sedikit lebih baik TAPI per-fold mixed + bertentangan dgn atribusi per-jam (04:00 = jam London TERBAIK justru kena buang) → TIDAK di-lock
		// Filter magnitude OF/bias: hipotesis "OF flip di micro swing = sumber rugi short"
		// TIDAK terbukti memperbaiki agregat (sweep IS: ofatr 0 terbaik +211R/PF1.59;
		// biasatr>0 konsisten memburukkan; counter-trend short memang berkurang −18→−10R
		// tapi ter-offset short searah-tren). Default 0 = perilaku lama; knob tersedia
		// (-ofatr/-biasatr) utk re-test saat sampel lebih besar.
		OFSwingATRMult:          0,
		BiasSwingATRMult:        0,
		DisableOB:               true,  // user 2026-06-02: logika OB diragukan — off semua TF (OOS PF 1.46→1.60, DD 14.7→9.0% bareng skip8)
		OBStrict:                false, // default OFF → detectOBs proxy lama (parity 0-drift). Aktifkan via -obstrict / ob_strict utk validasi OB versi-pertemuan-4 (relevan hanya bila DisableOB=false)
		DisableBPR:              false, // DIUJI 2026-06-04: buang BPR → OOS 2.08→1.97 (lebih buruk; BPR net-akretif via seleksi walau bucket lemah — pola "gate substitusi") → BPR TETAP aktif. knob tersedia (off)
		BPRDirectional:          true,  // LOCK 2026-06-04: BPR directional (arah = FVG lebih baru). Backtest IDENTIK (IS+OOS+label byte-sama — BPR ter-trade memang sudah directional; dua-arah cuma phantom di pool tak terpilih). Lock correctness-driven (faithful pertemuan 4 + bersihkan BPR arah-salah dari watchlist/narasi). false = dua-arah lama
		IFVGRequireNoSameDirFVG: true,  // IFVG Fallback Rule pertemuan 4 (user 2026-06-04): IFVG cuma sah kalau tak ada FVG searah live. Backtest NETRAL (IS −2R, OOS −2R, PF2.05/DD8.9% identik) tapi faithful ke rules + fix IFVG "terlalu jauh" di narasi/watchlist
		SkipEntryHoursNY:        "8",   // skip jam 08:00 NY (news hour 08:30 ET; PF0.62/−16R IS)
		LondonMinHourNY:         4,     // user 2026-06-02: entry London mulai 04:00 NY (buang 03:00)
		// RetraceTPToFVG DIUJI 2026-06-02 atas permintaan user → TIDAK works:
		// jarak entry→target FVG ternyata 10-17× risk SL 5m (yang "dangkal" itu
		// jarak dari SWING pemicu, bukan dari entry) → retrace 0% win/−18R IS
		// (vs RR standar −10R). Default FALSE; knob -retracetp utk re-test.
		RetraceTPToFVG: false,
		// OFBearConfirmWeeks DIUJI & GAGAL di engine (estimasi agent +25% tak
		// bertahan): delay flip 1 minggu MENGGANTI 33 short net-positif dgn 43
		// LONG di tengah koreksi nyata (PF0.71) → IS +212→+191R. Default 0.
		OFBearConfirmWeeks: 0,
		// MinBiasAgeDays=8 OOS-DOMINAN vs baseline (semua metrik): WF OOS
		// +180R/PF1.96/win42%/DD8.4% vs +166R/1.64/37.7%/8.5%. Kurva cutoff
		// mulus (4/6/8 → PF 1.73/1.76/1.91 IS), bukan knife-edge.
		MinBiasAgeDays:     8,
		MinBiasAgeDaysBull: 3, // user 2026-06-03: bullish cukup 3 hari (fresh-bull PF1.09 vs fresh-bear 0.47)
		// MaxOFAgeDays default OFF: standalone memangkas totR besar (+212→+140);
		// KOMBO biasage6+ofage60 = "mode konservatif" OOS PF2.36/DD4.8% tapi
		// −46R — tersedia via config bagi yang prioritas DD.
		MaxOFAgeDays: 0,
		SkipRetrace:  false, // diuji dulu (IS retrace n=10 PF0.33) — lock setelah OOS
		// MaxConfluence=3 + BBNeedsFVG=true DI-LOCK (uji user 2026-06-02):
		// IS 374tr/+241R/PF2.16/DD13R; OOS +202R/PF2.36/win46.8%/MaxDD6.7%,
		// SEMUA 4 fold PF>=2.05 (paling konsisten sejauh ini). Yang dibuang:
		// POI conf>3 (PF0.56) + cluster BB-tanpa-FVG (PF1.07 breakeven).
		MaxConfluence:         3,
		BBNeedsFVG:            true,
		BBRequireDisplacement: true, // LOCK 2026-06-09 (correctness, P&L-neutral): FVG penyah BB wajib displacement keluar range candle BB

		LondonSweepEntry: false, // rule London sweep-Asia — diuji & TIDAK menolong (net −1R)
		EntryTriggerMode: "itl", // alternatif disp/reject/dispreject/sweep — diuji 2026-06-03
		DispATRMult:      1.0,
		FractalPOI:       true,  // POI fractal multi-TF (H1+H4+D+W) — user 2026-06-01
		AgendaGate:       false, // agenda "farthest=sole target" non-viable (all:18tr, FVG-only:6tr) — sole-target terlalu langka; perlu framing GATE, lihat memory
		POIBreakWick:     false, // invalidasi PDR pakai BODY close (user 2026-06-01): wick terlalu agresif (PF1.00, bentrok stop-hunt); body jaga edge (OOS PF1.46)
	}
}

// TFData = candle per timeframe (kronologis, complete-only midpoint).
type TFData struct {
	Weekly []data.Candle
	Daily  []data.Candle
	H4     []data.Candle
	H1     []data.Candle
	M5     []data.Candle
	// M15 dipakai untuk QT per-session (Phase 2): klasifikasi A/X tiap Q1 sesi
	// (15m view, fractal turun dari daily-QT 1H). Tidak dipakai pipeline engine.Run
	// — diisi & dibaca oleh alertd (alert Q1 close). Loader lain boleh biarkan nil.
	M15 []data.Candle
}

// Signal = satu alert entry (skema ringkas Section L). Selalu di-emit kalau
// STEP 1–5 lolos (signal layer); WouldBeSkipped diisi execution layer (J.2).
// FlipTiming = label G.4 (entry_flip_timing: both_tagged, Section L breakdown).
// Membedakan entry dalam konteks flip OF vs OF mapan.
type FlipTiming int

const (
	FlipTimingNA        FlipTiming = iota // OF stabil, tidak dalam konteks flip
	FlipTimingEarly                       // B.3 Skenario B (sebelum LTH/LTL macro break definitif)
	FlipTimingDefinitif                   // dalam window 4-minggu setelah OF flip definitif
)

func (f FlipTiming) String() string {
	switch f {
	case FlipTimingEarly:
		return "early"
	case FlipTimingDefinitif:
		return "definitif"
	default:
		return "n/a"
	}
}

// flipContextWindowDays = window setelah OF flip definitif yang entry-nya
// di-tag FlipTimingDefinitif (vs FlipTimingNA = OF sudah mapan).
const flipContextWindowDays = 28

type Signal struct {
	Time  time.Time
	Dir   detectors.Direction
	Entry float64
	SL    float64
	TP    float64
	RR    float64

	// Konteks pipeline (tag, untuk breakdown report)
	Regime        state.Regime
	Session       detectors.SessionKind
	WeeklyPhase   detectors.Phase
	DailyPhase    detectors.Phase
	Scenario      detectors.Scenario
	DayType       detectors.DayType
	ITHITLType    detectors.ITType
	POITier       int
	POIConfluence int
	POITF         detectors.TFKind // TF asal POI (fractal multi-TF)
	POIKinds      string           // rincian komponen per kind, mis. "1×VI + 2×FVG" (poiKindSummary)
	MORel         string           // posisi entry relatif Midnight Open: "discount" | "premium" | "-" (MO belum terbentuk)
	RelEqSwept    string           // wait-for-sweep REH/REL: "swept_fresh" | "no_sweep" | "-" (tak ada grup relevan)
	FlipTiming    FlipTiming       // G.4: n/a | early | definitif (B.3 state machine)

	// Execution layer (J.2) — sinyal TETAP tercatat walau true
	WouldBeSkipped bool
	SkipReason     string

	// internal (untuk simulasi)
	fillM5Time time.Time
}

// Trade = Signal yang sudah ditutup (hasil simulasi K).
type Trade struct {
	Signal
	ExitTime   time.Time
	ExitPrice  float64
	ExitReason string // "tp" | "sl" | "friday_close" | "eod_data"
	RRealized  float64
	PnLUSD     float64
	Lot        float64
	RiskUSD    float64
	BalanceAt  float64 // basis minggu yang dipakai sizing
}

func (t Trade) Win() bool { return t.RRealized > 0 }

// gateCode = gate PERTAMA yang menggagalkan satu candle (urut pyramid). Dipakai
// diagnostik GateStats untuk tahu gate mana yang paling banyak makan korban.
// gatePass = lolos semua → Signal di-emit.
type gateCode int

const (
	gatePass         gateCode = iota
	gateData                  // 0: data weekly/H1 belum cukup
	gateOF                    // 1a: weekly Order Flow undefined
	gateOFStale               // 1b: weekly OF terlalu tua (> MaxOFAgeDays sejak flip)
	gateRetraceSkip           // 1b': regime retrace di-skip (SkipRetrace)
	gateInvalidation          // 1c: LTL/LTH sudah di-touch (invalidation)
	gateDailyBias             // 2: daily bias tak align / undefined
	gateBiasFresh             // 2b: daily bias baru flip (< MinBiasAgeDays)
	gateAMS                   // 3: tak ada ITL/ITH 1H aktif searah bias (Layer 3 AMS, C.4)
	gateSessionPM             // 4 #4: sesi PM (tak entry)
	gateSkipHour              // 4: jam NY di-skip (SkipEntryHoursNY, mis. news hour 08:00)
	gateLondonQT              // 4 Phase2: London Q1/Q2 (akumulasi/manipulasi) — tunggu Q3/Q4
	gateAsiaClose             // 4 #8: Asia belum close
	gatePhase                 // 4 #2,#3,D.5: phase tak tradeable
	gateDayType               // 4 #5: heavy_accum
	gateMO                    // 4.5: harga di sisi salah Midnight Open (premium utk buy / discount utk sell)
	gateRelEq                 // 4.6: wait-for-sweep — REH/REL relevan belum tersapu baru-baru ini
	gateFib                   // 5: zoneFib gagal (tak ada leg/retrace valid)
	gatePOI                   // 5 #6: tak ada POI/PD Array searah bias
	gateOpposingLiq           // 5: Kunci#3 fallback tapi tak ada opposing liquidity
	gateEntry                 // 6: tak ada trigger entry 5m
	gateSLSane                // 7: SL tak masuk akal (di sisi salah)
)

// GateLabel = nama human-readable per gateCode (urut pyramid).
func GateLabel(g gateCode) string {
	switch g {
	case gatePass:
		return "PASS (entry)"
	case gateData:
		return "0. Data tak cukup"
	case gateOF:
		return "1a. Weekly OF undefined"
	case gateOFStale:
		return "1b. Weekly OF terlalu tua"
	case gateRetraceSkip:
		return "1b'. Regime retrace (di-skip)"
	case gateInvalidation:
		return "1c. LTL/LTH ter-touch (invalidation)"
	case gateDailyBias:
		return "2. Daily bias tak align"
	case gateBiasFresh:
		return "2b. Daily bias baru flip (belum matang)"
	case gateAMS:
		return "3. AMS: tak ada ITL/ITH 1H aktif searah"
	case gateSessionPM:
		return "4. Sesi PM"
	case gateSkipHour:
		return "4. Jam di-skip (news hour)"
	case gateLondonQT:
		return "4. London Q1/Q2 (manipulasi)"
	case gateAsiaClose:
		return "4. Asia belum close"
	case gatePhase:
		return "4. QT phase tak tradeable"
	case gateDayType:
		return "4. Day type heavy_accum"
	case gateMO:
		return "4.5. MO: harga di sisi salah Midnight Open"
	case gateRelEq:
		return "4.6. RelEq: REH/REL belum tersapu (wait-for-sweep)"
	case gateFib:
		return "5. Fib zone gagal"
	case gatePOI:
		return "5. Tak ada POI searah"
	case gateOpposingLiq:
		return "5. Kunci#3 tanpa opposing-liq"
	case gateEntry:
		return "6. Tak ada trigger 5m"
	case gateSLSane:
		return "7. SL tak masuk akal"
	default:
		return "?"
	}
}

// GateStat = satu baris hitungan diagnostik.
type GateStat struct {
	Gate  gateCode
	Label string
	Count int
}

// GateStats menjalankan evaluate atas SEMUA candle H1 dan menghitung gate
// PERTAMA yang menggagalkan tiap candle (atau gatePass = entry). Mengikuti
// pipeline asli persis (memanggil evaluate yang sama dengan Run).
func GateStats(tf TFData, cfg Config) ([]GateStat, int, error) {
	loc, err := detectors.NYLocation()
	if err != nil {
		return nil, 0, err
	}
	counts := map[gateCode]int{}
	total := 0
	for i := range tf.H1 {
		now := tf.H1[i].Time.Add(time.Hour)
		_, _, g := evaluate(tf, cfg, loc, i, now)
		counts[g]++
		total++
	}
	order := []gateCode{
		gatePass, gateData, gateOF, gateOFStale, gateRetraceSkip, gateInvalidation, gateDailyBias, gateBiasFresh,
		gateAMS, gateSessionPM, gateSkipHour, gateLondonQT, gateAsiaClose, gatePhase, gateDayType,
		gateMO, gateRelEq, gateFib, gatePOI, gateOpposingLiq, gateEntry, gateSLSane,
	}
	out := make([]GateStat, 0, len(order))
	for _, g := range order {
		out = append(out, GateStat{Gate: g, Label: GateLabel(g), Count: counts[g]})
	}
	return out, total, nil
}

// Result = keluaran satu run backtest.
type Result struct {
	Signals []Signal
	Trades  []Trade
	Config  Config
	Start   time.Time
	End     time.Time
}

// Run menjalankan pipeline N.2 atas TFData. Mengembalikan semua sinyal + trade
// tersimulasi. Tidak menyentuh jaringan — murni atas candle yang diberikan.
func Run(tf TFData, cfg Config) (Result, error) {
	loc, err := detectors.NYLocation()
	if err != nil {
		return Result{}, err
	}
	res := Result{Config: cfg}
	if len(tf.H1) == 0 {
		return res, nil
	}
	res.Start = tf.H1[0].Time
	res.End = tf.H1[len(tf.H1)-1].Time

	// Dedup sinyal: per arah, simpan waktu konfirmasi 5m terakhir yang sudah
	// di-emit, supaya POI yang sama tidak banjir sinyal tiap candle 1H.
	lastTrigger := map[detectors.Direction]time.Time{}

	for i := range tf.H1 {
		now := tf.H1[i].Time.Add(time.Hour) // close candle 1H ini
		sig, ok, _ := evaluate(tf, cfg, loc, i, now)
		if !ok {
			continue
		}
		// dedup
		if t, seen := lastTrigger[sig.Dir]; seen && !sig.fillM5Time.After(t) {
			continue
		}
		lastTrigger[sig.Dir] = sig.fillM5Time
		res.Signals = append(res.Signals, sig)
	}

	// Simulasi tiap sinyal (K) → trade. R independen dari sizing (1:RR fixed).
	trades := simulateAll(tf.M5, cfg, loc, res.Signals)
	// Sizing + equity + weekly re-baseline (I.1), urut waktu fill.
	applySizing(&res, cfg, loc, trades)
	return res, nil
}

// snapshot = state STEP 0 untuk satu candle 1H (semua TF, complete-only).
type snapshot struct {
	weekly []data.Candle
	daily  []data.Candle
	h1     []data.Candle // rolling window
	price  float64

	ofDir     detectors.Direction
	ofOK      bool
	anchors   state.Anchors
	regime    state.Regime
	bias      detectors.Direction // arah trade aktual (OF + regime)
	minLegW   float64             // ambang magnitude swing weekly (OFSwingATRMult×ATR)
	earlyFlip bool                // true = Skenario B early path (bias counter weekly OF definitif)

	dailyBias detectors.Direction
	dailyOK   bool

	scenario    detectors.Scenario
	session     detectors.SessionKind
	weeklyPhase detectors.Phase
	dailyPhase  detectors.Phase
	dayType     detectors.DayType

	macroFib detectors.Fib
	fibOK    bool
	pois     []detectors.POI
}

// amsKind memetakan bias trade → jenis intermediate 1H yang WAJIB aktif (C.4):
// bias buy butuh ITL aktif, bias sell butuh ITH aktif.
func amsKind(bias detectors.Direction) detectors.ITKind {
	if bias == detectors.Bearish {
		return detectors.ITHigh
	}
	return detectors.ITLow
}

// evaluate menjalankan STEP 0–7 untuk candle 1H index i. ok=false = di-skip
// (salah satu gate J.1 gagal); ok=true → Signal siap (STEP 7 execution layer
// sudah diterapkan sebagai tag).
func evaluate(tf TFData, cfg Config, loc *time.Location, i int, now time.Time) (Signal, bool, gateCode) {
	s := snapshot{}

	// ---- STEP 0: update state semua TF (complete-only) ----
	s.weekly = closedBy(tf.Weekly, now)
	s.daily = closedBy(tf.Daily, now)
	h1Closed := tf.H1[:i+1] // candle i baru saja close
	s.h1 = window(h1Closed, cfg.H1Window)
	if len(s.h1) == 0 || len(s.weekly) < 3 {
		return Signal{}, false, gateData
	}
	s.price = tf.H1[i].Close

	var ok bool
	minLegW := ofMinLeg(s.weekly, cfg) // fix #5: filter magnitude swing weekly
	s.minLegW = minLegW
	var ofFlip time.Time
	s.ofDir, s.anchors, ofFlip, ok = state.WeeklyOFFull(s.weekly, minLegW, cfg.OFBearConfirmWeeks)
	s.ofOK = ok
	if !s.ofOK {
		return Signal{}, false, gateOF // STEP 1a: OF undefined → SKIP
	}
	// 1b: OF maturity cap — OF berumur > MaxOFAgeDays = impulse kehabisan tenaga.
	if cfg.MaxOFAgeDays > 0 && now.Sub(ofFlip) > time.Duration(cfg.MaxOFAgeDays)*24*time.Hour {
		return Signal{}, false, gateOFStale
	}
	s.regime = state.ComputeRegimeMin(s.weekly, s.ofDir, cfg.RetraceFVGGate, cfg.MinGapPips, minLegW)
	if cfg.ImpulseOnly {
		s.regime = state.RegimeImpulse
	}
	// SkipRetrace: regime retrace = NO TRADE (beda dari ImpulseOnly yang
	// MEMAKSA trade searah OF di periode yang sama). Subset retrace PF~0.3.
	if cfg.SkipRetrace && s.regime == state.RegimeRetrace {
		return Signal{}, false, gateRetraceSkip
	}
	s.bias = state.TradeDirection(s.ofDir, s.regime)

	// ---- STEP 1.5: daily bias + Skenario B detection (B.3) ----
	// Harus SEBELUM touchedInvalidation agar early path gunakan bias yang benar.
	var biasFlip time.Time
	var dailyRefLevel float64
	s.dailyBias, biasFlip, dailyRefLevel, s.dailyOK = state.DailyBiasRef(s.daily, biasMinLeg(s.daily, cfg))
	if cfg.AllowEarlyFlip && s.dailyOK && s.regime == state.RegimeImpulse {
		if isEarly, earlyDir := state.DetectSkenarioB(s.anchors, s.ofDir, s.dailyBias, dailyRefLevel); isEarly {
			s.bias = earlyDir // override bias → arah early flip
			s.earlyFlip = true
		}
	}

	// ---- STEP 1: bias + gate macro (J.1 #1, #9, #10) ----
	// 1c: invalidation guard — pakai s.bias (sudah di-override untuk early path).
	// earlyBullish (ofDir=BEARISH, bias=BULLISH): invalidation = price < LTL.
	// earlyBearish (ofDir=BULLISH, bias=BEARISH): invalidation = price > LTH.
	if touchedInvalidation(s) {
		return Signal{}, false, gateInvalidation
	}

	// ---- STEP 2: daily bias alignment ----
	flipTiming := FlipTimingNA
	if !s.earlyFlip {
		// Normal: strict alignment B2-Q3.
		if !s.dailyOK || s.dailyBias != s.bias {
			return Signal{}, false, gateDailyBias
		}
		// Tentukan flip context: definitif jika dalam window 4 minggu sejak OF flip.
		if now.Sub(ofFlip) < flipContextWindowDays*24*time.Hour {
			flipTiming = FlipTimingDefinitif
		}
	} else {
		// Early path (Skenario B): daily sudah searah earlyDir, skip strict alignment.
		// s.dailyBias == s.bias (= earlyDir) terjamin oleh DetectSkenarioB.
		flipTiming = FlipTimingEarly
	}
	// 2b: maturity floor berlaku untuk kedua jalur.
	if minAge := biasAgeMin(cfg, s.dailyBias); minAge > 0 && now.Sub(biasFlip) < time.Duration(minAge)*24*time.Hour {
		return Signal{}, false, gateBiasFresh
	}

	// ---- STEP 3: AMS — struktur intermediate 1H searah bias (Layer 3, C.4) ----
	// Buy → WAJIB ada ITL aktif (belum di-break ke bawah) di 1H; Sell → ITH aktif.
	// Tanpa struktur intermediate searah, "fokus arah mingguan" (C.4) belum terbentuk.
	if cfg.AMSGate {
		if _, ok := detectors.ActiveIntermediate(s.h1, amsKind(s.bias), cfg.BreakType, 0, cfg.AMSStrictStructure); !ok {
			return Signal{}, false, gateAMS
		}
	}

	// ---- STEP 4: QT timing + session + day type (J.1 #2,#3,#4,#5,#8) ----
	s.session = detectors.Session(tf.H1[i].Time, loc)
	if cfg.SessionPMGate && s.session == detectors.PM {
		return Signal{}, false, gateSessionPM // #4
	}
	// Skip-hour: jam NY terlarang entry (mis. 08:00 = news hour 08:30 ET).
	// Mode news-only (live): jam kandidat hanya di-skip bila candle memuat rilis
	// USD high-impact; tanpa rilis → boleh entry. Default (off) = blanket.
	if hourSkipped(tf.H1[i].Time, loc, cfg.SkipEntryHoursNY) {
		if cfg.SkipEntryNewsOnly {
			if cfg.NewsSkipHourStarts[tf.H1[i].Time] {
				return Signal{}, false, gateSkipHour // ada rilis jam ini → skip
			}
			// jam kandidat tapi tak ada rilis high-impact → lanjut (boleh entry)
		} else {
			return Signal{}, false, gateSkipHour // blanket (backtest/parity)
		}
	}
	// LondonSweepEntry: kalau likuiditas Asia trading-day ini SUDAH di-sweep
	// berlawanan bias, manipulasi London dianggap selesai → bypass gate
	// quarter/jam London (konsep user: Q2 sweep low Asia → Q3 naik → entry).
	londonBypass := cfg.LondonSweepEntry && s.session == detectors.London &&
		asiaSwept(h1Closed, loc, now, s.bias)
	// Phase 2 — QT per Session London: Q1 akumulasi & Q2 manipulasi (sweep Asia
	// berlawanan OF) → tunggu. Aman entry minimal Q3 (03:00 NY); LondonQ4Only
	// (eksperimen) menggeser ambang ke Q4 (04:30 NY).
	if s.session == detectors.London && (cfg.LondonQ34Only || cfg.LondonQ4Only) && !londonBypass {
		q := detectors.LondonQuarter(tf.H1[i].Time, loc)
		minQ := 3
		if cfg.LondonQ4Only {
			minQ = 4
		}
		if q < minQ {
			return Signal{}, false, gateLondonQT
		}
	}
	// User 2026-06-02: entry London hanya mulai jam LondonMinHourNY (default 4)
	// — candle 03:00 (sisa manipulasi) di-skip, 04:00 (jam terbaik) dipertahankan.
	if cfg.LondonMinHourNY > 0 && s.session == detectors.London && !londonBypass &&
		tf.H1[i].Time.In(loc).Hour() < cfg.LondonMinHourNY {
		return Signal{}, false, gateLondonQT
	}
	asiaH1, atr1h, asiaPrevClose, asiaClosed := asiaSlice(h1Closed, loc, cfg.ATRPeriod, now)
	if !asiaClosed && cfg.AsiaCloseGate {
		return Signal{}, false, gateAsiaClose // #8: Asia belum close → belum classifiable
	}
	s.scenario = detectors.ClassifyDailyG(asiaH1, asiaAnchor(cfg.AsiaGapAnchor, asiaPrevClose), atr1h, cfg.MinAtrMult, cfg.MinGapPips, cfg.AsiaRangeRatio, cfg.AsiaAXMode, cfg.AsiaRequireFVG)
	wd := detectors.TradingWeekday(tf.H1[i].Time, loc)
	s.weeklyPhase = detectors.WeeklyPhase(s.scenario, wd)
	s.dailyPhase = detectors.DailyPhase(s.scenario, s.session)
	if cfg.QTPhaseGate {
		// Weekly phase DIMATIKAN secara default (user 2026-06-01): hanya daily phase
		// yang gating. WeeklyPhaseGate=true mengembalikan AND weekly (D.5 lama).
		phaseOK := s.dailyPhase.Tradeable()
		if cfg.WeeklyPhaseGate {
			phaseOK = phaseOK && s.weeklyPhase.Tradeable()
		}
		if !phaseOK {
			return Signal{}, false, gatePhase // #2,#3 + kombinasi D.5
		}
	}
	s.dayType = classifyDayType(h1Closed, s.daily, loc, cfg, now)
	if cfg.DayTypeGate && s.dayType == detectors.DayHeavyAccum {
		return Signal{}, false, gateDayType // #5 (heavy_expanding lolos, tag caution)
	}

	// ---- STEP 4.5: Midnight Open (pertemuan 8) ----
	// MO = open candle 00:00 NY = divider premium/discount hari itu. Gate:
	// buy hanya di DISCOUNT (harga < MO), sell hanya di PREMIUM (harga > MO).
	// MO belum terbentuk (Asia) → pass-through. moPrice juga dipakai men-tag
	// Signal.MORel untuk breakdown report walau gate off.
	moPrice, _, moOK := detectors.MidnightOpenPrice(h1Closed, tf.H1[i].Time, loc)
	if cfg.MOGate && moOK && !moDiscountOK(s.price, s.bias, moPrice) {
		return Signal{}, false, gateMO
	}

	// ---- STEP 4.6: REH/REL wait-for-sweep (pertemuan 1-2 rule #6) ----
	// "Jangan sell di double top — tunggu REH disapu dulu" (mirror buy/REL).
	// relEqSwept di-tag ke Signal untuk breakdown walau gate off; gate hanya
	// menolak saat ada grup relevan & belum tersapu dalam FreshBars terakhir.
	relEqState := relEqSweepState(h1Closed, s.bias, cfg)
	if cfg.RelEqSweepGate && relEqState == "no_sweep" {
		return Signal{}, false, gateRelEq
	}

	// ---- STEP 5: POI / PD Array (Section F, J.1 #6) ----
	s.macroFib, s.fibOK = zoneFib(s.weekly, s.daily, s.ofDir, cfg.ZoneFibTF, cfg.MinSwingATRMultHTF, cfg.ATRPeriod, cfg.MinRetracePct, detectors.DefaultMaxCalibration, s.price)
	if !s.fibOK {
		if cfg.FibZoneGate {
			return Signal{}, false, gateFib
		}
		// Gate OFF: Fib tak terdefinisi → POI dipilih tanpa filter zona (Fib{}).
		s.macroFib = detectors.Fib{}
	}
	if cfg.FractalPOI {
		// POI fractal: H1 + H4 + D + W (semua closed-by-now → anti-lookahead).
		h4 := window(closedBy(tf.H4, now), cfg.H1Window)
		dWin := window(s.daily, cfg.H1Window)
		s.pois = fractalPOIs(s.h1, h4, dWin, s.weekly, cfg)
		// Agenda gate: kalau ada POI HTF terjauh searah bias, jadikan SATU-SATUNYA
		// target (abaikan POI lebih dekat/di atasnya). Tanpa HTF → pool penuh.
		if cfg.AgendaGate {
			if target, okT := agendaTarget(s.pois, s.price, s.bias, cfg.AgendaNearest); okT {
				s.pois = []detectors.POI{target}
			}
		}
	} else {
		pdrs := detectors.DetectPDRs(s.h1, cfg.MinGapPips, cfg.VIMinGapPips, cfg.FVGSwingBreakAdjacency, cfg.BPRMaxDistanceCandles, cfg.IFVGRequireNoSameDirFVG, cfg.OBStrict, cfg.BPRDirectional, cfg.BBRequireDisplacement, cfg.FVGBreakGeometric)
		pdrs = dropOB(pdrs, cfg.DisableOB)
		pdrs = dropBPR(pdrs, cfg.DisableBPR)
		pdrs = detectors.FilterLivePDRs(s.h1, pdrs, cfg.POIBreakWick) // buang PDR yg sudah di-break, kecuali IFVG
		s.pois = filterPOIs(detectors.FilterPOITier(detectors.BuildPOIs(pdrs, cfg.ConfluenceMin, poiMaxWidth(s.h1, cfg)), cfg.MaxPOITier), cfg)
	}
	var poi detectors.POI
	if cfg.POITouchWindowBars > 0 {
		// POI valid kalau disentuh dalam N candle H1 terakhir (bukan point-in-time).
		poi, ok = detectors.SelectPOITouched(s.pois, window(h1Closed, cfg.POITouchWindowBars), s.macroFib, s.bias)
	} else {
		poi, ok = detectors.SelectPOI(s.pois, s.price, s.macroFib, s.bias)
	}
	if !ok {
		// Kunci #3 fallback (F.5): tak ada POI/FVG searah bias → target = opposing
		// liquidity (Old High utk sell / Old Low utk buy). Hanya bila knob ON;
		// default OFF = perilaku lama (NO SETUP).
		if !cfg.Kunci3Fallback {
			return Signal{}, false, gatePOI
		}
		lvl, okLiq := opposingLiquidity(s.weekly, s.daily, s.bias, s.price)
		if !okLiq {
			return Signal{}, false, gateOpposingLiq // tak ada Old High/Low relevan → tetap NO SETUP
		}
		poi = fallbackPOI(s.bias, lvl, cfg.Kunci3FallbackBandPips)
	}

	// ---- STEP 6: trigger entry (Section C arah + G.2 eksekusi di 5m) ----
	m5 := m5Window(tf.M5, loc, cfg, now)
	trig, ok := entryTrigger5m(m5, s.bias, entryFromIdx(m5, now, cfg), cfg, poi)
	if !ok {
		return Signal{}, false, gateEntry
	}
	fillC := m5[trig.fillIdx]
	entry := fillC.Open

	// ---- STEP 7: emit (SL/TP/RR) ----
	sl := detectors.SLPrice(s.bias, cfg.SLMode, trig.pivot, poi, cfg.SLBufferPips)
	if !slSane(s.bias, entry, sl) {
		return Signal{}, false, gateSLSane
	}
	rr := detectors.RRTarget(wd) // Senin & Jumat → 1:2, selain itu 1:3
	tp := detectors.TPPrice(s.bias, entry, sl, rr)
	// User 2026-06-02: trade RETRACE menarget level FVG weekly itu sendiri
	// (fill 81% tapi dangkal — RR jauh tak selaras). RR di-recompute dari TP aktual.
	if cfg.RetraceTPToFVG && s.regime == state.RegimeRetrace {
		if tpFVG, rrFVG, okFVG := retraceTP(s, cfg, entry, sl); okFVG {
			tp, rr = tpFVG, rrFVG
		}
	}

	sig := Signal{
		Time:          fillC.Time,
		Dir:           s.bias,
		Entry:         entry,
		SL:            sl,
		TP:            tp,
		RR:            rr,
		Regime:        s.regime,
		Session:       s.session,
		WeeklyPhase:   s.weeklyPhase,
		DailyPhase:    s.dailyPhase,
		Scenario:      s.scenario,
		DayType:       s.dayType,
		ITHITLType:    trig.itType,
		POITier:       poi.Tier,
		POIConfluence: poi.Confluence(),
		POITF:         poi.TF,
		POIKinds:      poiKindSummary(poi),
		MORel:         moRel(entry, s.bias, moPrice, moOK),
		RelEqSwept:    relEqState,
		FlipTiming:    flipTiming,
		fillM5Time:    fillC.Time,
	}
	return sig, true, gatePass
}

// moDiscountOK = aturan zona Midnight Open (pertemuan 8): buy hanya saat harga
// DI BAWAH MO (discount), sell hanya DI ATAS MO (premium). Dipakai evaluate &
// Narrate (anti-drift).
func moDiscountOK(price float64, bias detectors.Direction, mo float64) bool {
	if bias == detectors.Bearish {
		return price > mo
	}
	return price < mo
}

// relEqTolerance = toleransi "equal" REH/REL dalam satuan harga: pips override
// (>0) menang, selain itu pct × harga. Dipakai evaluate & Narrate (anti-drift).
func relEqTolerance(price float64, cfg Config) float64 {
	if cfg.RelEqTolerancePips > 0 {
		return detectors.PipsToPrice(cfg.RelEqTolerancePips)
	}
	return cfg.RelEqTolerancePct / 100 * price
}

// relEqSweepState menilai wait-for-sweep untuk bias saat ini (sell→REH,
// buy→REL): "swept_fresh" = ada grup relevan yang masih hidup FreshBars candle
// lalu dan TERSAPU oleh candle terakhir; "no_sweep" = ada grup tapi belum
// tersapu; "-" = tidak ada grup relevan (tak ada liquidity yang perlu ditunggu).
func relEqSweepState(h1 []data.Candle, bias detectors.Direction, cfg Config) string {
	fresh := cfg.RelEqSweepFreshBars
	if fresh <= 0 || len(h1) <= fresh+3 {
		return "-"
	}
	kind := detectors.SwingLow // buy: tunggu REL (bawah) disapu
	if bias == detectors.Bearish {
		kind = detectors.SwingHigh // sell: tunggu REH (atas) disapu
	}
	past := h1[:len(h1)-fresh]
	tol := relEqTolerance(past[len(past)-1].Close, cfg)
	var groups []detectors.RelEqualLevel
	for _, g := range detectors.DetectRelEquals(past, tol, cfg.RelEqLookbackBars, cfg.RelEqMaxGapBars) {
		if g.Kind == kind {
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		return "-"
	}
	recent := h1[len(h1)-fresh:]
	for _, g := range groups {
		for _, c := range recent {
			if kind == detectors.SwingHigh && c.High > g.Level {
				return "swept_fresh"
			}
			if kind == detectors.SwingLow && c.Low < g.Level {
				return "swept_fresh"
			}
		}
	}
	return "no_sweep"
}

// moRel men-tag posisi entry relatif MO untuk breakdown report: "discount" =
// sisi benar menurut aturan MO (buy<MO / sell>MO), "premium" = sisi salah,
// "-" = MO belum terbentuk (sesi Asia / gap data).
func moRel(entry float64, bias detectors.Direction, mo float64, moOK bool) string {
	if !moOK {
		return "-"
	}
	if moDiscountOK(entry, bias, mo) {
		return "discount"
	}
	return "premium"
}

// retraceTP = TP trade retrace di level weekly FVG target (RetraceTPToFVG).
// Bias sell (retrace dari OF bullish) → TP = target.Top (FVG support di bawah);
// bias buy (retrace dari OF bearish) → TP = target.Bottom (FVG resistance di
// atas). ok=false bila target tak ada / di sisi salah dari entry / risk nol —
// caller fallback ke TP RR standar.
func retraceTP(s snapshot, cfg Config, entry, sl float64) (tp, rr float64, ok bool) {
	tgt, okT := state.RetraceTargetMin(s.weekly, s.ofDir, cfg.MinGapPips, s.minLegW)
	if !okT {
		return 0, 0, false
	}
	if s.bias == detectors.Bearish {
		tp = tgt.Top
		if tp >= entry {
			return 0, 0, false
		}
	} else {
		tp = tgt.Bottom
		if tp <= entry {
			return 0, 0, false
		}
	}
	risk := entry - sl
	if risk < 0 {
		risk = -risk
	}
	if risk == 0 {
		return 0, 0, false
	}
	dist := tp - entry
	if dist < 0 {
		dist = -dist
	}
	return tp, dist / risk, true
}

// touchedInvalidation (STEP 1c, J.1 #1): invalidation guard.
//
// Normal/retrace: pakai s.ofDir (arah OF definitif).
//   - Bullish OF: price < LTL → invalidation
//   - Bearish OF: price > LTH → invalidation
//
// Early flip (s.earlyFlip=true): pakai s.bias (= earlyDir, lawan ofDir).
//   - earlyBullish (ofDir=BEARISH, bias=BULLISH): price < LTL → Skenario A, abort
//   - earlyBearish (ofDir=BULLISH, bias=BEARISH): price > LTH → Skenario A, abort
func touchedInvalidation(s snapshot) bool {
	dir := s.ofDir
	if s.earlyFlip {
		dir = s.bias // earlyDir = lawan OF definitif
	}
	switch dir {
	case detectors.Bullish:
		return s.anchors.HasLTL && s.price < s.anchors.LTL.Price
	case detectors.Bearish:
		return s.anchors.HasLTH && s.price > s.anchors.LTH.Price
	}
	return false
}

// opposingLiquidity (Kunci #3, F.5) = level "Old High/Low" yang jadi target saat
// retracement TIDAK punya POI/FVG searah bias. Untuk bias SELL (OF bearish) →
// Old High TERDEKAT di ATAS harga; bias BUY (OF bullish) → Old Low TERDEKAT di
// BAWAH harga. Sumber = swing weekly/daily signifikan (zigzag); dipakai HTF dulu
// (weekly) lalu daily sebagai cadangan kalau weekly tak punya swing di sisi yang
// relevan. Mengembalikan ok=false kalau tak ada swing yang cocok.
//
// Catatan konsistensi: ini secara semantik = anchor LTH/LTL B.1 (lihat
// state.ComputeAnchors: LTL = swing low recent DI BAWAH harga, LTH = swing high
// recent DI ATAS harga = persis "opposing liquidity" tabel B.1), tapi dihitung di
// sini agar bisa memilih level TERDEKAT (Old High/Low pertama yang relevan) dari
// gabungan weekly+daily tanpa mengubah kontrak state.Anchors.
func opposingLiquidity(weekly, daily []data.Candle, bias detectors.Direction, price float64) (float64, bool) {
	if lvl, ok := nearestOldLevel(weekly, bias, price); ok {
		return lvl, true
	}
	return nearestOldLevel(daily, bias, price)
}

// nearestOldLevel mencari level Old High/Low terdekat di sisi relevan dari deret
// candle: bias bearish → swing high TERENDAH yang masih di ATAS harga (Old High
// terdekat di atas); bias bullish → swing low TERTINGGI yang masih di BAWAH harga
// (Old Low terdekat di bawah). Pakai turning point Zigzag (swing signifikan).
func nearestOldLevel(candles []data.Candle, bias detectors.Direction, price float64) (float64, bool) {
	z := detectors.Zigzag(detectors.DetectSwings(candles))
	best := 0.0
	found := false
	for _, s := range z {
		if bias == detectors.Bearish {
			// Old High di atas harga, ambil yang TERDEKAT (terendah di atas price).
			if s.Kind == detectors.SwingHigh && s.Price > price {
				if !found || s.Price < best {
					best, found = s.Price, true
				}
			}
		} else {
			// Old Low di bawah harga, ambil yang TERDEKAT (tertinggi di bawah price).
			if s.Kind == detectors.SwingLow && s.Price < price {
				if !found || s.Price > best {
					best, found = s.Price, true
				}
			}
		}
	}
	return best, found
}

// fallbackPOI (Kunci #3) = POI band tipis di sekitar level opposing-liquidity
// `lvl`, searah `bias`, supaya STEP entry 5m bisa lanjut normal. Band = ±bandPips
// (default fallbackBandPipsDefault) di tiap sisi level. Tier sengaja di-set 2
// supaya lolos MaxPOITier (gate quality) — fallback hanya dipakai ketika tak ada
// POI normal, jadi tak menggeser pemilihan POI biasa.
func fallbackPOI(bias detectors.Direction, lvl, bandPips float64) detectors.POI {
	if bandPips <= 0 {
		bandPips = fallbackBandPipsDefault
	}
	half := detectors.PipsToPrice(bandPips)
	return detectors.POI{
		Dir:    bias,
		Top:    lvl + half,
		Bottom: lvl - half,
		Tier:   2,
	}
}

// fallbackBandPipsDefault = setengah-lebar band fallback default (pip gold) kalau
// Config.Kunci3FallbackBandPips <= 0.
const fallbackBandPipsDefault = 30

// poiMaxWidth = cap lebar cluster POI dalam HARGA = POIMaxWidthATRMult × ATR_H1
// (Bug#5: cegah cluster strict-overlap chain transitif menyerap PDR berjauhan →
// band POI terlalu lebar & tier inflasi). Skala ke ATR supaya adaptif harga gold.
// Mengembalikan 0 (tanpa cap) kalau mult<=0 atau ATR tak bisa dihitung.
// buildTFPOIs mendeteksi PDR di satu TF → buang yang sudah di-break (body/wick) →
// cluster jadi POI → tag TF. confMin = confluence minimum TF tsb (H1 = ConfluenceMin,
// HTF = 1). Width cap pakai ATR TF tsb (poiMaxWidth auto-scale).
// trigResult = hasil trigger entry 5m lintas-mode (EntryTriggerMode). pivot =
// anchor SL ltf-structure (wick pivot ITL/ITH utk mode itl; ekstrem candle
// trigger utk mode lain). fillIdx = index m5 tempat FILL (entry = Open-nya).
type trigResult struct {
	fillIdx   int
	pivot     float64
	pivotTime time.Time
	kind      string // "ITL"/"ITH" (mode itl) atau nama mode
	itType    detectors.ITType
}

// entryTrigger5m = dispatcher trigger entry 5m per EntryTriggerMode — dipakai
// evaluate & Narrate (0 drift). Konfirmasi harus di index >= fromIdx (freshness
// EntryFreshBars); fill di candle berikutnya.
func entryTrigger5m(m5 []data.Candle, bias detectors.Direction, fromIdx int, cfg Config, poi detectors.POI) (trigResult, bool) {
	mode := cfg.EntryTriggerMode
	if mode == "" || mode == "itl" {
		itl, fillIdx, ok := detectors.EntryTriggerMin(m5, bias, fromIdx, cfg.BreakType, cfg.MinSwingPipsM5, cfg.RequireStandardTrigger, cfg.AMSStrictStructure)
		if !ok {
			return trigResult{}, false
		}
		return trigResult{fillIdx: fillIdx, pivot: itl.Pivot.Price, pivotTime: itl.Pivot.Time, kind: itl.Kind.String(), itType: itl.Type}, true
	}
	if fromIdx < 1 {
		fromIdx = 1
	}
	var swings []detectors.Swing
	if mode == "sweep" {
		swings = detectors.DetectSwings(m5)
	}
	wantSwing := detectors.SwingLow
	if bias == detectors.Bearish {
		wantSwing = detectors.SwingHigh
	}
	for j := fromIdx; j < len(m5)-1; j++ {
		c := m5[j]
		hit := false
		switch mode {
		case "disp", "dispreject":
			if atr, okA := detectors.ATRAt(m5, j, cfg.ATRPeriod); okA && atr > 0 {
				body := c.Close - c.Open
				if bias == detectors.Bearish {
					body = -body
				}
				hit = body >= cfg.DispATRMult*atr
			}
			if !hit && mode == "dispreject" {
				hit = rejectHit(c, bias, poi)
			}
		case "reject":
			hit = rejectHit(c, bias, poi)
		case "sweep":
			// swing 5m sejenis TERAKHIR yang sudah terkonfirmasi sebelum j.
			var lastP float64
			has := false
			for _, sw := range swings {
				if sw.Index+1 > j {
					break
				}
				if sw.Kind == wantSwing {
					lastP, has = sw.Price, true
				}
			}
			if has {
				if bias == detectors.Bullish {
					hit = c.Low < lastP
				} else {
					hit = c.High > lastP
				}
			}
		}
		if hit {
			pivot := c.Low
			if bias == detectors.Bearish {
				pivot = c.High
			}
			return trigResult{fillIdx: j + 1, pivot: pivot, pivotTime: c.Time, kind: mode, itType: detectors.ITStandard}, true
		}
	}
	return trigResult{}, false
}

// rejectHit = candle rejection: wick masuk zona POI, close keluar searah bias.
func rejectHit(c data.Candle, bias detectors.Direction, poi detectors.POI) bool {
	if bias == detectors.Bullish {
		return c.Low <= poi.Top && c.Low >= poi.Bottom-poiRejectSlack && c.Close > poi.Top
	}
	return c.High >= poi.Bottom && c.High <= poi.Top+poiRejectSlack && c.Close < poi.Bottom
}

// poiRejectSlack = toleransi tembus zona utk rejection (candle boleh sedikit
// menembus sisi jauh zona tanpa membatalkan rejection) — $2.
const poiRejectSlack = 2.0

// asiaSwept (rule London user 2026-06-02): likuiditas sesi Asia trading-day
// berjalan sudah DI-SWEEP berlawanan bias — manipulasi London dianggap selesai.
// Buy → ada candle H1 SETELAH Asia close yang wick menembus LOW Asia; sell →
// menembus HIGH Asia. false bila Asia belum close / tak ada data.
func asiaSwept(h1Closed []data.Candle, loc *time.Location, now time.Time, bias detectors.Direction) bool {
	asia, _, _, closed := asiaSlice(h1Closed, loc, 1, now)
	if !closed || len(asia) == 0 {
		return false
	}
	lo, hi := asia[0].Low, asia[0].High
	for _, c := range asia[1:] {
		if c.Low < lo {
			lo = c.Low
		}
		if c.High > hi {
			hi = c.High
		}
	}
	asiaEnd := detectors.TradingDayStart(now, loc).Add(6 * time.Hour) // 00:00 NY
	for i := len(h1Closed) - 1; i >= 0; i-- {
		c := h1Closed[i]
		if c.Time.Before(asiaEnd) {
			break // sudah masuk wilayah sesi Asia / hari sebelumnya
		}
		if bias == detectors.Bullish && c.Low < lo {
			return true
		}
		if bias == detectors.Bearish && c.High > hi {
			return true
		}
	}
	return false
}

// filterPOIs menerapkan filter kualitas level-POI (semua TF, 2026-06-02):
//   - MaxConfluence: buang POI ber-komponen > N (conf4+ terbukti PF 0.0-0.3);
//   - BBNeedsFVG: buang POI yang mengandung Breaker TANPA keluarga imbalance
//     (FVG/FVGBreak/IFVG) — BB hanya sah berdampingan FVG.
//
// Dipakai buildTFPOIs (jalur fractal) + jalur non-fractal evaluate & Narrate
// (0 drift).
func filterPOIs(pois []detectors.POI, cfg Config) []detectors.POI {
	if cfg.MaxConfluence <= 0 && !cfg.BBNeedsFVG {
		return pois
	}
	out := pois[:0]
	for _, p := range pois {
		if cfg.MaxConfluence > 0 && p.Confluence() > cfg.MaxConfluence {
			continue
		}
		if cfg.BBNeedsFVG {
			hasBB, hasFVG := false, false
			for _, c := range p.Components {
				switch c.Kind {
				case detectors.KindBB:
					hasBB = true
				case detectors.KindFVG, detectors.KindFVGBreak, detectors.KindIFVG:
					hasFVG = true
				}
			}
			if hasBB && !hasFVG {
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// dropOB membuang komponen Order Block dari pool PD Array (Config.DisableOB —
// logika OB diragukan user 2026-06-02, off semua TF sampai direview).
func dropOB(pdrs []detectors.PDR, disable bool) []detectors.PDR {
	if !disable {
		return pdrs
	}
	out := pdrs[:0]
	for _, p := range pdrs {
		if p.Kind != detectors.KindOB {
			out = append(out, p)
		}
	}
	return out
}

// dropBPR membuang komponen BPR dari pool PD Array (Config.DisableBPR). BPR =
// net-loser sbg POI entry pasif: dgn zona-FVG-terakhir-penuh (definisi benar
// pertemuan 4, user 2026-06-04) PF bucket 0.58/−13R & seret OOS 2.08→1.94. Edge
// asli BPR = mekanisme HOLD/BREAK directional yg di luar scope ImpulseOnly, jadi
// BPR di-off dari pool (tetap di-detect utk penanda; default ON).
func dropBPR(pdrs []detectors.PDR, disable bool) []detectors.PDR {
	if !disable {
		return pdrs
	}
	out := pdrs[:0]
	for _, p := range pdrs {
		if p.Kind != detectors.KindBPR {
			out = append(out, p)
		}
	}
	return out
}

// fvgBreakAllowedTF: true kalau TF ada di allow-list CSV (mis. "H1,D"). Cocokkan
// pakai TFKind.String() ("H1"/"H4"/"D"/"W"), case-insensitive, abaikan spasi.
func fvgBreakAllowedTF(csv string, tf detectors.TFKind) bool {
	want := tf.String()
	for _, part := range strings.Split(csv, ",") {
		if strings.EqualFold(strings.TrimSpace(part), want) {
			return true
		}
	}
	return false
}

// dropFVGBreak membuang penanda Tier-2 KindFVGBreak dari pool (Config.FVGBreakTFs
// di TF di luar allow-list). FVG dasarnya tetap di-emit sebagai KindFVG (Tier-4) oleh
// DetectPDRs, jadi POI tetap ada — hanya kehilangan promosi tier (label & efek seleksi).
func dropFVGBreak(pdrs []detectors.PDR) []detectors.PDR {
	out := pdrs[:0]
	for _, p := range pdrs {
		if p.Kind != detectors.KindFVGBreak {
			out = append(out, p)
		}
	}
	return out
}

// ofMinLeg = ambang magnitude swing weekly utk WeeklyOF/ComputeRegime
// (OFSwingATRMult × ATR weekly; 0 = filter off).
func ofMinLeg(weekly []data.Candle, cfg Config) float64 {
	if cfg.OFSwingATRMult <= 0 || len(weekly) <= cfg.ATRPeriod {
		return 0
	}
	if atr, ok := detectors.ATRAt(weekly, len(weekly)-1, cfg.ATRPeriod); ok {
		return cfg.OFSwingATRMult * atr
	}
	return 0
}

// biasAgeMin = ambang karantina umur-bias per ARAH bias (user 2026-06-03:
// asimetris — fresh-bullish PF1.09 jinak, fresh-bearish PF0.47 racun).
// Bullish pakai MinBiasAgeDaysBull bila >= 0, selain itu ikut MinBiasAgeDays.
func biasAgeMin(cfg Config, bias detectors.Direction) int {
	if bias == detectors.Bullish && cfg.MinBiasAgeDaysBull >= 0 {
		return cfg.MinBiasAgeDaysBull
	}
	return cfg.MinBiasAgeDays
}

// biasMinLeg = ambang magnitude swing daily utk DailyBias (BiasSwingATRMult × ATR daily).
func biasMinLeg(daily []data.Candle, cfg Config) float64 {
	if cfg.BiasSwingATRMult <= 0 || len(daily) <= cfg.ATRPeriod {
		return 0
	}
	if atr, ok := detectors.ATRAt(daily, len(daily)-1, cfg.ATRPeriod); ok {
		return cfg.BiasSwingATRMult * atr
	}
	return 0
}

// hourSkipped melaporkan apakah jam NY (awal candle H1) masuk daftar terlarang
// SkipEntryHoursNY (CSV, mis. "8" atau "8,20"). String kosong = tanpa skip.
func hourSkipped(t time.Time, loc *time.Location, csv string) bool {
	if csv == "" {
		return false
	}
	h := t.In(loc).Hour()
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil && n == h {
			return true
		}
	}
	return false
}

// BuildNewsSkipSet membangun set awal-jam UTC candle H1 yang memuat rilis USD
// high-impact yang jam-NY-nya termasuk candidateHoursCSV (= SkipEntryHoursNY).
// Dipakai caller LIVE (alertd/narrate) untuk mengisi Config.NewsSkipHourStarts
// → skip-hour jadi kondisional terhadap kalender minggu berjalan. Reuse
// hourSkipped agar logika jam-NY satu sumber (0-drift). loc = America/New_York.
// Rilis 08:30 ET → candle H1 08:00 (EDT 12:00Z / EST 13:00Z, dua-duanya jam-NY 8).
func BuildNewsSkipSet(events []news.Event, candidateHoursCSV string, loc *time.Location) map[time.Time]bool {
	set := make(map[time.Time]bool)
	for _, e := range news.FilterUSDHighImpact(events) {
		hs := e.Time.Truncate(time.Hour) // UTC = awal candle H1 pemuat rilis
		if hourSkipped(hs, loc, candidateHoursCSV) {
			set[hs] = true
		}
	}
	return set
}

func buildTFPOIs(candles []data.Candle, cfg Config, tf detectors.TFKind, confMin int) []detectors.POI {
	if len(candles) < 3 {
		return nil
	}
	pdrs := detectors.DetectPDRs(candles, cfg.MinGapPips, cfg.VIMinGapPips, cfg.FVGSwingBreakAdjacency, cfg.BPRMaxDistanceCandles, cfg.IFVGRequireNoSameDirFVG, cfg.OBStrict, cfg.BPRDirectional, cfg.BBRequireDisplacement, cfg.FVGBreakGeometric)
	pdrs = dropOB(pdrs, cfg.DisableOB)
	pdrs = dropBPR(pdrs, cfg.DisableBPR)
	if cfg.FVGBreakTFs != "" && !fvgBreakAllowedTF(cfg.FVGBreakTFs, tf) {
		pdrs = dropFVGBreak(pdrs) // per-TF: promosi Tier-2 hanya di TF allow-list
	}
	pdrs = detectors.FilterLivePDRs(candles, pdrs, cfg.POIBreakWick)
	pois := filterPOIs(detectors.BuildPOIs(pdrs, confMin, poiMaxWidth(candles, cfg)), cfg)
	for i := range pois {
		pois[i].TF = tf
	}
	return pois
}

// fractalPOIs menggabung POI dari H1 (ConfluenceMin + MaxPOITier) dengan POI HTF
// H4/D/W (confluence 1, bypass MaxPOITier — FVG HTF tunggal yg fresh sudah valid).
// Semua candle TF di-pass HARUS sudah closed-by-now (anti-lookahead). Dipakai
// engine.evaluate & Narrate (anti-drift).
func fractalPOIs(h1, h4, daily, weekly []data.Candle, cfg Config) []detectors.POI {
	pois := detectors.FilterPOITier(buildTFPOIs(h1, cfg, detectors.TFH1, cfg.ConfluenceMin), cfg.MaxPOITier)
	pois = append(pois, buildTFPOIs(h4, cfg, detectors.TFH4, 1)...)
	pois = append(pois, buildTFPOIs(daily, cfg, detectors.TFD, 1)...)
	pois = append(pois, buildTFPOIs(weekly, cfg, detectors.TFW, 1)...)
	return pois
}

// agendaTarget mengembalikan POI HTF (TF>=H4) searah bias yang PALING JAUH di arah
// draw: buy = Bottom terendah yang masih <= harga (harga draw turun ke situ); sell =
// Top tertinggi yang masih >= harga. Itulah "agenda besar" yg harus diambil dulu.
// ok=false kalau tak ada POI HTF searah bias di arah draw (→ fall back pool normal).
func agendaTarget(pois []detectors.POI, price float64, bias detectors.Direction, nearest bool) (detectors.POI, bool) {
	var best detectors.POI
	found := false
	for _, p := range pois {
		if p.TF < detectors.TFH4 || p.Dir != bias {
			continue
		}
		if !poiHasFVG(p) { // FVG-only (user 2026-06-01): skip OB/BB/VI noise (mis. OB weekly T5)
			continue
		}
		if bias == detectors.Bullish {
			if p.Bottom > price { // zona di atas harga — bukan draw-down
				continue
			}
			// farthest = Bottom terendah; nearest = Bottom tertinggi (paling dekat di bawah harga).
			if !found || (nearest && p.Bottom > best.Bottom) || (!nearest && p.Bottom < best.Bottom) {
				best, found = p, true
			}
		} else {
			if p.Top < price {
				continue
			}
			if !found || (nearest && p.Top < best.Top) || (!nearest && p.Top > best.Top) {
				best, found = p, true
			}
		}
	}
	return best, found
}

// poiHasFVG true kalau cluster POI mengandung komponen FVG (KindFVG/KindFVGBreak)
// — dipakai agenda gate (target = imbalance FVG, bukan OB/BB/VI).
func poiHasFVG(p detectors.POI) bool {
	for _, c := range p.Components {
		if c.Kind == detectors.KindFVG || c.Kind == detectors.KindFVGBreak {
			return true
		}
	}
	return false
}

func poiMaxWidth(h1 []data.Candle, cfg Config) float64 {
	if cfg.POIMaxWidthATRMult <= 0 || len(h1) <= cfg.ATRPeriod {
		return 0
	}
	atr, ok := detectors.ATRAt(h1, len(h1)-1, cfg.ATRPeriod)
	if !ok || atr <= 0 {
		return 0
	}
	return cfg.POIMaxWidthATRMult * atr
}

// zoneFib memilih sumber Fib untuk gate zona: weekly (makro) atau daily (presisi,
// default). Filter magnitude swing = atrMult × ATR(sumber, atrPeriod) — RELATIF
// volatilitas/harga (Bug#2 fix: pip absolut salah skala di gold $4500). atrMult<=0
// → tanpa filter. price = harga saat scan: leg DITOLAK (ok=false) bila harga sudah
// menembus leg (overshoot) — leg basi → NO SETUP jujur (lihat fibOvershoot).
func zoneFib(weekly, daily []data.Candle, dir detectors.Direction, tf ZoneFibTF, atrMult float64, atrPeriod int, minRetracePct float64, maxCalibration int, price float64) (detectors.Fib, bool) {
	src := weekly
	if tf == ZoneFibDaily {
		src = daily
	}
	minLegPrice := 0.0
	if atrMult > 0 && len(src) > atrPeriod {
		if atr, ok := detectors.ATRAt(src, len(src)-1, atrPeriod); ok {
			minLegPrice = atrMult * atr
		}
	}
	fib, ok := macroFib(src, dir, minLegPrice, minRetracePct, maxCalibration)
	if !ok {
		return detectors.Fib{}, false
	}
	if fibOvershoot(fib, price) {
		return detectors.Fib{}, false // leg sudah ditembus harga → stale, jangan dipakai
	}
	return fib, true
}

// fibOvershootBufferPips = toleransi overshoot di luar tepi leg (pip gold).
// 0 = strict edge (tolak begitu harga keluar Low..High). Knob lokal; bila perlu
// di-tune nanti, naikkan ke Config.
const fibOvershootBufferPips = 0

// fibOvershoot true bila harga sudah keluar dari rentang leg [Low..High] melebihi
// buffer: price > High+buf (overshoot ke atas, leg bullish tertembus di target /
// leg bearish tertembus di anchor) ATAU price < Low-buf (overshoot ke bawah).
// Fib.Low/High = ekstrem leg lepas dari arah (lihat detectors.NewFib), jadi cek
// generik dua sisi ini cukup untuk kedua arah.
func fibOvershoot(f detectors.Fib, price float64) bool {
	buf := detectors.PipsToPrice(fibOvershootBufferPips)
	return price > f.High+buf || price < f.Low-buf
}

// macroFib = Fibonacci zona makro dari last impulsive move TER-VALIDASI (A.1+A.2:
// leg searah OF yang retracement-nya lolos Rule of 0.5, dgn kalibrasi) atas deret
// candle. minLegPrice = filter magnitude swing (Bug#2). Bug#1 fix: pakai
// FindValidImpulseZ (A.1+A.2) di atas zigzag ter-filter, BUKAN FindLastImpulse
// (pasangan zigzag terakhir tanpa validasi Rule of 0.5).
func macroFib(candles []data.Candle, dir detectors.Direction, minLegPrice, minRetracePct float64, maxCalibration int) (detectors.Fib, bool) {
	z := detectors.ZigzagMin(detectors.DetectSwings(candles), minLegPrice)
	imp, ok := detectors.FindValidImpulseZ(z, candles, dir, minRetracePct, maxCalibration)
	if !ok {
		return detectors.Fib{}, false
	}
	return detectors.NewFib(imp), true
}

// slSane: SL di sisi yang benar relatif entry (buy: SL < entry; sell: SL > entry).
func slSane(dir detectors.Direction, entry, sl float64) bool {
	if dir == detectors.Bullish {
		return sl < entry
	}
	return sl > entry
}

// window mengambil cfg.H1Window candle terakhir (semua kalau lebih pendek).
func window(c []data.Candle, n int) []data.Candle {
	if n <= 0 || len(c) <= n {
		return c
	}
	return c[len(c)-n:]
}

// closedBy = prefix candle yang periodenya SUDAH selesai pada `now` (next-open
// proxy). Candle yang masih terbentuk di-drop → no lookahead.
func closedBy(candles []data.Candle, now time.Time) []data.Candle {
	n := sort.Search(len(candles), func(i int) bool { return candles[i].Time.After(now) })
	if n > 0 {
		n-- // candle ke-(n-1) masih forming (close == open candle berikut > now)
	}
	return candles[:n]
}

// asiaSlice mengambil candle H1 sesi Asia (18:00→00:00 NY) dari trading-day yang
// memuat `now`, plus ATR_1H pada candle Asia terakhir. asiaClosed=false kalau
// sesi Asia belum lengkap (belum sampai 00:00 NY) → daily belum classifiable.
func asiaSlice(h1Closed []data.Candle, loc *time.Location, atrPeriod int, now time.Time) (asia []data.Candle, atr1h, prevClose float64, asiaClosed bool) {
	if len(h1Closed) == 0 {
		return nil, 0, 0, false
	}
	dayStart := detectors.TradingDayStart(now, loc)
	asiaEnd := dayStart.Add(6 * time.Hour) // 00:00 NY
	firstIdx, lastIdx := -1, -1
	for i, c := range h1Closed {
		if !c.Time.Before(dayStart) && c.Time.Before(asiaEnd) {
			if firstIdx < 0 {
				firstIdx = i
			}
			asia = append(asia, c)
			lastIdx = i
		}
	}
	// prevClose = close candle 1H tepat SEBELUM sesi Asia (untuk gap-anchor D.2).
	if firstIdx > 0 {
		prevClose = h1Closed[firstIdx-1].Close
	}
	if len(asia) < 2 || now.Before(asiaEnd) {
		return asia, 0, prevClose, false
	}
	if a, ok := detectors.ATRAt(h1Closed, lastIdx, atrPeriod); ok {
		atr1h = a
	}
	return asia, atr1h, prevClose, true
}

// asiaAnchor mengembalikan prevClose bila knob gap-anchor aktif, else 0 (anchor open
// lama, 0-drift). Dipakai engine & narrate untuk gating gap-anchor D.2/Q1.
func asiaAnchor(on bool, prevClose float64) float64 {
	if on {
		return prevClose
	}
	return 0
}

// prevCloseBefore = close candle terakhir yang Time-nya STRICTLY sebelum t (0 bila tak
// ada). Untuk gap-anchor Q1 (candle M15 sebelum window sesi).
func prevCloseBefore(cs []data.Candle, t time.Time) float64 {
	prev := 0.0
	for _, c := range cs {
		if c.Time.Before(t) {
			prev = c.Close
		} else {
			break
		}
	}
	return prev
}

// classifyDayType (D.7, ringkas): pakai range sesi Asia & London trading-day
// berjalan vs ATR_daily. Hanya bedakan heavy_accum (gate #5) dari normal;
// heavy_expanding di-tag tapi tak nge-block.
func classifyDayType(h1Closed, daily []data.Candle, loc *time.Location, cfg Config, now time.Time) detectors.DayType {
	if len(daily) < cfg.ATRPeriod+1 {
		return detectors.DayNormal
	}
	atrDaily, ok := detectors.ATRAt(daily, len(daily)-1, cfg.ATRPeriod)
	if !ok || atrDaily <= 0 {
		return detectors.DayNormal
	}
	dayStart := detectors.TradingDayStart(now, loc)
	asiaR := sessionRange(h1Closed, dayStart, dayStart.Add(6*time.Hour))
	londonR := sessionRange(h1Closed, dayStart.Add(6*time.Hour), dayStart.Add(12*time.Hour))
	asiaNet := sessionNet(h1Closed, dayStart, dayStart.Add(12*time.Hour))
	if londonR > 0 && detectors.HeavyAccumStage1(asiaR, londonR, atrDaily, cfg.HeavyAccumMaxRangePct) {
		// Stage-1 (D.7 @06:00 NY): Asia & London dua-duanya kompresi → SUSPECTED accum.
		if cfg.HeavyAccumConfirmNY {
			// Stage-3 (hybrid Opsi C): confirm heavy_accum HANYA bila sesi NY (06:00–12:00)
			// juga kompresi DAN tak ada FVG 1H baru di NY. NY displacement (range besar /
			// FVG baru) = akumulasi pecah → BUKAN heavy_accum (suspected, tak nge-gate).
			// Kausal: sebelum NY ada candle, nyR=0 & no-FVG → confirm=true → blok London
			// dipertahankan (identik lama); blok baru dilepas SETELAH NY benar2 displace.
			nyFrom := dayStart.Add(12 * time.Hour)
			nyTo := dayStart.Add(18 * time.Hour)
			nyR := sessionRange(h1Closed, nyFrom, nyTo)
			nyHasFVG := len(detectors.DetectFVGs(sessionCandles(h1Closed, nyFrom, nyTo), cfg.MinGapPips)) > 0
			if detectors.HeavyAccumConfirm(nyR, atrDaily, cfg.HeavyAccumMaxRangePct, nyHasFVG) {
				return detectors.DayHeavyAccum
			}
			return detectors.DaySuspectedAccum // NY displaced → akumulasi pecah, tak di-gate
		}
		return detectors.DayHeavyAccum
	}
	if detectors.HeavyExpanding(asiaNet, atrDaily, 1.3) {
		return detectors.DayHeavyExpanding
	}
	return detectors.DayNormal
}

// sessionCandles = sub-slice candle dengan Time di [from, to) (untuk deteksi FVG
// per-sesi; sessionRange/sessionNet cukup angka, ini perlu candle utuh).
func sessionCandles(c []data.Candle, from, to time.Time) []data.Candle {
	var out []data.Candle
	for _, x := range c {
		if x.Time.Before(from) || !x.Time.Before(to) {
			continue
		}
		out = append(out, x)
	}
	return out
}

func sessionRange(c []data.Candle, from, to time.Time) float64 {
	hi, lo := 0.0, 0.0
	seen := false
	for _, x := range c {
		if x.Time.Before(from) || !x.Time.Before(to) {
			continue
		}
		if !seen {
			hi, lo, seen = x.High, x.Low, true
			continue
		}
		if x.High > hi {
			hi = x.High
		}
		if x.Low < lo {
			lo = x.Low
		}
	}
	if !seen {
		return 0
	}
	return hi - lo
}

func sessionNet(c []data.Candle, from, to time.Time) float64 {
	var first, last data.Candle
	seen := false
	for _, x := range c {
		if x.Time.Before(from) || !x.Time.Before(to) {
			continue
		}
		if !seen {
			first, seen = x, true
		}
		last = x
	}
	if !seen {
		return 0
	}
	d := last.Close - first.Open
	if d < 0 {
		d = -d
	}
	return d
}

// m5Window = candle M5 yang dipakai entry-trigger. Kalau cfg.M5DayWindow, batasi
// ke trading-day berjalan (G.2 konfirmasi terjadi di hari yang sama saat harga
// sampai POI); selain itu seluruh M5 sampai `now`.
func m5Window(m5 []data.Candle, loc *time.Location, cfg Config, now time.Time) []data.Candle {
	closed := closedBy(m5, now)
	if !cfg.M5DayWindow {
		return closed
	}
	dayStart := detectors.TradingDayStart(now, loc)
	lo := sort.Search(len(closed), func(i int) bool { return !closed[i].Time.Before(dayStart) })
	return closed[lo:]
}

// entryFromIdx = index M5 awal "fresh window" trigger entry: konfirmasi 5m hanya
// diterima kalau terjadi dalam cfg.EntryFreshBars candle H1 terakhir (s/d `now`),
// supaya FILL ≈ waktu keputusan (bukan trigger retrospektif dari awal trading-day).
// Deteksi swing tetap pakai seluruh window (konteks); ini hanya batas penerimaan.
// EntryFreshBars <= 0 → 0 (perilaku retrospektif lama).
func entryFromIdx(m5 []data.Candle, now time.Time, cfg Config) int {
	if cfg.EntryFreshBars <= 0 {
		return 0
	}
	from := now.Add(-time.Duration(cfg.EntryFreshBars) * time.Hour)
	return sort.Search(len(m5), func(i int) bool { return !m5[i].Time.Before(from) })
}
