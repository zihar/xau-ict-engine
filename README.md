# xau-ict-engine

POC personal untuk **backtest strategi trading XAUUSD** berbasis piramida
**TDA → Daily Bias → AMS → QT → PD Array → Entry**. Data harga ditarik dari
**OANDA v20 REST API** (read-only, std-lib only, tanpa SDK & tanpa eksekusi order).

> Logika strategi bukan didefinisikan di repo ini — sumbernya `BACKTEST_RULES.md` (Section A–N).
> `config.yaml` = mirror `engine.DefaultConfig()`. Riwayat eksperimen & keputusan lengkap ada di
> [`DECISIONS.md`](DECISIONS.md); instruksi & state terkini di [`CLAUDE.md`](CLAUDE.md).

## Metrik baseline (per 2026-06-05)

| | Trade | Win | Net R | PF | Max DD |
|---|---|---|---|---|---|
| **In-sample** (2022–2026) | 513 | 42.5% | +298R | 2.01 | 13R (6.3%) |
| **Walk-forward OOS** | 417 | — | +256R | 2.08 | 6.2% |

> ⚠️ Semua angka dari **1 rezim bull (2022–2026)** — sampel per-segmen kecil. Memperbesar
> sampel (`cmd/fetch -from 2018`) adalah prioritas.

## Arsitektur layer (A–N)

- **Detector** (`internal/detectors/`) — A swing/Fibonacci, C AMS ITH/ITL, D QT/session/phase/day-type,
  F PD Array/POI (FVG/FVGBreak/VI/BB/BPR/IFVG/OB), G/H/I entry + SL/TP/sizing.
- **State** (`internal/state/`) — B weekly OF / LTL-LTH / daily bias, E Kunci#2 regime.
- **Engine** (`internal/engine/`) — pipeline N.2 (Run/TFData/Config/Signal/Trade/Result) + simulasi K +
  sizing I.1 + flip timing G.4 + narasi langkah-demi-langkah (`Narrate`).
- **Report** (`internal/report/`) — metrik Section L + breakdown (tier×TF, jenis POI, confluence,
  flip_timing) + output CSV/XLSX.

## Menjalankan

Butuh token OANDA v20 (akun tipe **OANDA/fxTrade**, bukan MT4/MT5). Simpan di `.env`
(contoh di [`.env.example`](.env.example), gitignored).

```bash
set -a; . ./.env; set +a          # load OANDA_TOKEN, OANDA_ACCOUNT_ID, OANDA_ENV

go run ./cmd/fetch                # smoke-test token + download XAU_USD W/D/H4/H1/M15/M5 (default dari 2022-01-01)
go run ./cmd/fetch -from 2020-01-01 -to 2024-12-31

go run ./cmd/backtest             # ~4s atas cache 2022→2026 → report metrik
go run ./cmd/backtest -out trades.csv

go build ./... && go vet ./...
```

## Daftar tool (`cmd/`)

| Tool | Fungsi |
|---|---|
| `fetch` | Download & cache candle dari OANDA (paginasi 5000/req) |
| `backtest` | Load cache → `engine.Run` → report (Print/CSV/XLSX) |
| `sweep` | Grid sweep parameter → tabel komparatif ter-ranking |
| `walkforward` | Walk-forward IS→OOS split (validasi overfit) |
| `entries` | Report reason tiap entry + outcome (+ SVG per entry) |
| `gatestats` | Funnel kill-rate tiap gate |
| `narrate` | Narasi pyramid scan + chart SVG di 1 titik waktu |
| `dayscan` | Diagnostik karakter fase Manipulasi AMD |
| `alertd` | Daemon realtime → alert Telegram saat ada setup baru |
| `newsalert` | Alert rilis berita ekonomi (CPI/PPI/NFP) → bias gold |
| `qtscan` `amscan` `q1sweep` `ithfreq` `nfpqt` `fomcscan` | Diagnostik per-komponen |

## Daemon alert realtime (`cmd/alertd`)

Poll OANDA tiap interval → refresh cache → `engine.Run` → kirim alert Telegram saat sinyal layer baru
muncul (dedup persisten + freshness guard, read-only ke OANDA).

```bash
set -a; . ./.env; set +a          # + TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID
go run ./cmd/alertd -once         # satu siklus lalu keluar (cek manual)
go run ./cmd/alertd               # daemon loop (default tiap 5m)
```

Runtime produksi: launchd di Mac (`id.zihar.forex-alertd`) / systemd di Linux
(lihat [`deploy/`](deploy/)).

## Konvensi penting

- **Std-lib only** — client OANDA pakai `net/http` + `encoding/json`, bukan SDK.
- **Read-only ke OANDA** — hanya tarik data historis, tidak ada endpoint order/trade.
- **Candle harian anchor 18:00 NY**, hanya simpan candle `complete: true`, harga midpoint, waktu RFC3339 UTC.
- **Kredensial hanya dari env/`.env`** — jangan hard-code/commit. `data/`, `.env`, `*.pem` di-gitignore.
- **Mirror engine ↔ narrate wajib 0-drift** + paritas knob-off saat menambah fitur/gate.

## Struktur

```
cmd/                  # entrypoint per tool (fetch, backtest, sweep, walkforward, alertd, ...)
internal/
  oanda/              # client v20 REST read-only (HTTP + Bearer auth)
  data/               # domain type Candle + cache CSV (atomik temp+rename)
  detectors/          # layer A, C, D, F, G/H/I
  state/              # layer B, E
  engine/             # pipeline N.2 + simulasi K + sizing I + Narrate
  report/             # metrik Section L + breakdown + CSV/XLSX writer
  news/               # kalender ekonomi + klasifikasi surprise → bias XAUUSD
  viz/ chartann/      # render candlestick + anotasi ke SVG
  config/             # loader flat-YAML (mirror DefaultConfig)
deploy/               # unit systemd + wrapper launchd
config.yaml           # mirror engine.DefaultConfig()
CLAUDE.md             # instruksi + state terkini
DECISIONS.md          # riwayat eksperimen & keputusan lengkap
```

---

Go 1.26 · std-lib only · bahasa komunikasi Indonesia (term trading & identifier tetap English).
