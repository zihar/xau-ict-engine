# DECISIONS — forex-backtest (riwayat eksperimen & keputusan)

Log kronologis riwayat kalibrasi/eksperimen strategi. Dipisah dari `CLAUDE.md`
(2026-06-03) supaya instruksi operasional tetap ramping. State/default TERKINI ada
di `CLAUDE.md`; di sini jejak lengkap "kenapa" tiap keputusan + angka IS/OOS tiap ronde.
Sebagian pelajaran ter-backup juga di auto-memory (pelajaran-metodologi-backtest, protokol-validasi-strategi).

---

**Magnitude filter swing** sudah ada (`ZigzagMin`/`DetectIntermediatesMin`/`EntryTriggerMin`,
opt-in; fungsi lama = wrapper minLeg=0 → test lama tetap valid). Knob engine: `MinSwingPipsM5`
(filter ITL/ITH 5m trigger), `MinSwingPipsHTF` (zigzag weekly). **Sweep harness** `cmd/sweep`
menyapu grid param.

Temuan kalibrasi awal (sweep gap×conf×m5): baseline `gap=5 conf=2 m5=0` = −10R/PF0.55 (terburuk);
`gap=10 conf=2 m5=20` membalik jadi **+4R/PF1.24, winrate ~30%**; `m5=40` terlalu agresif (0% win).
Default `DefaultConfig` masih faithful (filter=0) — angka di-set lewat sweep, belum di-lock ke default.

**Fresh-trigger fill (refine 2026-05-31)**: `Config.EntryFreshBars` (default 1) — konfirmasi 5m HARUS terjadi dalam N candle H1 terakhir s/d `now`, jadi **FILL ≈ waktu keputusan** (bukan trigger retrospektif dari awal trading-day). 0 = perilaku lama. Helper `entryFromIdx`. Ini koreksi KEBENARAN: retrospektif mengisi di harga jam-jam lampau (artefak yang melambungkan metrik).

**Walk-forward (anchored, 4 fold, metode filter-by-date — Run sekali per kombinasi, trade difilter ke window IS/OOS; tak ada masalah warmup)**. ⚠️ Di engine fresh-trigger (terkoreksi): AGGREGATE OOS = BASELINE = −13R/PF0.32; tiap fold pilih/fallback ke default (IS sample 3–8 trade, IS-TOTR semua negatif). Temuan +3R/PF1.20 "tidak overfit" SEBELUMNYA itu **sebagian artefak fill retrospektif**. Jujur: **belum ada edge robust** — strategi butuh kalibrasi/pengembangan lebih + sampel lebih besar, bukan sekadar tuning. `go run ./cmd/walkforward`.

**Watchlist POI per-TF (2026-06-02)**: `ScanNarrative.NextPOIsByTF []NextPOITF` = POI valid TERDEKAT
per TF (H1/H4/D) × dua arah (searah bias + counter-bias), dihitung dari pois pipeline yang sama
(0 drift), SEBELUM AgendaGate. Tiap entri: zona INTI (irisan, `Top/Bottom`) + RENTANG PENUH komponen
(`FullTop/FullBottom`, band yang digambar di chart) + `Kinds` (rincian confluence, mis. "1×FVGBreak +
2×BB"). `cmd/narrate` selalu cetak seksi "POI terdekat per TF (pantau)". `cmd/alertd` kirim pesan
Telegram watchlist SAAT DAFTAR BERUBAH (dedup fingerprint `notify.State.LastWatchlist`, maks ~1/jam
saat H1 close) + tempel seksi sama di alert setup. Catatan tier: clusterTier bisa upgrade (BB+FVG→T2,
VI+konfluen→T1) jadi Tier POI bisa lebih baik dari komponen terbaiknya.

**Format pesan alertd = narasi piramida (2026-06-02, permintaan user — daftar zona ringkas susah
dibaca):** `engine.FormatNarrative(n, instrument, width)` = formatter BERSAMA narasi penuh
(header ┌─ SCAN + tiap langkah ✓/✗/• + decision; width 88 terminal / 42 Telegram-HP).
`cmd/narrate.printNarrative` memakainya (0 drift). `cmd/alertd.narrativeBlock` = narasi +
seksi POI per-TF dalam SATU blok monospace ``` (melindungi teks bebas dari parser Markdown
Telegram; dipotong per-baris di cap 3600 char) — dipakai di alert watchlist DAN lampiran
alert setup.

**Watchlist diff + trigger bermakna (2026-06-02, 2× revisi user):** pesan watchlist dikirim
**HANYA saat perubahan bermakna** (`watchlistTrigger`, anti-spam — heartbeat per-H1 sempat ON
lalu dimatikan user; `-heartbeat=true` utk menyalakan lagi): zona POI berubah, token REH/REL
berubah, atau harga MENYEBERANG MO (atas↔bawah). **Transisi MO HARIAN yang rutin (terbentuk
00:00 NY / hilang saat Asia open) TIDAK memicu** — state di-update diam-diam (log
`skip_mo_harian`). Pesan berubah → header "⚠️ ADA PERUBAHAN" + baris diff di-BOLD (zona
baru/hilang/batas/Tier/confluence + REH/REL TERSAPU/terbentuk + MO menyeberang; `watchlistDiff`
parse-balik fingerprint lama) + penanda `➜` pada baris zona terdampak di blok monospace (bold
tak dirender dalam ```). Estimasi ~5–10 pesan/hari (zona + REH/REL berumur ~6 jam + MO crossing).
Test `cmd/alertd/diff_test.go` (diff + trigger table-driven + round-trip fingerprint).
**Trigger manual: ketik `/watchlist` di chat Telegram bot** → alertd membalas watchlist lengkap
saat itu juga (goroutine `watchCommands` long-poll `notify.GetUpdates` 15s; backlog saat daemon
mati di-skip; chat lain diabaikan; TIDAK menyentuh state dedup). Test `telegram_test.go`.
**Klasifikasi Asia A/X (2026-06-03, permintaan user):** baris pantauan "Asia hari ini:
AKUMULASI (A) → ekspektasi AMDX" / "X (expansive) → XAMD" (`FormatAsiaLine`, dari
`ScanNarrative.AsiaClosed`+`AsiaScenario` = `ClassifyDaily` input identik step 4; tampil hanya
setelah Asia close, stabil s/d 18:00 NY). Token fingerprint `ASIA|A/X/-`: **Asia close (-→A/X,
momen 00:00 NY) MEMICU pesan** dengan diff bold "Asia close: …" (1 pesan rutin/hari yang
bermakna — sekaligus memuat posisi MO & narasi); reset harian (→"-" saat 18:00 NY) silent.
**Catatan offline + skrip pagi (2026-06-03):** `notify.State.LastTickTime` di-update &
disimpan TIAP tick (penanda liveness); gap > max(6×interval, 30m) → pesan diff watchlist
pertama pasca-restart diberi baris "🕘 _Daemon sempat offline ≈X jam — diff vs kondisi
sebelum offline; perubahan selama offline tidak terekam_" (`offlineNote`/`fmtDur` di
cmd/alertd, test `offline_test.go`; berlaku juga utk pesan "semua zona hilang"). Diff
pasca-offline = delta awal→akhir vs snapshot sebelum mati — kejadian intermediate semalam
memang hilang. **`scripts/pagi.sh`** = start pagi satu perintah: bootout daemon →
`alertd -once` (fetch incremental semalam + kirim watchlist pagi INSTAN, tak nunggu tick
pertama ≤5m) → bootstrap daemon lagi. (launchd RunAtLoad sudah auto-start saat login —
skrip ini utk fetch+pesan instan & recovery pasca-bootout manual.)

**✅ MIDNIGHT OPEN (pertemuan 8, 2026-06-02):** MO = open candle 00:00 NY (= `TradingDayStart`+6h;
HARUS dari H1, daily ber-anchor 18:00) = divider premium/discount hari itu. Implement:
`detectors.MidnightOpenTime/MidnightOpenPrice` (+`mo_test.go` DST/Asia/gap), gate STEP 4.5
`Config.MOGate` (buy hanya < MO, sell hanya > MO via `moDiscountOK`; Asia = pass-through),
narasi step "4½. MO" (mirror 0-drift, `[PENANDA, gate off]` saat off), garis oranye di chart
(`chartann`), `Signal.MORel` + breakdown report "MO relative", knob `mo_gate` (config.yaml) /
`-mogate` (backtest+walkforward) / `-mogate` grid (sweep). **HASIL UKUR: gate MERUGIKAN —
IS 416tr/+221R/PF1.91 → 282tr/+126R/PF1.73; WF OOS +180R/PF1.96 → +86R/PF1.63** (hanya DD
sedikit membaik 8.4→6.3%). Breakdown: entry premium-side justru profit (PF2.03 vs discount
PF2.15 — hampir setara) → MO TIDAK menambah edge di atas gate POI/Fib existing. **Default OFF
(penanda narasi/chart saja).** CPE (toleransi NY, fib MO→ekstrem hari) belum diimplement.
BACKTEST_RULES.md skip-list sudah di-update.

**MO×London DIANALISA (2026-06-02, permintaan user) — NO EDGE, tidak jadi gate:** cross-tab
MO-side×session (445 trade): London premium n=19 justru +10R/PF1.91 (≈rata2) → gate London-only
cuma buang profit; hipotesis "NY hanya bila London sudah sweep sisi discount MO" VAKUM —
142/143 (99.3%) NY trade sudah memenuhinya (London XAUUSD hampir selalu menyeberang MO).
Discount-side sedikit lebih baik (NY PF2.10 vs 1.86; PM 2.70 vs 2.35) tapi premium tetap
profit → MO = KONTEKS, bukan filter. **Watchlist MO (implemented):** `engine.FormatMOLine`
(formatter bersama narrate+alertd) baris "MO hari ini X — harga Y DI ATAS/BAWAH MO" di seksi
pantauan; token `MO|above/below/none` di fingerprint alertd → harga MENYEBERANG MO / MO
terbentuk / trading-day baru memicu ⚠️ ADA PERUBAHAN dengan diff bold (tanpa menambah volume
pesan — heartbeat tetap 1×/jam). `fpHasZones` bedakan "zona hilang" vs token-MO-saja. Test
diff_test.go +4 kasus MO + round-trip.

**✅ REH/REL — Relative Equal High/Low (pertemuan 1-2 + 10, 2026-06-02):**
`detectors.DetectRelEquals(candles, tolerance, lookback, maxGap)` (relequal.go + test) = grup 2+
swing 3-bar sejenis hampir selevel, **cluster KRONOLOGIS**: join bila dalam toleransi dari ekstrem
grup (`releq_tolerance_pct` default 0.05% — auto-scale antar-era; `releq_tolerance_pips` override)
DAN **gap dari anggota terakhir ≤ `releq_max_gap_bars` (default 10 candle per TF — koreksi user
2026-06-02: double top harus SEJAJAR dekat, bukan dua puncak kebetulan selevel berhari-hari
terpisah; sebelum koreksi grup 29 Mei + 2 Jun tergabung salah)**, dan BELUM disapu (wick menembus
ekstrem grup = swept; sweep dalam-toleransi = re-anchor grup baru, semantik disengaja & di-test).
Sanity pasca-maxGap: 82% titik waktu tetap punya ≥1 grup; contoh sah mis. REH 4497.14 (swing 13:00
& 20:00 NY 1 Jun) yang lalu disapu candle MO 2 Jun.
**ANALISA "pasti di-sweep?" (2026-06-02, 1066 REH + 1052 REL, maxGap-10):** klaim materi VALID —
REH **99.6%** akhirnya tersapu (median **6 candle H1**), REL **94.0%**; ≤24 candle 75.9%/71.2%.
**TAPI swing TUNGGAL juga 99.1%/93.7% (median 6–7)** — "pasti disapu" = sifat umum level dekat
harga di XAUUSD, bukan keistimewaan equal cluster (Δ≤1 ppt). Nilai REH/REL = LOKASI stop yang
diketahui (konteks reaksi pasca-sweep), bukan probabilitas sweep lebih tinggi. Konsekuensi: grup
berumur pendek (~6 jam) → watchlist alami berganti beberapa kali/hari.
Integrasi: step narasi "4¾. Liquidity — REH/REL" + `ScanNarrative.RelEquals` **FRACTAL multi-TF
(H1/H4/D — permintaan user 2026-06-02; per TF: REH terdekat atas + REL terdekat bawah; lookback
`releq_lookback_bars` CANDLE per TF → horizon melebar otomatis di HTF; `RelEqualMark.TF`)** +
`FormatRelEqLines` ber-TF ("REH H4 4536.82 (3× high) …") di watchlist narrate+alertd + garis/
marker cyan ber-label TF di chart (marker HTF di luar window plot di-skip) + token fingerprint
`REH|TF|level|count` (legacy 3-field dibaca sbg H1) → alert bold "REH H4 … TERSAPU — liquidity
diambil" saat harga melewati level (hilang-keluar-window = diam). Gate opsional `releq_sweep_gate`
(wait-for-sweep rule pertemuan 1-2 #6, basis H1; `-releqsweep` di backtest/walkforward/sweep;
fresh window `releq_sweep_fresh_bars`=6 H1) + tag `Signal.RelEqSwept` + breakdown "RelEq sweep".
**Baris MO watchlist TANPA angka (permintaan user):** `FormatMOLine` = "Saat ini harga DI
ATAS/BAWAH MO (premium/discount)" saja; diff crossing juga tanpa level — angka tetap ada di step
narasi 4½ & garis chart.
**HASIL UKUR — gate MERUGIKAN, default OFF:** breakdown baseline swept_fresh PF2.04 vs no_sweep
PF2.02 (identik — entry tanpa-sweep sama profitabelnya); IS 451tr/+244R/PF1.93 → gate ON
199tr/+98R/PF1.82; WF OOS +205R/PF2.04 → +74R/PF1.76. Konsisten MO: liquidity equal = KONTEKS
(penanda + alert sweep), bukan filter.

**Narrative + visual**: `engine.Narrate(tf, cfg, at) ScanNarrative` = narasi langkah-demi-langkah (TDA→Daily Bias→AMS→QT→POI→Entry 5m; regime/bias dihitung internal & dijelaskan menyatu di langkah DB, bukan node terpisah. AMS = gate keras: ITL aktif untuk buy / ITH aktif untuk sell di 1H, C.4) + level harga. `internal/viz.RenderSVG` render candlestick + anotasi POI/entry/SL/TP/EQ ke SVG (std-lib murni). `cmd/narrate -at <waktu> -svg out.svg` (atau `-find N` untuk memindai & menemukan momen SETUP VALID). Render SVG→PNG via `qlmanage -t -s 1000 -o /tmp x.svg`.

**DIAGNOSA kenapa belum profitable (3 subagent, 2026-05-31) — AKAR MASALAH:**
1. **Regime RETRACE = counter-trend sistematis** (akar utama). 21/24 trade = retrace (LAWAN weekly OF). Alignment vs tren: impulse 61% vs retrace **27%** (anti-correlated). Hampir semua rugi dari retrace (−13R). Penyebab kode: `ComputeRegime` (kunci2.go) TIDAK implement Section E penuh — tak ada target weekly-FVG, tak ada tracking FVG-fill untuk MENUTUP regime retrace → nyangkut counter-trend berbulan-bulan saat tren kuat.
2. **TP 1:3 terlalu jauh** (dampak exit terbesar). Cuma 2/24 punya MFE≥3R; 8 trade SL sempat ≥+1R lalu balik. Counterfactual: TP 1:1 → −15R jadi −5R.
3. **SL agak ketat** — median 0.7×ATR(H1); sebagian kena noise instan. Tapi melebarkan SL SAJA tak menolong (MFE kecil) — harus dipasang dengan TP lebih rendah / partial+BE.
4. **Zona/fib tak konsisten**: `macroFib` di-anchor ke arah OF tapi `SelectPOI` gate pakai bias (lawan, saat retrace); zona dicek di `POI.Mid()` bukan harga entry → 7 entry mendarat di zona salah; fib weekly basi (8/24 entry overshoot seluruh leg, `MinSwingPipsHTF=0` di 230 candle weekly).
5. **WeeklyOF rapuh/telat** (baca bearish di tengah bull market) + `MinSwingPipsHTF` tak dipakai konsisten (cuma di macroFib, tidak di WeeklyOF/ComputeRegime). fast_early dominan (20/24) tapi GEJALA dari retrace, bukan sebab.

