# xau-ict-engine

A personal POC for **backtesting the XAUUSD trading strategy** based on the
**TDA → Daily Bias → AMS → QT → PD Array → Entry** pyramid. Price data is pulled from the
**OANDA v20 REST API** (read-only, std-lib only, no SDK and no order execution).

> The strategy logic is not defined in this repo — its source is `BACKTEST_RULES.md` (Sections A–N).
> `config.yaml` = a mirror of `engine.DefaultConfig()`. The full experiment and decision history is in
> [`DECISIONS.md`](DECISIONS.md); instructions and current state are in [`CLAUDE.md`](CLAUDE.md).

## Baseline metrics (as of 2026-06-05)

| | Trades | Win | Net R | PF | Max DD |
|---|---|---|---|---|---|
| **In-sample** (2022–2026) | 513 | 42.5% | +298R | 2.01 | 13R (6.3%) |
| **Walk-forward OOS** | 417 | — | +256R | 2.08 | 6.2% |

> ⚠️ All figures come from **a single bull regime (2022–2026)** — the per-segment sample is small. Growing
> the sample (`cmd/fetch -from 2018`) is a priority.

## Layer architecture (A–N)

- **Detector** (`internal/detectors/`) — A swing/Fibonacci, C AMS ITH/ITL, D QT/session/phase/day-type,
  F PD Array/POI (FVG/FVGBreak/VI/BB/BPR/IFVG/OB), G/H/I entry + SL/TP/sizing.
- **State** (`internal/state/`) — B weekly OF / LTL-LTH / daily bias, E Key #2 regime.
- **Engine** (`internal/engine/`) — N.2 pipeline (Run/TFData/Config/Signal/Trade/Result) + K simulation +
  I.1 sizing + G.4 flip timing + step-by-step narration (`Narrate`).
- **Report** (`internal/report/`) — Section L metrics + breakdown (tier×TF, POI type, confluence,
  flip_timing) + CSV/XLSX output.

## Running

Requires an OANDA v20 token (account type **OANDA/fxTrade**, not MT4/MT5). Store it in `.env`
(example in [`.env.example`](.env.example), gitignored).

```bash
set -a; . ./.env; set +a          # load OANDA_TOKEN, OANDA_ACCOUNT_ID, OANDA_ENV

go run ./cmd/fetch                # smoke-test token + download XAU_USD W/D/H4/H1/M15/M5 (defaults from 2022-01-01)
go run ./cmd/fetch -from 2020-01-01 -to 2024-12-31

go run ./cmd/backtest             # ~4s over the 2022→2026 cache → metrics report
go run ./cmd/backtest -out trades.csv

go build ./... && go vet ./...
```

## Tool list (`cmd/`)

| Tool | Function |
|---|---|
| `fetch` | Download & cache candles from OANDA (pagination 5000/req) |
| `backtest` | Load cache → `engine.Run` → report (Print/CSV/XLSX) |
| `sweep` | Parameter grid sweep → ranked comparison table |
| `walkforward` | Walk-forward IS→OOS split (overfit validation) |
| `entries` | Report the reason for each entry + outcome (+ SVG per entry) |
| `gatestats` | Kill-rate funnel for each gate |
| `narrate` | Pyramid scan narration + chart SVG at a single point in time |
| `dayscan` | Diagnostic of AMD Manipulation phase character |
| `alertd` | Realtime daemon → Telegram alert when a new setup appears |
| `newsalert` | Economic news release alert (CPI/PPI/NFP) → gold bias |
| `qtscan` `amscan` `q1sweep` `ithfreq` `nfpqt` `fomcscan` | Per-component diagnostics |

## Realtime alert daemon (`cmd/alertd`)

Polls OANDA every interval → refreshes the cache → `engine.Run` → sends a Telegram alert when a new
layer signal appears (persistent dedup + freshness guard, read-only to OANDA).

```bash
set -a; . ./.env; set +a          # + TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID
go run ./cmd/alertd -once         # one cycle then exit (manual check)
go run ./cmd/alertd               # daemon loop (default every 5m)
```

Production runtime: launchd on Mac (`id.zihar.forex-alertd`) / systemd on Linux
(see [`deploy/`](deploy/)).

## Key conventions

- **Std-lib only** — the OANDA client uses `net/http` + `encoding/json`, not an SDK.
- **Read-only to OANDA** — only pulls historical data, no order/trade endpoint.
- **Daily candle anchored at 18:00 NY**, only stores `complete: true` candles, midpoint price, RFC3339 UTC time.
- **Credentials only from env/`.env`** — never hard-code/commit. `data/`, `.env`, `*.pem` are gitignored.
- **The engine ↔ narrate mirror must be 0-drift** + knob-off parity when adding a feature/gate.

## Structure

```
cmd/                  # entrypoint per tool (fetch, backtest, sweep, walkforward, alertd, ...)
internal/
  oanda/              # v20 REST read-only client (HTTP + Bearer auth)
  data/               # Candle domain type + CSV cache (atomic temp+rename)
  detectors/          # layers A, C, D, F, G/H/I
  state/              # layers B, E
  engine/             # N.2 pipeline + K simulation + I sizing + Narrate
  report/             # Section L metrics + breakdown + CSV/XLSX writer
  news/               # economic calendar + surprise classification → XAUUSD bias
  viz/ chartann/      # render candlestick + annotations to SVG
  config/             # flat-YAML loader (mirrors DefaultConfig)
deploy/               # systemd units + launchd wrapper
config.yaml           # mirrors engine.DefaultConfig()
CLAUDE.md             # instructions + current state
DECISIONS.md          # full experiment & decision history
```

---

Go 1.26 · std-lib only · communication language Indonesian (trading terms & identifiers stay English).
