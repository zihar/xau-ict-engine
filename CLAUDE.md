# forex-backtest

POC personal untuk **backtest strategi trading XAUUSD** berbasis piramida
TDA → DB → AMS → QT → PD Array → Entry. Data harga ditarik dari **OANDA v20 REST API**.

Bahasa komunikasi: **Bahasa Indonesia** (term trading & identifier kode tetap English).

> **Riwayat eksperimen/keputusan lengkap ada di [`DECISIONS.md`](DECISIONS.md)** (kalibrasi,
> diagnosa, tiap ronde fix + angka IS/OOS). File ini sengaja ramping = instruksi + state terkini.

## Sumber kebenaran rules

Logika strategi **bukan** didefinisikan di repo ini. Sumbernya:
`~/Documents/forex-lessons/BACKTEST_RULES.md` (Section A–N, **SEMUA CLOSED** per 2026-05-31).
`config.yaml` = **mirror `engine.DefaultConfig()`**, bisa di-load via
`go run ./cmd/backtest -config config.yaml` (loader `internal/config.Load`, flat-YAML std-lib).
Param turunan Section M rules — **kalau rules berubah, sinkronkan ke `config.yaml` + `DefaultConfig`.
Jangan invent parameter sendiri.**

Status coding: **SEMUA layer A–N IMPLEMENTED + tested & runnable end-to-end.**
- Detector (`internal/detectors/`): A swing/Fibonacci, C AMS ITH/ITL, D QT/session/phase/daytype,
  F PD Array/POI (FVG/FVGBreak/VI/BB/BPR/IFVG/OB), G/H/I entry+SL/TP/sizing.
- State (`internal/state/`): B weekly OF/LTL-LTH/daily bias (+ expose umur-flip + `DailyBiasRef`/`DetectSkenarioB`), E Kunci#2 regime.
- Engine (`internal/engine/`): pipeline N.2 (Run/TFData/Config/Signal/Trade/Result) + simulasi K + sizing I.1 + `FlipTiming` (G.4 early|definitif|n/a).
- Report (`internal/report/`): metrik Section L + breakdown (tier×TF, jenis POI, confluence, flip_timing) + CSV/XLSX.

## State & default terkini (2026-06-05)

**Metrik baseline default:** IS 513tr / win 42.5% / +298R / PF2.01 / DD13R(6.3%); **WF OOS 417tr / +256R /
PF2.08 / MaxDD6.2%** (`go run ./cmd/walkforward`). Progresi penuh di DECISIONS.md.
(Pra adj=12-lock: IS 502/+292/PF2.01/DD6.3%, OOS 407/+249/PF2.08/DD6.2%. Pra FVGBreak-per-TF: IS 522/+292/
PF1.97/DD8.1%, OOS 417/+248/PF2.05/DD8.9% — H1,D strictly-dominan, lihat bawah.)

**Default yang DI-LOCK (semua OOS-validated):**
- `ZoneFibTF=Daily` + gate zona dicek di HARGA (bukan `POI.Mid()`); guard overshoot Fib.
- Fib leg: `FindValidImpulseZ` (A.1 last-impulse + A.2 Rule-of-0.5) atas zigzag ATR-filter `MinSwingATRMultHTF=1.0`.
- `ImpulseOnly=true` (retrace dimatikan — hanya trade searah weekly OF).
- `RequireStandardTrigger=true` (buang fast_early); `EntryTriggerMode=itl` (= konsentrator edge).
- `MaxPOITier=3`; `MaxConfluence=3`; `BBNeedsFVG=true` (BB wajib ada keluarga FVG).
- `BBRequireDisplacement=true` (BB sah hanya kalau FVG penyah = displacement KELUAR range candle BB:
  bullish `FVG.Top>BB.High` / bearish `FVG.Bottom<BB.Low` — lock 2026-06-09 keputusan user, correctness:
  rules F.1 butuh impulsive, tafsir lama "asal ada FVG" longgar. OOS WASH bukan dominan (−3R/PF flat/
  DD ~flat, sentuh 3–5 trade) → lock atas dasar faithfulness, P&L dapat diabaikan. ⚠️ re-eval `-from 2018`).
