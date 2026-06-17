# Deploy forex-alertd to AWS EC2 Free Trial (Singapore region)

A guide to deploying the realtime alert daemon `alertd` to an **AWS EC2** VM in the
**Singapore (ap-southeast-1)** region. Why SG: from an Indonesian IP the connection to OANDA v20
is often flaky; the daemon in the SG region makes OANDA reachability stable **without a VPN**.

> **Why AWS (not Oracle/Azure):** Oracle was rejected by anti-fraud; Azure is not eligible for the free
> tier (only PAYG ~$12/mo). A new AWS **Free Plan** account (post Jul-2025) gets **~$120 in credits**, and
> **`t4g.small` has a 750 hr/mo free trial → compute $0** (verified: `BoxUsage:t4g.small`=$0).
> ⚠️ This trial is **specific to `t4g.small`** (NOT `t4g.micro`). `t4g.small` = ARM/aarch64 → cross-compile `arm64`.
>
> **⚠️ BINDING DEADLINE = 6 Dec 2026** (not 31 Dec). Free Plan dashboard: "183 days remaining,
> Dec 06 2026 — access ends when credits depleted or free period ends". Both the credits & the free period
> end on **6 Dec 2026**. See §8 below.
>
> **The archived Oracle setup is in [`README.md`](README.md)** (kept as a reference).

**Actual status:** deployed 2026-06-06. Public IP `<EC2_PUBLIC_IP>`, user `ubuntu`,
key `~/Projects/xau-ict-engine/forex-key.pem`. The Mac daemon (launchd) was turned off when moving here.

> **⚠️ SSH access from a network that blocks port 22:** some networks block outbound
> **port 22** (symptom: `nc <EC2_PUBLIC_IP> 22` times out, but `:443` succeeds via a proxy). From
> such a network, SSH MUST go through a **VPN or mobile phone tethering**. The public IP is dynamic →
> each time you change networks, **add the new IP to the security group first** (see §Operational summary)
> before SSH will work.

---

## 1. VM provisioning (EC2 t4g.small)

AWS Console → make sure the top-right region = **Asia Pacific (Singapore) ap-southeast-1** →
**EC2 → Launch instance**:

1. **Name:** `forex-alertd`.
2. **AMI:** Ubuntu Server 24.04 LTS, **64-bit (Arm)** architecture — Arm is required (t4g = ARM).
3. **Instance type:** **`t4g.small`** (2 vCPU / 2 GB). ⚠️ DO NOT use `t4g.micro` — the one with the
   free trial through Dec 2026 is `t4g.small`.
4. **Key pair:** Create new key pair (RSA, `.pem`) → download & save (`forex-key.pem`).
5. **Network/Security group:** allow **SSH (22)** from **My IP**. No need to open other ports
   (the daemon is outbound-only).
6. **Storage:** **8 GiB gp3** (the OANDA cache is only ~26 MB, growing ~7 MB/yr → 8 GB is more than enough).
7. **Launch instance.** Note the **Public IPv4**.

### Cost CAVEAT (important)

- **Free Plan + credits end 6 Dec 2026.** After that you MUST upgrade to a Paid plan → PAYG **~$16/mo**
  (t4g.small compute ~$12 + IPv4 ~$3.6 + EBS ~$0.64) or move to Vultr SG ~$5/mo. See §8.
- **Current running cost ~$4.2/mo gross** (IPv4 ~$3.6 + EBS 8GB ~$0.64; compute $0 via trial),
  **all covered by credits → net $0**. Until the deadline only a small fraction of the credit is used →
  the rest **expires** (what binds is the date, not the balance).
- **Check cost via CLI:** `get-cost-and-usage` grouped by SERVICE with no filter nets Usage(+) & Credit(−)
  to ~$0 (misleading). For the true GROSS, filter `RECORD_TYPE=Usage`. Also set a **Budget alert**
  (Billing → Budgets → *Zero spend budget*) just in case.

---

## 2. Cross-compile the binary from the Mac (arm64)

```bash
cd ~/Projects/xau-ict-engine
GOOS=linux GOARCH=arm64 go build -o /tmp/alertd ./cmd/alertd   # t4g = ARM/aarch64
```

Static binary (std-lib only) → no need to install Go on the VM.

---

## 3. Send files to the VM (scp)

