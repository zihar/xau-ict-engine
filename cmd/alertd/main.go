// Command alertd = daemon realtime: poll OANDA tiap interval → refresh cache
// candle → engine.Run → kirim alert Telegram saat ada SETUP baru (signal layer).
//
// Read-only ke OANDA (cuma FetchCandles); tidak ada eksekusi order. Dedup
// persisten via notify.State (file JSON) + freshness guard supaya tidak alert
// sinyal basi. Loop di-align ke batas interval + offset ~20 detik (beri waktu
// candle close di OANDA sebelum poll).
//
// Contoh:
//
//	set -a; . ./.env; set +a
//	go run ./cmd/alertd -once                 # satu siklus lalu keluar (cek manual)
//	go run ./cmd/alertd                        # daemon loop tiap 5 menit
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"xau-ict-engine/internal/config"
	"xau-ict-engine/internal/data"
	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/engine"
	"xau-ict-engine/internal/news"
	"xau-ict-engine/internal/notify"
	"xau-ict-engine/internal/oanda"
)

// granularity yang di-poll + di-cache (mirror cmd/backtest). dailyAlign=18 (18:00 NY).
var granularities = []string{"W", "D", "H4", "H1", "M15", "M5"}

const dailyAlign = 18

func main() {
	var (
		instrument  = flag.String("instrument", "XAU_USD", "instrumen")
		dir         = flag.String("dir", "data", "direktori cache CSV")
		cfgPath     = flag.String("config", "", "path config.yaml (kosong = engine.DefaultConfig)")
		interval    = flag.Duration("interval", 5*time.Minute, "jeda antar poll (loop mode)")
		once        = flag.Bool("once", false, "jalankan satu siklus lalu keluar")
		statePath   = flag.String("state", "data/alert_state.json", "file state dedup alert")
		freshness   = flag.Duration("freshness", 6*time.Minute, "umur maks sinyal terbaru agar tetap di-alert")
		fromStr     = flag.String("from", "2022-01-01", "tanggal bootstrap (YYYY-MM-DD) kalau cache kosong")
		heartbeat   = flag.Bool("heartbeat", false, "kirim watchlist tiap H1 close walau daftar tak berubah (default OFF — user 2026-06-02: anti-spam, kirim hanya saat perubahan bermakna)")
		noWatchlist = flag.Bool("no-watchlist", false, "matikan seluruh alert otomatis blok watchlist (POI per-TF + AMS ITL/ITH); /watchlist manual tetap jalan (default OFF = perilaku lama)")

		// --- News alert (reuse internal/news; default OFF = paritas perilaku lama) ---
		newsEnabled   = flag.Bool("news", false, "aktifkan alert news CPI/PPI/NFP via internal/news (default OFF = perilaku alertd lama persis)")
		newsEventsCSV = flag.String("news-events", "CPI,PPI,NFP", "indikator news dipantau (pisah koma)")
		newsPrewindow = flag.Duration("news-prewindow", 60*time.Minute, "kirim ANCANG-ANCANG news saat rilis tinggal ≤ durasi ini")
		newsStale     = flag.Duration("news-stale", 12*time.Hour, "jangan kirim PASCA-RILIS news bila rilis sudah lebih tua dari ini")
		newsNowcast   = flag.String("news-nowcast", "", "catatan leading indicator pra-rilis news (mis. Cleveland Fed nowcast)")
		newsState     = flag.String("news-state", "data/news_state.json", "file state dedup alert news (terpisah dari alert_state.json)")
		newsFeedCache = flag.String("news-feedcache", "data/news_feed_cache.json", "cache body feed news (fallback saat feed 429/down)")
		newsSupport   = flag.String("news-support", "$4.350–4.500", "zona harga kunci yang ditampilkan di pesan news")
		newsBLS       = flag.Bool("news-bls", false, "fallback API BLS (penerbit asli) untuk isi actual saat mirror FF telat publish")
		newsBLSKey    = flag.String("news-bls-key", os.Getenv("BLS_API_KEY"), "registration key BLS opsional (kosong=25 query/hari, terdaftar=500/hari)")
		newsSkip      = flag.Bool("news-skip", false, "skip-hour (08:00 NY) jadi KONDISIONAL: hanya skip bila ada rilis USD high-impact hari itu (kalender minggu berjalan; default OFF = blanket skip seperti backtest)")
	)
	flag.Parse()

	// --- Env wajib ---
	oandaToken := os.Getenv("OANDA_TOKEN")
	if oandaToken == "" {
		log.Fatal("OANDA_TOKEN kosong — set di env/.env (wajib)")
	}
	oandaEnv := os.Getenv("OANDA_ENV")
	if oandaEnv == "" {
		oandaEnv = "practice"
	}
	tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if tgToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN kosong — set di env/.env (wajib)")
	}
	tgChatID := os.Getenv("TELEGRAM_CHAT_ID")
	if tgChatID == "" {
		log.Fatal("TELEGRAM_CHAT_ID kosong — set di env/.env (wajib)")
	}

	bootstrapFrom, err := time.Parse("2006-01-02", *fromStr)
	if err != nil {
		log.Fatalf("-from %q bukan tanggal YYYY-MM-DD: %v", *fromStr, err)
	}

	// --- Config engine ---
	cfg := engine.DefaultConfig()
	if *cfgPath != "" {
		loaded, err := config.Load(*cfgPath)
		if err != nil {
			log.Fatalf("muat config: %v", err)
		}
		cfg = loaded
	}

	client := oanda.New(oandaToken, oandaEnv)
	notifier := notify.NewTelegram(tgToken, tgChatID)

	d := &daemon{
		client:        client,
		notifier:      notifier,
		cfg:           cfg,
		instrument:    *instrument,
		dir:           *dir,
		statePath:     *statePath,
		freshness:     *freshness,
		bootstrapFrom: bootstrapFrom,
		heartbeat:     *heartbeat,
		noWatchlist:   *noWatchlist,
		interval:      *interval,
		tg:            notifier,
		chatID:        tgChatID,

		// News (reuse internal/news). newsHTTP dibuat sekali (dipakai tiap tick).
		// newsNames dinormalisasi via news.ParseNames (uppercase/trim/dedup).
		newsEnabled:     *newsEnabled,
		newsNames:       news.ParseNames(*newsEventsCSV),
		newsPrewindow:   *newsPrewindow,
		newsStale:       *newsStale,
		newsNowcast:     *newsNowcast,
		newsStatePath:   *newsState,
		newsFeedCache:   *newsFeedCache,
		newsSupport:     *newsSupport,
		newsBLS:         *newsBLS,
		newsBLSKey:      *newsBLSKey,
		newsSkipEnabled: *newsSkip,
		newsHTTP:        &http.Client{Timeout: 20 * time.Second},
	}

	if *once {
		d.tick() // = runOnce + news (bila enabled), terbungkus recover yang sama dgn loop
		return
	}

	// Perintah bot Telegram (trigger manual): goroutine long-poll getUpdates.
	go d.watchCommands()

	// Loop di-align ke kelipatan interval berikutnya + offset 20 detik, lalu
	// ticker periodik. Offset memberi OANDA waktu meng-finalize candle close.
	const offset = 20 * time.Second
	now := time.Now()
	next := now.Truncate(*interval).Add(*interval).Add(offset)
	wait := time.Until(next)
	log.Printf("alertd start: instrument=%s interval=%s tick pertama dalam %s (≈%s)",
		*instrument, *interval, wait.Round(time.Second), next.Format("15:04:05"))
	time.Sleep(wait)

	d.tick() // tick pertama setelah alignment
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for range ticker.C {
		d.tick()
	}
}