**UJI impulse-only** (`Config.ImpulseOnly` / `go run ./cmd/backtest -impulse`): win **0% dari 5 trade**, −5R (vs default 8.3%/24/−16R). Memaksa searah-OF **bukan memunculkan edge, cuma memangkas sampel** (subset impulse di default pun 0% win). → masalah kualitas POI/entry menyeluruh + sampel kecil, bukan sekadar retrace. Chart entry-BUY visual jelas masuk di tengah DOWNTREND (lihat narrate SVG) — konfirmasi.

**✅ FIX (a) DONE — sumber zona/Fib (2026-05-31)**: `Config.ZoneFibTF` (default **ZoneFibDaily**) + gate zona dicek di HARGA (bukan `POI.Mid()`). Daily-fib (lebih banyak candle, non-stale) **membalik strategi**: default 24/8.3%/−16R/PF0.27 → **14/28.6%/+2R/PF1.20**; impulse-only-daily 12/33.3%/+4R/PF1.50. **Walk-forward OOS agregat: +3R / PF1.33 / win30.8% / MaxDD2.9%** (bertahan out-of-sample, fold 3&4 OOS +2/+6R). Lever = ganti sumber Fib weekly→daily (gate-at-price sendiri tak ubah angka). Caveat jujur: sampel kecil (14 trade), leak retrace counter-trend masih ada (klaster SL retrace) — sisa fix (b)/(e).

**✅ FIX SECTION E DONE (2026-05-31)**: `Config.RetraceFVGGate` (default true) + `state.ComputeRegime(weekly, ofDir, fvgGate, minGapPips)`. Regime RETRACE kini wajib punya **target weekly FVG** (di bawah swing-high pemicu utk bullish / di atas swing-low utk bearish — level TETAP relatif swing, BUKAN harga berjalan) dan **berakhir saat harga mengisi FVG** (`retraceFVGTarget` + cek fill). Bug awal (tautologi `price>target.Top` saat target dipilih `Top<price`) → diperbaiki pilih relatif `last.Price`. **Dampak: retrace 21→1 trade (leak counter-trend tertutup), TotalR +2R→+4R/PF1.71, walk-forward OOS +7R/PF2.40/win44%/MaxDD1.4%.** A/B: `go run ./cmd/backtest -retracegate=false` (unbounded) vs default. Test `kunci2_test.go`.

Progresi jujur: broken −16R → daily-fib +3R OOS → **Section E +7R OOS**. Edge positif IS & OOS, DD rendah. Caveat tetap: sampel kecil (9-12 trade), 3/4 fold walk-forward fallback.

**✅ FIX OVERSHOOT GUARD + FUNNEL DIAGNOSIS (2026-05-31, workflow)**:
- **Guard overshoot** (`fibOvershoot` di engine.go, buffer=0): `zoneFib` terima `price`, tolak Fib (ok=false → NO SETUP) bila harga sudah menembus seluruh leg. + regenerate narrative.svg basi.
- **`MinSwingPipsHTF` default 0→200** (sweep+OOS): 0–150 tak ngefek, ≥200 buang net-loser → backtest 8/37.5%/+4R/PF1.80; **walk-forward OOS +6R/PF3.00/win50%/MaxDD1.5%**. Provisional (sampel kecil).
- **🔍 FUNNEL DIAGNOSIS (kenapa entry sedikit — 12 dari 26073 candle = 0.05%)**: kill-rate sekuensial TDA 3.9% → Daily-Bias-strict **34.2%** → QT **90.5%** (16500→1569) → POI **98.2%** (1569→28) → Entry 57%. QT-fail: phase_not_tradeable 41.5% (OVER-FILTER; plafon timing struktural cuma 26.2% candle krn `PhaseTradeable` butuh weekly M/D AND daily M/D), PM 28.6% (wajar), Asia-belum-close 19.3% (wajar). POI: **89.7% mati krn harga tak sedang DI DALAM POI saat candle keputusan** (gate point-in-time terlalu sempit). **Dedup BUKAN penyebab (0 ditekan), confluence≥2 dampak kecil (conf=1 cuma +11), EntryFreshBars sekunder.**

**✅ FIX #1 POI WINDOW DONE (2026-05-31) — temuan menentukan:** `Config.POITouchWindowBars` (default **2**) + `detectors.SelectPOITouched` (POI valid kalau harga MENYENTUH zona dalam N candle H1 terakhir + belum-jebol + zona di Mid; bukan point-in-time). Flag `go run ./cmd/backtest -poiwindow=N`.
- **Sweep window**: 0→8 trade/+4R (noise), 1→63/+1R, 2→68/0R, 3→73/−5R, 6→82/−6R, 12→91/−7R. **Opportunity yg ke-skip MEMANG banyak (8→63 di window=1)** — dugaan user benar. TAPI win runtuh ke ~23-25% = breakeven RR1:3 → **TIDAK ADA EDGE di set realistis**; +4R/8-trade itu mirage sampel-kecil dari gate kelewat ketat. Walk-forward window=2 OOS 45 trade −5R/PF0.86 (slightly negatif).
- **🎯 EDGE TERKONSENTRASI (breakdown window=2, 68 trade)**: trigger **standard** PF1.75 (vs fast_early PF0.77), POI **Tier1** PF1.50 / Tier2 PF1.09 (vs T3/T4/T5 semua 0% win), regime impulse PF1.16 (retrace 0%). → arah fix berikut = **QUALITY GATE** (wajibkan standard ITL/ITH + Tier≤2 POI; buang fast_early & low-tier), BUKAN RR. Ini "analisa market/logic" yg user maksud.

**✅ QUALITY GATE DONE (2026-05-31) — edge OOS-validated:** `Config.RequireStandardTrigger` (default true, buang fast_early via `EntryTriggerMin(...,requireStandard)`) + `Config.MaxPOITier` (default 2, via `detectors.FilterPOITier`). Flag `-stdonly` / `-maxtier`.
- A/B (window=2): tanpa-quality 68tr/25%/0R → standard-only 19/37%/+9R → Tier≤2 57/30%/+11R → **FULL (std+Tier≤2) 17tr/41.2%/+11R/PF2.10**. **Walk-forward OOS 13tr/30.8%/+3R/PF1.33** (>breakeven 25% → bertahan OOS, BUKAN overfit murni). Breakdown default: semua standard; T1 PF1.29 + T2 PF4.00.
- **Reframe jujur**: "few entries" bukan masalah utama — masalahnya FULL set (68) penuh entry low-quality (fast_early/low-tier) yang breakeven. Quality gate ekstrak subset profitable (17 = few-but-good). Fix = LOGIC (buang fast_early & low-tier POI), bukan RR. Caveat: 17 IS/13 OOS masih kecil.

Progresi OOS: broken −16R → daily-fib +3R → Section E (window=0) +7R(noise n=8) → POI-window breakeven (true) → **quality gate +3R/PF1.33 (n=13, OOS-validated)**.

**✅ FIX BUG #2 DONE (2026-05-31): filter swing HTF jadi ATR-relative.** `Config.MinSwingATRMultHTF` (default 1.0) ganti `MinSwingPipsHTF` (yg ternyata cuma $20 = no-op). `zoneFib` hitung minLeg = mult×ATR(sumber, ATRPeriod) internal. Flag `-htfatr`. Sweep: ×1.0 terbaik (18tr/38.9%/+10R IS; **WF OOS +5R/PF1.50/win33%**). **Dampak nyata: Fib leg scan 2026-05-29 dari $136 minor → $320 dominan (4773.57→4453.39)** — keluhan user soal Fib teratasi. narrate.go + sample_charts regenerated.

**✅ FIX BUG #1 DONE (2026-05-31): A.1+A.2 wired.** `macroFib` sekarang pakai `detectors.FindValidImpulseZ(z, candles, dir, minRetracePct, maxCalib)` (A.1 last-impulse + A.2 Rule-of-0.5 + kalibrasi) di atas zigzag TER-FILTER ATR — bukan `FindLastImpulse` (pasangan terakhir tanpa validasi). `FindValidImpulse` di-refactor jadi wrapper `FindValidImpulseZ` (bukan dead code lagi). Threaded via `zoneFib(...,minRetracePct,maxCalibration,...)`; narrate konsisten. Dampak: backtest 18→**19tr/42.1%/+13R**; WF OOS +5R/PF1.50 (held); Fib leg scan 2026-05-29 = 4660.59→4501.03 (leg validasi terbaru, bracket ITH→low, masuk akal). `FindLastImpulse` kini cuma dipakai di test-nya sendiri.

**✅ FIX BUG #3/#5/#6 + NEXT-POI + KONSISTENSI (2026-05-31, workflow):**
- **#3**: mark ITL/ITH chart pakai `DetectIntermediatesMin(plot, bt, MarksMinATRMult×ATR_H1)` (default 1.0) → pivot di ekstrem nyata (terkonfirmasi render).
- **#5**: `Config.VIMinGapPips` (15=$1.50, 3×FVG) + `POIMaxWidthATRMult` (0.5×ATR_H1 cap lebar cluster) + `clusterTier` VI tak auto-Tier1 (butuh konfluen). `DetectPDRs(c, minGap, viMinGap)`, `BuildPOIs(pdrs, confMin, maxWidth)`.
- **#6**: VI wajib KEDUA candle searah; BB sudah sesuai F.1. **Kunci#1 Tier-2 (FVG@swing-break) DEFER** (butuh konteks swing).
- **Next-POI**: `ScanNarrative.{HasNextPOI,NextPOITop,NextPOIBottom,NextPOITier,NextPOIDistance}` + `detectors.NearestValidPOI`. Saat NO SETUP, narasi sebut "POI valid terdekat di X–Y (Tier N), ~Z poin — tunggu harga rally/turun ke zona itu" (jawab "harga berapa ditunggu").
- **Konsistensi engine↔narrate: 0 DRIFT** (audit knob-by-knob, semua identik). Cosmetic "NO SETUP" dobel di cmd di-fix.
- **Dampak**: backtest 19→14 trade tapi **win 42→50%, +13→+14R, PF ~2→3.00**; tier sehat (T2 PF3.50 dominan, T1 cuma 1=loser). **WF OOS naik +5R/PF1.50 → +8R/PF2.14**. Edge BERTAHAN & menguat. Caveat: sampel kecil (14/12 OOS), WF 4/4 fold fallback.

**✅ RONDE BESAR 2026-05-31 (workflow): Kunci#1 + Kunci#3 + BPR + IFVG + config.yaml + viz fix.**
- **Kunci#1** (FVG@swing-break→Tier2, `KindFVGBreak`, `FVGSwingBreakAdjacency`=3): **hipotesis user TERBUKTI** — entry sebelumnya dibuang sbg Tier-4 oleh gate MaxPOITier=2. Kunci#1 angkat 18/19 entry ke T2. Backtest 14→**19 trade**.
- **TAPI edge ter-DILUSI** (jujur): PF **3.00→1.75** IS, **WF OOS +8R/PF2.14 → +3R/PF1.25**. Entry tambahan nyata tapi kualitas lebih rendah di RR1:3. → arah lanjut: bikin Kunci#1 selektif (butuh confluence, bukan FVGBreak tunggal; sweep adjacency).
- **Uji tier-gate**: maxtier 2=3 (tak ada POI T3), 4/0 cuma +2 entry (keduanya menang). Kunci#1 sudah menyelamatkan mayoritas; longgar-gate marginal. Default maxtier=2 tetap.
- **Sweep**: htf=1.0 & VIMinGapPips=15 optimal/no-op (konfirmasi); lever nyata = POIMaxWidthATRMult & POITouchWindowBars. Kandidat width=1.0/window=1 → IS +13R/PF2.18 TAPI **belum di-lock** (risiko overfit, butuh OOS). Default tak diubah.
- **Kunci#3** (`Kunci3Fallback`, default OFF; `opposingLiquidity` Old High/Low): ON → 57 trade tapi PF1.17 (dilusi). Default OFF benar.
- **BPR** (`detectBPRs`, Tier3, BISI+SIBI overlap≤5) + **IFVG** (`detectIFVGs`, Tier4, FVG ter-mitigasi flip) — DONE. Smoke: FVGBreak 3826, BPR 1326, IFVG 3944.
- **config.yaml + `internal/config.Load` + flag `-config`** (flat-YAML std-lib, 31 key; 2026-06-02: +12 key gate toggle `asia_close_gate`/`qt_phase_gate`/`session_pm_gate`/dst — semua bool Config kini bisa di-set via YAML, mirror DefaultConfig diverifikasi diff backtest).
- **Viz**: Fib "ngaco" = **bug TAMPILAN** (angka benar) — anchor leg di luar window 120-candle → segmen ke-clamp jadi garis vertikal pojok; fixed (skip diagonal kalau anchor di luar window, pakai level + label "(di luar layar)"). Label PD box numpuk → di-stagger.
- **BUG ditemukan & fixed**: `cmd/entries` HANG (infinite loop di `drawPDBoxLabels`) saat banyak box sekolom — diperbaiki (loop ber-cap monoton) + regresi test.

**✅ FIX DRIFT NARRATE GATE (2026-06-02)**: gate toggle 2026-06-01/02 dulu cuma di-wire ke
`evaluate()` — `Narrate()` Step 4 masih hard-block PM / Asia-belum-close / phase-not-tradeable
TANPA cek config → cmd/narrate & alertd bilang "NO SETUP: sesi Asia belum close" padahal engine
tak memblokir. Fixed: narrate.go kini cek `SessionPMGate`/`AsiaCloseGate`/`QTPhaseGate`/`DayTypeGate`
(mirror evaluate persis); gate OFF ditampilkan sbg `[PENANDA, gate off]` (pola AMS). ⚠️ alertd yang
masih jalan dgn binary lama perlu rebuild+restart.