- `MinBiasAgeDays=8` / `MinBiasAgeDaysBull=3` (karantina umur-bias asimetris: bullish 3 hari, bearish 8).
- `DisableOB=true`; `SkipEntryHoursNY="8"` (news 08:30 ET); `LondonMinHourNY=4`.
  (OB versi-pertemuan-4 `OBStrict` diimplementasi 2026-06-04 — reversal+displacement-FVG+liquidity-sweep —
  tapi OOS-dominated PF2.05→1.77/DD8.9→9.8% → TETAP OFF. Determinisme PDR difix `lessPDR` total-order,
  0-drift P&L. Detail di DECISIONS.md.)
  - **Skip jam-8 KONDISIONAL (LIVE-only, 2026-06-15):** `SkipEntryNewsOnly`+`NewsSkipHourStarts`
    (`engine.BuildNewsSkipSet`) — saat alertd/narrate dijalankan dgn `-news-skip`, skip 08:00 NY
    hanya berlaku bila kalender minggu berjalan punya rilis USD high-impact di jam itu (jam tanpa
    news → boleh entry). **Backtest TIDAK tersentuh** (default `SkipEntryNewsOnly=false` = blanket
    skip; P&L 0-drift, 513/+299/PF2.01 identik) karena feed Forex Factory hanya minggu berjalan →
    skip news-conditional **tak bisa divalidasi OOS**. Divergensi backtest(blanket)↔live(news-only)
    **disengaja**: live punya kalender yang backtest tak punya. Feed gagal/cache-basi → fallback
    blanket (protektif). Keputusan user: hanya jam-8 (FOMC 14:00 dst tak tersentuh). Detail DECISIONS.md.
- `POITouchWindowBars=4`; `EntryFreshBars=1` (FILL ≈ waktu keputusan, bukan retrospektif).
- `IFVGRequireNoSameDirFVG=true` (IFVG Fallback Rule pertemuan 4: IFVG cuma sah kalau tak ada FVG
  searah live — lock 2026-06-04 correctness-driven, backtest netral, fix IFVG "terlalu jauh").
- `FVGBreakTFs="H1,D"` (promosi Tier-2 FVGBreak hanya H1+D, strip H4+W — lock 2026-06-04 OOS-dominan:
  R terjaga, PF 2.05→2.08, DD 8.9→6.2%; FVGBreak merugikan di H4, netral di D).
- `FVGSwingBreakAdjacency=12` (Kunci#1: FVG ≤12 candle dari swing-break → Tier-2 — lock 2026-06-04, keputusan
  user. Sweep 1→50: IS+OOS sama-sama memuncak di 12 (+7R OOS vs adj=3, PF/DD datar), saturasi ≥15. Bukan
  strictly-dominan (PF/DD datar) — gain = volume-kualitas-sama; ⚠️ faithfulness longgar (12d di D), re-cek
  saat `-from 2018`. Fallback internal `detectFVGSwingBreaks` adj≤0 ikut 3→12).
- `BPRDirectional=true` (BPR pertemuan 4 = directional, arah = FVG lebih baru, **zona IRISAN** — lock
  2026-06-04 correctness, backtest IDENTIK IS+OOS). Zona-FVG-terakhir-penuh & `DisableBPR` DIUJI &
  MERUGIKAN (zona-full BPR PF→0.58/OOS→1.94; buang-BPR OOS→1.97<2.08 krn BPR net-akretif via seleksi)
  → revert ke irisan, BPR tetap aktif. Detail DECISIONS.md.