// daemon membungkus dependensi + parameter satu siklus.
type daemon struct {
	client        *oanda.Client
	notifier      notify.Notifier
	cfg           engine.Config
	instrument    string
	dir           string
	statePath     string
	freshness     time.Duration
	bootstrapFrom time.Time
	heartbeat     bool             // kirim watchlist tiap H1 close walau daftar tak berubah
	noWatchlist   bool             // matikan seluruh alert otomatis blok watchlist (POI per-TF + AMS); /watchlist manual tetap
	interval      time.Duration    // jeda poll — basis ambang deteksi gap offline
	tg            *notify.Telegram // akses getUpdates (perintah bot /watchlist)
	chatID        string           // chat terdaftar — perintah dari chat lain diabaikan

	// --- News alert (reuse internal/news; aktif hanya bila newsEnabled) ---
	newsEnabled     bool
	newsNames       []string // indikator dipantau (ter-normalisasi)
	newsPrewindow   time.Duration
	newsStale       time.Duration
	newsNowcast     string
	newsStatePath   string // state dedup news (terpisah dari statePath alertd)
	newsFeedCache   string
	newsSupport     string
	newsBLS         bool         // fallback BLS saat mirror FF telat isi actual
	newsBLSKey      string       // registration key BLS opsional
	newsSkipEnabled bool         // skip-hour kondisional vs kalender (live-only); off = blanket
	newsHTTP        *http.Client // dibuat sekali di main()
}

// tick membungkus runOnce dengan recover supaya satu error tidak menjatuhkan
// daemon (proses tetap hidup ke tick berikutnya).
//
// News alert dipanggil DI SINI (di tick, bukan di dalam runOnce) dengan dua
// alasan: (1) news TIDAK bergantung pada OANDA/engine — runOnce bisa return lebih
// awal saat engine.Run/cache bermasalah, jadi menaruh news di tick memastikan
// alert news tetap jalan; (2) tetap terbungkus recover tick yang sama. PARITAS:
// bila -news=false, maybeSendNewsAlerts TIDAK PERNAH dipanggil → perilaku alertd
// lama identik (tak ada fetch feed, tak ada pesan tambahan, tak ada file state baru).
func (d *daemon) tick() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("tick panic dipulihkan: %v", r)
		}
	}()
	if d.newsSkipEnabled {
		d.refreshNewsSkip(time.Now().UTC()) // set cfg.NewsSkip* SEBELUM engine.Run
	}
	d.runOnce()
	if d.newsEnabled {
		d.maybeSendNewsAlerts(time.Now().UTC())
	}
}

// refreshNewsSkip menyetel d.cfg.NewsSkipHourStarts + SkipEntryNewsOnly dari
// kalender minggu berjalan SEBELUM engine.Run/Narrate, sehingga skip-hour
// (08:00 NY) jadi kondisional: hanya di-skip bila benar-benar ada rilis USD
// high-impact jam itu. Bila feed gagal / cuma cache (basi), fallback ke blanket
// skip (SkipEntryNewsOnly=false) demi proteksi — kita tak boleh "membuka" jam
// rawan tanpa kalender yang andal. Dipanggil HANYA bila d.newsSkipEnabled.
func (d *daemon) refreshNewsSkip(now time.Time) {
	loc, err := detectors.NYLocation()
	if err != nil {
		log.Printf("news-skip: muat lokasi NY gagal (fallback blanket): %v", err)
		d.cfg.SkipEntryNewsOnly = false
		return
	}
	events, fromCache, err := news.FetchCalendarCached(d.newsHTTP, d.newsFeedCache)
	if err != nil {
		log.Printf("news-skip: fetch kalender gagal (fallback blanket skip): %v", err)
		d.cfg.SkipEntryNewsOnly = false
		return
	}
	if fromCache {
		log.Printf("news-skip: ⚠️ feed live gagal — pakai cache (fallback blanket skip)")
		d.cfg.SkipEntryNewsOnly = false
		return
	}
	d.cfg.NewsSkipHourStarts = engine.BuildNewsSkipSet(events, d.cfg.SkipEntryHoursNY, loc)
	d.cfg.SkipEntryNewsOnly = true
	log.Printf("news-skip: aktif — %d jam-news high-impact di set (skip-hour kondisional)", len(d.cfg.NewsSkipHourStarts))
}

