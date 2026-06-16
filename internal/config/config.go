// Package config memuat parameter engine dari file flat-YAML subset (std-lib
// only — TIDAK ada dependency YAML eksternal, sesuai konvensi repo).
//
// Loader ini SENGAJA minimal: ia hanya memahami subset "key: value" datar yang
// cukup untuk men-tune Config Section M (lihat internal/engine/engine.go). Ia
// BUKAN parser YAML penuh — tidak ada nested map, list, anchor, multi-line, dst.
//
// Format yang didukung per baris:
//   - "key: value"            → override field Config (key snake_case)
//   - "# komentar"            → diabaikan
//   - "key: value # inline"   → komentar inline ikut diabaikan
//   - baris kosong / spasi    → diabaikan
//
// Tipe value: float, int, bool (true/false), string/enum (zone_fib_tf:
// weekly|daily, break_type: wick|body, sl_mode: ltf_structure|body_poi).
//
// Alur Load(path):
//  1. mulai dari engine.DefaultConfig()
//  2. untuk tiap baris valid, override field yang ADA di file
//  3. key tak dikenal → WARNING ke stderr, baris diabaikan (tidak error)
//  4. tipe salah (mis. "abc" untuk float) → error jelas (nomor baris + alasan)
package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"xau-ict-engine/internal/detectors"
	"xau-ict-engine/internal/engine"
)

// Load membaca file flat-YAML di path, mulai dari engine.DefaultConfig() lalu
// meng-override field yang muncul di file. Key tak dikenal menghasilkan warning
// (diabaikan); tipe value yang salah menghasilkan error dengan nomor baris.
func Load(path string) (engine.Config, error) {
	cfg := engine.DefaultConfig()

	f, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("buka config %q: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		key, val, ok := parseLine(sc.Text())
		if !ok {
			continue // baris kosong / komentar penuh
		}
		if err := applyKV(&cfg, key, val); err != nil {
			return cfg, fmt.Errorf("config %s baris %d: %w", path, lineNo, err)
		}
	}
	if err := sc.Err(); err != nil {
		return cfg, fmt.Errorf("baca config %q: %w", path, err)
	}
	return cfg, nil
}