**Default OFF (knob ada utk eksperimen):** `MOGate`, `RetraceTPToFVG`, `OFBearConfirmWeeks=0`,
`MaxOFAgeDays`, `LondonSweepEntry`, `Kunci3Fallback`, `ReleqSweepGate`, `LondonQ4Only`, `DisableBPR`,
`OBStrict`, `FVGBreakGeometric` (penentuan FVGBreak geometris = STH/STL struktural + level swing DI DALAM
zona FVG, mirror indikator Pine; OOS-DOMINATED 2026-06-11: IS/OOS −34R/−29R, PF & DD lebih buruk di
SEMUA adj 3–100 → OFF, opt-in utk re-eval `-from 2018`), `AsiaAXMode=ratio` (detektor Asia A/X versi
pertemuan 10 = true-move/range-sesi ≥ `AsiaRangeRatio`; default `atr` D.2 = 1.5×ATR),
`HeavyAccumConfirmNY` (stage-3 D.7: heavy_accum di-confirm hanya bila NY 06:00–12:00 juga kompresi & tak
ada FVG 1H baru, NY-displace → suspected/tak gate; OOS-DOMINATED 2026-06-15: +24R cuma dari volume,
PF 2.05→1.97, AvgR↓, MaxDD 6.4%→9.9% → OFF. Temuan: protektif gate #5 sebagian dari stage-1 yg "over-block";
versi faithful melepas trade yg merusak risiko. opt-in re-eval `-from 2018`). (Semua sudah diuji
& MERUGIKAN / netral — detail + angka di DECISIONS.md.)

> **Detektor Asia A/X (2026-06-04):** A/X **bukan gate P&L** (`qt_phase_gate=false`) → ganti detektor/syarat =
> 0 dampak backtest default (P&L IDENTIK, hanya tag amdx/xamd bergeser); murni label narasi/alert/watchlist.
> - **`AsiaRequireFVG=false` DI-DEFAULT-KAN (keputusan user 2026-06-04):** syarat-1 FVG D.2 dimatikan → X =
>   penentu murni true-move (`|close−open| ≥ 1.5×ATR_1H`); kehadiran FVG turun jadi **catatan** di
>   `FormatAsiaLine` ("Asia: X … — catatan: TANPA FVG 1H" via `ScanNarrative.AsiaHasFVG`). `=true` = D.2 lama.
> - `AsiaAXMode` default `atr` (ratio=pertemuan 10 diuji: sbg gate lebih buruk PF1.78<1.95, over-flag → OFF).
> - **`AsiaGapAnchor`/`Q1GapAnchor` DEFAULT ON (keputusan user 2026-06-15):** true-move di-anchor ke
>   **close candle SEBELUM sesi** (`|close_last − prevClose|`) bukan open candle pertama → gap pembukaan
>   sesi (Volume Imbalance) ikut terhitung sbg displacement. Aljabar: `(close−open)+(open−prevClose)=close−prevClose`;
>   gap lawan-arah net move → saling mengurangi (net kecil = akumulasi A). Detektor `ClassifyDailyG`
>   (`ClassifyDailyEx`=wrapper prevClose=0, 0-drift). P&L IDENTIK (512/+297.26 ON==OFF, A/X bukan gate);
>   split bergeser amdx 296→282 / xamd 216→230 (14 hari borderline A→X krn gap dulu diabaikan). `=false` = anchor open lama.
> - Diagnosa: `cmd/qtscan -mode atr|ratio|both [-fvg] [-ratio]` (default mirror engine = bandingkan label).

**Caveat jujur:** semua angka dari **1 rezim bull (2022–2026)**. Sampel per-segmen kecil.
Perbesar sampel (`cmd/fetch -from 2018`) = prioritas. **RR global DITOLAK user.**

**Sisa kerja:** ~~Kunci#1 selektif~~ (gugur 2026-06-03) → ~~FVGBreak per-TF~~ (**DONE 2026-06-04:
`FVGBreakTFs="H1,D"` di-lock — strip H4 = lever, OOS DD 8.9→6.2%**) → ~~sweep `FVGSwingBreakAdjacency`~~
(**DONE 2026-06-04: LOCK adj=12 — sweep 1→50 puncak IS+OOS di 12, +7R OOS vs adj=3 (PF/DD datar), saturasi ≥15;
baseline bergeser ke IS513/+298, OOS417/+256; DECISIONS.md**). Sisa: perbesar sampel `-from 2018` (makin
penting, semua angka 1-rezim-bull — re-cek puncak adj=12 di rezim lain); lock width/window via OOS; unit test `asiaSwept`.
- **Re-evaluasi BPR saat sampel diperbesar** (`-from 2018`): edge BPR RAPUH & zona-sensitif — PF2.08
  lama bergantung zona-irisan sempit; zona-FVG-penuh (lebih faithful) → OOS 1.94; buang BPR → 1.97.
  Sampel kini ~30 trade/1-rezim. Re-uji `bpr_directional`/`disable_bpr` + zona irisan-vs-full di sampel
  lebih besar utk putuskan tafsir zona yg benar & apakah BPR layak dipertahankan. Detail DECISIONS.md.