```bash
KEY=~/Projects/xau-ict-engine/forex-key.pem
HOST=ubuntu@<EC2_PUBLIC_IP>                       # replace with your Public IPv4
chmod 400 $KEY                                 # key permission (once)

# snapshot the cache (data/ is a symlink → resolve with -L)
cp -RL ~/Projects/xau-ict-engine/data/XAU_USD /tmp/forex-data

# layout on the VM (once)
ssh -i $KEY $HOST 'sudo mkdir -p /opt/forex/data/XAU_USD && sudo chown -R ubuntu:ubuntu /opt/forex'

# send binary + config + env + unit + cache
scp -i $KEY /tmp/alertd config.yaml .env deploy/forex-alertd.service $HOST:/opt/forex/
scp -i $KEY /tmp/forex-data/*.csv $HOST:/opt/forex/data/XAU_USD/
```

Final layout:

```
/opt/forex/
  alertd
  config.yaml
  .env
  data/XAU_USD/{W,D,H4,H1,M15,M5}.csv   # + alert_state.json (created automatically)
  forex-alertd.service
```

---

## 4. Telegram credentials (`/opt/forex/.env`)

Same as the Oracle setup — see [`README.md` §5](README.md). Minimum contents:

```
OANDA_TOKEN=...
OANDA_ENV=practice
TELEGRAM_BOT_TOKEN=...
TELEGRAM_CHAT_ID=...
```

---

## 5. Time sync (chrony — AWS default)

AWS Ubuntu **already uses chrony** (Amazon Time Sync `169.254.169.123`) — `systemd-timesyncd`
is NOT present in this image and **does not need** to be installed. Just verify:

```bash
timedatectl status        # check "System clock synchronized: yes" & "NTP service: active"
chronyc tracking          # optional, view the time source
```

---

## 6. Set up the systemd service

The `deploy/forex-alertd.service` unit already has `User=ubuntu` → **fits as-is** for the AWS AMI
(unlike the Azure plan, which needed `azureuser`).

```bash
ssh -i $KEY $HOST 'sudo cp /opt/forex/forex-alertd.service /etc/systemd/system/ && \
  sudo systemctl daemon-reload && sudo systemctl enable --now forex-alertd'
journalctl -u forex-alertd -f      # monitor the logs
```

`Restart=always` / `RestartSec=10` → the daemon comes back automatically after a crash/reboot.

---

## 7. End-to-end verification

```bash
ssh -i $KEY $HOST 'systemctl is-active forex-alertd'            # active
# from Telegram: send /watchlist to the bot → it should reply with the watchlist
ssh -i $KEY $HOST 'sudo reboot'                                  # test persistence
# wait ~40s, reconnect:
ssh -i $KEY $HOST 'systemctl is-active forex-alertd; uptime -p'  # active after reboot
```

---

## 8. Free Plan deadline reminder (6 Dec 2026)

The Free Plan + credits end on **6 Dec 2026** (dashboard: "183 days remaining, Dec 06 2026"). Two
reminders are set up (local/AWS, **not** claude.ai), brought forward to remind BEFORE 6 Dec:

- **AWS VM — systemd timer.** `/opt/forex/remind-trial.sh` (curl Telegram using
  `TELEGRAM_BOT_TOKEN`/`TELEGRAM_CHAT_ID` from `.env`), triggered by `forex-remind.timer`:

  ```ini
  # /etc/systemd/system/forex-remind.timer
  [Timer]
  OnCalendar=2026-12-01 01:00:00   # 08:00 WIB (VM = UTC) — 5 days before 6 Dec
  Persistent=true
  ```
  ```bash
  sudo systemctl enable --now forex-remind.timer
  systemctl list-timers forex-remind.timer
  ```
- **Mac Calendar.** Event 1 Dec 2026 08:00 + alarms 3 days before & on the day (`.ics` file `open`ed).

When the reminder fires, decide: **(a)** upgrade to a Paid plan **~$16/mo** (compute ~$12 + IPv4 ~$3.6 +
EBS ~$0.64; ⚠️ upgrade via Billing→Account, DO NOT go through joining an Organization/Control Tower → credits
expire), **(b)** move to **Vultr Singapore + PayPal** ~$5/mo permanently (identical steps, admin user
`root`), or **(c)** stop/destroy the instance. If you move/destroy: snapshot `.env` + `data/`
first; the Mac daemon can be turned back on (the plist is still there but was **disabled on 2026-06-08** →
`launchctl enable gui/$(id -u)/id.zihar.forex-alertd && launchctl bootstrap gui/$(id -u)
~/Library/LaunchAgents/id.zihar.forex-alertd.plist`).

