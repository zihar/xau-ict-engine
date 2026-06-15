// Command backtest menjalankan pipeline strategi (Section N.2) atas candle yang
// sudah di-cache (lihat `cmd/fetch`) lalu cetak metrik Section L + tulis CSV
// per-trade. Murni offline — tidak menyentuh OANDA / jaringan.
//
// Contoh:
//
//	go run ./cmd/backtest                         # default XAU_USD, exec-layer OFF
//	go run ./cmd/backtest -exec -out trades.csv   # execution layer ON (J.2) + CSV
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"forex-backtest/internal/config"
	"forex-backtest/internal/data"
	"forex-backtest/internal/engine"
	"forex-backtest/internal/report"
)

func main() {
	var (
		dir         = flag.String("data", "data", "direktori cache CSV")
		instrument  = flag.String("instrument", "XAU_USD", "instrumen")
		balance     = flag.Float64("balance", 25000, "saldo awal (basis minggu pertama)")
		execLayer   = flag.Bool("exec", false, "aktifkan execution layer J.2 (default OFF = alert mode)")
		impulse     = flag.Bool("impulse", false, "paksa regime IMPULSE (semua trade searah weekly OF; matikan retrace counter-OF)")
		retraceGate = flag.Bool("retracegate", true, "Section E: retrace terikat target weekly FVG & berakhir saat terisi (false = retrace unbounded lama)")
		poiWindow   = flag.Int("poiwindow", -1, "POITouchWindowBars: POI valid kalau disentuh dalam N candle H1 (0=point-in-time; <0=pakai default)")
		stdOnly     = flag.String("stdonly", "", "quality: hanya trigger standard ('true'/'false'; kosong=default)")
		maxTier     = flag.Int("maxtier", -1, "quality: hanya POI tier <= N (0=tanpa batas; <0=default)")
		htfATR      = flag.Float64("htfatr", -1, "MinSwingATRMultHTF: filter swing HTF >= N×ATR (0=off; <0=default)")
		csvOut      = flag.String("out", "", "tulis per-trade record ke CSV (kosong = tidak)")
		xlsxOut     = flag.String("xlsx", "", "tulis per-trade record ke Excel .xlsx (kolom reason per baris; kosong = tidak)")
		kunci3      = flag.Bool("kunci3", false, "F.5 Kunci #3 fallback opposing-liquidity saat tak ada POI (default OFF = perilaku lama)")
		asiaGate    = flag.Bool("asiagate", true, "gate Asia-close (false = OFF)")
		qtPhase     = flag.Bool("qtphase", true, "gate QT phase tradeable daily (false = OFF)")
		weeklyPhase = flag.Bool("weeklyphase", false, "gate weekly phase AND di atas daily (default OFF = weekly dimatikan)")
		dayType     = flag.Bool("daytype", true, "gate day-type heavy_accum (false = OFF)")
		londonQT    = flag.Bool("londonqt", true, "Phase2: London hanya Q3/Q4 (false = OFF, semua quarter London boleh)")
		amsGate     = flag.Bool("ams", true, "gate AMS struktur intermediate 1H (false = OFF)")
		moGate      = flag.Bool("mogate", false, "gate Midnight Open: buy hanya di bawah MO 00:00 NY, sell hanya di atas (pertemuan 8; default OFF)")
		releqSweep  = flag.Bool("releqsweep", false, "gate wait-for-sweep REH/REL: entry butuh liquidity searah bias tersapu baru-baru ini (default OFF)")
		releqTol    = flag.Float64("releqtol", -1, "RelEqTolerancePct: toleransi equal % harga (<0 = default 0.05)")
		poiWick     = flag.Bool("poiwick", true, "invalidasi PDR pakai wick (false = body close saja)")
		fractal     = flag.Bool("fractal", true, "POI fractal multi-TF H1+H4+D+W (false = H1 saja)")
		agenda      = flag.Bool("agenda", true, "agenda gate: target = FVG HTF (false = OFF)")
		agendaNear  = flag.Bool("agendanearest", false, "agenda: pilih FVG HTF TERDEKAT (true) vs terjauh (false)")
		fibGate     = flag.Bool("fibgate", true, "gate Fib zone (false = OFF, Fib jadi non-gating)")
		pmGate      = flag.Bool("pmgate", true, "gate sesi PM Q4 12:00-18:00 NY (false = OFF, izinkan entry PM)")
		cfgPath     = flag.String("config", "", "path config.yaml (kosong = engine.DefaultConfig); flag lain tetap override di atasnya")
		ofATR       = flag.Float64("ofatr", -1, "OFSwingATRMult: filter swing WeeklyOF/Regime >= N×ATR weekly (0=off; <0=default)")
		biasATR     = flag.Float64("biasatr", -1, "BiasSwingATRMult: filter swing DailyBias >= N×ATR daily (0=off; <0=default)")
		obDisable   = flag.Bool("obdisable", true, "buang Order Block dari pool PD Array semua TF (false = OB aktif lagi)")
		obStrict    = flag.Bool("obstrict", false, "OB versi-pertemuan-4: reversal+displacement-FVG+liquidity-sweep (false = proxy lama). Relevan hanya bila -obdisable=false")
		bprDir      = flag.Bool("bprdir", false, "BPR versi-pertemuan-4: directional 1-arah (FVG lebih baru, zona irisan) (false = dua-arah lama)")
		bprDisable  = flag.Bool("bprdisable", false, "buang BPR dari pool entry (diuji: OOS 2.08→1.97 lebih buruk → tetap aktif; knob re-test)")
		skipHours   = flag.String("skiphours", "<default>", "jam NY skip-entry CSV (mis. \"8\" atau \"8,20\"; \"\"=tanpa skip)")
		londonQ4    = flag.Bool("londonq4", false, "London hanya Q4 04:30-06:00 NY (eksperimen, lebih ketat dari Q3/Q4)")
		londonMinH  = flag.Int("londonminh", -1, "LondonMinHourNY: entry London hanya jam NY >= N (0=off; <0=default)")
		retraceTP   = flag.Bool("retracetp", true, "TP trade retrace = level weekly FVG target (false = RR standar)")
		bearConfirm = flag.Int("bearconfirm", -1, "OFBearConfirmWeeks: flip bull→bear butuh close minggu ke-N tetap di bawah close trigger (0=off; <0=default)")
		biasAge     = flag.Int("biasage", -1, "MinBiasAgeDays: entry hanya bila daily bias berumur >= N hari (0=off; <0=default)")
		ofAge       = flag.Int("ofage", -1, "MaxOFAgeDays: entry hanya bila weekly OF berumur <= N hari (0=off; <0=default)")
		skipRetrace = flag.Bool("skipretrace", false, "regime retrace = NO TRADE sama sekali (beda dari -impulse yg memaksa searah OF)")
		maxConf     = flag.Int("maxconf", -1, "MaxConfluence: buang POI ber-komponen > N, semua TF (0=off; <0=default)")
		bbFVG       = flag.Bool("bbfvg", false, "BBNeedsFVG: Breaker hanya sah dlm cluster yg juga mengandung FVG/FVGBreak/IFVG")
		londonSweep = flag.Bool("londonsweep", false, "LondonSweepEntry: bypass gate jam/quarter London bila likuiditas Asia sudah di-sweep berlawanan bias")
		biasAgeBull = flag.Int("biasagebull", -1, "MinBiasAgeDaysBull: karantina umur-bias khusus BULLISH (-1=ikut biasage; 0=tanpa karantina) — apply hanya bila flag di-set")
		trigMode    = flag.String("trigmode", "", "EntryTriggerMode: itl|disp|reject|dispreject|sweep (kosong=default)")
		dispATR     = flag.Float64("dispatr", -1, "DispATRMult: ambang body displacement × ATR m5 (<0=default)")
	)
	flag.Parse()

	// flagSet[name]=true kalau flag DI-SET eksplisit di CLI. Dipakai supaya flag
	// dengan nilai default tidak diam-diam menimpa nilai dari -config.
	flagSet := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { flagSet[f.Name] = true })

	load := func(g string) []data.Candle {
		path := data.CSVPath(*dir, *instrument, g)
		c, err := data.ReadCSV(path)
		if err != nil {
			log.Fatalf("baca %s: %v (jalankan `go run ./cmd/fetch` dulu)", path, err)
		}
		return c
	}

	tf := engine.TFData{
		Weekly: load("W"),
		Daily:  load("D"),
		H4:     load("H4"),
		H1:     load("H1"),
		M5:     load("M5"),
	}
	fmt.Printf("Loaded %s: W=%d D=%d H4=%d H1=%d M5=%d\n\n",
		*instrument, len(tf.Weekly), len(tf.Daily), len(tf.H4), len(tf.H1), len(tf.M5))

	// Basis config: -config kalau di-set, kalau tidak DefaultConfig.
	cfg := engine.DefaultConfig()
	if *cfgPath != "" {
		loaded, err := config.Load(*cfgPath)
		if err != nil {
			log.Fatalf("muat config: %v", err)
		}
		cfg = loaded
	}

	// Flag CLI override DI ATAS config. Hanya flag yang DI-SET eksplisit yang
	// menimpa (kecuali flag tanpa default sensibel seperti -stdonly yang pakai
	// sentinel sendiri), supaya nilai dari -config tidak terhapus default flag.
	if flagSet["balance"] {
		cfg.StartBalance = *balance
	}
	if flagSet["exec"] {
		cfg.ExecLayerOn = *execLayer
	}
	if flagSet["impulse"] {
		cfg.ImpulseOnly = *impulse
	}
	if flagSet["retracegate"] {
		cfg.RetraceFVGGate = *retraceGate
	}
	if flagSet["mogate"] {
		cfg.MOGate = *moGate
	}
	if flagSet["releqsweep"] {
		cfg.RelEqSweepGate = *releqSweep
	}
	if *releqTol >= 0 {
		cfg.RelEqTolerancePct = *releqTol
	}
	if *poiWindow >= 0 {
		cfg.POITouchWindowBars = *poiWindow
	}
	if *stdOnly == "true" {
		cfg.RequireStandardTrigger = true
	} else if *stdOnly == "false" {
		cfg.RequireStandardTrigger = false
	}
	if *maxTier >= 0 {
		cfg.MaxPOITier = *maxTier
	}
	if *htfATR >= 0 {
		cfg.MinSwingATRMultHTF = *htfATR
	}
	if flagSet["kunci3"] {
		cfg.Kunci3Fallback = *kunci3
	}
	if flagSet["asiagate"] {
		cfg.AsiaCloseGate = *asiaGate
	}
	if flagSet["qtphase"] {
		cfg.QTPhaseGate = *qtPhase
	}
	if flagSet["weeklyphase"] {
		cfg.WeeklyPhaseGate = *weeklyPhase
	}
	if flagSet["daytype"] {
		cfg.DayTypeGate = *dayType
	}
	if flagSet["londonqt"] {
		cfg.LondonQ34Only = *londonQT
	}
	if flagSet["ams"] {
		cfg.AMSGate = *amsGate
	}
	if flagSet["poiwick"] {
		cfg.POIBreakWick = *poiWick
	}
	if flagSet["fractal"] {
		cfg.FractalPOI = *fractal
	}
	if flagSet["agenda"] {
		cfg.AgendaGate = *agenda
	}
	if flagSet["agendanearest"] {
		cfg.AgendaNearest = *agendaNear
	}
	if flagSet["fibgate"] {
		cfg.FibZoneGate = *fibGate
	}
	if flagSet["pmgate"] {
		cfg.SessionPMGate = *pmGate
	}
	if *ofATR >= 0 {
		cfg.OFSwingATRMult = *ofATR
	}
	if *biasATR >= 0 {
		cfg.BiasSwingATRMult = *biasATR
	}
	if flagSet["obdisable"] {
		cfg.DisableOB = *obDisable
	}
	if flagSet["obstrict"] {
		cfg.OBStrict = *obStrict
	}
	if flagSet["bprdir"] {
		cfg.BPRDirectional = *bprDir
	}
	if flagSet["bprdisable"] {
		cfg.DisableBPR = *bprDisable
	}
	if *skipHours != "<default>" {
		cfg.SkipEntryHoursNY = *skipHours
	}
	if flagSet["londonq4"] {
		cfg.LondonQ4Only = *londonQ4
	}
	if *londonMinH >= 0 {
		cfg.LondonMinHourNY = *londonMinH
	}
	if flagSet["retracetp"] {
		cfg.RetraceTPToFVG = *retraceTP
	}
	if *bearConfirm >= 0 {
		cfg.OFBearConfirmWeeks = *bearConfirm
	}
	if *biasAge >= 0 {
		cfg.MinBiasAgeDays = *biasAge
	}
	if *ofAge >= 0 {
		cfg.MaxOFAgeDays = *ofAge
	}
	if flagSet["skipretrace"] {
		cfg.SkipRetrace = *skipRetrace
	}
	if *maxConf >= 0 {
		cfg.MaxConfluence = *maxConf
	}
	if flagSet["bbfvg"] {
		cfg.BBNeedsFVG = *bbFVG
	}
	if flagSet["londonsweep"] {
		cfg.LondonSweepEntry = *londonSweep
	}
	if flagSet["biasagebull"] {
		cfg.MinBiasAgeDaysBull = *biasAgeBull
	}
	if *trigMode != "" {
		cfg.EntryTriggerMode = *trigMode
	}
	if *dispATR >= 0 {
		cfg.DispATRMult = *dispATR
	}

	res, err := engine.Run(tf, cfg)
	if err != nil {
		log.Fatalf("run engine: %v", err)
	}

	report.Print(os.Stdout, res)

	if *csvOut != "" {
		if err := report.WriteCSV(*csvOut, res); err != nil {
			log.Fatalf("tulis CSV: %v", err)
		}
		fmt.Printf("\nPer-trade record (CSV) → %s\n", *csvOut)
	}
	if *xlsxOut != "" {
		if err := report.WriteXLSX(*xlsxOut, res); err != nil {
			log.Fatalf("tulis XLSX: %v", err)
		}
		abs, _ := filepath.Abs(*xlsxOut)
		fmt.Printf("\nPer-trade record (Excel + kolom reason) → %s\n", abs)
	}
}
