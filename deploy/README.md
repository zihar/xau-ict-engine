# Deploy forex-alertd ke Oracle Cloud Free Tier (region Singapore)

Panduan men-deploy daemon alert realtime `alertd` ke VM **Oracle Cloud Always Free**
(ARM Ampere) di region **Singapore**. Alasan pilih SG: dari IP Indonesia, koneksi ke
OANDA v20 sering labil; menjalankan daemon di region SG bikin reachability OANDA stabil
**tanpa perlu VPN**.

Daemon di-manage lewat **Tailscale** sehingga **tidak perlu membuka port inbound** apa pun
di VM (semua akses SSH/manajemen lewat alamat tailnet privat).

---

## 1. Provisioning VM (Always Free, ARM)

1. Login ke Oracle Cloud Console. Pilih region **Singapore (ap-singapore-1)** di pojok
   kanan atas.
2. **Compute > Instances > Create Instance.**
3. Shape: **VM.Standard.A1.Flex** (ARM Ampere — masuk kuota Always Free, mis. 1–4 OCPU /
   6–24 GB RAM). Untuk daemon ringan cukup 1 OCPU / 6 GB.
4. Image: **Ubuntu** (mis. Ubuntu 22.04/24.04 LTS, varian aarch64/ARM).
5. Tambahkan SSH public key milikmu (untuk akses pertama kali sebelum Tailscale aktif).
6. Networking: biarkan default. Karena manajemen lewat Tailscale, **tidak perlu**
   menambah ingress rule selain SSH bootstrap (bahkan SSH publik bisa ditutup setelah
   Tailscale jalan).

### CAVEAT Always Free (penting)

- **"Out of capacity"**: kapasitas shape A1 ARM di Always Free sering habis. Kalau gagal,
  **retry** beberapa kali, atau coba **Availability Domain (AD) lain** di region SG.
- **Idle reclaim**: instance Always Free yang dianggap idle bisa **di-reclaim** Oracle.
  Solusi: **upgrade akun ke Pay-As-You-Go (PAYG)**. Resource Always Free **tetap gratis**
  selama dalam batas Always Free, tapi instance tidak akan di-reclaim karena idle.

---

## 2. Cross-compile binary dari Mac

Daemon di-build untuk arsitektur ARM64 Linux dari mesin lokal (Mac):

```bash
cd ~/Documents/forex-backtest
GOOS=linux GOARCH=arm64 go build -o alertd ./cmd/alertd
```

Hasilnya binary `alertd` (statik, std-lib only) siap dijalankan di VM ARM Ubuntu.

---

## 3. Pasang Tailscale di VM

Akses pertama lewat SSH (IP publik + SSH key bootstrap), lalu pasang Tailscale:

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

Ikuti URL login untuk menautkan VM ke tailnet. Catat **alamat Tailscale** VM
(mis. `tailscale ip -4`). Setelah ini semua manajemen (SSH, scp) cukup lewat alamat
tailnet privat — **tanpa membuka port inbound** apa pun.

---

## 4. Kirim file ke VM via scp (lewat Tailscale)

Siapkan direktori `/opt/forex` di VM, lalu kirim binary + data + konfigurasi.
Ganti `<TAILSCALE_IP>` dengan alamat tailnet VM.

```bash
# di VM (sekali):
sudo mkdir -p /opt/forex/data
sudo chown -R ubuntu:ubuntu /opt/forex

# dari Mac:
scp alertd            ubuntu@<TAILSCALE_IP>:/opt/forex/
scp -r data           ubuntu@<TAILSCALE_IP>:/opt/forex/      # cache candle awal
scp .env              ubuntu@<TAILSCALE_IP>:/opt/forex/
scp config.yaml       ubuntu@<TAILSCALE_IP>:/opt/forex/
```

Pastikan layout akhir di VM:

```
/opt/forex/
  alertd
  config.yaml
  .env
  data/                 # termasuk alert_state.json (dibuat otomatis oleh alertd)
```

---

## 5. Kredensial Telegram

Daemon mengirim alert via bot Telegram. Dua nilai berikut dimasukkan ke `/opt/forex/.env`
(lihat `.env.example` di root repo):

- **TELEGRAM_BOT_TOKEN** — buat bot baru lewat **@BotFather** di Telegram
  (`/newbot` → ikuti prompt → BotFather kasih token).
- **TELEGRAM_CHAT_ID** — chat ID tujuan alert. Dua cara:
  - Kirim pesan apa pun ke bot kamu, lalu buka
    `https://api.telegram.org/bot<TOKEN>/getUpdates` dan baca `message.chat.id`.
  - Atau chat ke **@userinfobot** yang langsung menampilkan ID kamu.

Contoh `/opt/forex/.env` (lengkapi nilai yang masih kosong):

```
OANDA_TOKEN=...
OANDA_ACCOUNT_ID=...
OANDA_ENV=practice
TELEGRAM_BOT_TOKEN=...
TELEGRAM_CHAT_ID=...
```

---

## 6. Pastikan time sync aktif (wajib untuk align candle 5m)

Alignment candle 5 menit bergantung pada jam sistem yang akurat. Pastikan time sync hidup:

```bash
# systemd-timesyncd (default Ubuntu):
sudo systemctl enable --now systemd-timesyncd
timedatectl status        # cek "System clock synchronized: yes"

# alternatif: chrony
# sudo apt-get install -y chrony && sudo systemctl enable --now chrony
```

---

## 7. Setup systemd service

Salin unit ke systemd, reload, lalu enable + start:

```bash
sudo cp /opt/forex/deploy/forex-alertd.service /etc/systemd/system/   # atau scp unit-nya
sudo systemctl daemon-reload
sudo systemctl enable --now forex-alertd
```

Pantau log:

```bash
journalctl -u forex-alertd -f
```

Unit `forex-alertd.service` menjalankan:

```
/opt/forex/alertd -dir /opt/forex/data -config /opt/forex/config.yaml -state /opt/forex/data/alert_state.json
```

dengan `Restart=always` / `RestartSec=10` sehingga daemon otomatis bangkit lagi setelah
crash atau reboot.

---

## Ringkasan operasional

- **Akses VM**: hanya lewat Tailscale (tanpa port inbound publik).
- **Update binary**: build ulang di Mac (`GOOS=linux GOARCH=arm64`), `scp` ke
  `/opt/forex/alertd`, lalu `sudo systemctl restart forex-alertd`.
- **State alert** (`data/alert_state.json`) persisten antar-run → dedup alert tetap jalan
  meski daemon restart.
- **OANDA read-only**: daemon hanya menarik data; tidak ada eksekusi order.
