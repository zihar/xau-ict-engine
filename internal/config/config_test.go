package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"forex-backtest/internal/detectors"
	"forex-backtest/internal/engine"
)

// writeTemp menulis isi ke file sementara dan mengembalikan path-nya.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("tulis temp config: %v", err)
	}
	return path
}

// TestLoadOverride memastikan field yang ada di file di-override, sedangkan
// field yang absen tetap memakai default.
func TestLoadOverride(t *testing.T) {
	def := engine.DefaultConfig()

	content := `
# config uji — sebagian field saja
min_gap_pips: 10            # override float
confluence_min: 1           # override int
exec_layer_on: true         # override bool
break_type: body            # override enum
sl_mode: body_poi
zone_fib_tf: weekly         # override ZoneFibTF
risk_pct: 0.01
max_poi_tier: 4
asia_close_gate: true       # override gate toggle (default false)
day_type_gate: false        # override gate toggle (default true)
of_swing_atr_mult: 0        # override float fix OF (default 1.0)
skip_entry_hours_ny: 8,20   # override CSV jam skip (default "8")
`
	cfg, err := Load(writeTemp(t, content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// --- override ---
	if cfg.MinGapPips != 10 {
		t.Errorf("MinGapPips = %v, mau 10", cfg.MinGapPips)
	}
	if cfg.ConfluenceMin != 1 {
		t.Errorf("ConfluenceMin = %v, mau 1", cfg.ConfluenceMin)
	}
	if !cfg.ExecLayerOn {
		t.Errorf("ExecLayerOn = %v, mau true", cfg.ExecLayerOn)
	}
	if cfg.BreakType != detectors.BreakBody {
		t.Errorf("BreakType = %v, mau BreakBody", cfg.BreakType)
	}
	if cfg.SLMode != detectors.SLBodyPOI {
		t.Errorf("SLMode = %v, mau SLBodyPOI", cfg.SLMode)
	}
	if cfg.ZoneFibTF != engine.ZoneFibWeekly {
		t.Errorf("ZoneFibTF = %v, mau ZoneFibWeekly", cfg.ZoneFibTF)
	}
	if cfg.RiskPct != 0.01 {
		t.Errorf("RiskPct = %v, mau 0.01", cfg.RiskPct)
	}
	if cfg.MaxPOITier != 4 {
		t.Errorf("MaxPOITier = %v, mau 4 (override, beda dari default 3)", cfg.MaxPOITier)
	}
	if !cfg.AsiaCloseGate {
		t.Errorf("AsiaCloseGate = %v, mau true (override)", cfg.AsiaCloseGate)
	}
	if cfg.DayTypeGate {
		t.Errorf("DayTypeGate = %v, mau false (override)", cfg.DayTypeGate)
	}
	if cfg.OFSwingATRMult != 0 {
		t.Errorf("OFSwingATRMult = %v, mau 0 (override)", cfg.OFSwingATRMult)
	}
	if cfg.SkipEntryHoursNY != "8,20" {
		t.Errorf("SkipEntryHoursNY = %q, mau \"8,20\" (override)", cfg.SkipEntryHoursNY)
	}

	// --- field absen tetap default ---
	if cfg.VIMinGapPips != def.VIMinGapPips {
		t.Errorf("VIMinGapPips = %v, mau default %v", cfg.VIMinGapPips, def.VIMinGapPips)
	}
	if cfg.ATRPeriod != def.ATRPeriod {
		t.Errorf("ATRPeriod = %v, mau default %v", cfg.ATRPeriod, def.ATRPeriod)
	}
	if cfg.StartBalance != def.StartBalance {
		t.Errorf("StartBalance = %v, mau default %v", cfg.StartBalance, def.StartBalance)
	}
	if cfg.RetraceFVGGate != def.RetraceFVGGate {
		t.Errorf("RetraceFVGGate = %v, mau default %v", cfg.RetraceFVGGate, def.RetraceFVGGate)
	}
	if cfg.MaxTradePerDay != def.MaxTradePerDay {
		t.Errorf("MaxTradePerDay = %v, mau default %v", cfg.MaxTradePerDay, def.MaxTradePerDay)
	}
	if cfg.SessionPMGate != def.SessionPMGate {
		t.Errorf("SessionPMGate = %v, mau default %v", cfg.SessionPMGate, def.SessionPMGate)
	}
	if cfg.LondonQ34Only != def.LondonQ34Only {
		t.Errorf("LondonQ34Only = %v, mau default %v", cfg.LondonQ34Only, def.LondonQ34Only)
	}
}

// TestLoadEmptyEqualsDefault memastikan file kosong/komentar = DefaultConfig.
func TestLoadEmptyEqualsDefault(t *testing.T) {
	content := "# cuma komentar\n\n   \n# baris kosong di atas\n"
	cfg, err := Load(writeTemp(t, content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// reflect.DeepEqual: Config kini punya field map (NewsSkipHourStarts) → tak bisa
	// dibandingkan dgn ==. Map ini nil di keduanya (runtime-only, live-set) → setara.
	if !reflect.DeepEqual(cfg, engine.DefaultConfig()) {
		t.Errorf("file kosong harus = DefaultConfig\n got %+v\nwant %+v", cfg, engine.DefaultConfig())
	}
}

// TestLoadUnknownKeyIgnored memastikan key tak dikenal diabaikan (tanpa error).
func TestLoadUnknownKeyIgnored(t *testing.T) {
	content := "instrument: XAU_USD\nfoo_bar_baz: 123\nmin_gap_pips: 7\n"
	cfg, err := Load(writeTemp(t, content))
	if err != nil {
		t.Fatalf("Load harus abaikan key tak dikenal, malah error: %v", err)
	}
	if cfg.MinGapPips != 7 {
		t.Errorf("MinGapPips = %v, mau 7 (key dikenal tetap diproses)", cfg.MinGapPips)
	}
}

// TestLoadBadFloat memastikan tipe salah → error jelas dengan nomor baris.
func TestLoadBadFloat(t *testing.T) {
	content := "min_gap_pips: bukan_angka\n"
	_, err := Load(writeTemp(t, content))
	if err == nil {
		t.Fatal("mau error untuk float tak valid, dapat nil")
	}
	if !strings.Contains(err.Error(), "baris 1") {
		t.Errorf("error harus sebut nomor baris: %v", err)
	}
	if !strings.Contains(err.Error(), "min_gap_pips") {
		t.Errorf("error harus sebut key: %v", err)
	}
}

// TestLoadBadInt memastikan int tak valid → error.
func TestLoadBadInt(t *testing.T) {
	content := "confluence_min: 2.5\n"
	if _, err := Load(writeTemp(t, content)); err == nil {
		t.Fatal("mau error untuk int tak valid (2.5), dapat nil")
	}
}

// TestLoadBadBool memastikan bool tak valid → error.
func TestLoadBadBool(t *testing.T) {
	content := "exec_layer_on: yes\n"
	if _, err := Load(writeTemp(t, content)); err == nil {
		t.Fatal("mau error untuk bool tak valid (yes), dapat nil")
	}
}

// TestLoadBadEnum memastikan enum tak dikenal → error.
func TestLoadBadEnum(t *testing.T) {
	content := "zone_fib_tf: monthly\n"
	if _, err := Load(writeTemp(t, content)); err == nil {
		t.Fatal("mau error untuk zone_fib_tf=monthly, dapat nil")
	}
}

// TestLoadMissingFile memastikan path tak ada → error.
func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/path/yang/tidak/ada/config.yaml"); err == nil {
		t.Fatal("mau error untuk file tak ada, dapat nil")
	}
}
