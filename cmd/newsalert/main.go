// Command newsalert = alert rilis berita ekonomi (CPI/PPI/NFP) untuk XAUUSD.
// Menarik kalender Forex Factory (gratis, tanpa key), lalu untuk indikator yang
// dipantau mengirim DUA jenis alert ke Telegram:
//
//  1. ANCANG-ANCANG (pra-rilis) — saat rilis tinggal ≤ -prewindow: forecast vs
//     previous + nowcast leading (opsional) + level kunci + apa yang dipantau.
//  2. PASCA-RILIS — saat feed mengisi "actual": actual vs forecast → surprise →
//     bias XAUUSD via playbook (rezim 2026: data panas/kuat = bearish gold).
//
// Dedup persisten (news.State JSON) → aman dijalankan tiap beberapa menit oleh
// scheduler (systemd-timer/cron) tanpa kirim ganda. READ-ONLY: hanya tarik data.
//
// Contoh:
//
//	set -a; . ./.env; set +a
//	go run ./cmd/newsalert -event CPI -dry                 # cek (cetak ke stdout, tak kirim)
//	go run ./cmd/newsalert -event CPI,PPI -once            # satu evaluasi → kirim Telegram
//	go run ./cmd/newsalert -event CPI -nowcast "Cleveland Fed: m/m +0.32%, y/y 4.1%"
//	go run ./cmd/newsalert -event CPI,PPI -interval 5m     # loop (B): poll tiap 5 menit
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"forex-backtest/internal/news"
	"forex-backtest/internal/notify"
)

func main() {
	var (
		eventCSV  = flag.String("event", "CPI", "indikator dipantau (pisah koma): CPI,PPI,NFP")
		dry       = flag.Bool("dry", false, "cetak pesan ke stdout, JANGAN kirim Telegram (tanpa butuh env TG)")
		once      = flag.Bool("once", true, "satu evaluasi lalu keluar (false + -interval = loop)")
		interval  = flag.Duration("interval", 0, "loop tiap durasi ini (mis. 5m); 0 = sekali (pakai -once)")
		nowcast   = flag.String("nowcast", "", "catatan leading indicator pra-rilis (mis. Cleveland Fed nowcast)")
		support   = flag.String("support", "$4.350–4.500", "zona harga kunci yang ditampilkan di pesan")
		statePath = flag.String("state", "data/news_state.json", "file state dedup alert news")
		feedCache = flag.String("feedcache", "data/news_feed_cache.json", "cache body feed terakhir (fallback saat feed 429/down); kosong = tanpa cache")
		prewindow = flag.Duration("prewindow", 60*time.Minute, "kirim ANCANG-ANCANG saat rilis tinggal ≤ durasi ini")
		stale     = flag.Duration("stale", 12*time.Hour, "jangan kirim PASCA-RILIS bila rilis sudah lebih tua dari ini")
		bls       = flag.Bool("bls", false, "fallback API BLS (penerbit asli) untuk isi `actual` saat mirror FF telat publish")
		blsKey    = flag.String("bls-key", os.Getenv("BLS_API_KEY"), "registration key BLS opsional (kosong=25 query/hari, terdaftar=500/hari)")
		simulate  = flag.String("simulate", "", "PREVIEW pesan pasca-rilis dgn actual hipotetis: 'CPI m/m=0.5%,CPI y/y=4.5%' (paksa -dry, tak sentuh state)")
		digest    = flag.Bool("digest", false, "MODE reminder MINGGUAN: kirim daftar rilis high-impact (CPI/PPI/NFP/FOMC) pekan ini, dedup per-minggu (dipasang via timer Senin pagi)")
	)
	flag.Parse()

	names := news.ParseNames(*eventCSV)
	if len(names) == 0 {
		log.Fatal("-event kosong — isi mis. CPI,PPI,NFP")
	}

	// Mode preview: render pesan pasca-rilis dgn actual hipotetis (selalu dry).
	if *simulate != "" {
		if err := previewPost(names, parseSimulate(*simulate), *support, *feedCache); err != nil {
			log.Fatalf("simulate: %v", err)
		}
		return
	}

	var notifier notify.Notifier
	if !*dry {
		tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
		tgChatID := os.Getenv("TELEGRAM_CHAT_ID")
		if tgToken == "" || tgChatID == "" {
			log.Fatal("TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID kosong — set di env/.env (atau pakai -dry)")
		}
		notifier = notify.NewTelegram(tgToken, tgChatID)
	}

	app := &app{
		notifier:  notifier,
		dry:       *dry,
		names:     names,
		nowcast:   *nowcast,
		support:   *support,
		statePath: *statePath,
		feedCache: *feedCache,
		prewindow: *prewindow,
		stale:     *stale,
		bls:       *bls,
		blsKey:    *blsKey,
		http:      &http.Client{Timeout: 20 * time.Second},
	}

	if *digest {
		app.runDigest()
		return
	}

	if *interval > 0 && !*once {
		log.Printf("newsalert loop: event=%s interval=%s", strings.Join(names, ","), *interval)
		app.run()
		for range time.NewTicker(*interval).C {
			app.run()
		}
		return
	}
	app.run()
}