**✅ AUDIT GATE MENYELURUH (2026-06-02, funnel + A/B 13 gate + marginal-set + OOS):**
Baseline default kini **709 trade / win 35.3% / +216R / PF1.47 / DD26R (IS)**; WF OOS 544tr/+162R/PF1.46/DD14.7%.
- **Funnel** (`cmd/gatestats`): daily-bias 32.9% → POI 19.3% → trigger-5m 18.9% → heavy_accum 14.1% → LondonQ1/Q2 8.3%; PASS 2.7%.
- **Semua gate OFF terkonfirmasi benar OFF** (subset yang akan disaring justru profitable): QTPhase buang 358tr **PF1.61**, PM buang 151tr **PF2.15** (subset TERBAIK), Fib buang 554tr PF1.58, AMS buang 284tr PF1.52, AsiaClose buang 167tr PF1.33, Agenda → 7tr PF0.50.
- **Gate ON terkonfirmasi benar ON** (pola seragam: di-off-kan nambah R absolut tapi rusak risk-adjusted): DayType off → korban n=266 PF1.13, OOS PF1.46→1.33 DD14.7→21.9%; StdTrigger off → +1449tr fast_early PF1.10 (tak lagi se-toxic dulu, tapi OOS DD 14.7→**35%**); LondonQ34 korban PF0.85; MaxTier H1 korban PF1.03; Kunci3 ON +583tr PF1.15 OOS PF↓ (dilusi). FractalPOI = sumber edge (off → −493tr/−150R).
- **R-per-DD OOS: default 11.0 vs daytype-off 7.7 / stdonly-off 6.6 / kunci3-on 9.8** → default = optimal risk-adjusted; longgar-gate = mode volume (lebih R absolut, DD jauh lebih dalam).
- **🎯 Subset jelek DI DALAM set default (kandidat fix berikut, BUKAN gate baru asal-asalan):**
  (1) **Sisi BEARISH: 245tr cuma +1R** (D/bearish PF0.65, A/bearish PF0.74; semua edge di bullish). Hipotetis long-only: totR sama (+215R), PF1.77, DD 26→14R. Akar diduga WeeklyOF/bias short rapuh di bull market 2022-26 (diagnosa lama #5) — fix LOGIKA short, jangan gate long-only (curve-fit periode).
  (2) **London tetap lemah** meski Q34Only (PF1.01; T2/london PF0.60). Tanpa London: PF1.55, cuma −1R.
  (3) T3/asia PF0.56 (n=39) klaster aneh; T5-HTF PF1.10 (OB weekly bypass MaxTier by design — buang T5: PF1.57, −10R).
  Artefak: `/tmp/ga/` (base.csv + 13 CSV A/B + wf_*.txt). `cmd/walkforward` kini dukung `-config`.

**✅ RONDE FIX BIAS/OF + OB-OFF + SKIP-8 (2026-06-02, workflow 2 agent + staged IS/OOS):**
- **Default baru OOS-validated: 564tr/37.1%/+211R/PF1.59/DD18R (IS); WF OOS +170R/PF1.60/MaxDD9.0%** (vs lama 709tr/PF1.47/DD26R; OOS PF1.46/DD14.7%). Lever = `DisableOB=true` + `SkipEntryHoursNY="8"`.
- **Filter magnitude OF/bias (hipotesis fix #5) TIDAK terbukti**: `WeeklyOFMin/DailyBiasMin/ComputeRegimeMin/ComputeAnchorsMin` (state/, streaming anti-lookahead, wrapper lama=minLeg 0, test `orderflow_min_test.go`) + knob `OFSwingATRMult`/`BiasSwingATRMult` (`-ofatr`/`-biasatr`). Sweep IS: ofatr=0 TERBAIK (+211R); biasatr>0 konsisten buruk. Counter-trend short memang turun (−18→−10R) tapi ter-offset short searah. **Default keduanya 0** — short loss bukan (cuma) soal micro-flip OF; struktural lebih dalam.
- **Audit DailyBias (agent)**: BELUM bagus — agreement fwd5d bearish 44.3% (< koin, avg ret NEGATIF semua kondisi tren), bullish 57.6% OK; flip 17×/thn vs WeeklyOF 3×; (bull,bull)=+207R dari total +216R; db-lawan-arah-trade = 20% win/−22R. WeeklyOF fwd20d agree 65.1% (bullish 71.6%, bearish 53.3%).
- **RetraceFVG weekly (agent)**: TIDAK "pasti" — fill-rate **81%** (25/31 episode, CI95 64-91%), median 1 minggu (target sering dangkal, median $73); 19% gagal SEMUA bullish-OF bull-run; bearish 10/10. Filter ATR memangkas episode 31→14 tanpa menaikkan fill (79%). Saran agent: TP retrace konservatif (target=FVG, bukan RR1:3) / partial+BE.
- **OB off semua TF** (`DisableOB`, `dropOB` engine+narrate, user decision — logika OB diragukan): T5 hilang, pool lebih bersih.
- **Skip jam 08:00 NY** (`SkipEntryHoursNY` CSV, gate `gateSkipHour`): news hour 08:30 ET (PF0.62/−16R IS).
- **LondonQ4Only** (knob `-londonq4`): OOS PF1.66/DD7.8% vs F PF1.60/9.0% TAPI per-fold mixed + kontradiksi atribusi jam (04:00 = jam London terbaik PF1.50 justru kena buang; 05:00 yg dipertahankan PF0.92) → **TIDAK di-lock**, knob tersedia. London di F: +4R/PF1.07 (edge-less, bukan toxic). Konsep user (Q2 sweep→Q3 ITL 5m baru = entry) belum dimodelkan eksplisit — kandidat rule London khusus berikutnya.
- ofatr=1.5 OOS PF1.69 tapi DD12.1% & pola sweep tak stabil → tidak diadopsi, re-test saat sampel besar.
- Paritas dicek: kode baru semua-knob-off == binary lama EXACT; config.yaml mirror diff ✓; alertd rebuilt+restarted (PID baru).

**✅ RONDE LONDON-4 + RETRACE-TP + BEDAH OF/BIAS (2026-06-02, workflow 3 agent):**
- **Default baru: `LondonMinHourNY=4`** (buang candle 03:00, pertahankan 04:00 terbaik) → IS 541/+212R/PF1.63/DD17R; **WF OOS +166R/PF1.64/MaxDD8.5%** (naik dari PF1.60/9.0%). Knob `-londonminh`, key `london_min_hour_ny`.
- **`RetraceTPToFVG` DIUJI & GAGAL** (permintaan user "TP retrace = level FVG"): jarak entry→target FVG ternyata **10–17× risk SL-5m** (yang "dangkal" = jarak dari SWING pemicu, bukan dari entry) → retrace 0%win/−18R vs −10R RR-standar. Default **false**, knob `-retracetp`/`retrace_tp_to_fvg` + `state.RetraceTargetMin` & `retraceTP` (engine+narrate mirror) tetap ada utk re-test. Catatan: retrace n=18 PF0.38 — subset retrace memang masih busuk.
- **🔍 BEDAH WeeklyOF (katalog 13 flip)**: bullish **6/6 BENAR** (avg dir-8w +8.1%); bearish **5/7 WHIPSAW** (wick-break low dangkal di koreksi bull, reclaim <4 minggu). Fix terbaik (estimasi NET +25.3%): **asimetri konfirmasi-kelanjutan khusus bearish** — flip bull→bear baru sah bila weekly close BERIKUTNYA tetap di bawah harga flip (bullish tanpa syarat). Kandidat knob `WeeklyBearConfirmWeeks=1`. BELUM diimplementasi (butuh keputusan user).
- **🔍 BEDAH DailyBias**: definisi break TIDAK rusak (4 varian definisi + momentum naif semua gagal angkat bearish >45% — bearish<koin = fakta pasar bull-run). Lever satu-satunya: align-WeeklyOF (sudah implisit di pipeline) + freshness TERBALIK.
- **🔍 STALENESS (temuan TERBALIK dari dugaan)**: racun = **bias BARU FLIP** (bias_age≤5 hari: 118 trade, win 22%, PF0.77; bearish-fresh win 14.5%/−25R) — bukan bias tua (umur 16-60d justru PF~2.1). OF kebalikan: **of_age>60 hari = 60% sampel tapi PF1.29** (impulse kehabisan tenaga). **Cutoff scan IS: drop bias_age≤5 → totR 211→232/PF1.88 (n=446); KOMBO bias_age>5 & of_age≤60 → n=174/win49.4%/PF2.64, robust di kedua paruh & tiap tahun.** Kandidat: `MinBiasAgeDays`/`MaxOFAgeDays` (butuh expose umur-flip dari state) — ⚠️ cutoff hasil IS-mining, WAJIB OOS sebelum lock. BELUM diimplementasi.

**✅ RONDE GATE UMUR-FLIP + BEAR-CONFIRM (2026-06-02, staged IS→OOS):**
- **Default baru: `MinBiasAgeDays=8`** (gate `gateBiasFresh`: entry hanya bila daily bias berumur ≥8 hari sejak flip) — **OOS DOMINAN vs baseline di semua metrik: +180R/PF1.96/win42%/MaxDD8.4% vs +166R/1.64/37.7%/8.5%**. IS 416tr/+221R/PF1.91/DD16R. Kurva cutoff mulus (4/6/8→PF 1.73/1.76/1.91), bukan knife-edge.
- **State expose umur-flip**: `WeeklyOFFull(weekly,minLeg,bearConfirmWeeks)(dir,anchors,flipTime,ok)` + `DailyBiasFull(daily,minLeg)(dir,flipTime,ok)` (flipTime = completion candle flip; wrapper lama tetap). Gate baru `gateOFStale`/`gateBiasFresh` + mirror narasi (umur OF/bias tampil di step 1/2).
- **`OFBearConfirmWeeks` DIUJI & GAGAL** (estimasi agent NET+25% TIDAK bertahan di engine): delay flip 1 minggu mengganti 33 short net-positif (+6R) dengan **43 LONG di tengah koreksi nyata (PF0.71, −10R)** + 5 short telat (0% win) → IS +212→+191R. Default 0; knob `-bearconfirm`. Pelajaran: estimasi directional-return ≠ dampak engine (substitusi arah!).
- **`MaxOFAgeDays` default OFF**: standalone PF naik (2.17) tapi totR anjlok (+212→+140; bucket OF>60d masih +67R). **KOMBO biasage6+ofage60 = "mode konservatif"**: OOS 165tr/win46.7%/**PF2.36/MaxDD4.8%** tapi −46R — tersedia via config utk prioritas DD rendah. Full 3-gate terlalu ketat (53tr OOS).
- Knob: `-bearconfirm/-biasage/-ofage`; key `of_bear_confirm_weeks/min_bias_age_days/max_of_age_days`. Test `TestWeeklyOFBearConfirm` (reclaim → hangus; lanjut-turun → flip).
- **Progresi OOS hari ini: PF1.46/DD14.7% (pagi) → 1.60/9.0 (OB+skip8) → 1.64/8.5 (london4) → 1.96/8.4 (biasage8)**. Paritas knob-off == perilaku lama dicek tiap tahap; config.yaml mirror diff ✓; alertd rebuilt+restarted.

**✅ RETRACE DIMATIKAN — `ImpulseOnly=true` DEFAULT (2026-06-02, uji A/B/OOS):**
Subset retrace kronis busuk (default: n=10, win10%, PF0.33). Dua cara "matikan" diuji:
- **B `SkipRetrace`** (knob baru + gate `gateRetraceSkip` + mirror narasi; periode retrace NO TRADE): IS 406/+227R/PF1.97; OOS +180R/PF2.02/DD8.0%.
- **A `ImpulseOnly`** (periode gate-sweep-open tetap trade SEARAH OF): IS 445/+238R/PF1.92; **OOS +197R/PF2.01/DD7.9%, 4/4 fold positif — MENANG (R lebih besar, PF/DD setara)** → di-lock default.
Catatan: knob `ImpulseOnly` era config awal pernah diuji & buruk — konteks gate sudah berubah total; `SkipRetrace`/`RetraceFVGGate`/`RetraceTPToFVG` kini inert di default (regime tak pernah retrace) tapi tetap hidup utk eksperimen. **Default akhir hari ini: IS 445tr/41.6%/+238R/PF1.92/DD16R; OOS +197R/PF2.01/MaxDD7.9%.**
Progresi OOS 2026-06-02 (satu hari): **PF1.46/DD14.7% → 1.60/9.0 (OB-off+skip8) → 1.64/8.5 (london≥04) → 1.96/8.4 (biasage=8) → 2.01/7.9 (+197R, impulse-only)**.

**✅ REMINDER DONE — BREAKDOWN TIER×TF + MaxPOITier 2→3 (2026-06-02):**
- **Report**: kolom CSV/XLSX `poi_tf` + breakdown baru "POI tier×TF" (`report.Print`) — tier H1 vs HTF kini terpisah (sebelumnya tercampur & menyesatkan: T3/T4 di breakdown lama = HTF bypass, bukan H1).
- **Temuan struktur (default 445tr)**: POI H1 cuma **15% trade (65) & segmen TERLEMAH (PF1.56)**; mayoritas dari HTF: H4 207tr/PF1.98, D 144tr/PF2.01, W 29tr/PF1.82. Sel terbaik = **T4-HTF (FVG tunggal fresh): D-T4 PF3.50 (n=36), W-T4 PF2.60 (n=20), H4-T4 PF2.38 (n=39)** — nomor tier TIDAK menggambarkan kualitas utk HTF. W-T2 satu-satunya sel HTF negatif (n=9, PF0.71).
- **Uji buka tier (hanya pool H1)**: 2→3 nambah 6 trade (H1-T3, PF3.00 IS); 2→4/0 cuma +1 loser. **`MaxPOITier=3` di-lock**: OOS dominan tipis semua metrik (+205R/PF2.04/win43.1%/DD7.9% vs +197R/2.01/42.7%/7.9%). Caveat jujur: marginal n=6 — efek kecil, bukan game-changer. **Default akhir: IS 451tr/41.7%/+244R/PF1.93; OOS +205R/PF2.04/MaxDD7.9%.**
- Kandidat lanjutan dari tabel tier×TF: buang W-T2 (n=9 PF0.71, sampel kecil — biarkan dulu); H1 pool lemah menyeluruh → riset kualitas POI H1 (bukan tier-nya).

**✅ BREAKDOWN JENIS POI + CONFLUENCE + BEDAH H1 (2026-06-02):**
- **`Signal.POIKinds`** (via `poiKindSummary`) + kolom CSV/XLSX `poi_kinds` — jenis komponen POI kini terlacak per trade.
- **Jenis POI per TF (default 451tr)**: BINTANG = **FVG & FVGBreak** (D-1×FVG PF3.50 n=36; D-1×FVGBreak PF3.29; H4-1×FVGBreak/1×FVG PF2.24; W-1×FVG PF2.60). **LEMAH = BB (Breaker)** hampir semua TF: D-ada-BB **PF0.83/−6R** (n=48), D-1×BB PF0.92, H4-1×BB PF1.44. VI: H4 PF3.75 (n=10) tapi W PF0.40 (n=6) — sampel kecil. BPR netral.
- **🔥 CONFLUENCE TERBALIK dari intuisi**: makin banyak komponen beririsan makin BURUK, monoton di semua TF — H1 conf2 PF2.05→conf4+ 0.83; H4 conf1 2.00/conf2 3.27→**conf4+ 0.30**; D conf1 2.28→conf4+ 0.00. "Zona rame" = band lebar/kontes likuiditas, bukan kekuatan. (ConfluenceMin=2 utk H1 tetap — itu syarat minimum cluster H1; temuan ini soal SISI ATAS.) Kandidat uji berikut: **cap confluence ≤3** + **drop BB dari pool D (atau semua TF)** — belum diimplement, butuh OOS.
- **Bedah H1 (71tr/PF1.66)**: kelemahan H1 TERKONSENTRASI di (a) tahun 2022-23 (PF0.33/0.67; 2024-26 justru sehat, 2025 PF3.08) dan (b) sisi bearish (n=16 PF0.43 vs bullish PF2.23). Sesi: asia-H1 PF2.70 bagus, london-H1 0.83. Bukan rusak menyeluruh — rusak di short + era awal.
- Konsep dipertegas ke user: **Tier = peringkat kualitas JENIS PD Array** (T1 VI, T2 FVGBreak, T3 BB/BPR, T4 FVG/IFVG, T5 OB + upgrade cluster BB+FVG→T2, VI+konfluen→T1); **confluence = jumlah irisan** — dua sumbu berbeda. W-T2 = trade ber-POI weekly dgn clusterTier 2.

**✅ CONFLUENCE-CAP + BB-WAJIB-FVG DI-LOCK + AUDIT BEARISH (2026-06-02):**
- **Default baru: `MaxConfluence=3` + `BBNeedsFVG=true`** (helper `filterPOIs` semua TF, engine+narrate 0-drift; flag `-maxconf`/`-bbfvg`, key `max_confluence`/`bb_needs_fvg`). Yang dibuang: POI conf>3 (n=11, PF0.56) + cluster BB-tanpa-keluarga-FVG (n=62, PF1.07 breakeven). **IS 374tr/44.4%/+241R/PF2.16/DD13R; OOS +202R/PF2.36/win46.8%/MaxDD6.7% — SEMUA 4 fold PF≥2.05 (paling konsisten sejauh ini).** Ide BB-wajib-FVG = usul user, terbukti; drop-BB total tak perlu.
- **H1 `confluence_min=1` DIUJI & DITOLAK**: +74 trade PF1.13 → IS PF1.80/DD22 (vs 1.93/17); OOS conf1 PF1.87/DD11.8% — mode volume, kualitas turun. PDR H1 tunggal ≠ FVG HTF tunggal.
- **AUDIT BEARISH: fix-fix terbukti BEKERJA.** Alignment piramida DIJAMIN konstruksi (bias=arah WeeklyOF via ImpulseOnly; gate STEP2 wajib dailyBias==bias + umur≥8hr) — short HANYA terjadi saat TDA+DB bearish. Empiris (pra-lock-C3, 451tr): bearish 121tr/+33R/PF1.41 (pagi: 245tr/+1R/PF1.01); **2022 (bear market nyata) PF2.27/+42R**; short counter-trend tinggal n=14 dan POSITIF (PF1.67, dulu 76tr/−18R/PF0.70); 2025 NOL short. Sisa lemah = klaster era kecil: H1-bearish 2022-23 (n=12) & W-bearish 2023 (n=9) — bukan leak struktural masa kini.
- **Progresi OOS 2026-06-02 (full day): PF1.46/DD14.7% → 1.60/9.0 → 1.64/8.5 → 1.96/8.4 → 2.01/7.9 → 2.04/7.9 (maxtier3) → 2.36/6.7% (+202R, conf-cap+BB-FVG)**.

**✅ REVIEW FUNNEL "ENTRY TERLALU SEDIKIT" (2026-06-02, gatestats + uji lever):**
- Funnel default (26108 candle → 374 PASS = 1.4%, ~1.6 entry/minggu). Kill-rate KONDISIONAL per tahap: **trigger-5m 87.1%** (2530/2904) & **POI-tak-tersentuh 63.3%** (5008/7912) = bottleneck opportunity; daily-bias-align 31.5% (struktural piramida); bias-fresh<8d 23.4% & heavy_accum 23.2% (quality gates ter-OOS); London-jam 18.2% (struktural clock); skip-8 4.4%.
- **Uji lever volume (IS+OOS)**: `EntryFreshBars` 2/3 → PF anjlok (OOS 1.99/DD9.5); `POITouchWindowBars` 3 → OOS +215R/PF2.30/DD7.8; **4 → OOS +239R(+18%)/363tr(+31%)/PF2.19/DD8.3**; default C3 tetap PF2.36/DD6.7. **TIDAK ADA yang OOS-dominan — murni frontier volume↔kualitas** (R/DD: C3 30.1 > pw4 28.8 > pw3 27.6 > ef2 23.1). Default TIDAK diubah; pw4 = pilihan sah kalau prioritas frekuensi (`poi_touch_window_bars: 4`).
- Catatan jujur: entry sedikit = HARGA dari lonjakan kualitas hari ini (pagi 709tr/PF1.47 → kini 374tr/PF2.16) — funnel yang sama yang menghasilkan PF.

**✅ RULE LONDON SWEEP-ENTRY DIIMPLEMENT & DIUJI (2026-06-02, workflow audit 2 agent):**
- `Config.LondonSweepEntry` (default **false**) + helper `asiaSwept` (engine+narrate 0-drift, diaudit adversarial: LULUS, anti-lookahead/DST/gap/tie semua benar): jam London yang diblok (Q34/MinHour) di-bypass bila wick H1 pasca-Asia menembus ekstrem Asia BERLAWANAN bias. Flag `-londonsweep`, key `london_sweep_entry`.
- **HASIL: net-netral/negatif tipis — TIDAK di-lock.** Marginal MURNI ADITIF +12 trade (jam 02-03 NY, 4.5 thn) win 25%/−1R/PF0.89; IS PF 2.16→2.11; OOS +203R/PF2.31/DD7.8 (vs 2.36/6.7) ≈ noise.
- **Temuan kunci audit**: sweep Asia terjadi ~75% hari (sisi-lawan-bias ~40%) — kondisi sweep BUKAN bottleneck; yang menyaring = gate piramida lain (bias/POI/trigger). Jam London-awal jarang punya SEMUA syarat piramida → "trade terbuang" di London-awal memang sedikit secara alami. Catatan konsep: implementasi tak mensyaratkan REVERSAL pasca-sweep (12 trade aktual kebetulan semua reversal krn tersaring POI/ITL — tetap PF0.89, jadi syarat reversal pun takkan menolong di sampel ini).

**✅ RONDE VOLUME (2026-06-03, keputusan user): pw4 + bias-bull-3 DI-LOCK; trigger alternatif DIUJI & DITOLAK:**
- **Default baru: `POITouchWindowBars=4`** (sentuhan POI berlaku 4 jam) + **`MinBiasAgeDaysBull=3`** (karantina umur-bias ASIMETRIS: bullish 3 hari kuncinya fresh-bull PF1.09 jinak, bearish tetap 8; helper `biasAgeMin`, knob `-biasagebull`/`min_bias_age_days_bull`, -1=ikut global). **IS 524tr/42.2%/+294R/PF1.97/DD17R; OOS 419tr/+250R/PF2.05/MaxDD8.9%** (vs sebelumnya 374tr/PF2.16 & OOS PF2.36/6.7 — trade-off frekuensi DISETUJUI user: ~2.2 entry/minggu dari 1.6).
- **`EntryTriggerMode` (itl|disp|reject|dispreject|sweep)** — dispatcher `entryTrigger5m` (engine+narrate 0-drift; `rejectHit` + `poiRejectSlack` $2; disp = body>=`DispATRMult`×ATR m5; sweep = tembus swing 5m terakhir langsung entry). **SEMUA mode alternatif DITOLAK**: volume meledak 3-8× tapi kualitas runtuh — IS disp 1754tr/PF1.29/DD43, reject 1328/1.36/26, dispreject 2530/+537R/1.31/DD51, sweep 3207/1.15/DD64; OOS dispreject PF1.35/DD27.3%, reject PF1.37/DD19.3% (R/DD 16.7 & 13.2 vs default 28.1). **Konfirmasi struktur ITL = konsentrator edge** — bukan sekadar gate. Default `itl`; knob `-trigmode`/`-dispatr` tersedia (mode volume ekstrem kalau mau).
- Penjelasan ke user terdokumentasi: pw window = umur-berlaku sentuhan POI; biasage 2b kill = 8 hari PERTAMA tiap periode aligned (bukan trading bias lama); flip = close-break swing daily.

**✅ WATCHLIST REFORMAT 6-SEKSI (2026-06-03, permintaan user — narasi piramida penuh terlalu panjang dibaca di HP):**
Pesan WATCHLIST alertd (yang dikirim saat perubahan bermakna) kini ringkas 6-seksi, BUKAN narasi piramida ┌─SCAN.
Alert SETUP (saat ada entry valid) TETAP pakai narasi piramida penuh (`narrativeBlock`) — hanya watchlist yang dirombak.
- **Formatter bersama `engine.FormatWatchlist(n, instrument, width)`** (0-drift, dipakai `cmd/alertd.watchlistBlock` width 42 + `cmd/narrate.printWatchlist` width 88): (1) TDA = arah weekly OF saja; (2) Daily Bias = keterangan; (3) AMS = konfirmasi ITL/ITH 1H terbentuk/belum (`✓/✗ + pivot`); (4) QT = keterangan Q2 (manipulasi sweep Asia) & Q3 (ekspansi = window entry) + quarter saat ini; (5) **Komponen Array per-TF (H1/H4/D)**: komponen PD Array TERDEKAT ke harga (3 tiap sisi, dedup level identik) — **FVG dilabeli BISI (bullish/buy-side) / SIBI (bearish/sell-side)**, kind lain (VI/BB/BPR/IFVG/FVGBreak/OB) pakai namanya — + baris REL/REH per TF; (6) Info tambahan: **MO DENGAN harga open** (beda `FormatMOLine` yang sengaja tanpa angka) + "Asia hari ini: A/X" (reuse `FormatAsiaLine`, A/X Asia-close existing — TAK ada detektor London-close baru). Baris array di-wrap ke width dengan gutter `│` (rune-count, bukan byte) supaya rapi di HP.
- **Field baru `ScanNarrative`**: `WeeklyOFDir`/`DailyBiasNote`/`AMSChecked`/`AMSActive`/`AMSKind`/`AMSPivot`/`QTSession`/`QTLondonQ`/`ArrayByTF []TFArray` (PDR hidup per TF, deteksi via `liveTFPDRMarks` = mirror `buildTFPOIs` → 0 drift). Semua diisi di langkah Narrate yang sama (mirror step).
- Header bold luar pesan alertd disederhanakan (`🆕 kiriman pertama` / `⚠️ ADA PERUBAHAN` + diff / `ℹ️ heartbeat` / `📍 diminta manual`) — blok monospace `FormatWatchlist` punya header sendiri (📍+timestamp+harga). Trigger/dedup/diff (`watchlistFingerprint`/`watchlistDiff`/`watchlistTrigger`) TAK diubah (masih basis NextPOIsByTF+MO+Asia+RelEq). Penanda ➜ per-baris zona di-drop (layout tak lagi daftar POI-per-TF). Test `internal/engine/watchlist_test.go` (seksi + dedup + wrap-42). ⚠️ alertd live perlu rebuild+restart utk efek.

**✅ ALERT Q1-SESSION A/X (2026-06-03, permintaan user — "tiap penutupan Q1 session tentukan A/X, lihat 15m"):**
QT per-session (Phase 2, rules line 33/518) kini DI-DETECT live. Sebelumnya A/X cuma utk SELURUH sesi Asia (Q1 harian, 1H, @00:00 NY). Sekarang tiap **Q1 sesi** (Asia 18:00–19:30, London 00:00–01:30, NY-AM 06:00–07:30 NY) diklasifikasi A/X saat Q1 close, **dibaca dari 15m**.
- **Data M15 ditarik SAMA seperti H1/H4/M5** (keputusan user): `M15` ditambah ke `granularities` `cmd/fetch` + `cmd/alertd` (FetchCandles + cache + AppendNew, dailyAlign=18 — diabaikan utk intraday). `engine.TFData.M15` (TIDAK dipakai pipeline `engine.Run`; diisi/dibaca alertd saja; loader lain biarkan nil). ⚠️ alertd bootstrap M15 dari `-from` (default 2022) saat M15.csv belum ada — fetch awal lebih berat (one-time, ~110k candle/~13MB), tick berikutnya cuma delta. Smoke-test live OK: alignment menit 0/15/30/45, Q1 = tepat 6 candle.
- **Logika A/X = `ClassifyDaily` diturunkan ke 15m (fractal, 0 parameter baru)**: X bila Q1 ninggalin FVG-15m DAN |close−open| Q1 ≥ `MinAtrMult`×ATR_15m; selain itu A. Reuse knob `MinAtrMult`/`MinGapPips`/`ATRPeriod` existing.
- **Detector baru** (`qt.go`): `SessionStart` (jam awal sesi 18/00/06/12 NY, DST-aware) + `SessionQuarter` (generalisasi `LondonQuarter` ke semua sesi). Test `session_test.go`.
- **alertd** (seksi "3. Alert Q1 session close" di `runOnce`): `maybeSendQ1Alert` + `latestQ1Close` (Q1 close terakhir ≤ now; PM 13:30 di-EXCLUDE — di luar window entry, keputusan user) + guard "tick TEPAT setelah close" (`now.Sub(b) < interval`, tick :30:20 NY menangkapnya) + dedup `notify.State.LastQ1Close` (monoton). Pesan: `🕐 *Asia Q1 close (18:00–19:30 NY)* — AKUMULASI (A) / X (expansive)` + ekspektasi AMDX/XAMD. Test `cmd/alertd/q1_test.go`. ⚠️ alertd live perlu rebuild+restart utk aktif (akan bootstrap M15 dulu).
- **Trigger manual `/q1` (2026-06-03):** ketik `/q1` di chat bot → `sendManualQ1` balas A/X Q1 SEMUA sesi trading-day berjalan (Asia/London/NY-AM via `tradingDayQ1Slots`, wall-clock NY DST-aware; belum-close ditandai). Klasifikasi via `q1Scenario` (di-extract dari `maybeSendQ1Alert` → 0-drift alert otomatis↔manual); TIDAK menyentuh dedup. Di-handle goroutine `watchCommands` yang sama dgn `/watchlist`. Test `TestTradingDayQ1Slots`.

**Sisa:** bikin Kunci#1 selektif; lock width/window via OOS; perbesar sampel (`cmd/fetch -from 2018`) — makin penting (semua angka 1 rezim bull); unit test `asiaSwept` (tak urgent). RR global DITOLAK user.

**✅ CACHE TUNGGAL + FIX FETCH (2026-06-02):** `data/XAU_USD` di repo kini **symlink** →
`~/.forex-alertd/data/XAU_USD` (cache yang di-refresh alertd tiap **5 menit**; yang "per ~1 jam"
itu alert watchlist per H1 close, bukan interval poll). Semua tool (narrate/backtest/sweep/…)
otomatis baca data live — tak perlu fetch manual selama alertd hidup (narrate cetak "Data … s/d
candle H1 terakhir" untuk verifikasi). Backup cache lama: `data/XAU_USD.bak`. Pendukung:
- **Bug paginasi `FetchCandles` FIXED**: OANDA mengembalikan ulang candle yang MENGANDUNG `from`
  → duplikat tiap batas 5000-candle (terbukti: M5 62, H1 5, H4 1 dup; baris 5001/5002 identik).
  Guard `t.Before(cursor)` di candles.go + regression test `candles_test.go` (httptest 2 halaman).
  Bonus: log alertd "+1 candle" hantu tiap tick hilang (re-fetch tersaring di klien).
- **`WriteCSV` atomik** (temp + `os.Rename`): pembaca konkuren (narrate/backtest saat alertd
  menulis) tak pernah lihat CSV terpotong. Cache alertd di-dedup one-off → 0 duplikat semua TF.
- Implikasi: dataset backtest kini TUMBUH terus + duplikat hilang → angka bisa bergeser antar-run;
  untuk eksperimen reproducible, snapshot dulu (`cp -RL data/XAU_USD /tmp/snap`).

**🔬 KUNCI#1 / FVGBREAK — RE-MEASUREMENT + SWEEP ADJACENCY (2026-06-03, WIP, BELUM di-lock):**
Task lama "bikin Kunci#1 selektif (dilusi PF3.00→1.75)" **DIUKUR ULANG di default sekarang — premis GUGUR.**
FVGBreak kini NET-AKRETIF (gate conf-cap/BB-FVG/ImpulseOnly/biasage yang di-lock belakangan sudah
bersihkan entry toksik). Breakdown 524tr default (`/tmp/k1_base.csv`, kolom poi_kinds/poi_tf/poi_confluence):
- **ada FVGBreak 279tr/PF2.05** > total 1.97 > tanpa-FVGBreak 245tr/PF1.88.
- **`1×FVGBreak` SENDIRI = bucket TERKUAT: 104tr/win46.2%/+74R/PF2.32** (kebalikan "wajib confluence"!).
- FVGBreak + confluence 175tr/PF1.90 (lebih rendah — konsisten "confluence terbalik"). Sub-jelek kecil:
  FVGBreak+BPR+FVG 8tr/PF0.43 (noise-tier), FVGBreak+BB+FVG 69tr/PF1.83.
→ "wajibkan confluence" JUSTRU akan buang bucket terbaik. TIDAK diimplementasi.
**Sweep `FVGSwingBreakAdjacency` (knob ada, config-only; default 3) atas snapshot `/tmp/snapdata`:**
adj = |fvg.Index − swing.Index| max, jarak FVG ke swing yg di-break.
| adj | IS tr/totR/PF/DD% | OOS(baseline wf) tr/totR/PF/DD% | OOS R/DD |
|-----|-------------------|----------------------------------|----------|
| 1   | 495/+278/1.97/6.8 | 405/+249/2.09/7.5 | 33.2 |
| 2   | 513/+287/1.97/8.1 | 416/+254/2.08/9.1 | 27.9 |
| 3*  | 524/+294/1.97/8.1 | 426/+258/2.07/9.2 | 28.0 (default; parity ✓ reproduksi baseline) |
| 4   | 526/+288/1.94/9.1 | (kalah IS) | |
| 5   | 527/+284/1.92/9.0 | (kalah IS) | |
| 6   | 531/+288/1.93/9.0 | (kalah IS) | |
- **Melonggarkan (adj 4–6) RUGI** (PF turun, DD naik) → jendela ≤3 = batas atas benar, JANGAN dilonggarkan.
- **adj=1** = risk-adjusted terbaik (OOS R/DD 33.2, PF2.09, DD7.5%) TAPI −9R OOS/−21tr → TIDAK strictly-dominant.
  Delta PF noise; satu-satunya sinyal konsisten = DD lebih bersih (IS 8.1→6.8, OOS 9.2→7.5). **User: simpan dulu, BELUM lock.**
- **🔭 RENCANA BESOK: bandingkan FVGBreak per-TF (H4 & D), bukan cuma agregat.** Preview cross-tab (adj=3):
  **H1 +FVGBreak PF2.42 vs tanpa 1.81 (FVGBreak menyelamatkan pool H1 yg lemah!)**; H4 1.66 vs 1.71 (netral);
  D 2.39 vs 2.33 (setara-kuat); W n kecil. → hipotesis: edge FVGBreak terkonsentrasi di H1; adjacency mungkin
  layak disetel PER-TF. Repro: `for n in 1..6: echo "fvg_swing_break_adjacency: $n">/tmp/adjN.yaml;
  go run ./cmd/backtest -data /tmp/snapdata -config /tmp/adjN.yaml` (+ walkforward utk OOS).
  ⚠️ snapshot `/tmp/snapdata` ephemeral — regen `cp -RL data /tmp/snapdata` sebelum mulai (cache alertd tumbuh).

**✅ IFVG FALLBACK RULE DI-LOCK (2026-06-04, keputusan user — IFVG "terlalu jauh"):**
Keluhan user: IFVG yang muncul di narasi/watchlist terlalu jauh dari harga. Pelacakan ke
**pertemuan 4 (David Dharmawan)** menemukan implementasi `detectIFVGs` melewatkan **syarat
FALLBACK** dari definisi asli: IFVG = POI **cadangan** — *"ketika tidak ada FVG searah, lo bisa
pakai IFVG"* (transcript p4; summary.md "IFVG Fallback Rule"). Implementasi lama memperlakukan
SETIAP FVG mati sebagai IFVG abadi (tanpa batas recency/jarak, dikecualikan dari `FilterLivePDRs`)
→ arsip semua FVG gagal sepanjang sejarah = sumber "terlalu jauh".
- **Knob `Config.IFVGRequireNoSameDirFVG` (default `true` 2026-06-04).** Saat true, IFVG arah D
  HANYA di-emit kalau di momen flip (candle konfirmasi penembusan) TIDAK ada FVG searah D yang
  masih live. Helper `liveSameDirFVGExists` (semantik close-tembus, konsisten pemicu IFVG).
  Di-thread lewat `DetectPDRs(...,ifvgRequireNoSameDirFVG)`; mirror engine↔narrate 0-drift.
  Loader `ifvg_require_no_same_dir_fvg` + config.yaml. Test `TestIFVG_FallbackRule` (paritas OFF +
  perilaku ON). `TestIFVG_Inversion` lama tetap lolos (panggil dgn `false`).
- **Backtest NETRAL** (snapshot `/tmp/snap`, paritas OFF = baseline persis 524/+294/PF1.97 IS,
  419/+250/PF2.05/DD8.9 OOS): **IS ON 522tr/+292R/PF1.97/DD17R; WF OOS ON 417tr/+248R/PF2.05/DD8.9%.**
  Δ = −2tr/−2R dua-duanya (noise; PF & DD identik). Sebabnya IFVG sudah dikecualikan dari clustering
  searah (keputusan 2026-06-01) → nyaris tak pernah jadi POI entry. **Nilai fallback = KEBENARAN
  definisi + bersihkan IFVG spurious di narasi/chart/confluence-breakdown**, bukan metrik.
- Bukan "lock karena OOS-dominan" (protokol normal) — ini **lock karena correctness-driven + backtest
  tak merugikan**, keputusan eksplisit user. Catatan perf: `liveSameDirFVGExists` O(F)/flip → walkforward
  melambat (~2:15 vs ~1m); alertd live (1× DetectPDRs/tick) tak terpengaruh. ⚠️ alertd live perlu
  rebuild+restart utk efek di watchlist.

**✅ OB versi-pertemuan-4 DIIMPLEMENTASI tapi TETAP OFF (2026-06-04, OOS-dominated):**
Audit user "OB di kode sesuai pertemuan 4 belum?" → **belum**. `detectOBs` lama cuma proxy permukaan
("candle berlawanan → 1 candle close menembus high/low", emit di SETIAP titik). Pertemuan 4 (David
Dharmawan): OB = komponen **REVERSAL** — pergerakan lawan-arah TERAKHIR sebelum reversal, dgn 3 syarat
yg hilang di proxy: (1) displacement impulsif **ber-FVG**, (2) **liquidity sweep** old swing sebelum
reversal (pembeda Stop Hunt vs OB — inti pelajaran), (3) emit candle TERAKHIR sblm kaki displacement.
- **`detectOBsStrict` + `obSweptLiquidity`** (pdarray.go): iterasi tiap FVG (bukti impulsif) → candle
  lawan-arah terakhir dalam ≤3 candle sblm kaki FVG (skeleton `detectBBs`) → syarat swing-low(bull)/
  swing-high(bear) terdekat sblm OB DI-SWEEP (wick) dalam [0..f.Index+1]. Zona full candle (konsisten
  BB & `pdrBroken`; warning "wick ekstrem" = soal SELEKSI candle, sudah dijawab syarat 2).
- **Knob `Config.OBStrict` (default `false`)** di-thread lewat `DetectPDRs(...,obStrict)`; true=strict,
  false=proxy lama. Loader `ob_strict` + config.yaml + flag `-obstrict`. Relevan hanya bila `DisableOB=false`.
  Mirror engine↔narrate 0-drift (4 call-site dapat `cfg.OBStrict` sama).
- **🐛 BONUS FIX DETERMINISME (`lessPDR`):** paritas knob-off awalnya GAGAL (523→525 trade) walau OB
  di-`dropOB`. Akar: `DetectPDRs` menyortir pool yg MASIH berisi OB pakai `sort.Slice` non-stable key
  `Bottom` saja, OB dibuang BELAKANGAN → ganti himpunan OB (proxy↔strict) menggeser tie non-OB → cluster
  `BuildPOIs` bergeser. Fix: total-order `lessPDR` (Bottom→Top→Kind→Dir→Index) di kedua sort (DetectPDRs
  + BuildPOIs). **Keputusan trade & P&L baseline IDENTIK** (522tr/+292R/PF1.97, key-cols diff kosong) —
  hanya LABEL POI/tier per-trade yg reorder (mis. `1×BB + 1×FVG`→`1×FVGBreak + 1×BB`). Setelah fix,
  paritas obstrict-off **byte-identik** (0-drift exact). Fix ini permanen (perbaikan kebenaran).
- **Hasil (snapshot `/tmp/snap_xau`):** baseline OB-OFF tetap TERBAIK.

  | varian | IS tr/win/totR/PF | WF OOS tr/totR/PF/DD% |
  |--------|-------------------|------------------------|
  | **baseline DisableOB=true** | **522/42.1%/+292/1.97** | **417/+248/2.05/8.9** |
  | OB proxy (obdisable=false) | 632/40.5%/+317/1.84 | (tak diuji WF) |
  | OB strict (ob_strict=true) | 603/39.6%/+280/1.77 | 476/+221/1.77/9.8 |

  OB-strict **didominasi baseline di SEMUA metrik OOS** (PF 2.05→1.77, totR −27R, DD 8.9→9.8%, per-fold
  fold3 PF 1.43 vs 1.77). Bahkan versi faithful-rules pun net-dilutif. **Per protokol: JANGAN lock.**
  `DisableOB=true` tetap default; `OBStrict` knob tersedia (default off) utk re-test sampel besar
  (`-from 2018`). Konfirmasi instinct user 2026-06-02 ("logika OB diragukan") — sekarang dgn bukti OB
  versi-BENAR juga tak ber-edge, bukan cuma proxy yg salah.

**✅ FVGBREAK PER-TF DI-LOCK — `FVGBreakTFs="H1,D"` (2026-06-04, keputusan user):**
Lanjutan WIP "FVGBreak per-TF" (rencana 2026-06-03). Re-measure cross-tab di state SEKARANG (522tr,
post-IFVG-fallback + post-`lessPDR`): **H1 +FB PF2.29 vs −FB 2.17** (akretif), **H4 +FB 1.63 vs −FB
1.72** (FVGBreak MERUGIKAN; H4 = TF terlemah 1.67), **D +FB 2.43 vs −FB 2.36** (netral), W n=3 (noise).
→ lever sebenarnya = **matikan promosi FVGBreak di H4**.
- **Knob `Config.FVGBreakTFs` (string allow-list CSV TF, mis. "H1,D"; kosong=semua TF=paritas).**
  Di `buildTFPOIs`, kalau TF di luar allow-list → `dropFVGBreak` (buang KindFVGBreak SEBELUM BuildPOIs;
  FVG dasar tetap Tier-4). Helper `fvgBreakAllowedTF` (case-insensitive, trim). Loader `fvg_break_tfs`.
  Mirror engine↔narrate 0-drift (logika di `buildTFPOIs` yg dipakai `fractalPOIs` kedua jalur). Test
  `TestDropFVGBreak` + `TestFVGBreakAllowedTF`. Paritas knob-kosong = baseline persis (522tr/PF1.97).
- **Sweep varian (snapshot `/tmp/snapdata`, IS + WF OOS aggregate):**

  | fvg_break_tfs | IS tr/R/PF/DD% | WF OOS tr/R/PF/DD% | OOS R/DD |
  |---|---|---|---|
  | "" baseline | 522/+292/1.97/8.1 | 417/+248/2.05/8.9 | 27.9 |
  | H1 | 500/+290/2.01/6.3 | 406/+250/2.09/6.2 | 40.3 |
  | **H1,D (LOCK)** | 502/+292/2.01/6.3 | 407/+249/2.08/6.2 | 40.2 |
  | H1,D,W (strip-H4-saja) | 506/+291/2.00/6.8 | 411/+248/2.06/6.8 | 36.5 |

- **Semua varian strip-H4 menang OOS** (DD 8.9%→6.2-6.8%, PF↑, R terjaga, per-fold 3 naik/1 flat) =
  **strictly-dominant** (R↑/PF↑/DD↓ bareng). Pilih **H1,D**: principled (pertahankan FVGBreak di TF
  non-merugikan H1+D, buang merugikan/noise H4+W), jaga R IS penuh (+292=baseline). H1 vs H1,D praktis
  seri → D dikonfirmasi netral. **Per-TF adjacency (H4/D=adj1) TIDAK dikejar** — allow-list sederhana
  sudah tangkap ~seluruh gain (DD 8.9→6.2); adjacency per-TF = kompleksitas + overfit-risk 1-rezim.
- Baseline default BERGESER: IS 522→**502tr/+292R/PF2.01/DD13R(6.3%)**; WF OOS 417→**407tr/+249R/PF2.08/DD6.2%**.
  ⚠️ alertd live perlu rebuild+restart utk efek di watchlist/seleksi POI HTF.

**✅ SWEEP `FVGSwingBreakAdjacency` — LOCK adj=12 (2026-06-04, keputusan user; baseline H1,D).**
Sweep adj 2026-06-03 (DECISIONS:309) dilakukan PRA-`FVGBreakTFs` (baseline lama 524/OOS426). Diulang di atas
baseline ter-lock H1,D (502/OOS407) atas snapshot `/tmp/snapdir` (data 2022-01-02→2026-06-04, repro: `for a in
1 2 4 5 8 10 12 15 20 30 40 50: sed 's/^fvg_swing_break_adjacency: 3/...: $a/' config.yaml >/tmp/cfg_adjN.yaml;
go run ./cmd/walkforward -data /tmp/snapdir -config /tmp/cfg_adjN.yaml`). Baseline adj=3 reproduksi PERSIS
(AGGREGATE OOS 407/+249/2.08/6.2). Pertama dibatasi adj 1–5 (kesimpulan awal "tak ada yg dominan, default=3"),
lalu **diperluas 8→50 atas permintaan user → menemukan PUNCAK di adj=12** yg tak terlihat di rentang sempit.

| adj  | IS tr/R/PF/DD%     | AGGREGATE OOS tr/R/PF/DD% | BASELINE OOS tr/R/PF/DD% |
|------|--------------------|----------------------------|---------------------------|
| 1    | 484/+283/2.02/6.2  | 392/+240/2.09/**7.1**      | 401/+247/2.09/6.9 |
| 2    | 491/+285/2.01/6.3  | 391/+243/2.10/6.2          | 405/+252/2.11/7.0 |
| 3 (lama default) | 502/+292/2.01/6.3 | 407/+249/2.08/6.2 | 415/+256/2.09/7.1 (parity ✓) |
| 4    | 504/+290/2.00/6.3  | 409/+247/2.06/7.1          | 417/+254/2.08/7.1 |
| 5    | 504/+290/2.00/6.3  | 411/+249/2.07/6.2          | 419/+256/2.08/7.1 |
| 8    | 510/+293/—/6.3     | 414/+251/2.07/6.2          | 422/+258/2.08/7.1 |
| 10   | 511/+296/—/6.3     | 415/+254/2.08/6.2          | 423/+261/2.09/7.1 |
| **12 (LOCK)** | 513/**+298**/2.01/6.3 | 417/**+256**/2.08/6.2 | 425/**+263**/**2.10**/7.1 |
| 15   | 516/+295/—/6.3     | 420/+253/2.06/6.2          | 428/+260/2.07/7.1 |
| 20   | 516/+295/—/6.3     | 420/+253/2.06/6.2          | 428/+260/2.07/7.1 |
| 30/40/50 | 516/+295/—/6.3 | 411/+250/2.07/6.3          | 428/+260/2.07/7.1 |

- **Kurva non-monoton dengan PUNCAK TEGAS di adj=12** — IS, AGGREGATE OOS, BASELINE OOS **ketiganya** memuncak
  di 12 (koherensi IS↔OOS = mengurangi probabilitas noise murni). Naik landai 3→12, turun tipis 12→15, lalu
  **DATAR SEMPURNA 15→50 (saturasi detektor:** tak ada FVG sejauh itu yg belum tertangkap; 30/40/50 byte-identik 15/20).
- **adj=12 vs lama-default 3:** +6R IS / +7R OOS, **avgR identik** (0.581 vs 0.582 → kualitas per-trade sama),
  **PF datar** (OOS 2.08→2.08 AGG, 2.09→2.10 BASE), **DD identik** (6.3% IS / 6.2% AGG / 7.1% BASE). Bukan
  *strictly*-dominan (PF/DD datar) → ini "volume-lebih-banyak-pada-kualitas-&-risiko-sama", +7R (~3%).
- **Caveat faithfulness (jujur):** adj=12 = FVG sampai 12 candle (D = 12 hari) dari swing-break masih dipromosi
  Tier-2 → link kausal Kunci#1 longgar. **User memilih LOCK adj=12** (ambil +7R). Internal fallback `detectFVGSwingBreaks`
  (adj≤0) ikut diubah 3→12; komentar field + config.yaml disinkronkan. **Re-cek wajib saat sampel -from 2018**
  (puncak 12 bisa bergeser/lenyap di rezim lain — semua angka 1-rezim-bull).
- **Koreksi catatan 2026-06-03 (DECISIONS:320):** "adj=1 risk-adj terbaik" = **artefak IS** (di OOS DD adj=1 JUSTRU
  naik 6.2→7.1%); dan rentang sempit 1–5 menyembunyikan puncak sebenarnya di 12.
- Baseline default BERGESER: IS 502→**513tr/+298R/PF2.01/DD6.3%**; WF OOS AGGREGATE 407→**417tr/+256R/PF2.08/DD6.2%**.
  ⚠️ alertd live perlu rebuild+restart utk efek di watchlist/seleksi POI HTF.

**✅ BPR DIRECTIONAL DI-LOCK (2026-06-04, correctness-driven backtest-NETRAL — pola IFVG):**
Audit user "BPR sesuai pertemuan 4 belum?" → **deteksi ✓, pemakaian ✗**. BPR = POI terlemah
(breakdown PF1.16). Transkrip p4 (tr.1475–1733): BPR itu **DIRECTIONAL** (*"dari BPR kita bisa
menentukan arah market"*), edge via **HOLD/BREAK** (hit&respect→lanjut; hit&break→reverse). Kode
`detectBPRs` meng-emit BPR **DUA ARAH** (Bullish+Bearish zona sama, "netral") → confluence padding.
- **Knob `Config.BPRDirectional` (LOCK default `true`).** Saat true, `detectBPRs` emit SATU PDR arah =
  FVG **lebih baru** (Index lebih besar = imbalance terakhir yg menyeimbangkan; konvensi ICT). Di-thread
  `DetectPDRs(...,bprDirectional)` (signature jadi 8-arg); mirror engine↔narrate 0-drift. Loader
  `bpr_directional` + config.yaml + flag `-bprdir`. Test `TestBPR_Directional` (1-arah, no padding);
  `TestBPR_Overlap` lama tetap (panggil `false`).
- **Backtest IDENTIK TOTAL** (snapshot `/tmp/snap_xau`): IS false vs true = **502tr/42.6%/+292R/PF2.01**
  byte-sama (keputusan trade + label `poi_kinds` + 29 trade ber-BPR semua sama); WF OOS keduanya
  **407tr/+249R/PF2.08/DD6.2%** identik. **Sebab:** BPR yg BENAR-BENAR ter-trade ternyata SELALU sudah
  directional (arah FVG-baru = arah bias, krn ImpulseOnly + gate zona). Emisi dua-arah cuma **phantom di
  pool yg tak pernah menang seleksi** → menghapusnya nol-efek pada trade.
- **Implikasi jujur: directional TIDAK menyembuhkan PF lemah BPR (1.16).** Kelemahan BPR bukan dari
  padding di cluster ter-trade (itu sudah directional), tapi inheren ke setup ber-BPR. Lock ini
  **correctness-driven** (faithful p4 + bersihkan phantom BPR arah-salah dari watchlist/narasi/chart),
  bukan perbaikan metrik — sama persis pola IFVG Fallback. **HOLD/BREAK penuh (break→reverse entry) di
  luar scope** (bentrok ImpulseOnly) — dicatat sbg limitation.
- **Follow-up bila mau betulkan PF lemah BPR:** knob `DisableBPR` (buang BPR sama sekali, pola DisableOB)
  — BPR PF1.16, mungkin lebih baik dibuang; BELUM diuji.

**🔬 BPR ZONA "FVG-TERAKHIR-PENUH" DIUJI → REVERT ke IRISAN (2026-06-04, keputusan user):**
Koreksi user: zona BPR yg valid = **full range FVG terakhir yg membalas** ("BISI dibalas SIBI →
SIBI penuh = zona aktif"), BUKAN irisan. Diimplementasi di branch directional `detectBPRs`
(Top/Bottom = recent.Top/Bottom; overlap tetap syarat kualifikasi). Diuji + knob `DisableBPR`
ditambah (dropBPR pola dropOB; loader `disable_bpr` + flag `-bprdisable`).
- **Zona-full MERUGIKAN** (snapshot `/tmp/snap_xau`): BPR bucket PF **1.16→0.58** (n38, −13R, win18%);
  IS 502/2.01→**507/1.93**; WF OOS 407/+249/2.08/DD6.2 → **416/+228/1.94/DD8.9**. Zona melebar → 9 trade
  ber-BPR ekstra kualitas buruk.
- **`DisableBPR=true` (buang BPR) JUGA lebih buruk dari baseline:** IS 511/1.94, WF OOS **415/+233/1.97/
  DD8.4** (< 2.08). Membuang BPR malah **menambah** trade (507→511) → **pola "gate substitusi"**: BPR
  (zona-irisan) ternyata **net-AKRETIF lewat seleksi cluster** (baseline tanpa-BPR 1.97 < dgn-BPR 2.08)
  walau bucket-PF-nya lemah 1.16. Bucket-PF menyesatkan; net-effect vs no-BPR yg menentukan.
- **Temuan kunci:** PF 2.08 lama **bergantung pada zona irisan sempit**. Zona-full (lebih "textbook")
  menghancurkan edge BPR yg rapuh (sampel ~30, 1 rezim). **User pilih REVERT ke irisan** (argumen:
  overlap = "balanced region" juga tafsir ICT yg sah) → **pertahankan PF2.08/DD6.2 (lock tak bergeser)**.
- **State akhir:** `BPRDirectional=true` (LOCK, zona IRISAN), `DisableBPR=false` (BPR aktif — diuji,
  buang lebih buruk). Knob `bpr_directional`/`disable_bpr`/`-bprdir`/`-bprdisable` tersedia utk re-test
  (mis. saat sampel diperbesar `-from 2018` — edge BPR rapuh, layak re-evaluasi).
- **Display watchlist (klarifikasi user 2026-06-04):** filter FVGBreakTFs hanya di jalur SELEKSI POI
  (`buildTFPOIs`→`nextPOIsByTF`/NextPOIsByTF) — di H4/W zona dipilih sbg Tier-4 FVG. Seksi "Komponen
  Array per-TF" (`liveTFPDRMarks`) SENGAJA TAK ikut filter → tetap label "FVGBreak" di semua TF
  (struktur mentah, informasional). Zona TIDAK pernah hilang (FVG dasar selalu di-emit). Label array
  (FVGBreak) bisa beda dgn tier seleksi (Tier-4) di H4 = disengaja, bukan drift sinyal. Komentar di
  `liveTFPDRMarks` mencatat ini ("JANGAN perbaiki tanpa konfirmasi").

**🔬 DETEKTOR ASIA A/X RASIO vs ATR (2026-06-04, permintaan user — audit pertemuan 8 & 10 vs kode):**
- **Audit rules:** Pertemuan 8 = model AMD klasik (Asia SELALU A, belum ada X). Pertemuan 10 perkenalkan
  A/X: penentu = **true move = range open-to-close** sesi Asia (kecil→A/AMDX, ekspansif→X/XAMD).
  Instruktur EKSPLISIT: "bukan rumus matematis", angka "expensive" **tak pernah didefinisikan numerik**;
  satu-satunya kuantifikasi tentatif (checklist) = `|close−open|/range_sesi < 0.3` → A. Kode (`ClassifyDaily`,
  D.2) setia 100% ke rules-file: XAMD iff (FVG 1H) AND (`|close−open| ≥ 1.5×ATR_1H`). **2 operasionalisasi
  di rules-file BUKAN literal pelajaran:** (a) normalisasi pakai ATR (bukan rasio-thd-range-sesi), (b) FVG
  sbg gerbang AND. User minta eksperimen detektor rasio versi pertemuan 10.
- **Implementasi (knob, parity-safe):** `ClassifyDailyEx(...,rangeRatio,mode)` + wrapper `ClassifyDaily`=mode"atr"
  (caller lama 0-drift). Mode "ratio": expansive iff `|close−open|/candleRange(asia) ≥ rangeRatio`; FVG-AND tetap.
  Knob `AsiaAXMode` (default "atr") + `AsiaRangeRatio` (default 0.5); config `asia_ax_mode`/`asia_range_ratio`.
  Diwire ke 4 site live (engine evaluate, narrate ×2, alertd q1Scenario M15-fractal). `ClassifyWeekly` dapat
  perlakuan Ex sama (tapi weekly phase live diturunkan dari scenario daily via `WeeklyPhase`, jadi ikut otomatis).
  `cmd/qtscan` di-extend: `-mode atr|ratio|both` + `-ratio` → tabel hari yang BEDA.
- **Caveat menentukan — A/X BUKAN gate P&L** (`qt_phase_gate=false`; DECISIONS:193 gating phase merugikan).
  Verifikasi: backtest default (atr) = baseline 502tr/+292R/PF2.01/DD13R(6.3%); **ratio gate-OFF = P&L IDENTIK
  502/+292/PF2.01** (hanya tag amdx/xamd bergeser: atr 389/113 → ratio 359/143). Walkforward OOS default =
  407tr/+249R/PF2.08/DD6.2% (parity utuh). → **mengganti detektor = 0 dampak P&L di default.**
- **Label comparison (qtscan, snapshot 1140 hari):** atr 17.8% X vs ratio@0.5 28.6% X, sepakat 88.2%. Sweep
  ambang: T=0.3→41%X, 0.5→29%, 0.7→14%(≈atr). Ratio menangkap kasus "range besar tapi true-move kecil =
  akumulasi" yg atr salah-vonis X (mis. 2022-09-05: range16.16/move7.16/ratio0.44→A, atr→X krn move>thr5.22)
  — ini PERSIS poin pertemuan 10 "jangan terkecoh candle individual besar".
- **Gated comparison (qt_phase_gate=true, IS):** atr 225tr/+124R/PF1.95/DD4.7% vs **ratio 239tr/+112R/PF1.78/DD7%**
  → sebagai GATE ratio LEBIH BURUK dari atr; keduanya < no-gate PF2.01. Trade ber-tag xamd PF>amdx di kedua
  detektor (2.5 vs 1.8) → konsisten kenapa gating-phase merugikan (subset X justru profitable).
- **VERDICT (detektor) — TIDAK lock, default tetap `atr`.** Bukan OOS-dominan: 0 dampak P&L di default, lebih
  buruk sbg gate. Nilai ratio MURNI faithfulness-label (narasi/alert). Knob tetap ada utk eksperimen; bila kelak
  dipakai sbg label, T≈0.65–0.7 lebih cocok dgn framing instruktur "<0.3=A" tanpa over-flag.
- **TOGGLE SYARAT FVG (knob `AsiaRequireFVG`, default `true`; lanjutan — user "kalau syarat FVG dibuang?"):**
  FVG-AND = operasionalisasi rules-file D.2, BUKAN literal pertemuan 10 (instruktur cuma tekankan true-move).
  `requireFVG=false` → X jadi penentu MURNI true-move. `ClassifyDailyEx(...,mode,requireFVG)`; config
  `asia_require_fvg` + qtscan `-fvg`. Verifikasi:
  - **Default gate-OFF FVG-off = P&L IDENTIK** (502/+292/PF2.01); tag bergeser (atr-mode xamd 113→212).
  - **Label (snapshot):** FVG-off menaikkan X — atr 17.8%→21.2% (+3.4pp), **ratio 28.6%→43.5%** (X hampir
    separuh hari = over-flag). Sepakat atr↔ratio turun 88.2%→76.7%.
  - **Asia hari ini (2026-06-04, live):** FVG-on = A (tak ada FVG); **FVG-off = X di KEDUA mode** (atr move
    31.95 ≥ thrATR 29.48; ratio 0.60 ≥ 0.50). Gerakan Asia memang meaningful, cuma tak tinggalkan imbalance.
  - **Gated (qt_phase_gate=true, IS):** atr+FVGoff 226tr/+130R/PF2.01/DD4.4% (marginal > atr+FVGon 1.95);
    ratio+FVGoff 244tr/+121R/PF1.84/DD6.5%. **TETAP < no-gate +292R/PF2.01** — buang FVG tak membuka edge.
  - **VERDICT (FVG) — user MEMUTUSKAN default `false` (2026-06-04, "fvg dimatikan saja, jadi note tambahan").**
    Aman dilakukan krn 0 dampak P&L (A/X non-gating; head-to-head FVG on==off IDENTIK, dikonfirmasi ulang di
    baseline kini 513tr/+298R/PF2.01 — baseline geser 502→513 krn lock `FVGSwingBreakAdjacency=12` paralel,
    BUKAN efek FVG). Detektor TETAP `atr`. FVG turun jadi **catatan informatif**: `ScanNarrative.AsiaHasFVG` +
    `FormatAsiaLine` cetak "Asia: X … — catatan: TANPA FVG 1H" saat X tanpa FVG. Asia hari ini (2026-06-04) →
    **X (TANPA FVG 1H)**. Knob `asia_require_fvg=true` = pulihkan D.2 lama. qtscan `-fvg` default = mirror engine.
    Catatan: alertd Q1 (M15 fractal) ikut FVG-off → vonis X lebih sering; over-flag ratio (43.5%) tak relevan
    krn detektor=atr (+3.4pp saja).
- Re-evaluasi semua saat sampel diperbesar (`-from 2018`).

---

**KARAKTER FASE MANIPULASI — diagnostik `cmd/dayscan` (2026-06-07, jawab pertanyaan user):**
Tool baru read-only `cmd/dayscan` mengukur 3 pertanyaan empiris model AMD di DUA frame (intraday-sesi
+ weekly day-of-week), arah sweep ditentukan OF weekly (point-in-time, anti-lookahead). Bukan rule/gate —
murni observasional. Flag: `-frame intraday|weekly|both`, `-scenario amdx|xamd|all`, `-ny-poke-eps`, `-from/-to`, `-all`.
- **(1) Manipulasi SELALU sweep akumulasi? TIDAK — tergantung sisi.** Intraday (London sweep Asia):
  `sweptAny` (sisi mana pun) **92.6%**, tapi `sweptOF` (sisi sesuai OF: low saat bull / high saat bear) cuma
  **53.2%**. Weekly (hari-M sweep hari-A): sweptAny 89.6%, sweptOF 45.2%. → "menyenggol range" hampir selalu
  (9/10), tapi liquidity-grab TERARAH sesuai OF = koin (~53%, rules memang tak mewajibkan). Intraday AMDX
  sweptOF=52.0% **cocok persis memori `q1sweep` "Asia-di-London ~52%"** (validasi silang logika).
- **(2) Kedalaman (intraday, ATR_1H, %=lebar range akum):** OVERSHOOT saat sweep (n=597) median **0.87×ATR
  (≈49% range)**, p75 1.63× → poke moderat, bukan tembus jauh (= catatan mentor "ambil low terdekat aja").
  PENETRASI saat TIDAK sweep (n=525) median **66%** masuk range sebelum balik → tetap menusuk dalam meski
  tak grab likuiditas. Weekly mirip (overshoot 0.35×ATR_D/40% range, penetrasi 66%).
- **(3) Distribusi NY-AM (AMDX) searah OF atau berlawanan dulu?** Close searah-OF **53.1%** intraday (nyaris
  koin; 1-sesi-6jam berisik) vs **62.1% di weekly hari-D** (komitmen arah lebih tegas di level weekly).
  "Poke kontra-OF dulu": 50.6% pada eps=0 = ARTEFAK (tiap candle naik pasti turun setitik di bawah open);
  syaratkan poke nyata ≥0.25×ATR → runtuh **5.7%**, ≥0.5×ATR → 1.5%. → **NY-AM langsung distribusi searah OF
  TANPA judas berarti di pembukaannya** — manipulasi/sweep-nya sudah dibayar di sesi London (fase M). Koheren
  dgn struktur AMD: M=London=judas, D=NY=clean move.
- **Sanity:** AMDX/XAMD split dayscan 881/241 ≈ q1sweep 887/242 ≈ qtscan 898/243. Anti-lookahead OK (19 hari
  awal ter-skip `of`, OF weekly belum seed). Bounds OK (pen ∈[0,100], overshoot≥0). **Caveat: 1-rezim-bull,
  OF-Bearish kecil-n, poke-first weekly = proxy upper-bound (urutan intra-hari tak teramati di D). Re-run
  setelah `-from 2018`.**

---

## News alert CPI/PPI/NFP (`internal/news` + `cmd/newsalert`) — POC (A) DONE 2026-06-08

**Konteks:** user minta scheduler saat rilis CPI + ide connect API news utk "prediksi arah". Disepakati
(AskUserQuestion): **A (POC one-shot CPI) lalu B (news-aware permanen di alertd)**; ambisi prediksi =
**reaksi instan + ancang-ancang pre-release** (BUKAN forecasting angka aktual — disepakati tidak reliable).
Scheduling LOKAL/AWS (bukan claude.ai /schedule — preferensi user).

**Sumber data:** Forex Factory weekly JSON mirror `nfs.faireconomy.media/ff_calendar_thisweek.json` —
gratis tanpa key, std-lib `net/http`+`json` (sesuai konvensi repo). Field: title/country/date(RFC3339+offset)/
impact/forecast/previous/actual (`actual`="" sebelum rilis → pembeda pra/pasca). Dipilih di atas
Finnhub/FMP/Trading Economics (butuh key/berbayar). ⚠️ mirror tak-resmi → **rate-limit (HTTP 429)** bila
di-poll rapat (<1m); tiap 5m aman. Mitigasi: **cache body terakhir** (`news_feed_cache.json`) → fallback
saat live gagal (`FetchCalendarCached`, `fromCache` di-log). Diuji: live 429 → fallback cache → render OK.

**Klasifikasi → bias (rezim 2026):** `KindInflation` (CPI/PPI) actual>forecast=PANAS→bearish gold,
<forecast=LEMAH→bullish, =→netral; `KindJobs` (NFP) beat→bearish, miss→bullish. Asumsi rezim hawkish
(gold sensitif Fed/yield) di-EMBED ke pesan (bukan disembunyikan). **Anchor narasi inflasi = headline y/y**
(angka yang dikutip pasar) bukan m/m — fix penting: CPI Mei m/m fc 0,3%<prev 0,6% (turun, base-effect)
TAPI y/y fc 4,2%>3,8% (NAIK, hawkish); anchor m/m bikin narasi "mereda" menyesatkan → tambah
`divergenceNote` catatan base-effect. `ParseNumber` tangani %/K/M/B/koma/`<`.

**Pesan:** `BuildPreMessage` (forecast vs previous + arah ekspektasi + nowcast-manual + level kunci
$4.350–4.500 + "tunggu 15–30m") & `BuildPostMessage` (actual vs forecast per-event + surprise + bias +
playbook). Dedup `news.State` (pre/post per release key `CPI@<RFC3339>`). Countdown humanized; waktu
ditampilkan WIB+ET. Reuse `notify.Telegram`. Flag: `-event CPI,PPI,NFP` `-dry` `-simulate "Judul=nilai,…"`
(preview pasca-rilis, paksa dry) `-once`/`-interval` `-prewindow 60m` `-stale 12h` `-nowcast` `-feedcache`.

**Konsensus live (8 Jun, dari feed):** CPI y/y fc **4,2%** (prev 3,8% — lebih hawkish dari estimasi report
~3,8–4,0%), CPI m/m 0,3% (prev 0,6%), Core m/m 0,5%; PPI m/m 0,7% (prev 1,4%), Core PPI 0,5%.

**Deploy:** `deploy/forex-newsalert.{service,timer}` (systemd oneshot + OnCalendar `*:0/5`, `Persistent`,
DST-proof dari timestamp feed) + `deploy/README-newsalert.md` (cross-compile arm64 → /opt/forex, Mac cron
alt). Build/vet/test seluruh repo hijau; unit test `internal/news` (parse/filter/ParseNumber/group/headline/
bias inflasi+jobs/state dedup).

**B DONE 2026-06-08 (integrasi alertd):** logika keputusan `decide`/`pickTarget`/`parseNames` DIPROMOSIKAN
dari package `main cmd/newsalert` ke `internal/news` sebagai `Decide`/`PickTarget`/`ParseNames` (exported,
VERBATIM — prinsip 0-drift repo, satu sumber keputusan). `cmd/newsalert` kini memanggilnya (perilaku identik;
sanity `-dry -prewindow 96h` render sama). `alertd` dapat flag `-news` (bool, **default false = PARITAS**:
maybeSendNewsAlerts TIDAK PERNAH dipanggil → perilaku alertd lama persis, tak ada fetch feed / pesan tambahan /
file state baru) + `-news-events` (CPI,PPI,NFP) `-news-prewindow` (60m) `-news-stale` (12h) `-news-nowcast`
`-news-state` (`data/news_state.json`) `-news-feedcache` (`data/news_feed_cache.json`) `-news-support`.
`(*daemon).maybeSendNewsAlerts(now)` dipanggil di **tick** (bukan di dalam runOnce) — news independen OANDA,
jadi tetap jalan walau `engine.Run`/cache bermasalah (runOnce bisa return awal); terbungkus recover tick yang
sama; error fetch (429/down) di-log & return, tak crash. **Dedup `news_state.json` TERPISAH** dari
`alert_state.json` alertd (tak ada interferensi). `newsHTTP` (timeout 20s) dibuat sekali. Unit test
`internal/news/decide_test.go` (PRE/POST/NONE + PickTarget terdekat & buang-stale + ParseNames). Build/vet/
test repo hijau, gofmt clean. Trade-off vs standalone timer: 1 daemon (tak perlu systemd-timer terpisah);
timer tetap tersedia (`-news=false` = default, tak ganggu). (2) nowcast leading OTOMATIS: Cleveland Fed (CPI) WebFetch 403 →
butuh client UA-browser/endpoint data; ADP (NFP). Sementara manual `-nowcast`. (3) verifikasi pasca-rilis
NYATA Rabu 10 Jun: cek feed isi `actual` & pesan post terkirim benar. (4) seed cache produksi (1× fetch sukses).

---

## FIX QT Mingguan + Alert AMS ITH/ITL (2026-06-08, koreksi user + permintaan)

**✅ KOREKSI QT MINGGUAN — pakai detektor weekly yang benar (Senin/4H/D.4):**
User lapor "QT Mingguan: Sen=X" pagi (Senin) lalu berubah jadi "A" — masih Senin, belum close.
- **Akar masalah:** baris "QT Mingguan" (`ScanNarrative.QTSkenario`, `narrate.go`) keliru pakai
  detektor **HARIAN** `ClassifyDailyEx(asiaH1, atr1h, …)` (sesi Asia 18:00–00:00 NY, ATR **1H**),
  dihitung **ulang tiap hari**, tak tunggu Senin close. Saat Asia belum close `asiaSlice` balikin
  **`atr1h=0`** (`engine.go:asiaSlice`) → di mode "atr" `expansive = directional ≥ 1.5×0 = ≥0` →
  **`true` selalu → X dipaksa**; setelah Asia close (00:00 NY) ATR nyata → sering A. Itu flip-nya.
  Inkonsistensi: baris "Asia: A/X" (`QTDailyScenario`) SUDAH di-gate `if AsiaClosed`, "QT Mingguan" tidak.
- **Rules D.4** (BACKTEST_RULES.md:324-347): weekly QT = klasifikasi candle **SENIN** view **4H**,
  dinilai **di penutupan Senin**, **sekali untuk seminggu**. "Senin" (anchor 18:00) = **Senin 18:00 →
  Selasa 18:00 NY** → final **Selasa 18:00 NY** (user KONFIRMASI 2026-06-08: "betul, Senin close di
  Selasa 18:00"). XAMD (AND): (1) FVG 4H; (2) `|close−open|_Senin ≥ 1.5×ATR_4H`.
- **Fix:** helper baru `classifyWeeklyQT` (narrate.go, mirror `classifyMonthlyQT`): window Senin via
  `detectors.TradingDayStart` (anchor 18:00) → kumpul H4 di `[Sen 18:00, Sel 18:00)` → `detectors.
  ClassifyWeekly(monday4h, atr4h, MinAtrMult, MinGapPips)` (sudah ada sejak dayscan; mode "atr"+FVG
  wajib). Final hanya `now ≥ Selasa 18:00` → **dikunci** (window Senin tetap sepanjang minggu).
  Sebelum itu `QTWeeklyPending=true` → formatter tampil **"(menunggu Senin close)"** (BUKAN X-palsu).
  `QTSkenario`/`QTWeeklyPhase` kini dari weekly scenario; **skenario HARIAN (`scenario` dari asiaH1)
  TETAP** untuk `dailyPhase` (D.2 — tak diubah). Konflasi weekly=daily teratasi (terbukti: skenario
  harian berubah amdx/xamd Sel–Kam sementara weekly tetap AMDX, terkunci).
- **Verifikasi:** narrate Sen 12:00/20:00 NY → "(menunggu Senin close)"; Sel 20:00 → "Sen=A → AMDX"
  (final). Silang-cek `cmd/dayscan -frame weekly` minggu Senin 1-Jun = AMDX → COCOK.
- Engine `s.weeklyPhase` (engine.go) **TIDAK diubah** (masih dari daily scenario): `WeeklyPhaseGate`
  default OFF → tak gating, hanya `Signal.WeeklyPhase` (breakdown report). Mengubahnya akan menggeser
  bucket A/M/D/X breakdown tanpa nilai → sengaja dilewati (bukan inti permintaan user; display narrate
  sudah benar). **P&L IDENTIK baseline** (514tr/PF2.02/+301R/DD6.3%; semua gate QT/weekly OFF, narrate
  bukan jalur Run).

**✅ ALERT AMS ITH/ITL (permintaan user — penting per Pertemuan 6, "titik perubahan order flow"):**
- Dulu `watchlistFingerprint` tak memuat AMS → perubahan ITL/ITH tak pernah memicu Telegram (cuma
  tampil pasif di seksi AMS). Keputusan user: alert saat **TERBENTUK + BREAK**, **dua arah** (ITL &
  ITH walau lawan bias), **gabung watchlist diff**.
- **Fix:** `ScanNarrative.AMSITL/AMSITH` (`AMSStruct{Present,Pivot,PivotTime,Active,Type}`) dihitung
  dua arah di Step 3 AMS via `ActiveIntermediate(window 1H sama, ITLow/ITHigh, BreakType, minLeg=0)`
  — minLeg=0 IDENTIK gate AMS (set yang dialert = yang dipertimbangkan gate). `amsWatchLine` tampil
  dua arah (`ITL @ X (broken) · ITH @ Y (aktif) ◀bias`). `cmd/alertd`: `amsTokens` (fingerprint
  `AMS|kind|pivot|pivotTimeUnix|active`), `parseWatchlistFP` (+map ams; case `AMS` SEBELUM case zona
  — sama-sama 5 field), `amsFPChanged` (TERBENTUK=pivotTime baru, BREAK=aktif→broken; "hilang"
  diabaikan) di `watchlistTrigger`, baris diff di `watchlistDiff`, dan branch `case changed:` kirim
  "⚠️ AMS BERUBAH" walau tanpa zona POI. Golden test `TestFormatWatchlistSections` diupdate.
- ⚠️ Artefak sekali saat ganti binary: tick pertama tandai AMS "baru terbentuk" karena state lama
  (`alert_state.json`) belum punya token AMS → oldAMS kosong; self-correct tick berikutnya.

**Deploy:** cross-compile arm64 → scp → `systemctl restart forex-alertd` di AWS EC2 (2026-06-08,
[[deploy-alertd-azure-plan]]). Binary lama di-backup `/opt/forex/alertd.bak-20260608`. Tick pertama
binary baru sehat (watchlist terkirim, Q1 close terkirim, tanpa panic). Build/vet/`go test ./...` hijau.

## ✅ BB Require Displacement DI-LOCK (2026-06-09, keputusan user — correctness, P&L-neutral)

**Lubang yang ditemukan user (dari watchlist BB 4317.59–4347.10 yang janggal):** `detectBBs`
men-sah-kan BB cuma dgn syarat "ada FVG searah dalam ≤3 candle" (`bb_fvg_adjacency_max_candles`),
**tanpa peduli ukuran/posisi FVG**. Akibatnya FVG mungil yang gerakannya muter di area candle BB
sendiri tetap melahirkan BB. Rules F.1 (`BACKTEST_RULES.md:449-452`) sebenarnya bilang BB sah hanya
kalau retracement "langsung diikuti **impulsive move (ber-FVG)**" — "keberadaan FVG = bukti move
impulsive". Tafsir lama menerjemahkan itu terlalu longgar.

**Knob `bb_require_displacement` (engine `BBRequireDisplacement`):** BB sah hanya kalau FVG penyah =
**displacement yang KELUAR dari range candle BB** (klarifikasi user: "FVG-nya harus displacement yg
keluar dari range BB … yg jelas harus impulsive"). Kondisi di `detectBBs`:
- **bullish:** `FVG.Top > BB.High` (gap menembus/di atas range candle BB)
- **bearish:** `FVG.Bottom < BB.Low`

Gap yang nyangkut total di dalam `[BB.Low, BB.High]` → bukan impulsive → retracement itu bukan BB.
Satu kondisi ini menangkup dua tafsir user ("menembus keluar" & "di atasnya BB") sekaligus.

**Ukur (snapshot `/tmp/snap_bb`, data 2022-01-02→2026-06-07; OFF→ON):**
| | IS tr | IS R | IS PF | IS DD | OOS-baseline tr | OOS R | OOS PF | OOS DD | OOS-agg |
|---|---|---|---|---|---|---|---|---|---|
| OFF | 514 | +301 | 2.02 | 6.3% | 426 | +266 | 2.11 | 7.1% | 418/+259/2.10/6.2% |
| ON | 509 | +296 | 2.01 | 6.3% | 423 | +263 | 2.10 | 7.0% | 415/+256/2.09/6.2% |

**Verdict: WASH (BUKAN OOS-dominan).** Filter cuma menyentuh 3–5 trade di seluruh dataset; R −3
(OOS), PF flat (−0.01), DD −0.1pp (noise). **Tidak lolos ambang protokol "lock hanya bila dominan".**
User MEMUTUSKAN lock ON atas dasar **correctness/faithfulness** (rules butuh displacement impulsif),
biaya P&L dapat diabaikan (PF OOS tetap >2). Mirror engine↔narrate 0-drift (4 call site `DetectPDRs`
+ `detectBBs` signature). Default-config kini ON (509 trade tanpa `-config`). Build/vet/`go test ./...` hijau.

**Re-eval saat `-from 2018`:** efek 3–5 trade di 1-rezim-bull terlalu kecil utk pisahkan correctness
vs noise; verifikasi ulang puncak/dominansi di rezim lain (bareng re-eval BPR & adj=12).

## ✅ Koreksi QT Mingguan (candle-Senin off-by-one) + QT Session resolusi (2026-06-09, keputusan user)

Pemicu: user tanya kenapa watchlist live (Senin 8 Jun 20:05 EDT) masih "QT Mingguan menunggu Senin
close" & "QT Session Q1 Asia berjalan" padahal harusnya sudah lewat. Diagnosa dari kode+data VM:

**(1) QT Mingguan off-by-one candle Senin.** Fix 2026-06-08 men-set `mondayStart = sundayStart + 1 hari`
= **buka Senin 18:00 → tutup Selasa 18:00 NY**. Tapi di konvensi NY-close (anchor 18:00, ICT) candle yg
menampung **sesi hari Senin** (Asia/London/NY-AM Senin) adalah yg **buka Minggu 18:00 → tutup Senin 18:00**
(di data: `06-07T22:00Z`, O4321.55/H4353.45/L4268.48/C4330.07, sudah close Senin 18:00 EDT). Candle yg
dipakai kode lama justru menampung price action **Selasa** → QT Mingguan baru final Selasa 18:00, padahal
Senin sudah lewat. **Membatalkan keputusan 2026-06-08 ("tutup Selasa 18:00, dikonfirmasi user")** —
user 2026-06-09 menegaskan candle Senin = tutup Senin 18:00 EDT. Fix: hapus shift `+1 hari` di
`classifyWeeklyQT` → `mondayStart = TradingDayStart(now).AddDate(0,0,-weekday)` (= Minggu 18:00),
`mondayEnd = +24h` (= Senin 18:00). Diverifikasi data VM: `QT Mingguan Sen=A → AMDX` (final, terkunci).

**(2) QT Session quarter resolusi H1 → M15.** `QTSessionQ` dihitung dari candle **H1 terakhir** (19:00 EDT
→ `SessionQuarter`=Q1 krn 19:00<19:30); quarter 90m vs candle 60m → Q1 nampil sampai candle 20:00 EDT
close (21:00 EDT). Padahal alert "Q1 Asia close" pakai M15 (presisi, 19:30). Inkonsistensi. Fix: `qtRef` =
candle **M15 terakhir yg close** (`Time+15m ≤ now`), dipakai utk `QTSessionQ` + `sessSt`. Diverifikasi:
`QT Session Q2 Asia 19:30–21:00 = M` (Q1 berhenti tepat 19:30). LondonQuarter (gate London, P&L) TETAP H1.

Keduanya **display-only** (gate QT/weekly OFF di `evaluate`) → **P&L identik**, tak perlu re-uji IS/OOS.
Mirror engine↔narrate utuh (narate = jalur display yg sama). Build/vet/`go test ./...` hijau. Binary
arm64 di-redeploy ke AWS (backup `/opt/forex/alertd.bak-20260609b`), tick pertama bersih tanpa panic.

## RONDE — AMSStrictStructure WF OOS: strict (= indikator Pine) DITOLAK jadi default (2026-06-10)

User minta "samakan engine ke indikator Pine `ith_itl.pine` karena udah valid" (strict C.2/C.3:
enforce syarat-2 vs STL/STH KANAN + ratchet STL/STH terbaru). Logika ini SUDAH ada di engine di balik
flag `AMSStrictStructure` (dibuat 2026-06-09, default OFF, build/test/paritas OK) → "implement" =
tinggal flip default. Langkah WF OOS yang ke-pause akhirnya dijalankan (snapshot `/tmp/snap_ams`,
apples-to-apples baseline vs strict, `cmd/walkforward -config`):

| Metrik OOS (AGGREGATE) | Baseline (strict OFF) | Strict ON (= Pine) |
|---|---|---|
| Trades | 416 | 982 (2.4×) |
| Win% | 43.5 | 38.2 |
| Total R | +258 | +418 |
| Avg R/trade | +0.620 | +0.426 (−⅓) |
| **PF** | **2.10** | **1.69** |
| **MaxDD%** | **6.2** | **11.8** |

Strict **OOS-DOMINATED**: PF 2.10→1.69 (di bawah bar user PF>2), DD hampir 2× (6.2→11.8%), avg R/trade
anjlok ~⅓; total R lebih besar HANYA karena volume 2.4×. Per protokol (lock hanya bila OOS-dominan) +
bar kualitas user (trade-off volume OK *asal PF OOS>2*) → **keputusan user (AskUserQuestion): TETAP OFF**.
Engine default tak berubah (`AMSStrictStructure=false`); logika strict tetap tersedia via flag utk
eksperimen. **Indikator Pine `ith_itl.pine` tetap strict standalone** (divergensi sengaja dari engine
default, terdokumentasi di header file — BUKAN cerminan engine default). ⚠️ Re-eval saat sampel
diperbesar (`-from 2018`): strict mungkin beda perilaku di rezim non-bull (volume 2.4× = sensitif rezim).

---

## RONDE — FVGBreak geometris (mirror Pine) → knob default OFF (2026-06-11)

Konteks: koreksi indikator Pine `ith_itl.pine` (sesi sama) mengubah penentuan FVGBreak jadi
**GEOMETRIS** — swing acuan = STH/STL **struktural** (`Zigzag(DetectSwings)` alternating, bukan semua
pivot 3-bar mentah) + BOS dinilai dari **level swing JATUH DI DALAM zona FVG** (bukan flag-broken
retrospektif), swing harus terbentuk ≤adj candle SEBELUM FVG. User minta engine "disamakan karena sudah
benar" (correctness-driven). Diuji penuh sesuai protokol (paritas → IS → WF OOS) atas snapshot
`/tmp/snap_fvgbrk`.

**Cek look-ahead:** logika LAMA (`brokenUp/brokenDown`) scan candle sampai akhir slice — tapi engine
panggil `DetectPDRs(s.h1,...)` dgn `s.h1 = window(tf.H1[:i+1])` (hanya candle ≤ bar sekarang) → **kausal,
BUKAN look-ahead**. Jadi degradasi P&L versi baru = nyata, bukan koreksi bias.

**Hasil (snapshot sama):**
| | IS tr | IS R | IS PF | IS DD% | OOS tr | OOS R | OOS PF | OOS DD% |
|---|---|---|---|---|---|---|---|---|
| Lama (broken-flag, adj=12) | 512 | +297.3 | 2.01 | 6.3 | 418 | +257.3 | 2.09 | 6.2 |
| Geometris (adj=12) | 470 | +263.3 | 1.96 | 6.7 | 389 | +228.3 | 2.02 | 7.6 |

**Sweep adj utk versi geometris (jawab "makin besar makin bagus?"): TIDAK.** IS R datar/jenuh
254→272 (adj 3→100), OOS R 228→231, PF turun 2.02→2.00, DD naik 7.6→7.8. Di SEMUA adj tetap jauh di
bawah versi lama (OOS 257/PF2.09/DD6.2). Pengikat = syarat "level di DALAM zona", bukan jarak.

→ **OOS-DOMINATED** (strictly worse, BUKAN wash spt BBRequireDisplacement). Protokol "lock hanya bila
OOS-dominan" + keputusan user (AskUserQuestion): **knob `FVGBreakGeometric` DEFAULT OFF**. Engine default
P&L tak berubah (paritas bersih: knob-off = 512/+297.26 identik). Logika geometris tersimpan (opt-in
`fvg_break_geometric: true`) utk re-eval saat sampel diperbesar `-from 2018` (bisa berbalik di rezim lain).
**Indikator Pine TETAP versi geometris** (divergensi sengaja dari engine default, terdokumentasi).

---

**Gap-anchor true-move Asia/Q1 (`AsiaGapAnchor`/`Q1GapAnchor`, DEFAULT ON 2026-06-15).** Keputusan user:
klasifikasi A/X (D.2 Asia + Q1 sesi) harus ikut menghitung **gap antara close candle sebelumnya** dengan
pembukaan sesi, karena sering terjadi **Volume Imbalance** (body candle terputus dari close sebelumnya) —
jangan dihitung dari harga open saja. Implementasi: detektor baru `detectors.ClassifyDailyG` meng-anchor
`directional` ke `prevClose` (close candle persis sebelum sesi) alih-alih `asiaH1[0].Open` saat
`prevClose > 0`. Aljabar identik dgn menambah gap ke move open→close: `(close−open)+(open−prevClose) =
close−prevClose`. Konsekuensi yg benar: gap LAWAN-ARAH net move → saling mengurangi (net displacement kecil
= akumulasi A), sesuai logika VI. Mode "range" (high−low) tak terpengaruh; "atr"+"ratio" pakai anchor.
`ClassifyDailyEx` jadi thin-wrapper `ClassifyDailyG(prevClose=0)` → semua caller lama + weekly (D.4 tetap
anchor open, di luar scope) 0-drift. Call-site engine/narrate/alertd gating via `asiaAnchor(knob, prevClose)`;
`asiaSlice` diperluas mengembalikan prevClose (close candle 1H sebelum sesi), Q1 pakai `prevCloseBefore`
(M15 sebelum window). Knob `asia_gap_anchor`/`q1_gap_anchor` di config loader + `config.yaml`, default true.

**Paritas:** A/X **bukan gate P&L** (`qt_phase_gate=false`) → P&L **IDENTIK** ON vs OFF: 512 trade / +297.26R
(knob-off `asia_gap_anchor:false q1_gap_anchor:false` = default ON, byte-for-byte sama). Yang bergeser hanya
LABEL: split amdx **296→282** / xamd **216→230** — 14 hari borderline pindah A→X karena gap pembukaan yg dulu
diabaikan kini melewati ambang `1.5×ATR`. Murni faithfulness/narasi-alert-watchlist, P&L tak berubah →
di-lock atas dasar correctness (keputusan user), bukan P&L. Unit test `TestClassifyDailyGapAnchor` mengunci:
sesi datar+gap → X, sesi naik+gap-lawan-arah → A, prevClose=0 == `ClassifyDailyEx`.

---

**Stage-3 heavy_accum NY-confirm (`HeavyAccumConfirmNY`, OOS-DOMINATED → DEFAULT OFF 2026-06-15).**
Wire-in skema 3-tahap D.7 (hybrid Opsi C) yang sebelumnya nganggur di detektor (`HeavyAccumConfirm` +
`DaySuspectedAccum` ada tapi tak dipakai `classifyDayType`). Versi engine lama = stage-1 saja: Asia &
London dua-duanya range < 0.4×ATR_daily → langsung `DayHeavyAccum` (gate #5 block). Stage-3: hari itu
cuma **suspected**, di-confirm jadi heavy_accum HANYA bila sesi NY (06:00–12:00) juga kompresi (range <
0.4×ATR) DAN tak ada FVG 1H baru di NY; kalau NY displace (range besar / FVG baru) = akumulasi pecah →
`DaySuspectedAccum` (TAK di-gate). Kausal: sebelum candle NY ada, nyRange=0 & no-FVG → confirm=true →
blok London dipertahankan identik lama; blok baru dilepas setelah NY benar-benar displace. Knob
`heavy_accum_confirm_ny` (config loader + config.yaml), helper `sessionCandles` (slice candle per-window
utk DetectFVGs). Default OFF.

**Paritas:** knob-off = identik (kode baru sepenuhnya di balik `if cfg.HeavyAccumConfirmNY`; default cache
terkini 513tr/+299R). **IS knob-ON:** 609tr/+322R (+96tr/+23R) tapi AvgR 0.583→0.529, **MaxDD 6.3%→9.9%**.
**WF OOS (AGG):** OFF 416tr/+250R/AvgR0.601/PF**2.05**/DD**6.4%** → ON 488tr/+274R/AvgR0.561/PF**1.97**/DD**9.9%**.
ON menambah R **hanya lewat volume** — SEMUA metrik risiko memburuk (PF↓, AvgR↓, MaxDD +55% relatif) →
**OOS-DOMINATED**. Protokol "lock hanya bila OOS-dominan" → **knob DEFAULT OFF**, opt-in re-eval `-from 2018`.

**Temuan jujur:** efek protektif gate #5 sebagian **berasal dari stage-1 yang "over-block"** (memblokir
hari-suspected yang sebenarnya pecah di NY). Versi 3-tahap yang **lebih faithful ke rule D.7** justru melepas
trade-trade itu dan merusak risk-adjusted. Tension faithfulness-vs-performa nyata — beda dari
BBRequireDisplacement (wash, di-lock atas faithfulness); di sini DD memburuk material → faithfulness tak
mengalahkan. Default tetap simplified-stage-1. AWS TIDAK perlu redeploy (default behavior identik).

---

## Skip jam-8 KONDISIONAL terhadap kalender (LIVE-only) — 2026-06-15

**Motivasi (keputusan user):** `SkipEntryHoursNY="8"` membuang candle 08:00 NY **setiap hari**
(edge tervalidasi: jam terburuk, n=52/PF0.62/−16R, krn rilis 08:30 ET). Tapi skip ini **buta** — ikut
membuang 08:00 di hari **tanpa** rilis besar (mis. Senin 2026-06-15). User minta: skip jam-8 **hanya
saat benar-benar ada rilis USD high-impact** di sekitar 08:30 ET.

**Kendala yang menentukan desain:** feed Forex Factory (`internal/news`, `ff_calendar_thisweek.json`)
**hanya minggu berjalan** — tak ada kalender historis. Maka skip news-conditional **mustahil
divalidasi WF OOS** untuk backtest 2022–2026. Keputusan user: **(1) live-only** (backtest tetap blanket,
edge −16R + angka OOS aman); **(2) hanya jam-8** (FOMC 14:00/ISM 10:00 dst tak tersentuh).

**Implementasi (engine bebas-network; caller inject runtime):**
- `engine.Config`: 2 field baru runtime-only — `SkipEntryNewsOnly bool` + `NewsSkipHourStarts
  map[time.Time]bool` (BUKAN dari YAML/DefaultConfig; zero-value = perilaku lama). Gate (`evaluate`)
  + mirror `narrate.go`: jam kandidat (`hourSkipped`) di-skip hanya bila `!SkipEntryNewsOnly` ATAU
  candle ada di set; mode news-only + tak ada rilis → lanjut (boleh entry).
- `engine.BuildNewsSkipSet(events, candidateHoursCSV, loc)` (exported, reuse `hourSkipped` → 0-drift):
  dari `news.FilterUSDHighImpact`, key = `event.Time.Truncate(time.Hour)` (UTC = awal candle H1), masuk
  set bila jam-NY ∈ candidateHoursCSV. EDT 08:30=12:30Z→12:00Z & EST 08:30=13:30Z→13:00Z, dua-duanya
  jam-NY 8 (pemetaan benar via `loc`). Unit test `news_skip_test.go` (EDT/EST/empty).
- `cmd/alertd -news-skip`: `refreshNewsSkip` di `tick()` SEBELUM `runOnce`/`engine.Run`. Feed gagal /
  cache-basi (`fromCache`) → `SkipEntryNewsOnly=false` (**fallback blanket protektif** — tak buka jam
  rawan tanpa kalender andal). `cmd/narrate -news-skip` analog (cuma valid utk scan minggu berjalan).

**Paritas (0-drift backtest):** default `SkipEntryNewsOnly=false` → jalur gate identik. `go run
./cmd/backtest` = **513tr/+299R/PF2.01/DD13R(6.3%) IDENTIK** pra-perubahan. (Note: `config_test`
`==`→`reflect.DeepEqual` krn Config kini punya map.) Verifikasi live: `narrate -at "2026-06-15 08:00"
-news-skip` → "0 jam-news di set" → QT jam-8 **LOLOS** (sebelumnya Skip). Minggu W25 high-impact hanya
FOMC 14:00 (di luar kandidat jam-8) → benar 0.

**Divergensi backtest(blanket)↔live(news-only) DISENGAJA & didokumentasi:** live punya kalender yang
backtest tak punya. ⚠️ Re-eval bila kelak ada sumber kalender historis (bisa divalidasi OOS).