// runOnce = satu siklus: refresh cache → load TFData → engine.Run → freshness →
// dedup → kirim alert + simpan state. Dua jenis alert:
//  1. SETUP VALID (signal engine) — logika lama, + seksi POI per-TF sbg konteks.
//  2. WATCHLIST POI per-TF — dikirim hanya saat daftarnya BERUBAH (dedup via
//     fingerprint di notify.State; maks ~1 pesan per H1 close).
func (d *daemon) runOnce() {
	d.refreshCache()

	tf := d.loadTF()
	res, err := engine.Run(tf, d.cfg)
	if err != nil {
		log.Printf("engine.Run: %v", err)
		return
	}

	st, err := notify.LoadState(d.statePath)
	if err != nil {
		log.Printf("load state: %v (lanjut tanpa dedup)", err)
	}

	// Deteksi gap offline: tick normal berjarak ~interval; gap jauh di atasnya
	// berarti daemon sempat mati / Mac sleep. Pesan watchlist pertama pasca-
	// restart diberi catatan supaya diff besar tidak disalahartikan sebagai
	// perubahan mendadak — itu delta vs kondisi SEBELUM offline, dan kejadian
	// intermediate selama offline (zona muncul-lalu-tersapu dst) tidak terekam.
	now := time.Now().UTC()
	offNote := offlineNote(st.LastTickTime, now, d.interval)
	if offNote != "" {
		log.Printf("tick: gap offline terdeteksi (tick terakhir %s)", st.LastTickTime.Format(time.RFC3339))
	}
	st.LastTickTime = now
	stChanged := true // LastTickTime selalu maju → state disimpan tiap tick (penanda liveness)

	// Narasi titik H1 terkini = sumber watchlist POI per-TF + narasi piramida
	// (ditempel di alert setup maupun alert watchlist sebagai konteks).
	var n engine.ScanNarrative
	if len(tf.H1) > 0 {
		n = engine.Narrate(tf, d.cfg, tf.H1[len(tf.H1)-1].Time)
	}
	narBlock := narrativeBlock(n, d.instrument, nil) // lampiran alert SETUP (narasi piramida penuh)
	wlBlock := watchlistBlock(n, d.instrument)       // pesan WATCHLIST (6-seksi ringkas)
	adaZona := len(engine.FormatNextPOIsByTF(n)) > 0

	// --- 1. Alert SETUP VALID (signal terakhir engine) ---
	if len(res.Signals) == 0 {
		log.Printf("tick: signal=0 aksi=no_setup")
	} else {
		sig := res.Signals[len(res.Signals)-1]
		switch {
		// FRESHNESS: sinyal terbaru harus dalam jendela `freshness` dari now.
		case sig.Time.Before(now.Add(-d.freshness)):
			log.Printf("tick: signal=%d aksi=skip_basi (sinyal %s lebih tua dari %s)",
				len(res.Signals), sig.Time.Format(time.RFC3339), d.freshness)
		// DEDUP: hanya alert kalau lebih baru dari yang terakhir dikirim.
		case !sig.Time.After(st.LastAlertTime):
			log.Printf("tick: signal=%d aksi=skip_dedup (sudah pernah dialert %s)",
				len(res.Signals), sig.Time.Format(time.RFC3339))
		default:
			msg := formatSignal(sig, d.instrument)
			if narBlock != "" {
				msg += "\n\n" + narBlock
			}
			if err := d.notifier.SendMessage(msg); err != nil {
				log.Printf("tick: signal=%d aksi=gagal_kirim: %v", len(res.Signals), err)
			} else {
				st.LastAlertTime = sig.Time
				st.Dir = sig.Dir.String()
				stChanged = true
				log.Printf("tick: signal=%d aksi=terkirim (dir=%s entry=%.2f waktu=%s)",
					len(res.Signals), sig.Dir.String(), sig.Entry, sig.Time.Format(time.RFC3339))
			}
		}
	}

	// --- 2. Alert WATCHLIST POI per-TF: kirim saat PERUBAHAN BERMAKNA saja ---
	// (user 2026-06-02, anti-spam) changed = watchlistTrigger: zona POI berubah,
	// token REH/REL berubah, atau harga MENYEBERANG MO (atas↔bawah). Transisi
	// MO HARIAN yang rutin (terbentuk 00:00 NY / hilang saat Asia) TIDAK memicu
	// — state di-update diam-diam. Heartbeat per H1 tersedia via -heartbeat
	// (default OFF). Diff message tetap mem-bold apa yang berubah.
	// -no-watchlist mematikan seluruh blok ini (POI per-TF + AMS); /watchlist
	// manual tetap jalan via watchCommands.
	if !d.noWatchlist {
		fp := watchlistFingerprint(n)
		changed := watchlistTrigger(st.LastWatchlist, fp)
		newH1 := len(n.Steps) > 0 && n.At.After(st.LastWatchlistH1)

		msg, aksi := "", ""
		switch {
		case changed && adaZona && st.LastWatchlist == "":
			// Kiriman perdana (state kosong) — tanpa diff, semua zona memang "baru".
			msg = fmt.Sprintf("🆕 *Watchlist %s — kiriman pertama*\n%s", d.instrument, wlBlock)
			aksi = fmt.Sprintf("kiriman pertama, %d zona", len(n.NextPOIsByTF))
		case changed && adaZona:
			diff, _ := watchlistDiff(st.LastWatchlist, n)
			msg = "⚠️ *ADA PERUBAHAN*"
			if offNote != "" {
				msg += "\n" + offNote
			}
			for _, dl := range diff {
				msg += "\n• *" + dl + "*"
			}
			msg += "\n" + wlBlock
			aksi = fmt.Sprintf("daftar berubah, %d zona", len(n.NextPOIsByTF))
		case changed && fpHasZones(st.LastWatchlist):
			msg = fmt.Sprintf("📍 *Watchlist POI per TF — %s*\n⚠️ *Semua zona pantauan hilang (jebol/tak valid lagi).* Harga: `%.2f`",
				d.instrument, n.Price)
			if offNote != "" {
				msg += "\n" + offNote
			}
			aksi = "semua zona hilang"
		case changed:
			// Tak ada zona POI, tapi fingerprint berubah. Kirim HANYA bila ada perubahan
			// AMS (ITL/ITH terbentuk/break — Pertemuan 6); selain itu (run perdana / token
			// rutin) catat diam-diam supaya tak spam.
			diff, _ := watchlistDiff(st.LastWatchlist, n)
			var amsLines []string
			for _, dl := range diff {
				if strings.HasPrefix(dl, "AMS") {
					amsLines = append(amsLines, dl)
				}
			}
			if len(amsLines) > 0 {
				msg = "⚠️ *AMS BERUBAH*"
				if offNote != "" {
					msg += "\n" + offNote
				}
				for _, dl := range amsLines {
					msg += "\n• *" + dl + "*"
				}
				msg += "\n" + wlBlock
				aksi = "AMS berubah (tanpa zona)"
			} else {
				st.LastWatchlist = fp // run perdana, daftar kosong → catat saja
				stChanged = true
			}
		case fp != st.LastWatchlist:
			// Hanya transisi MO harian (none↔side) — bukan trigger; catat diam-diam
			// supaya tidak menumpuk jadi "crossing" palsu di tick berikutnya.
			st.LastWatchlist = fp
			stChanged = true
			log.Printf("tick: watchlist aksi=skip_mo_harian (transisi MO rutin, tanpa pesan)")
		case d.heartbeat && newH1 && adaZona:
			msg = fmt.Sprintf("ℹ️ *Tidak ada perubahan zona (heartbeat)*\n%s", wlBlock)
			aksi = fmt.Sprintf("heartbeat H1, %d zona tak berubah", len(n.NextPOIsByTF))
		case d.heartbeat && newH1:
			st.LastWatchlistH1 = n.At // candle baru tapi memang tak ada zona — catat saja
			stChanged = true
			log.Printf("tick: watchlist aksi=skip_kosong (candle H1 baru, tak ada zona)")
		default:
			// Transparansi log: tanpa baris ini, "daftar tak berubah" dan "ada yang
			// macet" sama-sama diam — susah dibedakan saat debugging.
			log.Printf("tick: watchlist aksi=skip_dedup (candle H1 sama, daftar %d zona tak berubah)", len(n.NextPOIsByTF))
		}
		if msg != "" {
			if err := d.notifier.SendMessage(msg); err != nil {
				log.Printf("tick: watchlist aksi=gagal_kirim: %v", err)
			} else {
				st.LastWatchlist = fp
				st.LastWatchlistH1 = n.At
				stChanged = true
				log.Printf("tick: watchlist aksi=terkirim (%s)", aksi)
			}
		}
	} else {
		log.Printf("tick: watchlist aksi=skip_nonaktif (-no-watchlist)")
	}

	// --- 3. Alert DAY-TYPE: heads-up sekali/trading-day saat heavy_expanding/heavy_accum ---
	// Dikirim setelah London close (06:00 NY), saat verdict day-type sudah stabil.
	// Dedup via st.LastDayTypeAlert (di-set di dalam).
	d.maybeSendDayTypeAlert(n, tf, now, &st)

	// --- 4. Alert Q1 SESSION CLOSE: A/X per Q1 tiap sesi (Phase 2, dibaca 15m) ---
	// Dikirim sekali saat Q1 sesi (Asia/London/NY-AM) baru close (19:30/01:30/
	// 07:30 NY). Dedup via st.LastQ1Close (di-set di dalam).
	d.maybeSendQ1Alert(tf, now, &st)

	if stChanged {
		if err := st.Save(d.statePath); err != nil {
			log.Printf("simpan state: %v", err)
		}
	}
}

// q1Sessions = sesi yang Q1-close-nya dialert (Asia/London/New York). PM Session
// (Q1 close 13:30) di luar window entry (SessionPMGate) → tidak dialert (keputusan user).
var q1Sessions = map[detectors.SessionKind]string{
	detectors.Asia: "Asia", detectors.London: "London", detectors.NYAM: "New York",
}

// latestQ1Close = waktu close Q1 sesi terakhir <= now + jenis sesinya. ok=false
// kalau sesi itu PM (di luar daftar alert). Q1 close = SessionStart + 90 menit.
func latestQ1Close(now time.Time, loc *time.Location) (time.Time, detectors.SessionKind, bool) {
	ss := detectors.SessionStart(now, loc)
	b := ss.Add(90 * time.Minute)
	if now.Before(b) {
		// Masih di Q1 sesi berjalan → Q1 terakhir yang sudah close = sesi sebelumnya.
		ss = detectors.SessionStart(ss.Add(-time.Minute), loc)
		b = ss.Add(90 * time.Minute)
	}
	sess := detectors.Session(ss, loc)
	_, ok := q1Sessions[sess]
	return b, sess, ok
}

