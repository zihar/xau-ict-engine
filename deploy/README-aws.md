# Deploy forex-alertd ke AWS EC2 Free Trial (region Singapore)

Panduan men-deploy daemon alert realtime `alertd` ke VM **AWS EC2** di region
**Singapore (ap-southeast-1)**. Alasan pilih SG: dari IP Indonesia koneksi ke OANDA v20
sering labil; daemon di region SG bikin reachability OANDA stabil **tanpa VPN**.

> **Kenapa AWS (bukan Oracle/Azure):** Oracle ditolak anti-fraud; Azure tidak eligible free
> tier (cuma PAYG ~$12/bln). AWS akun **Free Plan** baru (post Jul-2025) dapat **kredit ~$120**, dan
> **`t4g.small` punya free trial 750 jam/bln → compute $0** (terverifikasi: `BoxUsage:t4g.small`=$0).
> ⚠️ Trial ini **khusus `t4g.small`** (BUKAN `t4g.micro`). `t4g.small` = ARM/aarch64 → cross-compile `arm64`.
>
> **⚠️ DEADLINE MENGIKAT = 6 Des 2026** (bukan 31 Des). Dashboard Free Plan: "183 days remaining,
> Dec 06 2026 — access ends when credits depleted or free period ends". Kredit & free period
> dua-duanya berakhir **6 Des 2026**. Lihat §8 di bawah.
>
> **Setup arsip Oracle ada di [`README.md`](README.md)** (dipertahankan sebagai referensi).

**Status aktual:** dideploy 2026-06-06. Public IP `<EC2_PUBLIC_IP>`, user `ubuntu`,
key `~/Projects/forex-backtest/forex-key.pem`. Daemon Mac (launchd) dimatikan saat pindah ke sini.

> **⚠️ Akses SSH dari jaringan yang memblok port 22:** sebagian jaringan memblok outbound
> **port 22** (gejala: `nc <EC2_PUBLIC_IP> 22` timeout, tapi `:443` succeed lewat proxy). Dari
> jaringan seperti itu, SSH HARUS lewat **VPN atau tethering HP seluler**. IP publik dinamis →
> tiap ganti jaringan, **tambah IP baru ke security group dulu** (lihat §Ringkasan operasional)
> baru SSH jalan.

---

## 1. Provisioning VM (EC2 t4g.small)

Console AWS → pastikan region kanan-atas = **Asia Pacific (Singapore) ap-southeast-1** →
**EC2 → Launch instance**:

1. **Name:** `forex-alertd`.
2. **AMI:** Ubuntu Server 24.04 LTS, arsitektur **64-bit (Arm)** — wajib Arm (t4g = ARM).
3. **Instance type:** **`t4g.small`** (2 vCPU / 2 GB). ⚠️ JANGAN `t4g.micro` — yang punya
   free trial s/d Des 2026 adalah `t4g.small`.
4. **Key pair:** Create new key pair (RSA, `.pem`) → download & simpan (`forex-key.pem`).
5. **Network/Security group:** allow **SSH (22)** dari **My IP**. Tidak perlu buka port lain
   (daemon outbound-only).
6. **Storage:** **8 GiB gp3** (cache OANDA cuma ~26 MB, tumbuh ~7 MB/thn → 8 GB lebih dari cukup).
7. **Launch instance.** Catat **Public IPv4**.

### CAVEAT biaya (penting)

- **Free Plan + kredit berakhir 6 Des 2026.** Setelahnya HARUS upgrade Paid plan → PAYG **~$16/bln**
  (compute t4g.small ~$12 + IPv4 ~$3.6 + EBS ~$0.64) atau pindah Vultr SG ~$5/bln. Lihat §8.
- **Biaya berjalan sekarang ~$4.2/bln gross** (IPv4 ~$3.6 + EBS 8GB ~$0.64; compute $0 via trial),
  **seluruhnya ditutup kredit → net $0**. Sampai deadline cuma terpakai sebagian kecil kredit →
  sisanya **hangus** (yang mengikat = tanggalnya, bukan saldo).
- **Cek cost via CLI:** `get-cost-and-usage` group-by SERVICE tanpa filter = Usage(+) & Credit(−) net
  jadi ~$0 (menyesatkan). Untuk GROSS asli filter `RECORD_TYPE=Usage`. Set juga **Budget alert**
  (Billing → Budgets → *Zero spend budget*) untuk jaga-jaga.

---

## 2. Cross-compile binary dari Mac (arm64)

```bash
cd ~/Projects/forex-backtest
GOOS=linux GOARCH=arm64 go build -o /tmp/alertd ./cmd/alertd   # t4g = ARM/aarch64
```

Binary statik (std-lib only) → tak perlu install Go di VM.