// parseLine memecah satu baris jadi (key, value). ok=false untuk baris kosong
// atau komentar penuh. Komentar inline (# ...) dipangkas dari value.
func parseLine(raw string) (key, val string, ok bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	// pisah key dan value di ":" pertama
	idx := strings.Index(line, ":")
	if idx < 0 {
		// baris tanpa ":" — anggap bukan key-value yang kita pahami; lewati
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	val = strings.TrimSpace(line[idx+1:])
	// pangkas komentar inline
	if h := strings.Index(val, "#"); h >= 0 {
		val = strings.TrimSpace(val[:h])
	}
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

// applyKV meng-override satu field Config berdasarkan key snake_case. Key tak
// dikenal → warning + return nil (diabaikan). Tipe salah → error.
func applyKV(cfg *engine.Config, key, val string) error {
	switch key {
	// --- Deteksi ---
	case "min_gap_pips":
		return setFloat(&cfg.MinGapPips, key, val)
	case "vi_min_gap_pips":
		return setFloat(&cfg.VIMinGapPips, key, val)
	case "poi_max_width_atr_mult":
		return setFloat(&cfg.POIMaxWidthATRMult, key, val)

	// --- PD Array F.1 lanjutan ---
	case "fvg_swing_break_adjacency":
		return setFloat(&cfg.FVGSwingBreakAdjacency, key, val)
	case "fvg_break_geometric":
		return setBool(&cfg.FVGBreakGeometric, key, val)
	case "fvg_break_tfs":
		cfg.FVGBreakTFs = val // CSV TF allow-list promosi FVGBreak (mis. "H1,D"); kosong = semua TF
		return nil
	case "bpr_max_distance_candles":
		return setFloat(&cfg.BPRMaxDistanceCandles, key, val)
	case "confluence_min":
		return setInt(&cfg.ConfluenceMin, key, val)
	case "break_type":
		return setBreakType(&cfg.BreakType, key, val)
	case "atr_period":
		return setInt(&cfg.ATRPeriod, key, val)
	case "min_atr_mult":
		return setFloat(&cfg.MinAtrMult, key, val)
	case "asia_ax_mode":
		switch val {
		case "atr", "ratio", "range":
			cfg.AsiaAXMode = val
			return nil
		default:
			return fmt.Errorf("key %q: %q tidak dikenal (pakai atr/ratio/range)", key, val)
		}
	case "asia_range_ratio":
		return setFloat(&cfg.AsiaRangeRatio, key, val)
	case "asia_require_fvg":
		return setBool(&cfg.AsiaRequireFVG, key, val)
	case "asia_gap_anchor":
		return setBool(&cfg.AsiaGapAnchor, key, val)
	case "q1_ax_mode":
		switch val {
		case "atr", "ratio", "range":
			cfg.Q1AXMode = val
			return nil
		default:
			return fmt.Errorf("key %q: %q tidak dikenal (pakai atr/ratio/range)", key, val)
		}
	case "q1_range_ratio":
		return setFloat(&cfg.Q1RangeRatio, key, val)
	case "q1_gap_anchor":
		return setBool(&cfg.Q1GapAnchor, key, val)
	case "min_retrace_pct":
		return setFloat(&cfg.MinRetracePct, key, val)
	case "h1_window":
		return setInt(&cfg.H1Window, key, val)
	case "m5_day_window":
		return setBool(&cfg.M5DayWindow, key, val)
	case "poi_touch_window_bars":
		return setInt(&cfg.POITouchWindowBars, key, val)
	case "entry_fresh_bars":
		return setInt(&cfg.EntryFreshBars, key, val)

	// --- Magnitude filter swing ---
	case "min_swing_pips_m5":
		return setFloat(&cfg.MinSwingPipsM5, key, val)
	case "min_swing_atr_mult_htf":
		return setFloat(&cfg.MinSwingATRMultHTF, key, val)
	case "marks_min_atr_mult":
		return setFloat(&cfg.MarksMinATRMult, key, val)

	// --- Risk / SL / sizing ---
	case "sl_mode":
		return setSLMode(&cfg.SLMode, key, val)
	case "sl_buffer_pips":
		return setFloat(&cfg.SLBufferPips, key, val)
	case "risk_pct":
		return setFloat(&cfg.RiskPct, key, val)
	case "start_balance":
		return setFloat(&cfg.StartBalance, key, val)

	// --- Day type ---
	case "heavy_accum_max_range_pct":
		return setFloat(&cfg.HeavyAccumMaxRangePct, key, val)
	case "heavy_accum_confirm_ny":
		return setBool(&cfg.HeavyAccumConfirmNY, key, val)

	// --- Execution layer (J.2) ---
	case "exec_layer_on":
		return setBool(&cfg.ExecLayerOn, key, val)
	case "max_trade_per_day":
		return setInt(&cfg.MaxTradePerDay, key, val)
	case "concurrency":
		return setInt(&cfg.Concurrency, key, val)

	// --- Regime / gate ---
	case "impulse_only":
		return setBool(&cfg.ImpulseOnly, key, val)
	case "retrace_fvg_gate":
		return setBool(&cfg.RetraceFVGGate, key, val)
	case "zone_fib_tf":
		return setZoneFibTF(&cfg.ZoneFibTF, key, val)

	// --- Quality gate ---
	case "require_standard_trigger":
		return setBool(&cfg.RequireStandardTrigger, key, val)
	case "ams_strict_structure":
		return setBool(&cfg.AMSStrictStructure, key, val)
	case "max_poi_tier":
		return setInt(&cfg.MaxPOITier, key, val)

	// --- Kunci #3 fallback (F.5) ---
	case "kunci3_fallback":
		return setBool(&cfg.Kunci3Fallback, key, val)
	case "kunci3_fallback_band_pips":
		return setFloat(&cfg.Kunci3FallbackBandPips, key, val)

	// --- Gate toggles (keputusan user 2026-06-01/02) ---
	case "asia_close_gate":
		return setBool(&cfg.AsiaCloseGate, key, val)
	case "qt_phase_gate":
		return setBool(&cfg.QTPhaseGate, key, val)
	case "weekly_phase_gate":
		return setBool(&cfg.WeeklyPhaseGate, key, val)
	case "day_type_gate":
		return setBool(&cfg.DayTypeGate, key, val)
	case "fib_zone_gate":
		return setBool(&cfg.FibZoneGate, key, val)
	case "session_pm_gate":
		return setBool(&cfg.SessionPMGate, key, val)
	case "ams_gate":
		return setBool(&cfg.AMSGate, key, val)
	case "mo_gate":
		return setBool(&cfg.MOGate, key, val)
	case "releq_tolerance_pct":
		return setFloat(&cfg.RelEqTolerancePct, key, val)
	case "releq_tolerance_pips":
		return setFloat(&cfg.RelEqTolerancePips, key, val)
	case "releq_lookback_bars":
		return setInt(&cfg.RelEqLookbackBars, key, val)
	case "releq_max_gap_bars":
		return setInt(&cfg.RelEqMaxGapBars, key, val)
	case "releq_sweep_gate":
		return setBool(&cfg.RelEqSweepGate, key, val)
	case "releq_sweep_fresh_bars":
		return setInt(&cfg.RelEqSweepFreshBars, key, val)
	case "agenda_gate":
		return setBool(&cfg.AgendaGate, key, val)
	case "agenda_nearest":
		return setBool(&cfg.AgendaNearest, key, val)
	case "fractal_poi":
		return setBool(&cfg.FractalPOI, key, val)
	case "poi_break_wick":
		return setBool(&cfg.POIBreakWick, key, val)
	case "london_q34_only":
		return setBool(&cfg.LondonQ34Only, key, val)
	case "london_q4_only":
		return setBool(&cfg.LondonQ4Only, key, val)

	// --- Fix bias/OF + skip-hour + OB (2026-06-02) ---
	case "of_swing_atr_mult":
		return setFloat(&cfg.OFSwingATRMult, key, val)
	case "bias_swing_atr_mult":
		return setFloat(&cfg.BiasSwingATRMult, key, val)
	case "disable_ob":
		return setBool(&cfg.DisableOB, key, val)
	case "ob_strict":
		return setBool(&cfg.OBStrict, key, val)
	case "bpr_directional":
		return setBool(&cfg.BPRDirectional, key, val)
	case "disable_bpr":
		return setBool(&cfg.DisableBPR, key, val)
	case "ifvg_require_no_same_dir_fvg":
		return setBool(&cfg.IFVGRequireNoSameDirFVG, key, val)
	case "skip_entry_hours_ny":
		cfg.SkipEntryHoursNY = val // CSV jam NY, mis. "8" / "8,20"; kosongkan utk off
		return nil
	case "london_min_hour_ny":
		return setInt(&cfg.LondonMinHourNY, key, val)
	case "retrace_tp_to_fvg":
		return setBool(&cfg.RetraceTPToFVG, key, val)
	case "of_bear_confirm_weeks":
		return setInt(&cfg.OFBearConfirmWeeks, key, val)
	case "min_bias_age_days":
		return setInt(&cfg.MinBiasAgeDays, key, val)
	case "min_bias_age_days_bull":
		return setInt(&cfg.MinBiasAgeDaysBull, key, val)
	case "skip_retrace":
		return setBool(&cfg.SkipRetrace, key, val)
	case "max_confluence":
		return setInt(&cfg.MaxConfluence, key, val)
	case "bb_needs_fvg":
		return setBool(&cfg.BBNeedsFVG, key, val)
	case "bb_require_displacement":
		return setBool(&cfg.BBRequireDisplacement, key, val)
	case "london_sweep_entry":
		return setBool(&cfg.LondonSweepEntry, key, val)
	case "entry_trigger_mode":
		switch val {
		case "itl", "disp", "reject", "dispreject", "sweep":
			cfg.EntryTriggerMode = val
			return nil
		default:
			return fmt.Errorf("key %q: %q tidak dikenal (pakai itl/disp/reject/dispreject/sweep)", key, val)
		}
	case "disp_atr_mult":
		return setFloat(&cfg.DispATRMult, key, val)
	case "max_of_age_days":
		return setInt(&cfg.MaxOFAgeDays, key, val)
	case "allow_early_flip":
		return setBool(&cfg.AllowEarlyFlip, key, val)

	default:
		log.Printf("config: WARNING key tak dikenal %q diabaikan", key)
		return nil
	}
}

// --- helper setter per tipe ---

func setFloat(dst *float64, key, val string) error {
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fmt.Errorf("key %q: %q bukan float yang valid", key, val)
	}
	*dst = f
	return nil
}

func setInt(dst *int, key, val string) error {
	n, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("key %q: %q bukan integer yang valid", key, val)
	}
	*dst = n
	return nil
}