// maybeSendDayTypeAlert mengirim heads-up SEKALI per trading-day saat day-type
// terklasifikasi heavy_expanding atau heavy_accum (skip normal/suspected_accum).
// Verdict diambil dari n.QTDayType (reuse — tak panggil ulang engine). Dikirim pada/
// setelah London close (06:00 NY = TradingDayStart+12h), saat verdict sudah STABIL
// (range Asia & London penuh; window day-type tak masuk NY). Boleh telat dalam
// trading-day yang sama (daemon hidup mid-NY tetap kirim) — beda dgn Q1 yang ketat
// 1-interval. Anti-drift: verdict dihitung di waktu candle H1 terakhir, jadi syaratkan
// trading-day candle == trading-day now (cache basi lintas-hari → skip sampai nyusul).
func (d *daemon) maybeSendDayTypeAlert(n engine.ScanNarrative, tf engine.TFData, now time.Time, st *notify.State) {
	switch n.QTDayType {
	case "heavy_expanding", "heavy_accum":
	default:
		return // normal / suspected_accum / "" → diam
	}
	if len(tf.H1) == 0 {
		return
	}
	loc, err := detectors.NYLocation()
	if err != nil {
		return
	}
	at := tf.H1[len(tf.H1)-1].Time // waktu candle sumber verdict (n dihitung di sini)
	nowDay := detectors.TradingDayStart(now, loc)
	if !detectors.TradingDayStart(at, loc).Equal(nowDay) {
		return // cache basi lintas trading-day → verdict tak sinkron dgn now, tunggu refresh
	}
	londonClose := nowDay.Add(12 * time.Hour) // 06:00 NY
	if now.Before(londonClose) {
		return // London belum close → verdict belum stabil
	}
	if !nowDay.After(st.LastDayTypeAlert) {
		return // sudah dialert utk trading-day ini (dedup)
	}
	if err := d.notifier.SendMessage(formatDayTypeAlert(n.QTDayType, nowDay, loc)); err != nil {
		log.Printf("tick: daytype %s aksi=gagal_kirim: %v", n.QTDayType, err)
		return
	}
	st.LastDayTypeAlert = nowDay
	log.Printf("tick: daytype %s aksi=terkirim (trading-day %s)",
		n.QTDayType, nowDay.In(loc).Format("2006-01-02"))
}

// formatDayTypeAlert = pesan Telegram heads-up day-type heavy_* (label saja, tanpa
// angka). dayStart = TradingDayStart (18:00 NY) → tanggal sesi NY untuk konteks.
func formatDayTypeAlert(dayType string, dayStart time.Time, loc *time.Location) string {
	tgl := dayStart.In(loc).Format("Mon 02 Jan")
	if dayType == "heavy_expanding" {
		return fmt.Sprintf("📈 *Day type: HEAVY EXPANDING* (%s)\n"+
			"Asia+London sudah net-directional besar (>1.3×ATR daily).\n"+
			"Harga \"mahal\"/extended; hati-hati chase. Entry tetap jalan via pipeline (cuma caution).", tgl)
	}
	return fmt.Sprintf("🧊 *Day type: HEAVY ACCUM* (%s)\n"+
		"Asia & London dua-duanya kompresi (<0.4×ATR daily).\n"+
		"Pasar akumulasi/range. Engine BLOCK entry hari ini (gate #5). Jangan trade.", tgl)
}

// maybeSendQ1Alert mengklasifikasi A/X Q1 sesi yang baru close dan mengirim alert
// sekali. Logika A/X = ClassifyDaily diturunkan ke 15m (fractal): X bila Q1
// tinggalkan FVG-15m DAN |close−open| Q1 >= MinAtrMult × ATR_15m; selain itu A.
func (d *daemon) maybeSendQ1Alert(tf engine.TFData, now time.Time, st *notify.State) {
	loc, err := detectors.NYLocation()
	if err != nil {
		return
	}
	b, sess, ok := latestQ1Close(now, loc)
	if !ok {
		return
	}
	// Hanya picu pada tick TEPAT setelah close (dalam 1 interval) — jangan alert
	// Q1 yang sudah lama lewat (mis. daemon baru hidup di tengah sesi).
	if now.Sub(b) < 0 || now.Sub(b) >= d.interval {
		return
	}
	if !b.After(st.LastQ1Close) {
		return // sudah dialert (dedup)
	}
	scen, n, ok := d.q1Scenario(tf.M15, b)
	if !ok {
		log.Printf("tick: Q1 %s aksi=skip (data 15m belum cukup: %d candle)", q1Sessions[sess], n)
		return
	}
	start := b.Add(-90 * time.Minute)
	if err := d.notifier.SendMessage(formatQ1Alert(sess, start, b, scen)); err != nil {
		log.Printf("tick: Q1 %s aksi=gagal_kirim: %v", q1Sessions[sess], err)
		return
	}
	st.LastQ1Close = b
	log.Printf("tick: Q1 %s close aksi=terkirim (%s, window %s–%s NY, %d candle 15m)",
		q1Sessions[sess], scen.String(), start.In(loc).Format("15:04"), b.In(loc).Format("15:04"), n)
}

// q1Scenario mengklasifikasi A/X Q1 sesi dari M15: window [b-90m, b), ATR_15m di
// candle terakhir sebelum close b, lalu ClassifyDaily (turun 1 TF, fractal).
// ok=false bila data 15m belum cukup (cache dingin / gap). n = jumlah candle Q1.
// SATU sumber klasifikasi untuk alert otomatis & manual /q1 (0-drift).
func (d *daemon) q1Scenario(m15 []data.Candle, b time.Time) (scen detectors.Scenario, n int, ok bool) {
	start := b.Add(-90 * time.Minute)
	q1 := candlesInWindow(m15, start, b)
	if len(q1) < 2 {
		return detectors.AMDX, len(q1), false
	}
	idx := -1
	var prevClose float64
	for i, c := range m15 {
		if c.Time.Before(b) {
			idx = i
		}
		if c.Time.Before(start) { // candle M15 terakhir sebelum window Q1 → gap-anchor
			prevClose = c.Close
		}
	}
	atr, atrOK := detectors.ATRAt(m15, idx, d.cfg.ATRPeriod)
	if !atrOK {
		return detectors.AMDX, len(q1), false
	}
	anchor := 0.0
	if d.cfg.Q1GapAnchor {
		anchor = prevClose
	}
	return detectors.ClassifyDailyG(q1, anchor, atr, d.cfg.MinAtrMult, d.cfg.MinGapPips, d.cfg.Q1RangeRatio, d.cfg.Q1AXMode, false), len(q1), true
}

// candlesInWindow = candle dgn Time di [from, to) (slice terurut waktu).
func candlesInWindow(c []data.Candle, from, to time.Time) []data.Candle {
	var out []data.Candle
	for _, x := range c {
		if !x.Time.Before(from) && x.Time.Before(to) {
			out = append(out, x)
		}
	}
	return out
}

// formatQ1Alert = pesan Telegram klasifikasi A/X Q1 sesi.
func formatQ1Alert(sess detectors.SessionKind, start, end time.Time, scen detectors.Scenario) string {
	loc, _ := detectors.NYLocation()
	win := fmt.Sprintf("%s–%s NY", start.In(loc).Format("15:04"), end.In(loc).Format("15:04"))
	name := q1Sessions[sess]
	if scen == detectors.XAMD {
		return fmt.Sprintf("🕐 *%s Q1 close (%s)* — X (expansive)\nEkspektasi sesi: XAMD — Q1 sudah ekspansi (harga \"mahal\" di awal sesi). Dibaca dari 15m.", name, win)
	}
	return fmt.Sprintf("🕐 *%s Q1 close (%s)* — AKUMULASI (A)\nEkspektasi sesi: AMDX — Q2 cenderung manipulasi/sweep, window entry Q3. Dibaca dari 15m.", name, win)
}

// watchCommands = goroutine long-poll perintah bot Telegram. Saat ini hanya
// "/watchlist": balas watchlist lengkap saat diminta (trigger manual, user
// 2026-06-02) — TIDAK menyentuh state dedup (pesan perubahan otomatis tetap
// jalan normal). Backlog pesan sebelum daemon start di-skip (perintah saat
// daemon mati tidak di-replay). Panic di-recover supaya goroutine tetap hidup.
func (d *daemon) watchCommands() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("cmd: goroutine panic dipulihkan: %v — restart poll", r)
			go d.watchCommands()
		}
	}()

	// Skip backlog: baca semua update pending lalu mulai SETELAHnya.
	offset := int64(0)
	if ups, err := d.tg.GetUpdates(0, 0); err == nil && len(ups) > 0 {
		offset = ups[len(ups)-1].ID + 1
	}
	log.Printf("cmd: poll perintah bot aktif (/watchlist, /q1)")

	for {
		ups, err := d.tg.GetUpdates(offset, 15) // long-poll 15s (< timeout http 20s)
		if err != nil {
			time.Sleep(5 * time.Second) // jaringan labil — coba lagi
			continue
		}
		for _, u := range ups {
			offset = u.ID + 1
			cmd := strings.TrimSpace(u.Text)
			if i := strings.Index(cmd, "@"); i > 0 {
				cmd = cmd[:i] // bentuk grup: "/watchlist@NamaBot"
			}
			if cmd != "/watchlist" && cmd != "/q1" {
				continue
			}
			if d.chatID != "" && u.ChatID != d.chatID {
				log.Printf("cmd: %s dari chat %s DIABAIKAN (bukan chat terdaftar)", cmd, u.ChatID)
				continue
			}
			switch cmd {
			case "/watchlist":
				d.sendManualWatchlist()
			case "/q1":
				d.sendManualQ1()
			}
		}
	}
}