type app struct {
	notifier  notify.Notifier
	dry       bool
	names     []string
	nowcast   string
	support   string
	statePath string
	feedCache string
	prewindow time.Duration
	stale     time.Duration
	bls       bool
	blsKey    string
	http      *http.Client
}

// run = satu evaluasi: fetch → group → untuk tiap indikator dipantau, putuskan
// & kirim alert pra/pasca (dedup via state). Error fetch dicatat, tidak fatal
// (loop mode tetap hidup).
func (a *app) run() {
	events, fromCache, err := news.FetchCalendarCached(a.http, a.feedCache)
	if err != nil {
		log.Printf("fetch kalender: %v", err)
		return
	}
	if fromCache {
		log.Printf("⚠️ feed live gagal — pakai cache terakhir (data bisa basi)")
	}
	releases := news.GroupReleases(news.FilterUSDHighImpact(events), news.DefaultName)

	st, err := news.LoadState(a.statePath)
	if err != nil {
		log.Printf("load state: %v (lanjut tanpa dedup)", err)
	}

	now := time.Now().UTC()
	changed := false
	for _, name := range a.names {
		r, ok := news.PickTarget(releases, name, now, a.stale)
		if !ok {
			log.Printf("%s: tak ada rilis relevan minggu ini", name)
			continue
		}
		key := r.Key()
		// Fallback BLS: bila rilis sudah lewat tapi mirror FF belum isi actual,
		// tarik angka dari penerbit asli (BLS). Guard bulan-referensi di EnrichFromBLS
		// cegah isian prematur. Hanya saat -bls & belum pernah kirim post.
		blsUsed := false
		if a.bls && now.After(r.Time) && !r.Released() && !st.PostSent(key) {
			if er, n, err := news.EnrichFromBLS(r, a.http, a.blsKey, now); err != nil {
				log.Printf("%s: BLS enrich gagal: %v", name, err)
			} else if n > 0 {
				r, blsUsed = er, true
				log.Printf("%s: %d angka diisi dari BLS (mirror FF telat)", name, n)
			}
		}
		mode, mins := news.Decide(r, now, st, a.prewindow, a.stale)
		switch mode {
		case "pre":
			msg := news.BuildPreMessage(r, a.nowcast, a.support, mins)
			if a.send(msg) {
				st.MarkPre(key, now)
				changed = true
				log.Printf("%s: ANCANG-ANCANG terkirim (≈%d menit lagi)", name, mins)
			}
		case "post":
			msg := news.BuildPostMessage(r, a.support)
			if blsUsed {
				msg += news.BLSNote
			}
			if a.send(msg) {
				st.MarkPost(key, now)
				changed = true
				v := r.Classify()
				log.Printf("%s: PASCA-RILIS terkirim (bias=%s, %s)", name, v.Bias.String(), v.Label)
			}
		default:
			log.Printf("%s: tidak ada aksi (rilis %s, ≈%d menit dari now, pre_sent=%v post_sent=%v)",
				name, r.Time.In(time.FixedZone("WIB", 7*3600)).Format("Mon 15:04 WIB"),
				mins, st.PreSent(key), st.PostSent(key))
		}
	}

	if changed && !a.dry {
		if err := st.Save(a.statePath); err != nil {
			log.Printf("simpan state: %v", err)
		}
	}
}

