#!/usr/bin/env bash
# Auto-deploy pull-based untuk forex-alertd.
# Dipanggil forex-deploy.timer tiap 1 menit (sebagai root) via /opt/forex/deploy.sh.
# Idempotent: hanya build + restart kalau ada commit baru di origin/<BRANCH>.
# Build native arm64 di EC2 (std-lib only → tak perlu network module fetch).
set -euo pipefail

RUNTIME=/opt/forex
REPO="$RUNTIME/repo"
KEY="$RUNTIME/deploy_key"
BRANCH=main
GO=/usr/local/go/bin/go

export HOME=/root   # GOCACHE/GOMODCACHE default (build cache persisten antar-run)
export GIT_SSH_COMMAND="ssh -i $KEY -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"

cd "$REPO"

git fetch --quiet origin "$BRANCH"
LOCAL=$(git rev-parse HEAD)
REMOTE=$(git rev-parse "origin/$BRANCH")

# Tidak ada commit baru → diam (tick murah, sub-detik).
[ "$LOCAL" = "$REMOTE" ] && exit 0

ts() { date -u +%FT%TZ; }
echo "[$(ts)] deploy: ${LOCAL:0:8} -> ${REMOTE:0:8}"

# FF hard ke remote (ini deploy target read-only → buang perubahan lokal liar).
git reset --hard "origin/$BRANCH"

# Build ke temp lalu install atomik (binary lama tetap jalan kalau build gagal: set -e).
"$GO" build -o /tmp/alertd.new ./cmd/alertd
install -m 0755 /tmp/alertd.new "$RUNTIME/alertd"
rm -f /tmp/alertd.new

# Sync config.yaml dari repo (alertd dijalankan dgn -config /opt/forex/config.yaml).
install -m 0644 "$REPO/config.yaml" "$RUNTIME/config.yaml"

# Sync unit alertd kalau berubah (mis. flag ExecStart baru) → daemon-reload.
if ! cmp -s "$REPO/deploy/forex-alertd.service" /etc/systemd/system/forex-alertd.service; then
	install -m 0644 "$REPO/deploy/forex-alertd.service" /etc/systemd/system/forex-alertd.service
	systemctl daemon-reload
	echo "[$(ts)] unit forex-alertd.service diperbarui"
fi

systemctl restart forex-alertd

# Self-update skrip ini untuk run berikutnya (mv = atomik → run saat ini aman).
install -m 0755 "$REPO/deploy/deploy.sh" /tmp/deploy.sh.new
mv /tmp/deploy.sh.new "$RUNTIME/deploy.sh"

echo "[$(ts)] deploy OK @ $(git rev-parse --short HEAD) — forex-alertd: $(systemctl is-active forex-alertd)"