## Komponen & fitur (rujukan)

- **Narrative + visual** — `engine.Narrate(tf,cfg,at) ScanNarrative` = narasi langkah-demi-langkah
  (TDA→Daily Bias→AMS→QT→POI→Entry 5m; AMS = gate keras ITL/ITH C.4). `internal/viz.RenderSVG`
  render candlestick + anotasi (std-lib). `cmd/narrate -at <waktu> -svg out.svg` (`-find N` cari
  setup valid, `-recent`). SVG→PNG: `qlmanage -t -s 1000 -o /tmp x.svg`. Glue di `internal/chartann`.
- **Formatter bersama (0-drift engine↔output):** `FormatNarrative` (narasi piramida penuh, alert SETUP),
  `FormatWatchlist` (6-seksi ringkas, pesan watchlist; width 88 terminal / 42 Telegram-HP),
  `FormatMOLine` / `FormatAsiaLine` / `FormatRelEqLines`.
- **Watchlist** — POI valid terdekat per-TF (H1/H4/D) × 2 arah, dihitung dari pipeline yang sama.
  alertd kirim Telegram **hanya saat perubahan bermakna** (`watchlistTrigger`/`watchlistDiff`/
  fingerprint; anti-spam): zona POI, REH/REL, MO-crossing, Asia close, **+ AMS ITL/ITH
  terbentuk/break dua arah** (2026-06-08; `amsTokens`/`amsFPChanged`, `ScanNarrative.AMSITL/AMSITH`).
  Trigger manual: ketik `/watchlist` di chat bot.
- **Alert day-type** (2026-06-15; `maybeSendDayTypeAlert`/`formatDayTypeAlert`, dedup `State.LastDayTypeAlert`)
  — heads-up Telegram SEKALI per trading-day saat day-type (D.7) = `heavy_expanding`/`heavy_accum` (skip
  normal/suspected). Dikirim setelah **London close (06:00 NY)** saat verdict stabil; verdict reuse
  `ScanNarrative.QTDayType`. Anti-drift: syarat trading-day candle == trading-day now. Always-on, label-only.
- **Detektor konteks** — Midnight Open (`detectors.MidnightOpen*`, divider premium/discount, penanda saja),
  REH/REL (`DetectRelEquals`, fractal H1/H4/D), Asia/Q1-session A/X (`ClassifyDaily` diturunkan ke M15).
- **QT Mingguan (D.4) — FIX 2026-06-08, koreksi candle-Senin 2026-06-09:** watchlist `QTSkenario`
  dari `classifyWeeklyQT` (candle SENIN di 4H via `detectors.ClassifyWeekly`, FVG-4H wajib +
  `|close−open|≥1.5×ATR_4H`), dinilai **setelah Senin close** (konvensi NY-close/ICT: candle Senin =
  **BUKA Minggu 18:00 → TUTUP Senin 18:00 NY** = sesi hari Senin), **dikunci seminggu**; sebelum itu
  "(menunggu Senin close)". **Koreksi 2026-06-09 (keputusan user, batalkan def lama "tutup Selasa 18:00"):**
  fix 06-08 keliru geser 1 hari (pakai candle buka Senin 18:00 = sesi Selasa) → QT Mingguan baru final
  Selasa 18:00 padahal Senin sudah lewat. Sekarang final tepat Senin 18:00 EDT. Dulunya (pra-06-08) salah
  pakai detektor harian (Asia/1H) → `atr1h=0` saat Asia belum close → X-palsu flip tiap tick. Skenario
  harian (D.2) tak berubah. Display-only (gate QT/weekly off) → P&L identik. Detail di DECISIONS.md.
- **QT Session quarter — FIX 2026-06-09:** `QTSessionQ`/`QTSessPhase` di watchlist kini dihitung dari
  candle **M15 terakhir yang close** (bukan H1) → quarter 90m berganti tepat di batasnya (mis. Q1 Asia
  berakhir 19:30, konsisten dgn alert Q1-close M15) bukan tertahan resolusi H1 sampai 20:00/21:00.
  Display-only (gate QT di `evaluate` tetap H1) → P&L identik.