// runDigest = MODE reminder mingguan: fetch kalender → susun daftar rilis
// high-impact (CPI/PPI/NFP/FOMC) pekan ini → kirim sekali per-minggu (dedup
// via WeekKey di state). Dipanggil sekali per eksekusi (dipasang via timer
// Senin pagi); aman dijalankan ulang — minggu yang sudah terkirim di-skip.
func (a *app) runDigest() {
	events, fromCache, err := news.FetchCalendarCached(a.http, a.feedCache)
	if err != nil {
		log.Printf("fetch kalender: %v", err)
		return
	}
	if fromCache {
		log.Printf("⚠️ feed live gagal — pakai cache terakhir (data bisa basi)")
	}

	now := time.Now().UTC()
	st, err := news.LoadState(a.statePath)
	if err != nil {
		log.Printf("load state: %v (lanjut tanpa dedup)", err)
	}
	wkey := news.WeekKey(now)
	if st.DigestSent(wkey) {
		log.Printf("digest mingguan %s sudah terkirim — skip", wkey)
		return
	}

	rel := news.WeekDigestReleases(events)
	msg := news.BuildWeekDigest(rel, now)
	if a.send(msg) {
		st.MarkDigest(wkey, now)
		if !a.dry {
			if err := st.Save(a.statePath); err != nil {
				log.Printf("simpan state: %v", err)
			}
		}
		log.Printf("digest mingguan terkirim (%d rilis, minggu %s)", len(rel), wkey)
	}
}

// send mengirim pesan (Telegram atau stdout untuk -dry). return true bila sukses.
func (a *app) send(msg string) bool {
	if a.dry {
		println("\n---------- (dry-run, tidak dikirim) ----------")
		println(msg)
		println("----------------------------------------------")
		return true
	}
	if err := a.notifier.SendMessage(msg); err != nil {
		log.Printf("kirim Telegram gagal: %v", err)
		return false
	}
	return true
}

// previewPost menarik feed live, lalu untuk tiap indikator dipantau mengisi
// actual hipotetis (actuals[judul]=nilai) dan mencetak pesan pasca-rilis ke
// stdout. Tidak kirim Telegram, tidak sentuh state — murni untuk pratinjau/uji.
func previewPost(names []string, actuals map[string]string, support, feedCache string) error {
	events, fromCache, err := news.FetchCalendarCached(&http.Client{Timeout: 20 * time.Second}, feedCache)
	if err != nil {
		return err
	}
	if fromCache {
		log.Printf("⚠️ feed live gagal — preview pakai cache terakhir (data bisa basi)")
	}
	releases := news.GroupReleases(news.FilterUSDHighImpact(events), news.DefaultName)
	now := time.Now().UTC()
	for _, name := range names {
		r, ok := news.PickTarget(releases, name, now, 365*24*time.Hour)
		if !ok {
			log.Printf("%s: tak ada rilis di feed (lewati preview)", name)
			continue
		}
		sim := r.WithActual(actuals)
		println("\n---------- PREVIEW pasca-rilis (simulasi, tidak dikirim) ----------")
		println(news.BuildPostMessage(sim, support))
		println("-------------------------------------------------------------------")
	}
	return nil
}

// parseSimulate memecah "Judul=nilai,Judul2=nilai2" → map[judul]nilai.
func parseSimulate(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k, v := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
		if k != "" {
			out[k] = v
		}
	}
	return out
}
