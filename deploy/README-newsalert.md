# Deploy `newsalert` — alert rilis berita ekonomi (CPI/PPI/NFP) untuk XAUUSD

`cmd/newsalert` menarik kalender ekonomi (Forex Factory weekly JSON, gratis tanpa
key), lalu kirim alert Telegram:

- **ANCANG-ANCANG (pra-rilis)** — saat rilis tinggal ≤ `-prewindow` (default 60m):
  forecast vs previous + nowcast leading (opsional) + level kunci.
- **PASCA-RILIS** — saat feed mengisi `actual`: actual vs forecast → surprise →
  bias XAUUSD via playbook (rezim 2026: data panas/kuat = bearish gold).

Dedup persisten (`news_state.json`) → aman dijalankan tiap 5 menit oleh timer.
Read-only: tidak ada eksekusi order.

## Cek lokal dulu (tanpa kirim)

```bash
set -a; . ./.env; set +a
go run ./cmd/newsalert -event CPI,PPI -dry -prewindow 96h      # render pesan ancang-ancang
# Preview pesan PASCA-RILIS dengan angka hipotetis (tak kirim, tak sentuh state):
go run ./cmd/newsalert -event CPI -simulate "CPI m/m=0.5%,CPI y/y=4.5%,Core CPI m/m=0.6%,Core CPI y/y=3.0%"
```

⚠️ Feed Forex Factory **rate-limit** (HTTP 429) bila di-poll terlalu rapat —
jangan jalankan lebih sering dari ~1×/menit. Tiap 5 menit aman.

## Fallback BLS (`-bls`) — angka resmi saat mirror FF telat

Mirror faireconomy kadang **telat mengisi `actual`** (terbukti CPI 10 Jun: 50+
menit pasca-rilis `actual` masih kosong). `-bls` menambal ini dengan menarik angka
dari **API resmi U.S. Bureau of Labor Statistics** (penerbit asli CPI/PPI/NFP) —
tersedia seketika di jam rilis (08:30 ET), gratis, JSON, std-lib.

```bash
go run ./cmd/newsalert -event CPI -bls -dry -stale 24h   # cek (tarik actual dari BLS)
```

- Hanya mengisi event yang `actual`-nya masih kosong, dan **hanya untuk bulan
  referensi rilis** (guard cegah isian prematur/salah-bulan).
- m/m & y/y **dihitung dari indeks BLS** (presisi 3 desimal) → bisa ±0.1 dari angka
  resmi yang dibulatkan. Pesan menandai sumber via footer.
- BLS tanpa key = **25 query/hari**; daftar gratis → 500/hari (`BLS_API_KEY` env
  atau `-bls-key`). Karena fallback hanya dipanggil di jendela pasca-rilis sampai
  `actual` masuk lalu dedup berhenti, pemakaian normal jauh di bawah limit.
- Di `alertd`: flag `-news-bls` (+ `-news-bls-key`). Sudah aktif di
  `deploy/forex-alertd.service`.

## Deploy ke AWS (systemd timer, sejalan dgn alertd)

Box prod = EC2 ARM (t4g) Singapore, `/opt/forex`, user `ubuntu`, `.env` di
`/opt/forex/.env` (sudah ada `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID`).

```bash
# 1) cross-compile arm64 (dari Mac) lalu copy
GOOS=linux GOARCH=arm64 go build -o bin/newsalert ./cmd/newsalert
scp -i forex-key.pem bin/newsalert ubuntu@<IP>:/opt/forex/newsalert
scp -i forex-key.pem deploy/forex-newsalert.{service,timer} ubuntu@<IP>:/tmp/

# 2) pasang unit + aktifkan timer (di server)
sudo mv /tmp/forex-newsalert.service /tmp/forex-newsalert.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forex-newsalert.timer

# 3) cek
systemctl list-timers forex-newsalert.timer       # kapan fire berikutnya
sudo systemctl start forex-newsalert.service       # paksa satu evaluasi sekarang
journalctl -u forex-newsalert.service -n 30 --no-pager
```

Timer fire tiap 5 menit (`OnCalendar=*:0/5`, `Persistent=true` → run yang
terlewat saat reboot tetap jalan). Self-decide pra/pasca + dedup → DST-proof
(jam rilis diambil dari timestamp feed, bukan hardcode).

### Hari rilis: isi nowcast (opsional, "ancang-ancang pre-release")

Cleveland Fed nowcast (CPI) / ADP (NFP) belum di-fetch otomatis. Saat hari H,
bisa override manual lewat env service atau jalankan sekali manual:

```bash
sudo systemctl stop forex-newsalert.timer    # opsional, hindari bentrok
/opt/forex/newsalert -event CPI \
  -nowcast "Cleveland Fed: m/m +0.32%, y/y 4.1%" -prewindow 6h
sudo systemctl start forex-newsalert.timer
```

## Alternatif: jalankan via `alertd` dengan `-news` (1 daemon)

Sejak 2026-06-08 `alertd` bisa sekalian kirim alert news (reuse `internal/news`)
→ cukup **1 daemon** (tak perlu timer/service `newsalert` terpisah). **Default
`-news=false` = paritas** (perilaku alertd lama persis; tak ada fetch feed news).
Aktifkan dengan menambah `-news` (+ flag `-news-*` opsional). Dedup news pakai
file TERPISAH (`data/news_state.json`) dari `alert_state.json` alertd.

```ini
# di forex-alertd.service, tambahkan flag -news ke ExecStart:
ExecStart=/opt/forex/alertd -news -news-events CPI,PPI,NFP -news-state /opt/forex/data/news_state.json
```

Flag alertd: `-news` (bool, default false), `-news-events` (CPI,PPI,NFP),
`-news-prewindow` (60m), `-news-stale` (12h), `-news-nowcast`, `-news-state`,
`-news-feedcache`, `-news-support`, `-news-bls` (fallback BLS), `-news-bls-key`.
News dievaluasi tiap tick alertd (default 5m), independen dari pipeline OANDA
(tetap jalan walau engine/cache bermasalah).

## Opsi lokal (Mac) — cron

Kalau mau tes di Mac tanpa systemd (alertd Mac sudah di-disable, prod di AWS):

```bash
# crontab -e — tiap 5 menit, load env lalu evaluasi
*/5 * * * * cd ~/Documents/forex-backtest && set -a && . ./.env && set +a && ./bin/newsalert -event CPI,PPI -state data/news_state.json >> /tmp/newsalert.log 2>&1
```

## Flag penting

| Flag | Default | Fungsi |
|------|---------|--------|
| `-event` | `CPI` | indikator dipantau (CSV): `CPI,PPI,NFP` |
| `-prewindow` | `60m` | kirim ancang-ancang saat rilis tinggal ≤ ini |
| `-stale` | `12h` | jangan kirim pasca-rilis bila rilis sudah lebih tua dari ini |
| `-nowcast` | `""` | catatan leading indicator pra-rilis (manual) |
| `-support` | `$4.350–4.500` | zona harga kunci di pesan |
| `-dry` | `false` | cetak ke stdout, jangan kirim (tanpa env TG) |
| `-simulate` | `""` | preview pasca-rilis dgn actual hipotetis `Judul=nilai,...` |
| `-interval` | `0` | loop tiap durasi (alternatif timer); `0` = sekali |