- **News alert (`internal/news` + `cmd/newsalert`)** — tarik kalender ekonomi (Forex Factory weekly JSON,
  gratis tanpa key, std-lib + cache fallback saat 429/down), klasifikasi surprise (actual vs forecast) →
  bias XAUUSD via playbook **rezim 2026** (inflasi panas / jobs kuat = bearish gold; anchor narasi inflasi =
  y/y + catatan base-effect). Kirim Telegram pra-rilis (forecast+previous+nowcast-manual) & pasca-rilis
  (surprise→bias). Dedup `news_state.json`. Dijadwalkan via systemd-timer AWS (`deploy/forex-newsalert.{service,timer}`,
  tiap 5m, DST-proof dari timestamp feed). **POC (A) DONE 2026-06-08**; **B DONE 2026-06-08: alertd `-news`
  (paritas default-off)** — `Decide`/`PickTarget`/`ParseNames` dipromosikan ke `internal/news` (reuse), alertd
  dapat flag `-news` (+ `-news-events/-news-prewindow/-news-stale/-news-nowcast/-news-state/-news-feedcache/-news-support`);
  `maybeSendNewsAlerts` di tick (guarded recover, independen OANDA), dedup `news_state.json` terpisah.
  Detail DECISIONS.md. ⚠️ Nowcast leading (Cleveland Fed/ADP) belum auto — sementara via flag `-nowcast`.
  **Reminder mingguan (2026-06-10):** `newsalert -digest` = heads-up Senin pagi daftar rilis high-impact pekan
  ini (CPI/PPI/NFP+FOMC) per-hari WIB; dedup per-minggu (`WeekKey`/`Sent.Digest`), `internal/news/digest.go`.
  Jadwal timer khusus `deploy/forex-newsdigest.{service,timer}` (`OnCalendar=Mon 00:00 UTC`=07:00 WIB). DEPLOYED AWS 2026-06-11 (binary `newsalert` + unit dipasang di `/opt/forex`, timer aktif → next Mon 15 Jun; Persistent tak catch-up saat enable jadi tak ada kiriman tak terduga; dry-run di server verif positif 2 rilis W24).
- **Harness** — `cmd/sweep` (grid param), `cmd/walkforward` (anchored 4-fold IS→OOS, dukung `-config`),
  `cmd/entries` (reason tiap entry + outcome), `cmd/gatestats` (funnel kill-rate).
- **Diagnostik AMD** — `cmd/dayscan` (karakter fase Manipulasi: %sweep akumulasi + kedalaman overshoot/penetrasi
  + arah distribusi NY vs OF; frame intraday+weekly). Temuan 2026-06-07 di DECISIONS.md: sweep TERARAH ~53%
  (sisi-mana-pun 92%), overshoot median 0.87×ATR, NY-AM distribusi langsung searah OF tanpa judas (judas di London).
- **Protokol kalibrasi:** knob → cek paritas (knob-off == perilaku lama) → IS sweep → **WF OOS**
  → lock HANYA bila OOS-dominan. Mirror engine↔narrate WAJIB 0-drift. Lihat auto-memory
  `protokol-validasi-strategi` + `pelajaran-metodologi-backtest`.

## Struktur

