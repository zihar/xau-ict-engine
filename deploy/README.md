# Deploy forex-alertd to Oracle Cloud Free Tier (Singapore region)

A guide to deploying the realtime alert daemon `alertd` to an **Oracle Cloud Always Free**
VM (ARM Ampere) in the **Singapore** region. Why SG: from an Indonesian IP, the connection to
OANDA v20 is often flaky; running the daemon in the SG region makes OANDA reachability stable
**without needing a VPN**.

The daemon is managed via **Tailscale**, so there is **no need to open any inbound port**
on the VM (all SSH/management access goes through the private tailnet address).

---

## 1. VM provisioning (Always Free, ARM)

1. Log in to the Oracle Cloud Console. Select the **Singapore (ap-singapore-1)** region in the
   top right corner.
2. **Compute > Instances > Create Instance.**
3. Shape: **VM.Standard.A1.Flex** (ARM Ampere — within the Always Free quota, e.g. 1–4 OCPU /
   6–24 GB RAM). For a lightweight daemon, 1 OCPU / 6 GB is enough.
4. Image: **Ubuntu** (e.g. Ubuntu 22.04/24.04 LTS, the aarch64/ARM variant).
5. Add your own SSH public key (for the first access before Tailscale is active).
6. Networking: leave the defaults. Because management goes through Tailscale, there is **no need**
   to add an ingress rule beyond the SSH bootstrap (the public SSH can even be closed once
   Tailscale is running).

### Always Free CAVEAT (important)

- **"Out of capacity"**: the A1 ARM shape capacity in Always Free is often exhausted. If it fails,
  **retry** a few times, or try **another Availability Domain (AD)** in the SG region.
- **Idle reclaim**: an Always Free instance deemed idle can be **reclaimed** by Oracle.
  Solution: **upgrade the account to Pay-As-You-Go (PAYG)**. Always Free resources **stay free**
  as long as they are within the Always Free limits, but the instance will not be reclaimed for being idle.

---

## 2. Cross-compile the binary from the Mac

The daemon is built for the ARM64 Linux architecture from the local machine (Mac):

```bash
cd ~/Documents/xau-ict-engine
GOOS=linux GOARCH=arm64 go build -o alertd ./cmd/alertd
```

The result is an `alertd` binary (static, std-lib only) ready to run on the ARM Ubuntu VM.

---

## 3. Install Tailscale on the VM

First access via SSH (public IP + SSH key bootstrap), then install Tailscale:

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

Follow the login URL to link the VM to the tailnet. Note the VM's **Tailscale address**
(e.g. `tailscale ip -4`). After this, all management (SSH, scp) goes through the private
tailnet address — **without opening any inbound port**.

---

## 4. Send files to the VM via scp (over Tailscale)

Prepare the `/opt/forex` directory on the VM, then send the binary + data + configuration.
Replace `<TAILSCALE_IP>` with the VM's tailnet address.

```bash
# on the VM (once):
sudo mkdir -p /opt/forex/data
sudo chown -R ubuntu:ubuntu /opt/forex

# from the Mac:
scp alertd            ubuntu@<TAILSCALE_IP>:/opt/forex/
scp -r data           ubuntu@<TAILSCALE_IP>:/opt/forex/      # initial candle cache
scp .env              ubuntu@<TAILSCALE_IP>:/opt/forex/
scp config.yaml       ubuntu@<TAILSCALE_IP>:/opt/forex/
```

Make sure the final layout on the VM is:

```
/opt/forex/
  alertd
  config.yaml
  .env
  data/                 # including alert_state.json (created automatically by alertd)
```

---

## 5. Telegram credentials

The daemon sends alerts via a Telegram bot. The following two values go into `/opt/forex/.env`
(see `.env.example` in the repo root):

- **TELEGRAM_BOT_TOKEN** — create a new bot via **@BotFather** on Telegram
  (`/newbot` → follow the prompts → BotFather gives you a token).
- **TELEGRAM_CHAT_ID** — the destination chat ID for alerts. Two ways:
  - Send any message to your bot, then open
    `https://api.telegram.org/bot<TOKEN>/getUpdates` and read `message.chat.id`.
  - Or chat with **@userinfobot**, which shows your ID directly.

Example `/opt/forex/.env` (fill in the values that are still empty):

```
OANDA_TOKEN=...
OANDA_ACCOUNT_ID=...
OANDA_ENV=practice
TELEGRAM_BOT_TOKEN=...
TELEGRAM_CHAT_ID=...
```

---

## 6. Make sure time sync is active (required to align 5m candles)

The 5-minute candle alignment depends on an accurate system clock. Make sure time sync is running:

```bash
# systemd-timesyncd (Ubuntu default):
sudo systemctl enable --now systemd-timesyncd
timedatectl status        # check "System clock synchronized: yes"

# alternative: chrony
# sudo apt-get install -y chrony && sudo systemctl enable --now chrony
```

---

## 7. Set up the systemd service

Copy the unit into systemd, reload, then enable + start:

```bash
sudo cp /opt/forex/deploy/forex-alertd.service /etc/systemd/system/   # or scp the unit
sudo systemctl daemon-reload
sudo systemctl enable --now forex-alertd
```

Monitor the logs:

```bash
journalctl -u forex-alertd -f
```

The `forex-alertd.service` unit runs:

```
/opt/forex/alertd -dir /opt/forex/data -config /opt/forex/config.yaml -state /opt/forex/data/alert_state.json
```

with `Restart=always` / `RestartSec=10` so the daemon comes back automatically after
a crash or reboot.

---

## Operational summary

- **VM access**: only via Tailscale (no public inbound port).
- **Updating the binary**: rebuild on the Mac (`GOOS=linux GOARCH=arm64`), `scp` to
  `/opt/forex/alertd`, then `sudo systemctl restart forex-alertd`.
- **Alert state** (`data/alert_state.json`) persists across runs → alert dedup keeps working
  even when the daemon restarts.
- **OANDA read-only**: the daemon only pulls data; there is no order execution.
