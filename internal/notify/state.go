package notify

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

// State = penanda alert terakhir yang sudah dikirim, supaya setup yang sama
// tidak dialert ulang antar-run daemon (dedup persisten).
type State struct {
	LastAlertTime time.Time `json:"last_alert_time"`
	Dir           string    `json:"dir"`
	// LastWatchlist = fingerprint daftar POI per-TF terakhir yang dialert
	// (string deterministik dari level zona) — dipakai mendeteksi PERUBAHAN
	// daftar (pesan diff) antar kiriman.
	LastWatchlist string `json:"last_watchlist,omitempty"`
	// LastWatchlistH1 = waktu candle H1 terakhir yang watchlist-nya sudah
	// dikirim (heartbeat: kirim sekali per H1 close walau daftar tak berubah).
	LastWatchlistH1 time.Time `json:"last_watchlist_h1"`
	// LastTickTime = waktu tick daemon terakhir, di-update & disimpan TIAP
	// siklus (penanda liveness). Gap besar antar-tick = daemon sempat offline
	// (shutdown/sleep) → pesan watchlist pertama pasca-restart diberi catatan
	// konteks "diff vs kondisi sebelum offline".
	LastTickTime time.Time `json:"last_tick_time"`
	// LastQ1Close = waktu close Q1 sesi terakhir yang sudah dialert (A/X per-
	// session, Phase 2). Dedup: alert Q1 dikirim sekali per Q1 close (monoton naik).
	LastQ1Close time.Time `json:"last_q1_close,omitempty"`
	// LastDayTypeAlert = TradingDayStart (18:00 NY) dari trading-day yang day-type
	// heavy_*-nya sudah dialert. Dedup: alert day-type dikirim sekali per trading-day
	// (monoton naik). Zero = belum pernah.
	LastDayTypeAlert time.Time `json:"last_daytype_alert,omitempty"`
}

// LoadState membaca state dari path. File tidak ada → State{} kosong + nil error
// (run pertama, belum pernah alert).
func LoadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	return s, nil
}

// Save menulis state sebagai JSON indented (mode 0644).
func (s State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