```
cmd/fetch/main.go        # entrypoint: smoke-test token → download & cache candle
cmd/backtest/main.go     # entrypoint: load cache → engine.Run → report.Print/WriteCSV
cmd/sweep/main.go        # entrypoint: grid sweep param → tabel komparatif ter-ranking
cmd/walkforward/main.go  # entrypoint: walk-forward IS→OOS split (validasi overfit)
cmd/narrate/main.go      # entrypoint: narasi pyramid scan + chart SVG (1 titik waktu; -find awal / -recent akhir)
cmd/entries/main.go      # entrypoint: report reason TIAP entry + outcome; -svgdir = 1 SVG per entry; -detail N = narasi penuh
cmd/alertd/main.go       # daemon realtime: poll OANDA tiap interval → engine.Run → alert Telegram saat ada setup baru
cmd/dayscan/main.go      # entrypoint: diagnostik karakter fase Manipulasi AMD (sweep akum/kedalaman/distribusi vs OF; intraday+weekly)
cmd/newsalert/main.go    # entrypoint: alert rilis berita ekonomi (CPI/PPI/NFP) → Telegram; pra-rilis (forecast+nowcast) & pasca-rilis (surprise→bias gold); -dry/-simulate/-once/-interval
internal/news/           # kalender ekonomi (Forex Factory JSON, std-lib, +cache fallback) + klasifikasi surprise→bias XAUUSD + builder pesan
internal/walkforward/    # anchored walk-forward: sweep IS pilih param, eval OOS
internal/viz/chart.go    # render candlestick + anotasi (POI/entry/SL/TP) ke SVG (std-lib)
internal/engine/narrate.go # Narrate(): narasi langkah-demi-langkah + anchor Fib leg + ITH/ITL + pivot trigger
internal/chartann/         # glue ScanNarrative→viz.Annotations (POI/Fib leg+level/marker ITH-ITL/trigger/entry-SL-TP)
internal/xlsx/             # writer .xlsx minimal std-lib (archive/zip + xml; inline string + auto-numeric)
internal/
  oanda/                 # client v20 REST read-only (TIDAK ada eksekusi order)
    client.go            #   HTTP + Bearer auth + pilih base URL practice/live
    account.go           #   GetAccounts() — smoke-test token
    candles.go           #   FetchCandles() — paginasi 5000/req + alignment (guard t.Before(cursor) anti-dup)
    types.go             #   struct respons OANDA
  data/                  # domain + cache
    candle.go            #   type Candle (Time UTC, OHLC float64, Volume)
    store.go             #   WriteCSV (atomik temp+rename) / ReadCSV / AppendNew per (instrument, granularity)
  detectors/ state/ engine/ report/ notify/ config/   # layer A–N + alert state + config loader
data/XAU_USD             # SYMLINK → ~/.forex-alertd/data/XAU_USD (cache live alertd; gitignored)
```

## Menjalankan

Butuh token OANDA v20 (akun tipe **OANDA/fxTrade**, bukan MT4/MT5). Simpan di `.env`
(lihat `.env.example`, gitignored).

```bash
set -a; . ./.env; set +a       # load OANDA_TOKEN, OANDA_ACCOUNT_ID, OANDA_ENV
go run ./cmd/fetch             # smoke-test + download XAU_USD W/D/H4/H1/M15/M5, default dari 2022-01-01
go run ./cmd/fetch -from 2020-01-01 -to 2024-12-31
go run ./cmd/backtest [-exec] [-out trades.csv]   # ~4s atas cache 2022→2026
go build ./... && go vet ./...
```

**Cache tunggal:** `data/XAU_USD` = symlink → `~/.forex-alertd/data/XAU_USD` (di-refresh alertd
tiap 5 menit). Semua tool baca data live otomatis — tak perlu fetch manual selama alertd hidup.
Dataset tumbuh terus → angka backtest bisa bergeser antar-run; untuk reproducible, snapshot dulu
(`cp -RL data/XAU_USD /tmp/snap`).

### Daemon alert realtime (`cmd/alertd`)

Poll OANDA tiap interval → refresh cache (`data.AppendNew`, anti-duplikat) → `engine.Run` →
kirim alert Telegram saat sinyal layer baru muncul. Dedup persisten via `internal/notify.State`
(JSON `data/alert_state.json`) + freshness guard. Read-only ke OANDA (tidak ada eksekusi order).

```bash
set -a; . ./.env; set +a       # + TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID
go run ./cmd/alertd -once      # satu siklus lalu keluar (cek manual)
go run ./cmd/alertd            # daemon loop (default tiap 5m, tick di-align ke batas interval + 20s)
```

Flag: `-instrument` (XAU_USD), `-dir`, `-config` (opsional), `-interval` (5m), `-once`,
`-state`, `-freshness` (6m), `-from` (2022-01-01 bootstrap kalau cache kosong), `-heartbeat`,
`-news-skip` (skip jam-8 KONDISIONAL vs kalender minggu ini — lihat blok default; off=blanket).

**Env wajib alertd:** `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID` (+ `OANDA_TOKEN`, `OANDA_ENV`
default `practice`). Semua dari env/`.env` — jangan hard-code/commit.