// q1Slot = satu sesi (Asia/London/NY-AM) trading-day + jam start & Q1-close NY.
type q1Slot struct {
	sess         detectors.SessionKind
	start, close time.Time
}

// tradingDayQ1Slots = 3 sesi trading-day yang memuat now, dgn jam start & Q1-close
// (wall-clock NY, DST-aware). Asia 18:00→19:30 (hari ds), London 00:00→01:30 &
// NY-AM 06:00→07:30 (ds+1).
func tradingDayQ1Slots(now time.Time, loc *time.Location) []q1Slot {
	ds := detectors.TradingDayStart(now, loc) // 18:00 NY
	y, m, d := ds.Date()
	y2, m2, d2 := ds.AddDate(0, 0, 1).Date()
	mk := func(yy int, mm time.Month, dd, h, mi int) time.Time { return time.Date(yy, mm, dd, h, mi, 0, 0, loc) }
	return []q1Slot{
		{detectors.Asia, mk(y, m, d, 18, 0), mk(y, m, d, 19, 30)},
		{detectors.London, mk(y2, m2, d2, 0, 0), mk(y2, m2, d2, 1, 30)},
		{detectors.NYAM, mk(y2, m2, d2, 6, 0), mk(y2, m2, d2, 7, 30)},
	}
}

// sendManualQ1 membalas klasifikasi A/X Q1 tiap sesi trading-day berjalan
// (Asia/London/NY-AM) atas permintaan /q1. Pakai klasifikasi yang sama dengan
// alert otomatis (q1Scenario) dan TIDAK menyentuh dedup LastQ1Close. Sesi yang
// Q1-nya belum close ditandai "belum close".
func (d *daemon) sendManualQ1() {
	loc, err := detectors.NYLocation()
	if err != nil {
		_ = d.notifier.SendMessage("⚠️ NYLocation error — tidak bisa klasifikasi Q1.")
		return
	}
	tf := d.loadTF()
	if len(tf.M15) == 0 {
		_ = d.notifier.SendMessage("⚠️ Cache M15 kosong — Q1 belum bisa diklasifikasi (tunggu bootstrap M15).")
		return
	}
	now := time.Now().UTC()
	lines := []string{"🕐 *Q1 session A/X — trading day ini (diminta manual)*"}
	for _, s := range tradingDayQ1Slots(now, loc) {
		win := fmt.Sprintf("%s–%s NY", s.start.In(loc).Format("15:04"), s.close.In(loc).Format("15:04"))
		name := q1Sessions[s.sess]
		switch {
		case now.Before(s.close):
			lines = append(lines, fmt.Sprintf("• %s Q1 (%s): belum close", name, win))
		default:
			scen, n, ok := d.q1Scenario(tf.M15, s.close)
			if !ok {
				lines = append(lines, fmt.Sprintf("• %s Q1 (%s): data 15m belum cukup (%d candle)", name, win, n))
				continue
			}
			ax := "AKUMULASI (A) → AMDX"
			if scen == detectors.XAMD {
				ax = "X (expansive) → XAMD"
			}
			lines = append(lines, fmt.Sprintf("• %s Q1 (%s): %s", name, win, ax))
		}
	}
	if err := d.notifier.SendMessage(strings.Join(lines, "\n")); err != nil {
		log.Printf("cmd: /q1 aksi=gagal_kirim: %v", err)
		return
	}
	log.Printf("cmd: /q1 aksi=terkirim (manual)")
}

// sendManualWatchlist mengirim watchlist saat ini atas permintaan /watchlist.
// Pakai cache yang sama dengan tick (segar ≤1 interval poll); aman konkuren
// dengan refreshCache karena WriteCSV atomik (rename).
func (d *daemon) sendManualWatchlist() {
	tf := d.loadTF()
	if len(tf.H1) == 0 {
		_ = d.notifier.SendMessage("⚠️ Cache H1 kosong — tidak bisa membangun watchlist.")
		return
	}
	n := engine.Narrate(tf, d.cfg, tf.H1[len(tf.H1)-1].Time)
	msg := fmt.Sprintf("📍 *Watchlist %s (diminta manual)*\n%s",
		d.instrument, watchlistBlock(n, d.instrument))
	if err := d.notifier.SendMessage(msg); err != nil {
		log.Printf("cmd: /watchlist aksi=gagal_kirim: %v", err)
		return
	}
	log.Printf("cmd: /watchlist aksi=terkirim (manual, %d zona)", len(n.NextPOIsByTF))
}

// refreshCache memperbarui cache CSV tiap granularity dari last-candle s/d now.
// Per-granularity di-retry kecil (3x backoff) karena OANDA labil dari ID; error
// satu granularity dicatat tapi tidak menjatuhkan siklus (lanjut granularity lain).
func (d *daemon) refreshCache() {
	to := time.Now().UTC()
	for _, g := range granularities {
		last, ok, err := data.LastCandleTime(d.dir, d.instrument, g)
		if err != nil {
			log.Printf("refresh %s: baca last-candle: %v", g, err)
			continue
		}
		from := d.bootstrapFrom
		if ok {
			from = last.Add(time.Second)
		}

		fresh, err := d.fetchWithRetry(g, from, to)
		if err != nil {
			log.Printf("refresh %s: fetch gagal (skip): %v", g, err)
			continue
		}
		if len(fresh) == 0 {
			continue
		}
		if err := data.AppendNew(d.dir, d.instrument, g, fresh); err != nil {
			log.Printf("refresh %s: append: %v", g, err)
			continue
		}
		log.Printf("refresh %s: +%d candle", g, len(fresh))
	}
}