---

## 9. Auto-deploy on `git push` (pull-based)

On every `git push` to `origin/main`, EC2 pulls & deploys itself. The repo is **public** → EC2
fetches anonymously via **HTTPS, with no deploy key / no credentials at all**, **without** opening a port
(outbound-only), AWS cost **$0** (t4g.small compute is covered by the free trial, git transfer is negligible).

**How it works:** `forex-deploy.timer` triggers `/opt/forex/deploy.sh` (as root) every **1 minute**.
Each tick `git fetch` compares the `origin/main` SHA vs local — if they match, it does nothing (sub-second).
If there is a new commit → `git reset --hard origin/main` → **build arm64 native on EC2**
(std-lib only, no module fetch) → atomic binary install → sync `config.yaml` + the
`forex-alertd.service` unit (if changed) → `systemctl restart forex-alertd`. The script also
self-updates (atomic mv), so changes to `deploy.sh` are deployed too.

### One-time setup (on EC2)

```bash
ssh -i ~/Projects/xau-ict-engine/forex-key.pem ubuntu@<EC2_PUBLIC_IP>   # via VPN/tethering from the office

# copy the setup script to the VM (or fetch it from the repo after the first clone)
scp -i ~/path/to/forex-key.pem \
  deploy/setup-autodeploy.sh ubuntu@<EC2_PUBLIC_IP>:/tmp/

# install Go, clone the repo (anonymous HTTPS), install & enable the timer, first build
sudo bash /tmp/setup-autodeploy.sh
```

Public repo → no deploy key needed. The script clones via HTTPS directly in one run.

Done — from now on just `git push`, and ~1 minute later it's live.

### Operations

```bash
systemctl list-timers forex-deploy.timer    # timer next-run
journalctl -u forex-deploy -f               # log of each deploy (build/restart)
sudo systemctl start forex-deploy.service   # force a deploy now (don't wait for a tick)
sudo systemctl disable --now forex-deploy.timer   # turn off auto-deploy (back to manual §Summary)
```

> ⚠️ The EC2 layout becomes: `/opt/forex/repo` (read-only clone) + `/opt/forex/{alertd,config.yaml,.env,data/}`
> (runtime). `.env` & `data/` are **not** in the repo → never overwritten by a deploy. Disk: the Go toolchain
> ~450 MB + build cache → check `df -h /` still has room on the 8 GB volume (Ubuntu ~3 GB; enough).

## Operational summary

- **VM access:** `ssh -i ~/Projects/xau-ict-engine/forex-key.pem ubuntu@<EC2_PUBLIC_IP>`
  (SSH 22 is restricted to whitelisted IPs; **from the office you need VPN/tethering** — port 22 is blocked).
  **Add the current IP to the whitelist** (AWS CLI, region ap-southeast-1):
  ```bash
  MYIP=$(curl -s https://api.ipify.org)
  aws ec2 authorize-security-group-ingress --group-id <SECURITY_GROUP_ID> \
    --ip-permissions "IpProtocol=tcp,FromPort=22,ToPort=22,IpRanges=[{CidrIp=$MYIP/32,Description=zihar}]"
  # view rules: aws ec2 describe-security-groups --group-ids <SECURITY_GROUP_ID> \
  #   --query 'SecurityGroups[].IpPermissions[].IpRanges[]' --output table
  ```
- **Updating the binary:**
  ```bash
  GOOS=linux GOARCH=arm64 go build -o /tmp/alertd ./cmd/alertd
  scp -i $KEY /tmp/alertd ubuntu@<EC2_PUBLIC_IP>:/opt/forex/
  ssh -i $KEY ubuntu@<EC2_PUBLIC_IP> 'sudo systemctl restart forex-alertd'
  ```
- **Alert state** (`data/XAU_USD/alert_state.json`) persists across runs → dedup keeps working.
- **OANDA read-only:** the daemon only pulls data; there is no order execution.
- **Cost savings:** only 1 instance (750 hr/mo free trial ≈ 1 VM 24/7; a second instance = over quota).
