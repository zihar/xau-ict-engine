# Deploy `newsalert` — economic news release alert (CPI/PPI/NFP) for XAUUSD

`cmd/newsalert` pulls the economic calendar (Forex Factory weekly JSON, free, no
key), then sends a Telegram alert:

- **HEADS-UP (pre-release)** — when the release is ≤ `-prewindow` away (default 60m):
  forecast vs previous + leading nowcast (optional) + key levels.
- **POST-RELEASE** — when the feed populates `actual`: actual vs forecast → surprise →
  XAUUSD bias via the playbook (2026 regime: hot/strong data = bearish gold).

Persistent dedup (`news_state.json`) → safe to run every 5 minutes by a timer.
Read-only: no order execution.

## Check locally first (without sending)

```bash
set -a; . ./.env; set +a
go run ./cmd/newsalert -event CPI,PPI -dry -prewindow 96h      # render the heads-up message
# Preview the POST-RELEASE message with hypothetical numbers (no send, no state touched):
go run ./cmd/newsalert -event CPI -simulate "CPI m/m=0.5%,CPI y/y=4.5%,Core CPI m/m=0.6%,Core CPI y/y=3.0%"
```

⚠️ The Forex Factory feed is **rate-limited** (HTTP 429) if polled too frequently —
do not run it more often than ~1×/minute. Every 5 minutes is safe.

## BLS fallback (`-bls`) — official numbers when the FF mirror is late

The faireconomy mirror sometimes **populates `actual` late** (confirmed CPI 10 Jun: 50+
minutes post-release, `actual` still empty). `-bls` patches this by pulling the numbers
from the **official U.S. Bureau of Labor Statistics API** (the original publisher of CPI/PPI/NFP) —
available immediately at release time (08:30 ET), free, JSON, std-lib.

```bash
go run ./cmd/newsalert -event CPI -bls -dry -stale 24h   # check (pull actual from BLS)
```

- Only fills events whose `actual` is still empty, and **only for the release's reference
  month** (a guard prevents premature/wrong-month fills).
- m/m & y/y are **computed from the BLS index** (3-decimal precision) → may be ±0.1 from the
  rounded official figures. The message marks the source via a footer.
- BLS without a key = **25 queries/day**; a free registration → 500/day (`BLS_API_KEY` env
  or `-bls-key`). Because the fallback is only called in the post-release window until
  `actual` arrives and then dedup stops, normal usage is well under the limit.
- In `alertd`: flag `-news-bls` (+ `-news-bls-key`). Already active in
  `deploy/forex-alertd.service`.

## Deploy to AWS (systemd timer, alongside alertd)

The prod box = EC2 ARM (t4g) Singapore, `/opt/forex`, user `ubuntu`, `.env` at
`/opt/forex/.env` (already has `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID`).

```bash
# 1) cross-compile arm64 (from the Mac) then copy
GOOS=linux GOARCH=arm64 go build -o bin/newsalert ./cmd/newsalert
scp -i forex-key.pem bin/newsalert ubuntu@<IP>:/opt/forex/newsalert
scp -i forex-key.pem deploy/forex-newsalert.{service,timer} ubuntu@<IP>:/tmp/

# 2) install the units + enable the timer (on the server)
sudo mv /tmp/forex-newsalert.service /tmp/forex-newsalert.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forex-newsalert.timer

# 3) check
systemctl list-timers forex-newsalert.timer       # when it fires next
sudo systemctl start forex-newsalert.service       # force one evaluation now
journalctl -u forex-newsalert.service -n 30 --no-pager
```

The timer fires every 5 minutes (`OnCalendar=*:0/5`, `Persistent=true` → a run
missed during a reboot still runs). It self-decides pre/post + dedup → DST-proof
(release time is taken from the feed timestamp, not hardcoded).

### Release day: fill in the nowcast (optional, "pre-release heads-up")

The Cleveland Fed nowcast (CPI) / ADP (NFP) are not auto-fetched yet. On the day,
you can override manually via the service env or run once manually:

```bash
sudo systemctl stop forex-newsalert.timer    # optional, avoid a clash
/opt/forex/newsalert -event CPI \
  -nowcast "Cleveland Fed: m/m +0.32%, y/y 4.1%" -prewindow 6h
sudo systemctl start forex-newsalert.timer
```

## Alternative: run via `alertd` with `-news` (1 daemon)

Since 2026-06-08 `alertd` can also send news alerts (reusing `internal/news`)
→ just **1 daemon** (no separate `newsalert` timer/service needed). **Default
`-news=false` = parity** (exactly the old alertd behavior; no news feed fetch).
Enable it by adding `-news` (+ optional `-news-*` flags). News dedup uses a
SEPARATE file (`data/news_state.json`) from alertd's `alert_state.json`.

```ini
# in forex-alertd.service, add the -news flag to ExecStart:
ExecStart=/opt/forex/alertd -news -news-events CPI,PPI,NFP -news-state /opt/forex/data/news_state.json
```

alertd flags: `-news` (bool, default false), `-news-events` (CPI,PPI,NFP),
`-news-prewindow` (60m), `-news-stale` (12h), `-news-nowcast`, `-news-state`,
`-news-feedcache`, `-news-support`, `-news-bls` (BLS fallback), `-news-bls-key`.
News is evaluated on every alertd tick (default 5m), independent of the OANDA pipeline
(it keeps running even if the engine/cache has issues).

## Local option (Mac) — cron

If you want to test on the Mac without systemd (the Mac alertd is already disabled, prod is on AWS):

```bash
# crontab -e — every 5 minutes, load env then evaluate
*/5 * * * * cd ~/Documents/xau-ict-engine && set -a && . ./.env && set +a && ./bin/newsalert -event CPI,PPI -state data/news_state.json >> /tmp/newsalert.log 2>&1
```

## Key flags

| Flag | Default | Function |
|------|---------|--------|
| `-event` | `CPI` | indicators to watch (CSV): `CPI,PPI,NFP` |
| `-prewindow` | `60m` | send the heads-up when the release is ≤ this away |
| `-stale` | `12h` | don't send post-release if the release is already older than this |
| `-nowcast` | `""` | leading indicator note for pre-release (manual) |
| `-support` | `$4.350–4.500` | key price zone in the message |
| `-dry` | `false` | print to stdout, don't send (no TG env needed) |
| `-simulate` | `""` | preview post-release with hypothetical actuals `Title=value,...` |
| `-interval` | `0` | loop every duration (alternative to a timer); `0` = once |