// fetchWithRetry memanggil FetchCandles dengan retry 3x backoff linear (1s,2s,3s).
func (d *daemon) fetchWithRetry(g string, from, to time.Time) ([]data.Candle, error) {
	const maxAttempt = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempt; attempt++ {
		candles, err := d.client.FetchCandles(d.instrument, g, from, to, dailyAlign)
		if err == nil {
			return candles, nil
		}
		lastErr = err
		if attempt < maxAttempt {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	return nil, lastErr
}

// loadTF membaca 5 CSV cache ke engine.TFData. CSV yang gagal dibaca → slice
// kosong (engine menangani data tak cukup via gateData).
func (d *daemon) loadTF() engine.TFData {
	read := func(g string) []data.Candle {
		c, err := data.ReadCSV(data.CSVPath(d.dir, d.instrument, g))
		if err != nil {
			log.Printf("loadTF %s: %v", g, err)
			return nil
		}
		return c
	}
	return engine.TFData{
		Weekly: read("W"),
		Daily:  read("D"),
		H4:     read("H4"),
		H1:     read("H1"),
		M15:    read("M15"),
		M5:     read("M5"),
	}
}

// narrativeBlock merangkai NARASI PIRAMIDA penuh (engine.FormatNarrative —
// formatter bersama dengan cmd/narrate, tampilan identik; width 42 supaya rapi
// di layar HP) + seksi "POI terdekat per TF" dalam SATU blok monospace Telegram.
// Blok ``` melindungi teks bebas narasi (underscore dkk.) dari parser Markdown.
// mark = key "TF ARAH" (wlKey) zona yang berubah → barisnya diprefix "➜ "
// (bold tidak dirender di dalam blok monospace, jadi pakai penanda). Dipotong
// per-baris bila mendekati cap 4096 char Telegram. Kosong kalau narasi belum
// bisa dihitung (cache H1 kosong).
func narrativeBlock(n engine.ScanNarrative, instrument string, mark map[string]bool) string {
	if len(n.Steps) == 0 {
		return ""
	}
	lines := engine.FormatNarrative(n, instrument, 42)
	lines = append(lines, "")
	if al := engine.FormatAsiaLine(n); al != "" {
		lines = append(lines, al)
	}
	lines = append(lines, engine.FormatMOLine(n))
	lines = append(lines, engine.FormatRelEqLines(n)...)
	if wl := engine.FormatNextPOIsByTF(n); len(wl) > 0 {
		lines = append(lines, "", "POI terdekat per TF (pantau):")
		// FormatNextPOIsByTF = 2 baris per entri NextPOIsByTF (zona + confluence),
		// jadi entri ke-i/2 menentukan penanda baris zona (i genap).
		for i, l := range wl {
			prefix := "  "
			if i%2 == 0 && i/2 < len(n.NextPOIsByTF) && mark[wlKey(n.NextPOIsByTF[i/2])] {
				prefix = "➜ "
			}
			lines = append(lines, prefix+l)
		}
	}
	const maxLen = 3600 // sisakan ruang utk header/seksi signal di luar blok
	total := 0
	var kept []string
	for _, l := range lines {
		if total+len(l)+1 > maxLen {
			kept = append(kept, "… (terpotong)")
			break
		}
		kept = append(kept, l)
		total += len(l) + 1
	}
	return "```\n" + strings.Join(kept, "\n") + "\n```"
}

// watchlistBlock = pesan PANTAUAN ringkas (engine.FormatWatchlist — formatter
// bersama dgn cmd/narrate). TEKS BIASA (bukan code block monospace) supaya tak
// ke-wrap/patah di HP — Telegram membungkus baris panjang secara natural. Width 0 =
// jangan hard-wrap (tiap komponen Array sudah satu baris pendek). Instrumen
// di-sanitasi (XAU_USD→XAUUSD) agar underscore tak ditafsir italic oleh parser
// Markdown (body kini di luar pelindung ```). Dipotong di cap 3600 char.
func watchlistBlock(n engine.ScanNarrative, instrument string) string {
	safe := strings.ReplaceAll(instrument, "_", "") // XAU_USD → XAUUSD (Markdown-aman)
	lines := engine.FormatWatchlist(n, safe, 0)
	const maxLen = 3600
	total := 0
	var kept []string
	for _, l := range lines {
		if total+len(l)+1 > maxLen {
			kept = append(kept, "… (terpotong)")
			break
		}
		kept = append(kept, l)
		total += len(l) + 1
	}
	return strings.Join(kept, "\n")
}

// wlKey = key display satu entri watchlist ("H4 SELL") — dipakai menandai baris
// zona yang berubah (➜) di blok monospace dan di baris diff.
func wlKey(p engine.NextPOITF) string {
	arah := "BUY"
	if p.Dir.String() == "bearish" {
		arah = "SELL"
	}
	return p.TF.String() + " " + arah
}

// asiaToken = klasifikasi Asia untuk token fingerprint: "A" (akumulasi→AMDX) /
// "X" (expansive→XAMD) / "-" (sesi Asia masih berjalan).
func asiaToken(n engine.ScanNarrative) string {
	switch {
	case !n.AsiaClosed:
		return "-"
	case n.AsiaScenario == detectors.XAMD:
		return "X"
	default:
		return "A"
	}
}

// moSide = sisi harga relatif Midnight Open untuk token fingerprint:
// "above" (premium) / "below" (discount) / "none" (MO belum terbentuk).
func moSide(n engine.ScanNarrative) string {
	switch {
	case !n.HasMO:
		return "none"
	case n.Price < n.MOPrice:
		return "below"
	default:
		return "above"
	}
}

// wlFPEntry = satu entri hasil parse-balik fingerprint watchlist lama.
type wlFPEntry struct {
	rng  string // "bot-top" persis seperti di fingerprint (%.2f-%.2f)
	tier string // "T2"
	conf int
}

// wlRelEqFP = token REH/REL hasil parse-balik fingerprint lama.
type wlRelEqFP struct {
	level string // "%.2f" persis seperti di fingerprint
	count int
}

// wlAMSFP = token AMS (ITL/ITH) hasil parse-balik fingerprint lama.
type wlAMSFP struct {
	pivot     string // "%.2f" persis seperti di fingerprint
	pivotTime int64  // unix detik pivot — identitas unik intermediate (deteksi terbentuk-baru)
	active    bool   // belum di-break ke arah pembalik
}

// amsTokens = token fingerprint AMS dua arah (ITL & ITH), hanya yang Present.
// Format "AMS|<kind>|<pivot>|<pivotTimeUnix>|<active 0/1>".
func amsTokens(n engine.ScanNarrative) []string {
	var out []string
	add := func(kind string, s engine.AMSStruct) {
		if !s.Present {
			return
		}
		a := 0
		if s.Active {
			a = 1
		}
		out = append(out, fmt.Sprintf("AMS|%s|%.2f|%d|%d", kind, s.Pivot, s.PivotTime.Unix(), a))
	}
	add("ITL", n.AMSITL)
	add("ITH", n.AMSITH)
	return out
}

// amsFPChanged = true bila ada event AMS yang LAYAK di-alert (Pertemuan 6):
// TERBENTUK (ITL/ITH baru ter-konfirmasi → pivotTime berubah) atau BREAK
// (pivot sama, aktif→broken). Event "hilang" (ada→tak-ada) diabaikan.
func amsFPChanged(o, n map[string]wlAMSFP) bool {
	for _, kind := range []string{"ITL", "ITH"} {
		nv, nok := n[kind]
		if !nok {
			continue
		}
		ov, ook := o[kind]
		if !ook || ov.pivotTime != nv.pivotTime {
			return true // TERBENTUK
		}
		if ov.active && !nv.active {
			return true // BREAK
		}
	}
	return false
}

// parseWatchlistFP mem-parse fingerprint (format watchlistFingerprint sendiri:
// entri zona "TF|dir|bot-top|T%d|%d" + token MO "MO|side" + token liquidity
// "REH|TF|level|count"/"REL|TF|level|count", ber-separator ";") jadi map zona
// ber-key "TF|dir" + urutan key + sisi MO lama ("none" utk legacy) + map REH/REL
// lama ber-key "REH H1" dst (token legacy 3-field tanpa TF dianggap H1).
// Entri rusak di-skip.
func parseWatchlistFP(fp string) (zones map[string]wlFPEntry, order []string, mo, asia string, rel map[string]wlRelEqFP, ams map[string]wlAMSFP) {
	zones = map[string]wlFPEntry{}
	rel = map[string]wlRelEqFP{}
	ams = map[string]wlAMSFP{}
	mo = "none"
	asia = "-"
	for _, part := range strings.Split(fp, ";") {
		f := strings.Split(part, "|")
		switch {
		case len(f) == 2 && f[0] == "MO":
			mo = f[1]
		case len(f) == 2 && f[0] == "ASIA":
			asia = f[1]
		case len(f) == 3 && (f[0] == "REH" || f[0] == "REL"): // legacy tanpa TF = H1
			cnt, _ := strconv.Atoi(f[2])
			rel[f[0]+" H1"] = wlRelEqFP{level: f[1], count: cnt}
		case len(f) == 4 && (f[0] == "REH" || f[0] == "REL"):
			cnt, _ := strconv.Atoi(f[3])
			rel[f[0]+" "+f[1]] = wlRelEqFP{level: f[2], count: cnt}
		case len(f) == 5 && f[0] == "AMS": // AMS|kind|pivot|pivotTimeUnix|active — sebelum case zona (sama-sama 5 field)
			t, _ := strconv.ParseInt(f[3], 10, 64)
			ams[f[1]] = wlAMSFP{pivot: f[2], pivotTime: t, active: f[4] == "1"}
		case len(f) == 5:
			key := f[0] + "|" + f[1]
			conf, _ := strconv.Atoi(f[4])
			zones[key] = wlFPEntry{rng: f[2], tier: f[3], conf: conf}
			order = append(order, key)
		}
	}
	return zones, order, mo, asia, rel, ams
}

// fpHasZones = true bila fingerprint lama memuat entri ZONA (bukan cuma token
// MO/ASIA/REH/REL) — pembeda "semua zona hilang" vs "memang dari dulu kosong".
func fpHasZones(fp string) bool {
	_, order, _, _, _, _ := parseWatchlistFP(fp)
	return len(order) > 0
}

// watchlistTrigger menentukan apakah perbedaan fingerprint LAYAK mengirim pesan
// (user 2026-06-02, anti-spam): zona POI berubah, token REH/REL berubah, harga
// MENYEBERANG MO (atas↔bawah), atau ASIA CLOSE terklasifikasi (-→A/X, momen
// 00:00 NY — permintaan user). Transisi MO none↔side dan reset Asia harian
// (A/X→"-" saat trading day baru 18:00 NY) BUKAN trigger.
func watchlistTrigger(oldFP, newFP string) bool {
	oz, _, omo, oasia, orel, oams := parseWatchlistFP(oldFP)
	nz, _, nmo, nasia, nrel, nams := parseWatchlistFP(newFP)
	if !zonesFPEqual(oz, nz) || !relFPEqual(orel, nrel) {
		return true
	}
	if amsFPChanged(oams, nams) { // AMS ITL/ITH terbentuk atau di-break (Pertemuan 6)
		return true
	}
	if oasia != nasia && nasia != "-" { // Asia close (-→A/X) atau flip A↔X; reset →"-" silent
		return true
	}
	return omo != nmo && omo != "none" && nmo != "none" // MO: crossing saja
}

func zonesFPEqual(a, b map[string]wlFPEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func relFPEqual(a, b map[string]wlRelEqFP) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// watchlistDiff membandingkan fingerprint lama dengan kondisi sekarang.
// return: baris deskripsi perubahan (untuk seksi bold di pesan — zona
// baru/hilang/atribut berubah + transisi sisi MO) + set wlKey zona yang
// berubah/baru (untuk penanda ➜ di blok monospace; MO tidak ikut ➜).
func watchlistDiff(oldFP string, n engine.ScanNarrative) ([]string, map[string]bool) {
	old, oldOrder, oldMO, oldAsia, oldRel, oldAMS := parseWatchlistFP(oldFP)
	cur := n.NextPOIsByTF
	var lines []string
	marks := map[string]bool{}

	// AMS ITH/ITL (Pertemuan 6: titik perubahan order flow intra-week). Dua arah.
	// TERBENTUK (pivot baru ter-konfirmasi) / BREAK (aktif→broken). Catatan vs bias.
	for _, kind := range []string{"ITL", "ITH"} {
		s := n.AMSITL
		brkDir, ofNote, confNote := "ke bawah", "potensi flip OF bearish", "konfirmasi bullish intraday"
		if kind == "ITH" {
			s = n.AMSITH
			brkDir, ofNote, confNote = "ke atas", "potensi flip OF bullish", "konfirmasi bearish intraday"
		}
		if !s.Present {
			continue
		}
		biasNote := " (lawan bias)"
		if kind == n.AMSKind {
			biasNote = " (searah bias)"
		}
		ov, ook := oldAMS[kind]
		switch {
		case !ook || ov.pivotTime != s.PivotTime.Unix():
			state := "aktif"
			if !s.Active {
				state = "langsung broken"
			}
			lines = append(lines, fmt.Sprintf("AMS: %s baru terbentuk @ %.2f (1H, %s) — %s%s", kind, s.Pivot, state, confNote, biasNote))
		case ov.active && !s.Active:
			lines = append(lines, fmt.Sprintf("AMS: %s @ %.2f DI-BREAK %s (1H) — %s%s", kind, s.Pivot, brkDir, ofNote, biasNote))
		}
	}

	// Asia close (00:00 NY): klasifikasi final A vs X — penentu ekspektasi
	// skenario harian (permintaan user). Reset harian (→"-") tanpa baris.
	if newAsia := asiaToken(n); newAsia != oldAsia && newAsia != "-" {
		if newAsia == "X" {
			lines = append(lines, "Asia close: X (expansive) — ekspektasi XAMD hari ini")
		} else {
			lines = append(lines, "Asia close: AKUMULASI (A) — ekspektasi AMDX hari ini")
		}
	}

	// Transisi sisi Midnight Open (menyeberang / terbentuk / trading day baru).
	// Tanpa angka level MO (permintaan user) — cukup kondisinya.
	if newMO := moSide(n); newMO != oldMO {
		switch {
		case oldMO == "none" && newMO == "above":
			lines = append(lines, "MO trading day baru terbentuk — harga mulai DI ATAS MO (premium)")
		case oldMO == "none" && newMO == "below":
			lines = append(lines, "MO trading day baru terbentuk — harga mulai DI BAWAH MO (discount)")
		case newMO == "none":
			lines = append(lines, "Trading day baru (Asia) — MO hari ini belum terbentuk")
		case newMO == "above":
			lines = append(lines, "MO: harga MENYEBERANG ke ATAS MO — masuk premium")
		default:
			lines = append(lines, "MO: harga MENYEBERANG ke BAWAH MO — masuk discount")
		}
	}

	// Transisi liquidity REH/REL (per TF — fractal): TERSAPU (harga melewati
	// level lama) = momen actionable rule wait-for-sweep; terbentuk/berubah =
	// info. Hilang karena keluar window scan (harga TIDAK melewati level) →
	// diam (bukan kejadian).
	curRel := map[string]engine.RelEqualMark{}
	for _, m := range n.RelEquals {
		curRel[m.Kind+" "+m.TF.String()] = m
	}
	for _, kind := range []string{"REH", "REL"} {
		for _, tfk := range []string{"H1", "H4", "D"} {
			key := kind + " " + tfk
			o, hadOld := oldRel[key]
			c, hasNew := curRel[key]
			word := "high"
			if kind == "REL" {
				word = "low"
			}
			switch {
			case hadOld && !hasNew:
				lvl, err := strconv.ParseFloat(o.level, 64)
				swept := err == nil &&
					((kind == "REH" && n.Price > lvl) || (kind == "REL" && n.Price < lvl))
				if swept {
					lines = append(lines, fmt.Sprintf("%s %s %s (%d×) TERSAPU — liquidity diambil", kind, tfk, o.level, o.count))
				}
			case !hadOld && hasNew:
				lines = append(lines, fmt.Sprintf("%s %s baru terbentuk di %.2f (%d× %s)", kind, tfk, c.Level, c.Count, word))
			case hadOld && hasNew && (o.level != fmt.Sprintf("%.2f", c.Level) || o.count != c.Count):
				lines = append(lines, fmt.Sprintf("%s %s: %s (%d×) → %.2f (%d×)", kind, tfk, o.level, o.count, c.Level, c.Count))
			}
		}
	}

	seen := map[string]bool{}
	for _, p := range cur {
		key := p.TF.String() + "|" + p.Dir.String()
		seen[key] = true
		disp := wlKey(p)
		rng := fmt.Sprintf("%.2f-%.2f", p.FullBottom, p.FullTop)
		tier := fmt.Sprintf("T%d", p.Tier)
		o, ada := old[key]
		if !ada {
			lines = append(lines, fmt.Sprintf("Zona BARU: %s %.2f→%.2f (Tier %d, %d komponen)",
				disp, p.FullBottom, p.FullTop, p.Tier, p.Confluence))
			marks[disp] = true
			continue
		}
		var ch []string
		if o.rng != rng {
			ch = append(ch, fmt.Sprintf("batas %s jadi %s", fmtRng(o.rng), fmtRng(rng)))
		}
		if o.tier != tier {
			ch = append(ch, fmt.Sprintf("Tier %s jadi %s",
				strings.TrimPrefix(o.tier, "T"), strings.TrimPrefix(tier, "T")))
		}
		if o.conf != p.Confluence {
			ch = append(ch, fmt.Sprintf("confluence %d jadi %d komponen", o.conf, p.Confluence))
		}
		if len(ch) > 0 {
			lines = append(lines, disp+": "+strings.Join(ch, ", "))
			marks[disp] = true
		}
	}
	for _, key := range oldOrder {
		if seen[key] {
			continue
		}
		f := strings.Split(key, "|")
		arah := "BUY"
		if f[1] == "bearish" {
			arah = "SELL"
		}
		lines = append(lines, fmt.Sprintf("Zona HILANG: %s %s %s", f[0], arah, fmtRng(old[key].rng)))
	}
	return lines, marks
}

// fmtRng "4588.88-4607.51" → "4588.88→4607.51" (separator fingerprint → panah display).
func fmtRng(r string) string { return strings.Replace(r, "-", "→", 1) }

// offlineNote = catatan konteks bila gap sejak tick terakhir jauh melebihi
// interval poll (daemon mati / Mac sleep, mis. server dimatikan semalam).
// Ambang = max(6×interval, 30 menit) supaya tick lambat/jaringan labil tidak
// false-positive. lastTick zero (state lama / run perdana) atau gap normal → "".
// Italic `_…_` aman: catatan ini di LUAR blok monospace, parse_mode=Markdown.
func offlineNote(lastTick, now time.Time, interval time.Duration) string {
	if lastTick.IsZero() {
		return ""
	}
	thr := 6 * interval
	if thr < 30*time.Minute {
		thr = 30 * time.Minute
	}
	if gap := now.Sub(lastTick); gap >= thr {
		return fmt.Sprintf("🕘 _Daemon sempat offline ≈%s — diff vs kondisi sebelum offline; perubahan selama offline tidak terekam._", fmtDur(gap))
	}
	return ""
}

// fmtDur memformat durasi gaya ringkas: "45 menit", "9 jam", "9 jam 15 menit",
// "2 hari 12 jam" (≥48 jam menit dibuang — presisi tak relevan lagi).
func fmtDur(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	switch {
	case m < 60:
		return fmt.Sprintf("%d menit", m)
	case m < 48*60:
		if m%60 == 0 {
			return fmt.Sprintf("%d jam", m/60)
		}
		return fmt.Sprintf("%d jam %d menit", m/60, m%60)
	default:
		h := m / 60
		return fmt.Sprintf("%d hari %d jam", h/24, h%24)
	}
}

// watchlistFingerprint = string deterministik kondisi watchlist: entri zona POI
// per-TF + token sisi Midnight Open ("MO|above/below/none") — perubahan salah
// satunya (termasuk harga menyeberang MO) memicu pesan ⚠️ ADA PERUBAHAN.
func watchlistFingerprint(n engine.ScanNarrative) string {
	var parts []string
	for _, p := range n.NextPOIsByTF {
		parts = append(parts, fmt.Sprintf("%s|%s|%.2f-%.2f|T%d|%d",
			p.TF.String(), p.Dir.String(), p.FullBottom, p.FullTop, p.Tier, p.Confluence))
	}
	parts = append(parts, "MO|"+moSide(n))
	parts = append(parts, "ASIA|"+asiaToken(n))
	for _, m := range n.RelEquals {
		parts = append(parts, fmt.Sprintf("%s|%s|%.2f|%d", m.Kind, m.TF.String(), m.Level, m.Count))
	}
	parts = append(parts, amsTokens(n)...) // AMS ITL/ITH dua arah (perubahan → alert)
	return strings.Join(parts, ";")
}

// maybeSendNewsAlerts = satu evaluasi alert news (CPI/PPI/NFP) dalam daemon
// alertd, reuse internal/news + helper bersama news.Decide/PickTarget. Logika
// identik dengan cmd/newsalert: untuk tiap indikator dipantau, pilih rilis
// terdekat → putuskan pre/post → kirim Telegram (dedup via news.State JSON
// TERPISAH dari alert_state.json alertd). Error fetch dicatat & RETURN (jangan
// crash) — feed Forex Factory bisa 429/down. Dipanggil HANYA bila d.newsEnabled.
func (d *daemon) maybeSendNewsAlerts(now time.Time) {
	events, fromCache, err := news.FetchCalendarCached(d.newsHTTP, d.newsFeedCache)
	if err != nil {
		log.Printf("news: fetch kalender gagal (skip tick): %v", err)
		return
	}
	if fromCache {
		log.Printf("news: ⚠️ feed live gagal — pakai cache terakhir (data bisa basi)")
	}
	releases := news.GroupReleases(news.FilterUSDHighImpact(events), news.DefaultName)

	st, err := news.LoadState(d.newsStatePath)
	if err != nil {
		log.Printf("news: load state: %v (lanjut tanpa dedup)", err)
	}

	changed := false
	for _, name := range d.newsNames {
		r, ok := news.PickTarget(releases, name, now, d.newsStale)
		if !ok {
			continue
		}
		key := r.Key()
		// Fallback BLS: rilis sudah lewat tapi mirror FF belum isi actual → tarik
		// dari penerbit asli (BLS). Guard bulan-referensi di EnrichFromBLS cegah
		// isian prematur. Hanya saat -news-bls & belum pernah kirim post.
		blsUsed := false
		if d.newsBLS && now.After(r.Time) && !r.Released() && !st.PostSent(key) {
			if er, n, err := news.EnrichFromBLS(r, d.newsHTTP, d.newsBLSKey, now); err != nil {
				log.Printf("news: %s BLS enrich gagal: %v", name, err)
			} else if n > 0 {
				r, blsUsed = er, true
				log.Printf("news: %s %d angka diisi dari BLS (mirror FF telat)", name, n)
			}
		}
		mode, mins := news.Decide(r, now, st, d.newsPrewindow, d.newsStale)
		switch mode {
		case "pre":
			msg := news.BuildPreMessage(r, d.newsNowcast, d.newsSupport, mins)
			if err := d.notifier.SendMessage(msg); err != nil {
				log.Printf("news: %s ANCANG-ANCANG aksi=gagal_kirim: %v", name, err)
				continue
			}
			st.MarkPre(key, now)
			changed = true
			log.Printf("news: %s ANCANG-ANCANG terkirim (≈%d menit lagi)", name, mins)
		case "post":
			msg := news.BuildPostMessage(r, d.newsSupport)
			if blsUsed {
				msg += news.BLSNote
			}
			if err := d.notifier.SendMessage(msg); err != nil {
				log.Printf("news: %s PASCA-RILIS aksi=gagal_kirim: %v", name, err)
				continue
			}
			st.MarkPost(key, now)
			changed = true
			v := r.Classify()
			log.Printf("news: %s PASCA-RILIS terkirim (bias=%s, %s)", name, v.Bias.String(), v.Label)
		}
	}

	if changed {
		if err := st.Save(d.newsStatePath); err != nil {
			log.Printf("news: simpan state: %v", err)
		}
	}
}

// formatSignal merangkai pesan Markdown ringkas dari Signal (HANYA field exported).
func formatSignal(sig engine.Signal, inst string) string {
	emoji := "🟢"
	arah := "BUY"
	if sig.Dir.String() == "bearish" {
		emoji = "🔴"
		arah = "SELL"
	}
	return fmt.Sprintf(
		"%s *%s %s*\n"+
			"Entry: `%.2f`\n"+
			"SL: `%.2f`\n"+
			"TP: `%.2f`\n"+
			"RR: `1:%.1f`\n"+
			"Session: %s\n"+
			"POI Tier: %d\n"+
			"Waktu: %s",
		emoji, arah, inst,
		sig.Entry, sig.SL, sig.TP, sig.RR,
		sig.Session.String(), sig.POITier,
		sig.Time.Format("2006-01-02 15:04 MST"),
	)
}
