#!/usr/bin/env bash
# Setup auto-deploy pull-based di EC2. Jalankan: sudo bash setup-autodeploy.sh
#
# Repo PUBLIC → fetch anonim via HTTPS (tanpa deploy key / kredensial apa pun).
# Idempotent & re-runnable: install Go, clone/sinkron repo, pasang timer.
#
# Setelah ini, cukup `git push` dari mesinmu; EC2 cek origin/main tiap 1 menit,
# build arm64 + restart forex-alertd otomatis saat ada commit baru.
set -euo pipefail

RUNTIME=/opt/forex
REPO="$RUNTIME/repo"
REPO_URL="https://github.com/zihar/xau-ict-engine.git"
BRANCH=main
GO_VERSION=1.26.3
GO_TARBALL="go${GO_VERSION}.linux-arm64.tar.gz"

[ "$(id -u)" -eq 0 ] || { echo "Jalankan dengan sudo/root."; exit 1; }

# Tool dasar.
if ! command -v git >/dev/null || ! command -v curl >/dev/null; then
	apt-get update -qq
	apt-get install -y -qq git curl
fi

mkdir -p "$RUNTIME"

# 1) Install Go (native arm64) kalau belum ada.
if [ ! -x /usr/local/go/bin/go ]; then
	echo "Install Go ${GO_VERSION}..."
	curl -fsSL -o "/tmp/${GO_TARBALL}" "https://go.dev/dl/${GO_TARBALL}"
	rm -rf /usr/local/go
	tar -C /usr/local -xzf "/tmp/${GO_TARBALL}"
	rm -f "/tmp/${GO_TARBALL}"
fi

# 2) Clone repo (atau pastikan remote benar kalau sudah ada).
if [ ! -d "$REPO/.git" ]; then
	git clone --branch "$BRANCH" "$REPO_URL" "$REPO"
else
	git -C "$REPO" remote set-url origin "$REPO_URL"
	git -C "$REPO" fetch --quiet origin "$BRANCH"
	git -C "$REPO" reset --hard "origin/$BRANCH"
fi

# 3) Pasang skrip deploy + unit + timer.
install -m 0755 "$REPO/deploy/deploy.sh"             "$RUNTIME/deploy.sh"
install -m 0644 "$REPO/deploy/forex-deploy.service"  /etc/systemd/system/forex-deploy.service
install -m 0644 "$REPO/deploy/forex-deploy.timer"    /etc/systemd/system/forex-deploy.timer
systemctl daemon-reload
systemctl enable --now forex-deploy.timer

echo
echo "Setup selesai. Timer aktif:"
systemctl list-timers forex-deploy.timer --no-pager || true
echo
echo "Build perdana sekarang (sinkronkan binary ke commit terbaru):"
"$RUNTIME/deploy.sh" || true
echo
echo "Pantau: journalctl -u forex-deploy -f   |   journalctl -u forex-alertd -f"