---

## 3. Kirim file ke VM (scp)

```bash
KEY=~/Projects/forex-backtest/forex-key.pem
HOST=ubuntu@<EC2_PUBLIC_IP>                       # ganti dgn Public IPv4-mu
chmod 400 $KEY                                 # permission key (sekali)

# snapshot cache (data/ adalah symlink → resolve dgn -L)
cp -RL ~/Projects/forex-backtest/data/XAU_USD /tmp/forex-data

# layout di VM (sekali)
ssh -i $KEY $HOST 'sudo mkdir -p /opt/forex/data/XAU_USD && sudo chown -R ubuntu:ubuntu /opt/forex'

# kirim binary + config + env + unit + cache
scp -i $KEY /tmp/alertd config.yaml .env deploy/forex-alertd.service $HOST:/opt/forex/
scp -i $KEY /tmp/forex-data/*.csv $HOST:/opt/forex/data/XAU_USD/
```

Layout akhir:

```
/opt/forex/
  alertd
  config.yaml
  .env
  data/XAU_USD/{W,D,H4,H1,M15,M5}.csv   # + alert_state.json (dibuat otomatis)
  forex-alertd.service
```

---

## 4. Kredensial Telegram (`/opt/forex/.env`)

Sama seperti setup Oracle — lihat [`README.md` §5](README.md). Minimal isi:

```
OANDA_TOKEN=...
OANDA_ENV=practice
TELEGRAM_BOT_TOKEN=...
TELEGRAM_CHAT_ID=...
```

---

## 5. Time sync (chrony — bawaan AWS)

AWS Ubuntu **sudah pakai chrony** (Amazon Time Sync `169.254.169.123`) — `systemd-timesyncd`
TIDAK ada di image ini dan **tak perlu** dipasang. Cukup verifikasi:

```bash
timedatectl status        # cek "System clock synchronized: yes" & "NTP service: active"
chronyc tracking          # opsional, lihat sumber waktu
```

---

## 6. Setup systemd service

Unit `deploy/forex-alertd.service` sudah `User=ubuntu` → **cocok apa adanya** untuk AMI AWS
(beda dari rencana Azure yang butuh `azureuser`).

```bash
ssh -i $KEY $HOST 'sudo cp /opt/forex/forex-alertd.service /etc/systemd/system/ && \
  sudo systemctl daemon-reload && sudo systemctl enable --now forex-alertd'
journalctl -u forex-alertd -f      # pantau log
```

`Restart=always` / `RestartSec=10` → daemon bangkit otomatis setelah crash/reboot.

---

## 7. Verifikasi end-to-end

```bash
ssh -i $KEY $HOST 'systemctl is-active forex-alertd'            # active
# dari Telegram: kirim /watchlist ke bot → harus balas watchlist
ssh -i $KEY $HOST 'sudo reboot'                                  # uji persistence
# tunggu ~40s, reconnect:
ssh -i $KEY $HOST 'systemctl is-active forex-alertd; uptime -p'  # active setelah reboot
```

---

## 8. Reminder deadline Free Plan (6 Des 2026)

Free Plan + kredit berakhir **6 Des 2026** (dashboard: "183 days remaining, Dec 06 2026"). Dua
reminder dipasang (lokal/AWS, **bukan** claude.ai), dimajukan agar mengingatkan SEBELUM 6 Des:

- **VM AWS — systemd timer.** `/opt/forex/remind-trial.sh` (curl Telegram pakai
  `TELEGRAM_BOT_TOKEN`/`TELEGRAM_CHAT_ID` dari `.env`), dipicu `forex-remind.timer`:

  ```ini
  # /etc/systemd/system/forex-remind.timer
  [Timer]
  OnCalendar=2026-12-01 01:00:00   # 08:00 WIB (VM = UTC) — H-5 sebelum 6 Des
  Persistent=true
  ```
  ```bash
  sudo systemctl enable --now forex-remind.timer
  systemctl list-timers forex-remind.timer
  ```
- **Calendar Mac.** Event 1 Des 2026 08:00 + alarm H-3 & hari-H (file `.ics` di-`open`).

Saat reminder fire, putuskan: **(a)** upgrade Paid plan **~$16/bln** (compute ~$12 + IPv4 ~$3.6 +
EBS ~$0.64; ⚠️ upgrade via Billing→Account, JANGAN lewat join Organization/Control Tower → kredit
hangus), **(b)** pindah **Vultr Singapore + PayPal** ~$5/bln permanen (langkah identik, admin user
`root`), atau **(c)** matikan/destroy instance. Kalau pindah/destroy: snapshot `.env` + `data/`
dulu; daemon Mac bisa dihidupkan lagi (plist masih ada tapi **di-disable 2026-06-08** →
`launchctl enable gui/$(id -u)/id.zihar.forex-alertd && launchctl bootstrap gui/$(id -u)
~/Library/LaunchAgents/id.zihar.forex-alertd.plist`).