func setBool(dst *bool, key, val string) error {
	switch strings.ToLower(val) {
	case "true":
		*dst = true
	case "false":
		*dst = false
	default:
		return fmt.Errorf("key %q: %q bukan bool yang valid (true/false)", key, val)
	}
	return nil
}

func setBreakType(dst *detectors.BreakType, key, val string) error {
	switch strings.ToLower(val) {
	case "wick":
		*dst = detectors.BreakWick
	case "body":
		*dst = detectors.BreakBody
	default:
		return fmt.Errorf("key %q: %q tidak dikenal (pakai wick/body)", key, val)
	}
	return nil
}

func setSLMode(dst *detectors.SLMode, key, val string) error {
	switch strings.ToLower(val) {
	case "ltf_structure":
		*dst = detectors.SLLtfStructure
	case "body_poi":
		*dst = detectors.SLBodyPOI
	default:
		return fmt.Errorf("key %q: %q tidak dikenal (pakai ltf_structure/body_poi)", key, val)
	}
	return nil
}

func setZoneFibTF(dst *engine.ZoneFibTF, key, val string) error {
	switch strings.ToLower(val) {
	case "weekly":
		*dst = engine.ZoneFibWeekly
	case "daily":
		*dst = engine.ZoneFibDaily
	default:
		return fmt.Errorf("key %q: %q tidak dikenal (pakai weekly/daily)", key, val)
	}
	return nil
}
