#!/bin/zsh
# pagi.sh — nyalakan stack alert pagi hari dalam SATU perintah:
#   1. stop daemon launchd (kalau jalan) — hindari race tulis cache/state dgn langkah 2
#   2. alertd -once: fetch incremental candle yang tertinggal semalam + langsung
#      kirim watchlist pagi ke Telegram (diff vs kondisi sebelum offline, dengan
#      catatan "🕘 daemon sempat offline ≈X jam") — tanpa menunggu tick pertama
#      daemon (yang baru jalan ≤ interval+20s ≈ 5 menit)
#   3. start ulang daemon launchd (RunAtLoad+KeepAlive — jalan terus sampai shutdown)
#
# Catatan: launchd (RunAtLoad) sebenarnya SUDAH otomatis menyalakan alertd saat
# login — skrip ini untuk: hasil fetch + pesan pagi yang instan, dan memastikan
# agent ter-load lagi kalau sebelumnya sempat di-bootout manual.
#
# Pakai: ~/Documents/xau-ict-engine/scripts/pagi.sh
set -euo pipefail

LABEL=id.zihar.forex-alertd
RUNTIME="$HOME/.forex-alertd"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
GUI="gui/$(id -u)"

[[ -x "$RUNTIME/alertd" ]] || { echo "✗ binary $RUNTIME/alertd tidak ada"; exit 1; }
[[ -f "$RUNTIME/.env" ]]   || { echo "✗ $RUNTIME/.env tidak ada (OANDA/TELEGRAM env)"; exit 1; }

echo "== 1/3 stop daemon (kalau jalan) =="
if launchctl bootout "$GUI/$LABEL" 2>/dev/null; then
    echo "daemon distop sementara"
else
    echo "daemon memang belum jalan"
fi

echo "== 2/3 fetch data tertinggal + kirim watchlist pagi (alertd -once) =="
cd "$RUNTIME"
set -a; . ./.env; set +a
./alertd -once

echo "== 3/3 start daemon =="
launchctl bootstrap "$GUI" "$PLIST"
sleep 1
launchctl print "$GUI/$LABEL" | grep -E 'state =|pid =' || true
echo "✓ Selesai. Log daemon: tail -f $RUNTIME/logs/alertd.log"