---

## 9. Auto-deploy saat `git push` (pull-based)

Tiap `git push` ke `origin/main`, EC2 menarik & deploy sendiri — **tanpa** menaruh
SSH key/AWS creds di GitHub, **tanpa** membuka port (outbound-only, kebal firewall kantor),
biaya AWS **$0** (compute t4g.small ditutup free trial, transfer git diabaikan).

**Cara kerja:** `forex-deploy.timer` memicu `/opt/forex/deploy.sh` (sbg root) tiap **1 menit**.
Tiap tick `git fetch` membandingkan SHA `origin/main` vs lokal — kalau sama, diam (sub-detik).
Kalau ada commit baru → `git reset --hard origin/main` → **build arm64 native di EC2**
(std-lib only, tak ada fetch modul) → install binary atomik → sync `config.yaml` + unit
`forex-alertd.service` (kalau berubah) → `systemctl restart forex-alertd`. Skrip ikut
self-update (mv atomik) jadi perubahan `deploy.sh` ikut ter-deploy.

### Setup sekali (di EC2)

```bash
ssh -i ~/Projects/forex-backtest/forex-key.pem ubuntu@<EC2_PUBLIC_IP>   # via VPN/tethering dari kantor

# salin skrip setup ke VM (atau ambil dari repo setelah clone perdana)
scp -i ~/Projects/forex-backtest/forex-key.pem \
  deploy/setup-autodeploy.sh ubuntu@<EC2_PUBLIC_IP>:/tmp/

# Run-1: bikin deploy key + cetak PUBLIC KEY
sudo bash /tmp/setup-autodeploy.sh
```

Run-1 mencetak **public key**. Paste ke GitHub: **repo Settings → Deploy keys → Add deploy
key**, centang **read-only** (JANGAN allow write). Lalu:

```bash
# Run-2: install Go, clone repo, pasang & enable timer, build perdana
sudo bash /tmp/setup-autodeploy.sh
```

Selesai — mulai sekarang cukup `git push`, ~1 menit kemudian live.

### Operasi

```bash
systemctl list-timers forex-deploy.timer    # next-run timer
journalctl -u forex-deploy -f               # log tiap deploy (build/restart)
sudo systemctl start forex-deploy.service   # paksa deploy sekarang (tak nunggu tick)
sudo systemctl disable --now forex-deploy.timer   # matikan auto-deploy (balik ke manual §Ringkasan)
```

> ⚠️ Layout EC2 jadi: `/opt/forex/repo` (clone read-only) + `/opt/forex/{alertd,config.yaml,.env,data/}`
> (runtime). `.env` & `data/` **tidak** di repo → tak pernah ditimpa deploy. Disk: Go toolchain
> ~450 MB + build cache → cek `df -h /` masih lega di volume 8 GB (Ubuntu ~3 GB; cukup).

## Ringkasan operasional

- **Akses VM:** `ssh -i ~/Projects/forex-backtest/forex-key.pem ubuntu@<EC2_PUBLIC_IP>`
  (SSH 22 dibatasi ke IP ter-whitelist; **dari kantor butuh VPN/tethering** — port 22 diblok).
  **Tambah IP saat ini ke whitelist** (AWS CLI, region ap-southeast-1):
  ```bash
  MYIP=$(curl -s https://api.ipify.org)
  aws ec2 authorize-security-group-ingress --group-id <SECURITY_GROUP_ID> \
    --ip-permissions "IpProtocol=tcp,FromPort=22,ToPort=22,IpRanges=[{CidrIp=$MYIP/32,Description=zihar}]"
  # lihat rules: aws ec2 describe-security-groups --group-ids <SECURITY_GROUP_ID> \
  #   --query 'SecurityGroups[].IpPermissions[].IpRanges[]' --output table
  ```
- **Update binary:**
  ```bash
  GOOS=linux GOARCH=arm64 go build -o /tmp/alertd ./cmd/alertd
  scp -i $KEY /tmp/alertd ubuntu@<EC2_PUBLIC_IP>:/opt/forex/
  ssh -i $KEY ubuntu@<EC2_PUBLIC_IP> 'sudo systemctl restart forex-alertd'
  ```
- **State alert** (`data/XAU_USD/alert_state.json`) persisten antar-run → dedup tetap jalan.
- **OANDA read-only:** daemon hanya menarik data; tidak ada eksekusi order.
- **Hemat biaya:** cuma 1 instance (750 jam/bln free trial ≈ 1 VM 24/7; instance kedua = lewat kuota).