**Runtime produksi di Mac ini: launchd** `id.zihar.forex-alertd` (`~/Library/LaunchAgents/`,
KeepAlive=true) → wrapper `~/.forex-alertd/run.sh` (cwd di LUAR `~/Documents` agar bebas TCC macOS).
Log: `~/.forex-alertd/logs/alertd.log`. Cache yang ditulis = target symlink repo. Update binary:
`go build -o bin/alertd ./cmd/alertd && cp bin/alertd ~/.forex-alertd/alertd && launchctl
kickstart -k gui/$(id -u)/id.zihar.forex-alertd` (jendela aman ubah cache: `bootout` dulu, lalu
`bootstrap gui/$(id -u) ~/Library/LaunchAgents/id.zihar.forex-alertd.plist`). `scripts/pagi.sh` =
start pagi (bootout → `alertd -once` fetch+watchlist instan → bootstrap). Deploy Linux: `deploy/forex-alertd.service`.

**Auto-deploy AWS saat `git push` (pull-based, sejak 2026-06-15):** prod `alertd` jalan di **EC2
Singapore** (systemd `forex-alertd`, `/opt/forex`; daemon Mac di-disable). Cukup `git push origin
main` → ~1 menit kemudian live, **tanpa scp/ssh manual**. Mekanisme: `forex-deploy.timer` (tiap 1
menit, root) → `/opt/forex/deploy.sh` cek SHA `origin/main`; kalau beda → `git reset --hard` → build
arm64 native di EC2 (std-lib only, no module fetch) → install binary atomik → sync `config.yaml` +
unit `forex-alertd.service` (kalau berubah, +`daemon-reload`) → `restart forex-alertd`; skrip
self-update. Repo `zihar/xau-ict-engine` **public** → fetch anonim HTTPS (tak perlu deploy key).
Clone di `/opt/forex/repo`; `.env`+`data/` di `/opt/forex` (tak di repo → tak ditimpa). File:
`deploy/{deploy.sh,forex-deploy.{service,timer},setup-autodeploy.sh}`, doc `deploy/README-aws.md §9`.
Pantau: `journalctl -u forex-deploy -f`. Paksa: `sudo systemctl start forex-deploy.service`. Matikan:
`sudo systemctl disable --now forex-deploy.timer`. Setup ulang (host/repo baru): `setup-autodeploy.sh`
(re-runnable). Manual scp = fallback kalau auto-deploy mati (lihat README-aws §Ringkasan operasional).

## Keputusan & konvensi penting

- **Std-lib only.** Client OANDA pakai `net/http`+`encoding/json`, bukan SDK. Pertahankan
  kecuali ada alasan kuat (diskusikan dulu dengan user).
- **Read-only ke OANDA.** Repo ini cuma tarik data historis. JANGAN tambah endpoint order/trade.
- **Candle harian anchor 18:00 NY** (`daily_candle_anchor_hour_ny: 18`) → `dailyAlignment=18` +
  `alignmentTimezone=America/New_York`. Sanity check: kolom `time` di `D.csv` harus 22:00 (EDT)
  atau 23:00 (EST) UTC.
- **Hanya simpan candle `complete: true`**, harga **midpoint** (`price=M`). Waktu RFC3339 UTC.
- **Granularity Phase 1**: `W, D, H4, H1, M15, M5`. Instrumen: `XAU_USD`. (M15 dipakai alert
  Q1-session A/X; tak dipakai pipeline `engine.Run`.)
- **Kredensial** HANYA dari env/`.env`. Jangan hard-code/commit. `data/` & `.env` di `.gitignore`.
- **Mirror engine↔narrate WAJIB 0-drift** + paritas knob-off saat tambah fitur/gate.

## OANDA gotcha (kalau setup ulang akun)

Demo HARUS pilih **region SG** agar muncul platform tipe **"OANDA" (= fxTrade/OANDA Web)** —
cuma tipe ini yang dapat v20 REST API + portal "Manage API Access". MT4/MT5/TradingView **tidak**
punya API token. Base URL: demo `api-fxpractice.oanda.com`, live `api-fxtrade.oanda.com`.
